package logs

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"demo-lab/server/sentinel"
)

// fakeIngest registra lo recibido y falla las primeras `failFirst` veces con `failWith`.
type fakeIngest struct {
	mu        sync.Mutex
	batches   [][]sentinel.Event
	failFirst int
	failWith  error
	calls     int
	status    string
}

func (f *fakeIngest) Ingest(_ context.Context, token string, ev []sentinel.Event) ([]sentinel.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failFirst {
		return nil, f.failWith
	}
	f.batches = append(f.batches, ev)
	st := f.status
	if st == "" {
		st = "processed"
	}
	out := make([]sentinel.IngestResult, len(ev))
	for i := range out {
		out[i].Status = st
	}
	return out, nil
}

func (f *fakeIngest) total() (n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.batches {
		n += len(b)
	}
	return
}

func resolver(platform string) (Target, bool) {
	return Target{Token: "tok", Components: map[string]string{"pasarela-pagos": "comp-1"}}, platform == "mercado"
}

func rec(level string, i int) Record {
	return Record{Platform: "mercado", Service: "pasarela-pagos", Level: level, Message: fmt.Sprintf("evento %d", i), Format: "json", TraceID: "t"}
}

func newShipper(in Ingester, p Policy) *Shipper {
	s := NewShipper(in, resolver, "")
	s.backoff = time.Millisecond
	_ = s.SetPolicy(p)
	return s
}

// TC-04
func TestPolitica_FiltraPorNivelYMuestreaInfo(t *testing.T) {
	in := &fakeIngest{}
	s := newShipper(in, Policy{Enabled: true, LevelMin: Warning, InfoSampleRate: 0.1, BudgetPerMin: 1000})
	for i := 0; i < 100; i++ {
		s.Offer(rec(Info, i))
	}
	s.Offer(rec(Warning, 0))
	s.Offer(rec(Error, 0))
	s.Flush(context.Background())
	// level_min=warning deja fuera los INFO: solo viajan los 2.
	if in.total() != 2 {
		t.Errorf("con level_min=warning viajaron %d eventos, quiero 2", in.total())
	}

	in2 := &fakeIngest{}
	s2 := newShipper(in2, Policy{Enabled: true, LevelMin: Info, InfoSampleRate: 0.1, BudgetPerMin: 1000})
	for i := 0; i < 100; i++ {
		s2.Offer(rec(Info, i))
	}
	s2.Flush(context.Background())
	if in2.total() != 10 {
		t.Errorf("muestreo 0,1 de 100 INFO: viajaron %d, quiero 10", in2.total())
	}
	if m := newShipper(in, Policy{Enabled: false, LevelMin: Info, InfoSampleRate: 1, BudgetPerMin: 5}); m != nil {
		m.Offer(rec(Critical, 1))
		if m.Metrics().Pending != 0 {
			t.Errorf("con el envío apagado no se encola nada")
		}
	}
}

func TestPresupuesto_PriorizaErroresSobreRuido(t *testing.T) {
	in := &fakeIngest{}
	s := newShipper(in, Policy{Enabled: true, LevelMin: Info, InfoSampleRate: 1, BudgetPerMin: 100})
	s.now = func() time.Time { return time.Unix(1000, 0) } // misma ventana de un minuto
	for i := 0; i < 500; i++ {
		s.Offer(rec(Info, i))
	}
	for i := 0; i < 500; i++ {
		s.Offer(rec(Error, i))
	}
	s.Flush(context.Background())
	if got := in.total(); got > 100 {
		t.Errorf("se enviaron %d eventos, el presupuesto es 100 (más un 25%% para críticos)", got)
	}
	m := s.Metrics()
	if m.OverBudget == 0 {
		t.Errorf("debería haber eventos fuera de presupuesto")
	}
	// Los INFO solo usaron la mitad; lo que queda es de errores.
	errores := 0
	for _, b := range in.batches {
		for _, e := range b {
			if strings.Contains(e.Payload, `"severity":"error"`) {
				errores++
			}
		}
	}
	if errores < 50 {
		t.Errorf("solo %d errores pasaron; el ruido no debe desplazarlos", errores)
	}
	// Ventana nueva: el presupuesto se renueva.
	s.now = func() time.Time { return time.Unix(1100, 0) }
	s.Offer(rec(Error, 1))
	if s.Metrics().Pending != 1 {
		t.Errorf("tras un minuto el presupuesto debería renovarse")
	}
}

func TestReintentos_NoDuplicanAnteCaidaTransitoria(t *testing.T) {
	in := &fakeIngest{failFirst: 2, failWith: &sentinel.HTTPError{Code: http.StatusServiceUnavailable}}
	s := newShipper(in, Policy{Enabled: true, LevelMin: Info, InfoSampleRate: 1, BudgetPerMin: 1000})
	for i := 0; i < 5; i++ {
		s.Offer(rec(Error, i))
	}
	s.Flush(context.Background())
	if in.total() != 5 {
		t.Errorf("tras 2 fallos 503 debería haberse entregado el lote una vez: %d eventos", in.total())
	}
	if m := s.Metrics(); m.Sent != 5 || m.Pending != 0 {
		t.Errorf("métricas: %+v", m)
	}
}

