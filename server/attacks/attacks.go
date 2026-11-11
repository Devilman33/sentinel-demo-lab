// Package attacks genera ataques sintéticos contra los servicios del propio
// módulo. El objetivo siempre es un servicio del catálogo, que se resuelve a su
// puerto local: no hay forma de pedir un host o una URL, así que el generador no
// puede apuntar fuera del módulo (spec §2.10, TC-12).
package attacks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/runtime"
)

var ErrInvalid = errors.New("ataque inválido")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Kind describe un tipo de ataque y los servicios contra los que tiene sentido.
type Kind struct {
	Name    string   `json:"name"`
	Label   string   `json:"label"`
	Targets []string `json:"targets"`
}

var kinds = []Kind{
	{"brute_force", "Fuerza bruta", []string{"auth-clientes", "auth-banca", "auth-pacientes"}},
	{"credential_stuffing", "Credential stuffing", []string{"auth-clientes", "auth-banca", "auth-pacientes"}},
	{"mfa_bypass", "Bypass de segundo factor", []string{"auth-banca"}},
	{"sqli", "Inyección SQL", []string{"api-catalogo", "storefront-web", "banca-en-linea", "portal-paciente", "agenda-medica"}},
	{"exfiltration", "Exfiltración de datos", []string{"ficha-clinica-db", "core-bancario", "db-transaccional"}},
	{"scraping", "Bots / scraping", []string{"api-catalogo", "agenda-medica"}},
}

// Kinds devuelve el catálogo de ataques.
func Kinds() []Kind { return append([]Kind(nil), kinds...) }

// closingNotes es la nota de cierre de cada tipo de ataque. La usa el reinicio
// cuando el incidente es de Seguridad y no hay un escenario del servicio que lo
// explique mejor: el corpus de similares debe leer una resolución propia, no la
// nota genérica del reinicio.
var closingNotes = map[string]string{
	"brute_force":         "Se bloquearon las direcciones origen en el WAF, se limitaron los intentos por cuenta y las cuentas atacadas restablecieron su clave.",
	"credential_stuffing": "Se activó el desafío adicional para las IP sospechosas, se bloquearon los rangos origen y se notificó a las cuentas con intentos exitosos para cambiar su clave.",
	"mfa_bypass":          "Se invalidaron los tokens emitidos durante el intento, se exigió un segundo factor nuevo y se reforzó la validación del código.",
	"sqli":                "El WAF bloqueó las cargas de inyección; se revisó que las consultas siguieran parametrizadas y se endurecieron las reglas de búsqueda.",
	"exfiltration":        "Se revocó la credencial de la cuenta de servicio, se cortó el acceso y se revisó el registro para confirmar que no hubo salida de datos.",
	"scraping":            "Se aplicó limitación por IP y por sesión, y se bloqueó el rango que concentraba las peticiones automatizadas.",
}

// ClosingNote devuelve la nota de cierre de un tipo de ataque, o "" si no hay.
func ClosingNote(kind string) string { return closingNotes[kind] }

// ValidKind valida plataforma, objetivo y tipo sin lanzar nada.
func ValidKind(platform, target, kind string) error {
	p, ok := catalog.BySlug(platform)
	if !ok {
		return invalid("plataforma %q desconocida", platform)
	}
	// El objetivo es un slug del catálogo: cualquier otra cosa (host, URL, puerto) se rechaza.
	if _, ok := p.Service(target); !ok {
		return invalid("%q no es un servicio de %s: solo se puede atacar a los servicios del módulo", target, platform)
	}
	for _, k := range kinds {
		if k.Name != kind {
			continue
		}
		for _, t := range k.Targets {
			if t == target {
				return nil
			}
		}
		return invalid("%s no aplica a %s (objetivos: %v)", k.Label, target, k.Targets)
	}
	return invalid("tipo %q desconocido", kind)
}

const (
	MaxIntensity = 10  // eventos por segundo
	MaxDuration  = 600 // segundos
)

// Attack es un ataque en curso o terminado.
type Attack struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Target    string    `json:"target"`
	Kind      string    `json:"kind"`
	Intensity int       `json:"intensity"`
	DurationS int       `json:"duration_s"`
	Origin    string    `json:"origin"`
	StartedAt time.Time `json:"started_at"`
	Sent      int       `json:"sent"`
	Done      bool      `json:"done"`

	cancel context.CancelFunc
}

type Generator struct {
	client *http.Client

	mu      sync.Mutex
	seq     int
	attacks map[string]*Attack
}

