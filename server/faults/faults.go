// Package faults es el motor de fallas: aplica y retira alteraciones de
// comportamiento sobre servicios simulados concretos. El runtime consulta el
// efecto combinado de las fallas activas de cada servicio en cada petición.
package faults

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	TypeDown       = "down"            // modo "503" (responde 503) o "refused" (cierra el puerto)
	TypeDegraded   = "degraded"        // latency_ms + jitter_ms
	TypeErrors     = "errors"          // error_rate 0..1
	TypeSaturation = "saturation"      // max_concurrency
	TypeSlowDep    = "slow_dependency" // target + latency_ms: solo la llamada a un upstream
)

type Params struct {
	Mode           string  `json:"mode,omitempty"`
	LatencyMs      int     `json:"latency_ms,omitempty"`
	JitterMs       int     `json:"jitter_ms,omitempty"`
	ErrorRate      float64 `json:"error_rate,omitempty"`
	MaxConcurrency int     `json:"max_concurrency,omitempty"`
	Target         string  `json:"target,omitempty"`
}

type Fault struct {
	ID        string     `json:"id"`
	Platform  string     `json:"platform"`
	Service   string     `json:"service"`
	Type      string     `json:"type"`
	Params    Params     `json:"params"`
	Origin    string     `json:"origin"` // console | script | marathon
	StartedAt time.Time  `json:"started_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Effect es el resultado combinado de todas las fallas activas de un servicio.
type Effect struct {
	Down           bool
	Refused        bool
	LatencyMs      int
	JitterMs       int
	ErrorRate      float64
	MaxConcurrency int
	DepLatencyMs   map[string]int
}

// Valid dice si el servicio existe y si lo hace el destino de slow_dependency;
// lo inyecta quien conoce el catálogo, para que este paquete no dependa de él.
type Valid func(platform, service string) bool

type Engine struct {
	valid    Valid
	onChange func(platform, service string)
	now      func() time.Time

	mu     sync.Mutex
	seq    int
	faults map[string]*Fault
	timers map[string]*time.Timer
}

func New(valid Valid, onChange func(platform, service string)) *Engine {
	return &Engine{valid: valid, onChange: onChange, now: time.Now, faults: map[string]*Fault{}, timers: map[string]*time.Timer{}}
}

var ErrInvalid = errors.New("falla inválida")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

func (e *Engine) validate(platform, service, typ string, p Params) error {
	if e.valid != nil && !e.valid(platform, service) {
		return invalid("el servicio %s/%s no existe", platform, service)
	}
	switch typ {
	case TypeDown:
		if p.Mode != "503" && p.Mode != "refused" {
			return invalid("mode debe ser 503 o refused")
		}
	case TypeDegraded:
		if p.LatencyMs < 1 || p.LatencyMs > 30000 || p.JitterMs < 0 || p.JitterMs > p.LatencyMs {
			return invalid("latency_ms debe estar entre 1 y 30000 y jitter_ms entre 0 y latency_ms")
		}
	case TypeErrors:
		if p.ErrorRate <= 0 || p.ErrorRate > 1 {
			return invalid("error_rate debe estar entre 0 (excluido) y 1")
		}
	case TypeSaturation:
		if p.MaxConcurrency < 1 {
			return invalid("max_concurrency debe ser al menos 1")
		}
	case TypeSlowDep:
		if p.LatencyMs < 1 || p.LatencyMs > 30000 || e.valid == nil || !e.valid(platform, p.Target) {
			return invalid("slow_dependency necesita un target existente y latency_ms entre 1 y 30000")
		}
	default:
		return invalid("tipo %q desconocido", typ)
	}
	return nil
}

// Apply activa una falla. durationS = 0 la deja hasta que se retire.
func (e *Engine) Apply(platform, service, typ string, p Params, durationS int, origin string) (Fault, error) {
	if err := e.validate(platform, service, typ, p); err != nil {
		return Fault{}, err
	}
	if durationS < 0 {
		return Fault{}, invalid("duration_s no puede ser negativo")
	}
	e.mu.Lock()
	e.seq++
	f := &Fault{ID: fmt.Sprintf("f%d", e.seq), Platform: platform, Service: service, Type: typ, Params: p, Origin: origin, StartedAt: e.now().UTC()}
	if durationS > 0 {
		exp := f.StartedAt.Add(time.Duration(durationS) * time.Second)
		f.ExpiresAt = &exp
		id := f.ID
		e.timers[id] = time.AfterFunc(time.Duration(durationS)*time.Second, func() { e.Remove(id) })
	}
	e.faults[f.ID] = f
	out := *f
	e.mu.Unlock()
	e.notify(platform, service)
	return out, nil
}

// Remove retira una falla; devuelve false si no existía (p. ej. ya expiró).
func (e *Engine) Remove(id string) bool {
	e.mu.Lock()
	f, ok := e.faults[id]
	if ok {
		delete(e.faults, id)
		if t := e.timers[id]; t != nil {
			t.Stop()
			delete(e.timers, id)
		}
	}
	e.mu.Unlock()
	if ok {
		e.notify(f.Platform, f.Service)
	}
	return ok
}

// RestoreAll retira todas las fallas de una plataforma ("" = todas) y devuelve cuántas.
func (e *Engine) RestoreAll(platform string) int {
	e.mu.Lock()
	var gone []Fault
	for id, f := range e.faults {
		if platform == "" || f.Platform == platform {
			gone = append(gone, *f)
			delete(e.faults, id)
			if t := e.timers[id]; t != nil {
				t.Stop()
				delete(e.timers, id)
			}
		}
	}
	e.mu.Unlock()
	for _, f := range gone {
		e.notify(f.Platform, f.Service)
	}
	return len(gone)
}

func (e *Engine) notify(platform, service string) {
	if e.onChange != nil {
		e.onChange(platform, service)
	}
}

// List devuelve las fallas activas de una plataforma ("" = todas).
func (e *Engine) List(platform string) []Fault {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Fault{}
	for _, f := range e.faults {
		if platform == "" || f.Platform == platform {
			out = append(out, *f)
		}
	}
	return out
}

// EffectFor combina las fallas activas de un servicio: la latencia y los
// errores se toman del más severo, no se suman.
func (e *Engine) EffectFor(platform, service string) Effect {
	e.mu.Lock()
	defer e.mu.Unlock()
	var ef Effect
	for _, f := range e.faults {
		if f.Platform != platform || f.Service != service {
			continue
		}
		p := f.Params
		switch f.Type {
		case TypeDown:
			ef.Down = true
			ef.Refused = ef.Refused || p.Mode == "refused"
		case TypeDegraded:
			if p.LatencyMs > ef.LatencyMs {
				ef.LatencyMs, ef.JitterMs = p.LatencyMs, p.JitterMs
			}
		case TypeErrors:
			ef.ErrorRate = max(ef.ErrorRate, p.ErrorRate)
		case TypeSaturation:
			if ef.MaxConcurrency == 0 || p.MaxConcurrency < ef.MaxConcurrency {
				ef.MaxConcurrency = p.MaxConcurrency
			}
		case TypeSlowDep:
			if ef.DepLatencyMs == nil {
				ef.DepLatencyMs = map[string]int{}
			}
			ef.DepLatencyMs[p.Target] = max(ef.DepLatencyMs[p.Target], p.LatencyMs)
		}
	}
	return ef
}
