package logs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"demo-lab/server/sentinel"
)

// Policy decide qué logs viajan a Sentinel. Cada evento "processed" se
// vectoriza en Bedrock, así que el presupuesto acota el costo.
type Policy struct {
	Enabled        bool    `json:"enabled"`
	LevelMin       string  `json:"level_min"`        // info | warning | error | critical
	InfoSampleRate float64 `json:"info_sample_rate"` // 0..1, aplica a los INFO que pasan el nivel mínimo
	BudgetPerMin   int     `json:"budget_per_min"`   // por plataforma
}

func DefaultPolicy() Policy {
	return Policy{Enabled: true, LevelMin: Warning, InfoSampleRate: 0.05, BudgetPerMin: 120}
}

// Metrics es lo que muestra la consola sobre el envío.
type Metrics struct {
	Sent       int64  `json:"sent"`
	Processed  int64  `json:"accepted_processed"`
	Incomplete int64  `json:"accepted_incomplete"`
	Discarded  int64  `json:"discarded_by_sentinel"`
	Pending    int    `json:"pending"`
	Filtered   int64  `json:"filtered_by_policy"`
	OverBudget int64  `json:"over_budget"`
	QueueDrops int64  `json:"queue_drops"`
	DeadLetter int64  `json:"dead_letter"`
	LastError  string `json:"last_error,omitempty"`
}

// Target dice a dónde enviar los logs de una plataforma.
type Target struct {
	Token      string
	Components map[string]string // slug -> id del componente en Sentinel
}

// Resolver entrega el destino de una plataforma; ok=false si aún no se aprovisionó.
type Resolver func(platform string) (Target, bool)

type Ingester interface {
	Ingest(ctx context.Context, token string, events []sentinel.Event) ([]sentinel.IngestResult, error)
}

const (
	batchSize    = 200
	batchEvery   = 2 * time.Second
	queueCap     = 5000
	maxAttempts  = 5
	firstBackoff = 500 * time.Millisecond
	budgetWindow = time.Minute
)

type Shipper struct {
	ingest  Ingester
	resolve Resolver
	deadDir string
	backoff time.Duration
	now     func() time.Time

	mu       sync.Mutex
	policy   Policy
	m        Metrics
	queue    []Record
	infoSeen int64
	window   map[string]*budget

	wake chan struct{}
}

type budget struct {
	start time.Time
	used  int
}

func NewShipper(in Ingester, resolve Resolver, deadDir string) *Shipper {
	return &Shipper{
		ingest: in, resolve: resolve, deadDir: deadDir, backoff: firstBackoff, now: time.Now,
		policy: DefaultPolicy(), window: map[string]*budget{}, wake: make(chan struct{}, 1),
	}
}

func (s *Shipper) Policy() Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

func (s *Shipper) SetPolicy(p Policy) error {
	if p.LevelMin == "" {
		p.LevelMin = Warning
	}
	switch {
	case Weight(p.LevelMin) == 0 && p.LevelMin != Info:
		return errors.New("level_min debe ser info, warning, error o critical")
	case p.InfoSampleRate < 0 || p.InfoSampleRate > 1:
		return errors.New("info_sample_rate debe estar entre 0 y 1")
	case p.BudgetPerMin < 1:
		return errors.New("budget_per_min debe ser al menos 1")
	}
	s.mu.Lock()
	s.policy = p
	s.mu.Unlock()
	return nil
}

func (s *Shipper) Metrics() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.m
	m.Pending = len(s.queue)
	return m
}

