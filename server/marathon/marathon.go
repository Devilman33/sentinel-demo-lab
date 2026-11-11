// Package marathon produce historia real por la API pública: cada ciclo provoca
// una falla o un ataque, espera el incidente que abre Sentinel, lo asigna, deja
// pasar un tiempo de resolución y lo cierra con notas y categoría (spec §2.9).
// Funciona igual contra el Sentinel local y el de AWS.
package marathon

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"demo-lab/server/faults"
	"demo-lab/server/history"
	"demo-lab/server/sentinel"
)

// Deps es lo que la maratón necesita del módulo y de Sentinel.
type Deps interface {
	ApplyFault(platform, service, typ string, p faults.Params) error
	Attack(platform, target, kind string, intensity, durationS int) error
	SetTraffic(platform string, rps float64) error
	RestorePlatform(platform string)
	ProjectID(platform string) string
	Alerts(ctx context.Context) ([]sentinel.Alert, error)
	Assign(ctx context.Context, alertID string) error
	Resolve(ctx context.Context, alertID, notes, category string) error
}

type Config struct {
	Hours      float64 `json:"hours"`
	CadenceMin float64 `json:"cadence_min"`
	// MttrMin y MttrMax acotan la espera entre la asignación y el cierre.
	MttrMin time.Duration `json:"-"`
	MttrMax time.Duration `json:"-"`
	// DetectTimeout es cuánto espera a que Sentinel abra el incidente.
	DetectTimeout time.Duration `json:"-"`
	Poll          time.Duration `json:"-"`
	Seed          uint64        `json:"seed"`
}

func (c *Config) defaults() error {
	if c.Hours <= 0 || c.Hours > 72 || c.CadenceMin <= 0 {
		return errors.New("hours debe estar entre 0 y 72 y cadence_min ser mayor que 0")
	}
	if c.MttrMin == 0 {
		c.MttrMin, c.MttrMax = 2*time.Minute, 6*time.Minute
	}
	if c.MttrMax < c.MttrMin {
		c.MttrMax = c.MttrMin
	}
	if c.DetectTimeout == 0 {
		c.DetectTimeout = 4 * time.Minute
	}
	if c.Poll == 0 {
		c.Poll = 5 * time.Second
	}
	if c.Seed == 0 {
		c.Seed = uint64(time.Now().UnixNano())
	}
	return nil
}

// Cycle es el registro de un ciclo.
type Cycle struct {
	Started  time.Time `json:"started"`
	Scenario string    `json:"scenario"`
	Platform string    `json:"platform"`
	Service  string    `json:"service"`
	Opened   []string  `json:"opened"`   // incidentes que abrió Sentinel
	Resolved []string  `json:"resolved"` // incidentes que la maratón cerró
	Error    string    `json:"error,omitempty"`
	Finished bool      `json:"finished"`
}

type Status struct {
	Running bool      `json:"running"`
	Until   time.Time `json:"until,omitempty"`
	Cycles  []Cycle   `json:"cycles"`
}

type Runner struct {
	D Deps

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
	st     Status
}

func New(d Deps) *Runner { return &Runner{D: d} }

var ErrRunning = errors.New("la maratón ya está en marcha")

// Start inicia la maratón en segundo plano.
func (r *Runner) Start(cfg Config) error {
	if err := cfg.defaults(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.st.Running {
		return ErrRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel, r.done = cancel, make(chan struct{})
	r.st = Status{Running: true, Until: time.Now().Add(time.Duration(cfg.Hours * float64(time.Hour)))}
	go r.loop(ctx, cfg, r.done)
	return nil
}

// Stop detiene la maratón, deja las plataformas sanas y espera a que termine el ciclo en curso.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.st
	s.Cycles = make([]Cycle, len(r.st.Cycles)) // nunca nil: el JSON debe salir como []
	for i, c := range r.st.Cycles {
		c.Opened = append([]string{}, c.Opened...)
		c.Resolved = append([]string{}, c.Resolved...)
		s.Cycles[i] = c
	}
	return s
}

func (r *Runner) update(i int, fn func(*Cycle)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.st.Cycles[i])
}

