package marathon

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"demo-lab/server/faults"
	"demo-lab/server/sentinel"
)

// fakeSentinel abre una alerta cuando el módulo provoca algo, como haría Sentinel.
type fakeSentinel struct {
	mu       sync.Mutex
	alerts   []sentinel.Alert
	assigned map[string]bool
	closed   map[string][2]string
	faults   int
	restored int
	noAlert  bool
}

func newFake() *fakeSentinel {
	return &fakeSentinel{assigned: map[string]bool{}, closed: map[string][2]string{}}
}

func (f *fakeSentinel) open(platform string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults++
	if f.noAlert {
		return
	}
	f.alerts = append(f.alerts, sentinel.Alert{ID: "al" + string(rune('a'+len(f.alerts))), ProjectID: "proj-" + platform, Status: "activo"})
}

func (f *fakeSentinel) ApplyFault(p, s, t string, _ faults.Params) error { f.open(p); return nil }
func (f *fakeSentinel) Attack(p, t, k string, _, _ int) error            { f.open(p); return nil }
func (f *fakeSentinel) SetTraffic(string, float64) error                 { return nil }
func (f *fakeSentinel) RestorePlatform(string)                           { f.mu.Lock(); f.restored++; f.mu.Unlock() }
func (f *fakeSentinel) ProjectID(p string) string                        { return "proj-" + p }
func (f *fakeSentinel) Alerts(context.Context) ([]sentinel.Alert, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sentinel.Alert(nil), f.alerts...), nil
}
func (f *fakeSentinel) Assign(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigned[id] = true
	return nil
}
func (f *fakeSentinel) Resolve(_ context.Context, id, notes, cat string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed[id] = [2]string{notes, cat}
	for i := range f.alerts {
		if f.alerts[i].ID == id {
			f.alerts[i].Status = "resuelto"
		}
	}
	return nil
}

func quick(hours float64) Config {
	return Config{Hours: hours, CadenceMin: 0.001, MttrMin: 10 * time.Millisecond, MttrMax: 20 * time.Millisecond,
		DetectTimeout: time.Second, Poll: 5 * time.Millisecond, Seed: 42}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 400; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no ocurrió a tiempo: %s", what)
}

// TC-18: cada falla provocada abre un incidente que la maratón asigna y cierra con notas y categoría.
func TestCadaFallaSeAbreSeAsignaYSeCierraPorLaAPI(t *testing.T) {
	f := newFake()
	r := New(f)
	if err := r.Start(quick(1)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "tres ciclos cerrados", func() bool {
		n := 0
		for _, c := range r.Status().Cycles {
			if c.Finished && len(c.Resolved) > 0 {
				n++
			}
		}
		return n >= 3
	})
	r.Stop()

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.closed) < 3 {
		t.Fatalf("cerrados %d", len(f.closed))
	}
	for id, c := range f.closed {
		if c[0] == "" || (c[1] != "Operativo" && c[1] != "Seguridad") {
			t.Errorf("%s cerrado sin notas o categoría: %v", id, c)
		}
		if !f.assigned[id] {
			t.Errorf("%s se cerró sin asignarlo antes", id)
		}
	}
	if st := r.Status(); st.Running {
		t.Error("tras Stop no debe seguir en marcha")
	}
}

func TestSinIncidenteElCicloQuedaRegistradoYLaPlataformaSana(t *testing.T) {
	f := newFake()
	f.noAlert = true
	r := New(f)
	cfg := quick(1)
	cfg.DetectTimeout = 50 * time.Millisecond
	if err := r.Start(cfg); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ciclo con error", func() bool {
		c := r.Status().Cycles
		return len(c) > 0 && c[0].Finished
	})
	r.Stop()
	c := r.Status().Cycles[0]
	if c.Error == "" || len(c.Resolved) != 0 {
		t.Errorf("debía registrar el error: %+v", c)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.restored == 0 {
		t.Error("tras un ciclo fallido la plataforma debe quedar restaurada")
	}
}

func TestNoSePuedeIniciarDosVeces(t *testing.T) {
	r := New(newFake())
	if err := r.Start(quick(1)); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	if err := r.Start(quick(1)); !errors.Is(err, ErrRunning) {
		t.Errorf("esperaba ErrRunning, fue %v", err)
	}
}

func TestConfigInvalida(t *testing.T) {
	r := New(newFake())
	for _, c := range []Config{{Hours: 0, CadenceMin: 1}, {Hours: 100, CadenceMin: 1}, {Hours: 1, CadenceMin: 0}} {
		if err := r.Start(c); err == nil {
			t.Errorf("%+v debía rechazarse", c)
		}
	}
}