func New() *Generator {
	return &Generator{client: &http.Client{Timeout: 5 * time.Second}, attacks: map[string]*Attack{}}
}

// Launch valida y lanza un ataque; la validación ocurre antes de cualquier petición.
func (g *Generator) Launch(platform, target, kind string, intensity, durationS int, origin string) (Attack, error) {
	if err := ValidKind(platform, target, kind); err != nil {
		return Attack{}, err
	}
	if intensity < 1 || intensity > MaxIntensity {
		return Attack{}, invalid("intensity debe estar entre 1 y %d eventos por segundo", MaxIntensity)
	}
	if durationS < 1 || durationS > MaxDuration {
		return Attack{}, invalid("duration_s debe estar entre 1 y %d", MaxDuration)
	}
	p, _ := catalog.BySlug(platform)
	svc, _ := p.Service(target)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(durationS)*time.Second)
	g.mu.Lock()
	g.seq++
	a := &Attack{ID: fmt.Sprintf("a%d", g.seq), Platform: platform, Target: target, Kind: kind, Intensity: intensity,
		DurationS: durationS, Origin: origin, StartedAt: time.Now().UTC(), cancel: cancel}
	g.attacks[a.ID] = a
	g.mu.Unlock()

	go g.run(ctx, a, runtime.BaseURL(svc))
	return g.snapshot(a), nil
}

func (g *Generator) snapshot(a *Attack) Attack {
	g.mu.Lock()
	defer g.mu.Unlock()
	c := *a
	c.cancel = nil
	return c
}

func (g *Generator) run(ctx context.Context, a *Attack, base string) {
	defer func() {
		g.mu.Lock()
		a.Done = true
		g.mu.Unlock()
		a.cancel()
	}()
	t := time.NewTicker(time.Second / time.Duration(a.Intensity))
	defer t.Stop()
	for i := 1; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.send(ctx, base, payload(a.Kind, a.Target, i))
			g.mu.Lock()
			a.Sent++
			g.mu.Unlock()
		}
	}
}

func (g *Generator) send(ctx context.Context, base string, body map[string]any) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/attack", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := g.client.Do(req); err == nil {
		res.Body.Close()
	}
}

// payload arma la petición número i. Las IP salen del rango de documentación
// 203.0.113.0/24 (RFC 5737), que no existe en internet.
func payload(kind, target string, i int) map[string]any {
	ip := func(n int) string { return fmt.Sprintf("203.0.113.%d", 10+n) }
	switch kind {
	case "brute_force":
		return map[string]any{"kind": kind, "ip": ip(i % 2), "account": "cliente.demo", "seq": i}
	case "credential_stuffing":
		return map[string]any{"kind": kind, "ip": ip(i % 3), "account": fmt.Sprintf("usuario%03d", i), "accounts": i / 3, "seq": i}
	case "mfa_bypass":
		return map[string]any{"kind": kind, "ip": ip(0), "account": "cliente.demo", "seq": i}
	case "sqli":
		return map[string]any{"kind": kind, "ip": ip(i % 2), "seq": i}
	case "exfiltration":
		return map[string]any{"kind": kind, "ip": ip(5), "account": "svc-reportes", "records": 20000 + i*1500, "seq": i}
	default:
		return map[string]any{"kind": kind, "ip": ip(7), "rate": 600 + i*10, "seq": i}
	}
}

// Stop detiene un ataque; devuelve false si no existe.
func (g *Generator) Stop(id string) bool {
	g.mu.Lock()
	a, ok := g.attacks[id]
	g.mu.Unlock()
	if ok {
		a.cancel()
	}
	return ok
}

// StopAll detiene los ataques de una plataforma ("" = todas) y devuelve cuántos.
func (g *Generator) StopAll(platform string) int {
	g.mu.Lock()
	var cancels []context.CancelFunc
	for _, a := range g.attacks {
		if !a.Done && (platform == "" || a.Platform == platform) {
			cancels = append(cancels, a.cancel)
		}
	}
	g.mu.Unlock()
	for _, c := range cancels {
		c()
	}
	return len(cancels)
}

// List devuelve los ataques (los más recientes primero), con los terminados incluidos.
func (g *Generator) List() []Attack {
	g.mu.Lock()
	out := make([]Attack, 0, len(g.attacks))
	for _, a := range g.attacks {
		c := *a
		c.cancel = nil
		out = append(out, c)
	}
	g.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}