func (r *Runner) loop(ctx context.Context, cfg Config, done chan struct{}) {
	defer close(done)
	defer func() {
		r.mu.Lock()
		r.st.Running = false
		r.mu.Unlock()
	}()
	rng := rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0xa5a5))
	scs := history.Scenarios()
	until := time.Now().Add(time.Duration(cfg.Hours * float64(time.Hour)))
	cadence := time.Duration(cfg.CadenceMin * float64(time.Minute))
	for time.Now().Before(until) && ctx.Err() == nil {
		begin := time.Now()
		sc := scs[rng.IntN(len(scs))]
		r.mu.Lock()
		r.st.Cycles = append(r.st.Cycles, Cycle{Started: begin, Scenario: sc.Title, Platform: sc.Platform, Service: sc.Service})
		idx := len(r.st.Cycles) - 1
		r.mu.Unlock()

		err := r.cycle(ctx, cfg, rng, sc, idx)
		r.D.RestorePlatform(sc.Platform)
		_ = r.D.SetTraffic(sc.Platform, 0)
		r.update(idx, func(c *Cycle) {
			c.Finished = true
			if err != nil {
				c.Error = err.Error()
			}
		})
		if !sleep(ctx, max(cadence-time.Since(begin), 0)) {
			return
		}
	}
}

// attackKinds elige el ataque que corresponde al servicio de un escenario de seguridad.
var attackKinds = map[string]string{
	"auth-banca": "brute_force", "auth-clientes": "credential_stuffing",
	"ficha-clinica-db": "exfiltration", "api-catalogo": "sqli",
}

// inject provoca el escenario: una caída para los críticos, errores para los
// de severidad alta y degradación para el resto; un ataque para seguridad.
func (r *Runner) inject(sc history.Scenario) error {
	if sc.Category == "Seguridad" {
		kind, ok := attackKinds[sc.Service]
		if !ok {
			return fmt.Errorf("sin ataque definido para %s", sc.Service)
		}
		return r.D.Attack(sc.Platform, sc.Service, kind, 3, 60)
	}
	switch sc.Severity {
	case "critica":
		return r.D.ApplyFault(sc.Platform, sc.Service, faults.TypeDown, faults.Params{Mode: "refused"})
	case "alta":
		return r.D.ApplyFault(sc.Platform, sc.Service, faults.TypeErrors, faults.Params{ErrorRate: 0.6})
	default:
		return r.D.ApplyFault(sc.Platform, sc.Service, faults.TypeDegraded, faults.Params{LatencyMs: 900, JitterMs: 100})
	}
}

func (r *Runner) cycle(ctx context.Context, cfg Config, rng *rand.Rand, sc history.Scenario, idx int) error {
	project := r.D.ProjectID(sc.Platform)
	if project == "" {
		return fmt.Errorf("la plataforma %s no está aprovisionada", sc.Platform)
	}
	before, err := r.D.Alerts(ctx)
	if err != nil {
		return fmt.Errorf("listar alertas: %w", err)
	}
	known := map[string]bool{}
	for _, a := range before {
		known[a.ID] = true
	}

	if err := r.D.SetTraffic(sc.Platform, 3); err != nil {
		return err
	}
	if err := r.inject(sc); err != nil {
		return fmt.Errorf("provocar: %w", err)
	}

	// Espera al incidente: uno que Sentinel abra ahora, no uno que ya existía.
	var mine []sentinel.Alert
	deadline := time.Now().Add(cfg.DetectTimeout)
	for len(mine) == 0 && time.Now().Before(deadline) {
		if !sleep(ctx, cfg.Poll) {
			return ctx.Err()
		}
		now, err := r.D.Alerts(ctx)
		if err != nil {
			continue
		}
		for _, a := range now {
			if !known[a.ID] && a.ProjectID == project && a.Status != "resuelto" {
				mine = append(mine, a)
			}
		}
	}
	if len(mine) == 0 {
		return errors.New("Sentinel no abrió ningún incidente a tiempo")
	}
	r.update(idx, func(c *Cycle) {
		for _, a := range mine {
			c.Opened = append(c.Opened, a.ID)
		}
	})
	for _, a := range mine {
		_ = r.D.Assign(ctx, a.ID) // el responsable es el usuario de la maratón
	}

	wait := cfg.MttrMin
	if span := cfg.MttrMax - cfg.MttrMin; span > 0 {
		wait += time.Duration(rng.Int64N(int64(span)))
	}
	if !sleep(ctx, wait) {
		return ctx.Err()
	}
	r.D.RestorePlatform(sc.Platform)

	// Cierra todo lo que esta falla dejó abierto (el de logs y el del sondeo, si siguen abiertos).
	notes := sc.Notes[rng.IntN(len(sc.Notes))]
	after, err := r.D.Alerts(ctx)
	if err != nil {
		return fmt.Errorf("listar alertas: %w", err)
	}
	var errs []string
	for _, a := range after {
		if known[a.ID] || a.ProjectID != project || a.Status == "resuelto" {
			continue
		}
		if err := r.D.Resolve(ctx, a.ID, notes, sc.Category); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		r.update(idx, func(c *Cycle) { c.Resolved = append(c.Resolved, a.ID) })
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