// Offer aplica la política a un log y lo encola si corresponde. Es el
// suscriptor que se registra en el Logger.
func (s *Shipper) Offer(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.policy
	if !p.Enabled || Weight(r.Level) < Weight(p.LevelMin) {
		s.m.Filtered++
		return
	}
	if r.Level == Info && !s.sampleInfo(p.InfoSampleRate) {
		s.m.Filtered++
		return
	}
	if !s.withinBudget(r, p.BudgetPerMin) {
		s.m.OverBudget++
		return
	}
	if len(s.queue) >= queueCap {
		s.queue = s.queue[1:] // se pierde el más viejo; la copia local lo conserva
		s.m.QueueDrops++
	}
	s.queue = append(s.queue, r)
	if len(s.queue) >= batchSize {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// sampleInfo deja pasar la fracción rate de los INFO, de forma determinista
// (cada 1/rate-ésimo), para que las pruebas y los ensayos sean repetibles.
func (s *Shipper) sampleInfo(rate float64) bool {
	s.infoSeen++
	if rate <= 0 {
		return false
	}
	step := int64(1/rate + 0.5)
	return step <= 1 || s.infoSeen%step == 0
}

// withinBudget reparte el presupuesto por prioridad: los INFO pueden usar hasta
// la mitad, los WARN tres cuartos, los ERROR todo y los CRITICAL un 25 % más.
// Así, en una caída, el ruido no desplaza a los errores que importan.
func (s *Shipper) withinBudget(r Record, perMin int) bool {
	now := s.now()
	b := s.window[r.Platform]
	if b == nil || now.Sub(b.start) >= budgetWindow {
		b = &budget{start: now}
		s.window[r.Platform] = b
	}
	limit := float64(perMin)
	switch r.Level {
	case Info:
		limit *= 0.5
	case Warning:
		limit *= 0.75
	case Critical:
		limit *= 1.25
	}
	if float64(b.used) >= limit {
		return false
	}
	b.used++
	return true
}

// Run vacía la cola en lotes hasta que ctx termina.
func (s *Shipper) Run(ctx context.Context) {
	t := time.NewTicker(batchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
		s.Flush(ctx)
	}
}

// Flush envía todo lo pendiente, plataforma por plataforma, en lotes de hasta batchSize.
func (s *Shipper) Flush(ctx context.Context) {
	s.mu.Lock()
	pending := s.queue
	s.queue = nil
	s.mu.Unlock()

	byPlatform := map[string][]Record{}
	for _, r := range pending {
		byPlatform[r.Platform] = append(byPlatform[r.Platform], r)
	}
	for platform, recs := range byPlatform {
		for len(recs) > 0 {
			n := min(batchSize, len(recs))
			s.sendBatch(ctx, platform, recs[:n])
			recs = recs[n:]
		}
	}
}

// sendBatch envía un lote reintentando con backoff ante red caída o 5xx, sin
// duplicar lo aceptado: un lote entero se acepta o se reintenta. Un 4xx no se
// reintenta; va a dead-letter con la respuesta.
func (s *Shipper) sendBatch(ctx context.Context, platform string, recs []Record) {
	tgt, ok := s.resolve(platform)
	if !ok {
		s.fail("plataforma "+platform+" sin aprovisionar", len(recs), recs, false)
		return
	}
	events := make([]sentinel.Event, len(recs))
	for i, r := range recs {
		events[i] = toEvent(r, tgt)
	}
	var lastErr error
	delay := s.backoff
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := s.ingest.Ingest(ctx, tgt.Token, events)
		if err == nil {
			s.record(res, len(events))
			return
		}
		lastErr = err
		var he *sentinel.HTTPError
		if errors.As(err, &he) && he.Code >= 400 && he.Code < 500 {
			s.fail(err.Error(), len(recs), recs, true)
			return
		}
		select {
		case <-ctx.Done():
			s.requeue(recs)
			return
		case <-time.After(delay):
		}
		delay *= 2
	}
	s.requeue(recs)
	s.mu.Lock()
	s.m.LastError = lastErr.Error()
	s.mu.Unlock()
}

func (s *Shipper) record(res []sentinel.IngestResult, sent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m.Sent += int64(sent)
	for _, r := range res {
		switch r.Status {
		case "processed":
			s.m.Processed++
		case "incomplete":
			s.m.Incomplete++
		default:
			s.m.Discarded++
		}
	}
	s.m.LastError = ""
}

func (s *Shipper) requeue(recs []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(append([]Record{}, recs...), s.queue...)
	if over := len(s.queue) - queueCap; over > 0 {
		s.queue = s.queue[over:]
		s.m.QueueDrops += int64(over)
	}
}

// fail registra un fallo que no se reintenta. Con deadLetter guarda los logs
// y la causa en data/dead-letter/ para no perderlos en silencio.
func (s *Shipper) fail(text string, n int, recs []Record, deadLetter bool) {
	s.mu.Lock()
	s.m.LastError = text
	if deadLetter {
		s.m.DeadLetter += int64(n)
	}
	s.mu.Unlock()
	if deadLetter && s.deadDir != "" {
		_ = os.MkdirAll(s.deadDir, 0o755)
		raw, _ := json.Marshal(map[string]any{"error": text, "records": recs})
		name := filepath.Join(s.deadDir, s.now().UTC().Format("20060102T150405.000000000")+".json")
		_ = os.WriteFile(name, raw, 0o644)
	}
}

func toEvent(r Record, t Target) sentinel.Event {
	out := Render(r)
	return sentinel.Event{
		Source: out.Source, SourceID: "demo-lab/" + r.Platform + "/" + r.Service,
		ComponentID: t.Components[r.Service], Format: out.Format, Payload: out.Payload,
	}
}

// ShipResult resume un envío manual.
type ShipResult struct {
	Queued   int `json:"queued"`
	Accepted int `json:"accepted"`
	Rejected int `json:"rejected"`
}

// ShipNow reenvía los logs de disco que cumplen el filtro, sin política ni
// presupuesto (el director decidió enviarlos). Con dryRun solo cuenta.
func (s *Shipper) ShipNow(ctx context.Context, l *Logger, f Filter, dryRun bool) (ShipResult, error) {
	recs, err := l.FromDisk(f)
	if err != nil {
		return ShipResult{}, err
	}
	res := ShipResult{Queued: len(recs)}
	if dryRun {
		return res, nil
	}
	byPlatform := map[string][]Record{}
	for _, r := range recs {
		byPlatform[r.Platform] = append(byPlatform[r.Platform], r)
	}
	for platform, list := range byPlatform {
		tgt, ok := s.resolve(platform)
		if !ok {
			res.Rejected += len(list)
			continue
		}
		for len(list) > 0 {
			n := min(batchSize, len(list))
			events := make([]sentinel.Event, n)
			for i, r := range list[:n] {
				events[i] = toEvent(r, tgt)
			}
			out, err := s.ingest.Ingest(ctx, tgt.Token, events)
			if err != nil {
				res.Rejected += n
			} else {
				s.record(out, n)
				for _, o := range out {
					if o.Status == "discarded" {
						res.Rejected++
					} else {
						res.Accepted++
					}
				}
			}
			list = list[n:]
		}
	}
	return res, nil
}
