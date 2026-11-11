// Package scripts ejecuta guiones: secuencias temporizadas de tráfico, fallas,
// ataques y acciones de usuario (spec §2.11). Un guion es un JSON; los incluidos
// están en library/ y van dentro del binario (el módulo no usa dependencias
// externas, así que no hay YAML).
package scripts

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"demo-lab/server/attacks"
	"demo-lab/server/catalog"
	"demo-lab/server/faults"
)

/* ── Definición ─────────────────────────────────────────────────────────── */

type Traffic struct {
	RPS float64 `json:"rps"`
}

type Fault struct {
	Service   string        `json:"servicio"`
	Type      string        `json:"tipo"`
	Params    faults.Params `json:"params"`
	DurationS int           `json:"duracion_s,omitempty"`
}

type Attack struct {
	Service   string `json:"servicio"`
	Kind      string `json:"tipo"`
	Intensity int    `json:"intensidad"`
	DurationS int    `json:"duracion_s"`
}

type UserAction struct {
	Action string `json:"accion"`
	Repeat int    `json:"repetir,omitempty"`
}

// Step es un paso: tiene exactamente una acción.
type Step struct {
	At         string      `json:"en"`
	Traffic    *Traffic    `json:"trafico,omitempty"`
	Fault      *Fault      `json:"falla,omitempty"`
	Attack     *Attack     `json:"ataque,omitempty"`
	UserAction *UserAction `json:"accion_usuario,omitempty"`
	Restore    string      `json:"restaurar,omitempty"` // "todo"
	Note       string      `json:"nota,omitempty"`

	at time.Duration
}

type Script struct {
	ID       string `json:"id"`
	Title    string `json:"titulo"`
	Platform string `json:"plataforma"`
	Steps    []Step `json:"pasos"`
}

// Duration es el instante del último paso.
func (s Script) Duration() time.Duration {
	if len(s.Steps) == 0 {
		return 0
	}
	return s.Steps[len(s.Steps)-1].at
}

var ErrInvalid = errors.New("guion inválido")

func invalid(id, format string, a ...any) error {
	return fmt.Errorf("%w (%s): %s", ErrInvalid, id, fmt.Sprintf(format, a...))
}

// Validate comprueba un guion contra el catálogo y deja los pasos ordenados.
func (s *Script) Validate() error {
	if s.ID == "" || strings.ContainsAny(s.ID, "/\\ ") {
		return invalid(s.ID, "id vacío o con caracteres no permitidos")
	}
	p, ok := catalog.BySlug(s.Platform)
	if !ok {
		return invalid(s.ID, "plataforma %q desconocida", s.Platform)
	}
	if len(s.Steps) == 0 {
		return invalid(s.ID, "sin pasos")
	}
	for i := range s.Steps {
		st := &s.Steps[i]
		d, err := time.ParseDuration(st.At)
		if err != nil || d < 0 {
			return invalid(s.ID, "paso %d: «en» debe ser una duración como 20s o 2m30s", i+1)
		}
		st.at = d
		n := 0
		for _, set := range []bool{st.Traffic != nil, st.Fault != nil, st.Attack != nil, st.UserAction != nil, st.Restore != ""} {
			if set {
				n++
			}
		}
		if n != 1 {
			return invalid(s.ID, "paso %d: debe tener exactamente una acción", i+1)
		}
		switch {
		case st.Traffic != nil:
			if st.Traffic.RPS < 0 || st.Traffic.RPS > 50 {
				return invalid(s.ID, "paso %d: rps entre 0 y 50", i+1)
			}
		case st.Fault != nil:
			if _, ok := p.Service(st.Fault.Service); !ok {
				return invalid(s.ID, "paso %d: el servicio %q no existe en %s", i+1, st.Fault.Service, s.Platform)
			}
		case st.Attack != nil:
			if err := attacks.ValidKind(s.Platform, st.Attack.Service, st.Attack.Kind); err != nil {
				return invalid(s.ID, "paso %d: %v", i+1, err)
			}
		case st.UserAction != nil:
			if _, ok := p.Action(st.UserAction.Action); !ok {
				return invalid(s.ID, "paso %d: la acción %q no existe en %s", i+1, st.UserAction.Action, s.Platform)
			}
			if st.UserAction.Repeat < 0 || st.UserAction.Repeat > 100 {
				return invalid(s.ID, "paso %d: repetir entre 0 y 100", i+1)
			}
		case st.Restore != "":
			if st.Restore != "todo" {
				return invalid(s.ID, "paso %d: restaurar solo admite «todo»", i+1)
			}
		}
	}
	sort.SliceStable(s.Steps, func(i, j int) bool { return s.Steps[i].at < s.Steps[j].at })
	return nil
}

//go:embed library/*.json
var library embed.FS

