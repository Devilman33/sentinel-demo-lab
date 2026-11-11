package scripts

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"demo-lab/server/faults"
)

type rec struct {
	mu   sync.Mutex
	log  []string
	when []time.Time
}

func (r *rec) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, s)
	r.when = append(r.when, time.Now())
}
func (r *rec) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

func (r *rec) SetTraffic(p string, rps float64) error { r.add("trafico"); return nil }
func (r *rec) ApplyFault(p, s, t string, _ faults.Params, _ int) error {
	r.add("falla:" + s)
	return nil
}
func (r *rec) Attack(p, t, k string, _, _ int) error { r.add("ataque:" + k); return nil }
func (r *rec) UserAction(p, a string, n int)         { r.add("accion:" + a) }
func (r *rec) RestoreAll(p string)                   { r.add("restaurar") }

func script(steps ...Step) Script {
	s := Script{ID: "t", Title: "t", Platform: "mercado", Steps: steps}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no ocurrió a tiempo: %s", what)
}

func newRunner(a Actions, s Script) *Runner {
	r := NewRunner(a, []Script{s})
	r.Tick = 5 * time.Millisecond
	return r
}

// TC-13: cada paso ocurre a su tiempo relativo y el guion termina solo.
func TestEjecutaLosPasosEnOrden(t *testing.T) {
	a := &rec{}
	r := newRunner(a, script(
		Step{At: "60ms", Fault: &Fault{Service: "db-transaccional", Type: "down", Params: faults.Params{Mode: "refused"}}},
		Step{At: "0s", Traffic: &Traffic{RPS: 2}},
		Step{At: "120ms", Restore: "todo"},
	))
	run, err := r.Start("t")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "fin", func() bool { g, _ := r.Get(run.ID); return g.State == StateFinished })
	got := a.snapshot()
	want := []string{"trafico", "falla:db-transaccional", "restaurar"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orden %v, esperaba %v", got, want)
		}
	}
	if g, _ := r.Get(run.ID); len(g.Events) != 3 {
		t.Errorf("eventos %d, esperaba 3", len(g.Events))
	}
}

// TC-13: la pausa detiene el reloj del guion; al reanudar, el resto ocurre descontándola.
func TestPausaYReanudar(t *testing.T) {
	a := &rec{}
	r := newRunner(a, script(
		Step{At: "0s", Traffic: &Traffic{RPS: 1}},
		Step{At: "150ms", Restore: "todo"},
	))
	run, _ := r.Start("t")
	waitFor(t, "primer paso", func() bool { return len(a.snapshot()) == 1 })
	if _, err := r.Pause(run.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond) // más que los 150 ms que faltaban
	if n := len(a.snapshot()); n != 1 {
		t.Fatalf("en pausa no debe avanzar, hubo %d pasos", n)
	}
	if g, _ := r.Get(run.ID); g.State != StatePaused {
		t.Fatalf("estado %s", g.State)
	}
	if _, err := r.Pause(run.ID); err == nil {
		t.Error("pausar dos veces debe fallar")
	}
	resumed := time.Now()
	if _, err := r.Resume(run.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "paso final", func() bool { return len(a.snapshot()) == 2 })
	a.mu.Lock()
	took := a.when[1].Sub(resumed)
	a.mu.Unlock()
	if took < 100*time.Millisecond {
		t.Errorf("tras reanudar el paso salió a los %v: no se descontó bien la pausa", took)
	}
}