func TestReintentos_UnAgotadoVuelveALaCola(t *testing.T) {
	in := &fakeIngest{failFirst: 99, failWith: fmt.Errorf("sin red")}
	s := newShipper(in, Policy{Enabled: true, LevelMin: Info, InfoSampleRate: 1, BudgetPerMin: 1000})
	s.Offer(rec(Error, 1))
	s.Flush(context.Background())
	if m := s.Metrics(); m.Pending != 1 || m.LastError == "" {
		t.Errorf("tras agotar reintentos el log debe seguir pendiente: %+v", m)
	}
}

func TestRechazo4xx_VaADeadLetterSinReintentar(t *testing.T) {
	dir := t.TempDir()
	in := &fakeIngest{failFirst: 99, failWith: &sentinel.HTTPError{Code: 403, Body: "project_id ajeno"}}
	s := NewShipper(in, resolver, dir)
	_ = s.SetPolicy(Policy{Enabled: true, LevelMin: Info, InfoSampleRate: 1, BudgetPerMin: 100})
	s.Offer(rec(Error, 1))
	s.Flush(context.Background())
	if in.calls != 1 {
		t.Errorf("un 4xx no se reintenta: %d llamadas", in.calls)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("dead-letter: %d archivos", len(files))
	}
	if raw, _ := os.ReadFile(files[0]); !strings.Contains(string(raw), "project_id ajeno") {
		t.Errorf("el dead-letter debe guardar la respuesta de Sentinel")
	}
	if m := s.Metrics(); m.DeadLetter != 1 || m.Pending != 0 {
		t.Errorf("métricas: %+v", m)
	}
}

func TestRender_FormatosYComponenteEnLegacy(t *testing.T) {
	json := Render(Record{Time: time.Now(), Level: Error, Service: "a", Platform: "p", Message: "m", Format: "json", ErrorKind: "x"})
	if json.Source != "cloud" || !strings.Contains(json.Payload, `"service":"a"`) || !strings.Contains(json.Payload, `"timestamp"`) {
		t.Errorf("json: %+v", json)
	}
	text := Render(Record{Time: time.Now(), Level: Warning, Service: "a", Message: "m", Format: "text"})
	if text.Source != "legacy" || text.Format != "text" || strings.Contains(text.Payload, `"service"`) {
		t.Errorf("texto: %+v", text)
	}
	xml := Render(Record{Time: time.Now(), Level: Error, Service: "core-bancario", Message: "a < b & c", Format: "xml"})
	if xml.Source != "legacy" || !strings.Contains(xml.Payload, "<service>core-bancario</service>") || !strings.Contains(xml.Payload, "a &lt; b &amp; c") {
		t.Errorf("xml: %+v", xml)
	}
	ev := toEvent(Record{Platform: "mercado", Service: "pasarela-pagos", Format: "text", Time: time.Now()}, Target{Components: map[string]string{"pasarela-pagos": "comp-1"}})
	if ev.ComponentID != "comp-1" {
		t.Errorf("los legacy viajan con component_id para que Sentinel los atribuya")
	}
}

// TC-05
func TestEnvioManual_SoloLoQueCumpleElFiltro(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	t.Cleanup(l.Close)
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		l.Emit(Record{Time: now.Add(-time.Hour), Platform: "mercado", Service: "pasarela-pagos", Level: Error, Message: "viejo", Format: "json"})
	}
	for i := 0; i < 3; i++ {
		l.Emit(Record{Time: now, Platform: "mercado", Service: "pasarela-pagos", Level: Error, Message: "reciente", Format: "json"})
	}
	l.Emit(Record{Time: now, Platform: "mercado", Service: "pasarela-pagos", Level: Info, Message: "info", Format: "json"})
	l.Emit(Record{Time: now, Platform: "mercado", Service: "api-catalogo", Level: Error, Message: "otro servicio", Format: "json"})

	in := &fakeIngest{}
	s := newShipper(in, DefaultPolicy())
	f := Filter{Platform: "mercado", Service: "pasarela-pagos", MinLevel: Error, Since: now.Add(-time.Minute)}

	seco, err := s.ShipNow(context.Background(), l, f, true)
	if err != nil || seco.Queued != 3 || in.total() != 0 {
		t.Fatalf("dry_run: %+v %v, enviados=%d", seco, err, in.total())
	}
	res, err := s.ShipNow(context.Background(), l, f, false)
	if err != nil || res.Queued != 3 || res.Accepted != 3 || res.Rejected != 0 || in.total() != 3 {
		t.Errorf("envío manual: %+v %v, enviados=%d", res, err, in.total())
	}
	in.status = "discarded"
	if res, _ := s.ShipNow(context.Background(), l, f, false); res.Rejected != 3 {
		t.Errorf("lo que Sentinel descarta se informa como rechazado: %+v", res)
	}
}

func TestLogger_RotaYConservaCopiaLocal(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	t.Cleanup(l.Close)
	l.Emit(Record{Platform: "banco", Service: "core-bancario", Level: Info, Message: "x", Format: "xml"})
	if _, err := os.Stat(filepath.Join(dir, "banco", "core-bancario.ndjson")); err != nil {
		t.Errorf("falta la copia local: %v", err)
	}
	recs, err := l.FromDisk(Filter{Platform: "banco"})
	if err != nil || len(recs) != 1 {
		t.Errorf("FromDisk: %v %v", recs, err)
	}
}