func parse(name string, raw []byte) (Script, error) {
	var s Script
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("%s: %w", name, err)
	}
	if err := s.Validate(); err != nil {
		return s, fmt.Errorf("%s: %w", name, err)
	}
	return s, nil
}

// Load lee y valida todos los guiones de una carpeta. Un archivo roto no impide
// cargar los demás: se devuelve en errs.
func Load(dir string) (list []Script, errs []error) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s, err := parse(filepath.Base(f), raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, errs
}

// Builtin devuelve los guiones que vienen dentro del binario, para que el
// módulo funcione sin carpetas sueltas.
func Builtin() (list []Script, errs []error) {
	files, _ := fs.Glob(library, "library/*.json")
	for _, f := range files {
		raw, err := library.ReadFile(f)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s, err := parse(path.Base(f), raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, errs
}

// Library junta los guiones incluidos con los de una carpeta opcional; uno de la
// carpeta con el mismo id reemplaza al incluido.
func Library(dir string) (list []Script, errs []error) {
	list, errs = Builtin()
	if dir == "" {
		return list, errs
	}
	extra, more := Load(dir)
	errs = append(errs, more...)
	for _, e := range extra {
		replaced := false
		for i := range list {
			if list[i].ID == e.ID {
				list[i], replaced = e, true
			}
		}
		if !replaced {
			list = append(list, e)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list, errs
}

/* ── Ejecución ──────────────────────────────────────────────────────────── */

// Actions es lo que un guion puede hacer sobre el módulo; lo implementa la API.
type Actions interface {
	SetTraffic(platform string, rps float64) error
	ApplyFault(platform, service, typ string, p faults.Params, durationS int) error
	Attack(platform, target, kind string, intensity, durationS int) error
	UserAction(platform, action string, times int)
	// RestoreAll deja la plataforma sana: retira fallas y detiene ataques.
	RestoreAll(platform string)
}

const (
	StateRunning  = "running"
	StatePaused   = "paused"
	StateFinished = "finished"
	StateAborted  = "aborted"
)

// Run es una ejecución. Sus campos se leen con Runner.Get/List.
type Run struct {
	ID        string        `json:"id"`
	ScriptID  string        `json:"script_id"`
	Title     string        `json:"title"`
	Platform  string        `json:"platform"`
	State     string        `json:"state"`
	Elapsed   time.Duration `json:"elapsed_ns"`
	Total     time.Duration `json:"total_ns"`
	NextStep  int           `json:"next_step"` // índice (desde 0) del próximo paso
	Steps     int           `json:"steps"`
	StartedAt time.Time     `json:"started_at"`
	Events    []RunEvent    `json:"events"`
}

type RunEvent struct {
	At      time.Duration `json:"at_ns"`
	Summary string        `json:"summary"`
	Error   string        `json:"error,omitempty"`
}

type Runner struct {
	Act  Actions
	Tick time.Duration // resolución del reloj del guion

	mu      sync.Mutex
	seq     int
	scripts map[string]Script
	runs    map[string]*runState
}

type runState struct {
	Run
	script Script
	pause  bool
	abort  chan struct{}
}

func NewRunner(act Actions, list []Script) *Runner {
	r := &Runner{Act: act, Tick: 100 * time.Millisecond, scripts: map[string]Script{}, runs: map[string]*runState{}}
	for _, s := range list {
		r.scripts[s.ID] = s
	}
	return r
}

// Scripts devuelve los guiones disponibles.
func (r *Runner) Scripts() []Script {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Script, 0, len(r.scripts))
	for _, s := range r.scripts {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

var (
	ErrNotFound = errors.New("no existe")
	ErrState    = errors.New("estado no válido para esa operación")
)

// Start ejecuta un guion. Solo puede haber una ejecución activa por plataforma:
// dos guiones a la vez sobre la misma plataforma se pisarían las fallas.
func (r *Runner) Start(scriptID string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.scripts[scriptID]
	if !ok {
		return Run{}, fmt.Errorf("guion %q: %w", scriptID, ErrNotFound)
	}
	for _, rs := range r.runs {
		if rs.Platform == s.Platform && (rs.State == StateRunning || rs.State == StatePaused) {
			return Run{}, fmt.Errorf("%w: ya hay una ejecución activa en %s (%s)", ErrState, s.Platform, rs.ID)
		}
	}
	r.seq++
	rs := &runState{script: s, abort: make(chan struct{}), Run: Run{
		ID: fmt.Sprintf("r%d", r.seq), ScriptID: s.ID, Title: s.Title, Platform: s.Platform, State: StateRunning,
		Total: s.Duration(), Steps: len(s.Steps), StartedAt: time.Now().UTC(),
	}}
	r.runs[rs.ID] = rs
	go r.loop(rs)
	return rs.Run, nil
}

// loop avanza el reloj del guion solo mientras no está en pausa, así cada paso
// ocurre a su tiempo relativo descontando las pausas.
func (r *Runner) loop(rs *runState) {
	t := time.NewTicker(r.Tick)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-rs.abort:
			return
		case now := <-t.C:
			r.mu.Lock()
			if rs.State != StateRunning && rs.State != StatePaused {
				r.mu.Unlock()
				return
			}
			if !rs.pause {
				rs.Elapsed += now.Sub(last)
			}
			last = now
			var due []Step
			for rs.NextStep < len(rs.script.Steps) && rs.script.Steps[rs.NextStep].at <= rs.Elapsed {
				due = append(due, rs.script.Steps[rs.NextStep])
				rs.NextStep++
			}
			finished := rs.NextStep >= len(rs.script.Steps) && !rs.pause
			if finished {
				rs.State = StateFinished
			}
			elapsed := rs.Elapsed
			r.mu.Unlock()

			for _, st := range due {
				r.mu.Lock()
				aborted := rs.State == StateAborted
				r.mu.Unlock()
				if aborted {
					return // abortar deja la plataforma sana: no se aplica nada más
				}
				summary, err := r.exec(rs.Platform, st)
				ev := RunEvent{At: elapsed, Summary: summary}
				if err != nil {
					ev.Error = err.Error()
				}
				r.mu.Lock()
				rs.Events = append(rs.Events, ev)
				r.mu.Unlock()
			}
			if finished {
				return
			}
		}
	}
}

func (r *Runner) exec(platform string, st Step) (string, error) {
	switch {
	case st.Traffic != nil:
		return fmt.Sprintf("tráfico a %.1f usuarios/s", st.Traffic.RPS), r.Act.SetTraffic(platform, st.Traffic.RPS)
	case st.Fault != nil:
		f := st.Fault
		return fmt.Sprintf("falla %s en %s", f.Type, f.Service), r.Act.ApplyFault(platform, f.Service, f.Type, f.Params, f.DurationS)
	case st.Attack != nil:
		a := st.Attack
		return fmt.Sprintf("ataque %s contra %s", a.Kind, a.Service), r.Act.Attack(platform, a.Service, a.Kind, a.Intensity, a.DurationS)
	case st.UserAction != nil:
		n := max(st.UserAction.Repeat, 1)
		r.Act.UserAction(platform, st.UserAction.Action, n)
		return fmt.Sprintf("acción de usuario «%s» ×%d", st.UserAction.Action, n), nil
	default:
		r.Act.RestoreAll(platform)
		return "restaurar la plataforma", nil
	}
}

func (r *Runner) change(id string, from []string, to string, pause bool) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rs, ok := r.runs[id]
	if !ok {
		return Run{}, fmt.Errorf("ejecución %q: %w", id, ErrNotFound)
	}
	okState := false
	for _, f := range from {
		okState = okState || rs.State == f
	}
	if !okState {
		return rs.Run, fmt.Errorf("%w: está %s", ErrState, rs.State)
	}
	rs.State, rs.pause = to, pause
	return rs.Run, nil
}

func (r *Runner) Pause(id string) (Run, error) {
	return r.change(id, []string{StateRunning}, StatePaused, true)
}

func (r *Runner) Resume(id string) (Run, error) {
	return r.change(id, []string{StatePaused}, StateRunning, false)
}

// Abort detiene el guion y deja la plataforma sana.
func (r *Runner) Abort(id string) (Run, error) {
	run, err := r.change(id, []string{StateRunning, StatePaused}, StateAborted, false)
	if err != nil {
		return run, err
	}
	r.mu.Lock()
	rs := r.runs[id]
	close(rs.abort)
	r.mu.Unlock()
	r.Act.RestoreAll(run.Platform)
	return run, nil
}

// AbortAll aborta toda ejecución activa (reinicio del escenario).
func (r *Runner) AbortAll() int {
	var ids []string
	r.mu.Lock()
	for id, rs := range r.runs {
		if rs.State == StateRunning || rs.State == StatePaused {
			ids = append(ids, id)
		}
	}
	r.mu.Unlock()
	for _, id := range ids {
		_, _ = r.Abort(id)
	}
	return len(ids)
}

func (r *Runner) Get(id string) (Run, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rs, ok := r.runs[id]
	if !ok {
		return Run{}, false
	}
	return copyRun(rs), true
}

func (r *Runner) List() []Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Run, 0, len(r.runs))
	for _, rs := range r.runs {
		out = append(out, copyRun(rs))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func copyRun(rs *runState) Run {
	c := rs.Run
	c.Events = append([]RunEvent(nil), rs.Events...)
	return c
}