// TC-13: abortar deja la plataforma sana y no ejecuta más pasos.
func TestAbortarRestauraYDetiene(t *testing.T) {
	a := &rec{}
	r := newRunner(a, script(
		Step{At: "0s", Traffic: &Traffic{RPS: 1}},
		Step{At: "300ms", Fault: &Fault{Service: "db-transaccional", Type: "down", Params: faults.Params{Mode: "503"}}},
	))
	run, _ := r.Start("t")
	waitFor(t, "primer paso", func() bool { return len(a.snapshot()) == 1 })
	if _, err := r.Abort(run.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	got := a.snapshot()
	if len(got) != 2 || got[1] != "restaurar" {
		t.Fatalf("pasos %v: debía quedar solo el tráfico y la restauración", got)
	}
	if g, _ := r.Get(run.ID); g.State != StateAborted {
		t.Errorf("estado %s", g.State)
	}
	if _, err := r.Abort(run.ID); err == nil {
		t.Error("abortar de nuevo debe fallar")
	}
}

func TestUnaEjecucionActivaPorPlataforma(t *testing.T) {
	r := newRunner(&rec{}, script(Step{At: "1s", Restore: "todo"}))
	if _, err := r.Start("t"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Start("t"); err == nil {
		t.Error("dos ejecuciones a la vez en la misma plataforma deben rechazarse")
	}
	r.AbortAll()
}

func TestValidacion(t *testing.T) {
	bad := map[string]Script{
		"servicio inexistente":  {ID: "a", Platform: "mercado", Steps: []Step{{At: "0s", Fault: &Fault{Service: "nada", Type: "down"}}}},
		"plataforma":            {ID: "a", Platform: "nada", Steps: []Step{{At: "0s", Restore: "todo"}}},
		"dos acciones":          {ID: "a", Platform: "mercado", Steps: []Step{{At: "0s", Restore: "todo", Traffic: &Traffic{RPS: 1}}}},
		"sin acción":            {ID: "a", Platform: "mercado", Steps: []Step{{At: "0s"}}},
		"tiempo":                {ID: "a", Platform: "mercado", Steps: []Step{{At: "mañana", Restore: "todo"}}},
		"ataque fuera de lugar": {ID: "a", Platform: "mercado", Steps: []Step{{At: "0s", Attack: &Attack{Service: "pasarela-pagos", Kind: "brute_force", Intensity: 1, DurationS: 5}}}},
		"acción inexistente":    {ID: "a", Platform: "mercado", Steps: []Step{{At: "0s", UserAction: &UserAction{Action: "volar"}}}},
		"id con ruta":           {ID: "../a", Platform: "mercado", Steps: []Step{{At: "0s", Restore: "todo"}}},
	}
	for name, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: debía rechazarse", name)
		}
	}
}

// Los guiones incluidos en el binario deben cargar y validar.
func TestGuionesIncluidos(t *testing.T) {
	list, errs := Builtin()
	for _, e := range errs {
		t.Errorf("guion inválido: %v", e)
	}
	if len(list) < 3 {
		t.Errorf("se esperaban al menos 3 guiones, hay %d", len(list))
	}
}

// Un guion de la carpeta opcional reemplaza al incluido con el mismo id y suma los nuevos.
func TestBibliotecaCombinaLaCarpetaConLosIncluidos(t *testing.T) {
	base, _ := Builtin()
	dir := t.TempDir()
	reemplazo := `{"id":"` + base[0].ID + `","titulo":"mío","plataforma":"mercado","pasos":[{"en":"0s","restaurar":"todo"}]}`
	nuevo := `{"id":"zz-nuevo","titulo":"nuevo","plataforma":"banco","pasos":[{"en":"0s","restaurar":"todo"}]}`
	roto := `{"id":`
	for name, body := range map[string]string{"a.json": reemplazo, "b.json": nuevo, "c.json": roto} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	list, errs := Library(dir)
	if len(errs) != 1 {
		t.Errorf("el archivo roto debe informarse sin impedir los demás: %v", errs)
	}
	if len(list) != len(base)+1 {
		t.Fatalf("hay %d guiones, quiero %d", len(list), len(base)+1)
	}
	for _, s := range list {
		if s.ID == base[0].ID && s.Title != "mío" {
			t.Errorf("el guion de la carpeta no reemplazó al incluido: %q", s.Title)
		}
	}
	if sinCarpeta, _ := Library(""); len(sinCarpeta) != len(base) {
		t.Errorf("sin carpeta deben quedar los incluidos")
	}
}
