// Package runtime levanta los servicios simulados: un listener HTTP por
// servicio, que atiende peticiones de verdad, llama a sus dependencias por
// HTTP, aplica las fallas activas y escribe su propio log.
package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/faults"
	"demo-lab/server/logs"
)

const (
	depTimeout    = 3 * time.Second
	healthTimeout = 2 * time.Second
	slowThreshold = 500 // ms: sobre esto el servicio registra una advertencia de latencia
	// hopMargin es lo que cada salto espera de más por cada nivel de
	// dependencias que tiene debajo: así se agota primero el salto más profundo
	// y el 504 que sube nombra al servicio lento, no al primero de la cadena.
	hopMargin = 250 * time.Millisecond
)

type Runtime struct {
	Host   string // interfaz donde escuchan los servicios
	Logger *logs.Logger
	Faults *faults.Engine

	client    *http.Client
	mu        sync.Mutex
	instances map[string]*instance
}

func New(host string, l *logs.Logger) *Runtime {
	// Sin Timeout global: cada llamada fija su propia espera (hopTimeout y
	// healthTimeout) con el contexto.
	rt := &Runtime{Host: host, Logger: l, client: &http.Client{}, instances: map[string]*instance{}}
	rt.Faults = faults.New(rt.exists, rt.Sync)
	return rt
}

func (rt *Runtime) exists(platform, service string) bool {
	p, ok := catalog.BySlug(platform)
	if !ok {
		return false
	}
	_, ok = p.Service(service)
	return ok
}

type instance struct {
	rt       *Runtime
	platform catalog.Platform
	svc      catalog.Service
	inflight atomic.Int64
	waf      *wafState

	mu  sync.Mutex
	srv *http.Server
}

func key(platform, service string) string { return platform + "/" + service }

// BaseURL es la dirección con la que los demás servicios del módulo llaman a uno.
func BaseURL(s catalog.Service) string { return fmt.Sprintf("http://127.0.0.1:%d", s.Port) }

// Start abre el listener de cada servicio que no esté caído con "refused".
func (rt *Runtime) Start() error {
	for _, p := range catalog.All() {
		for _, s := range p.Services {
			in := &instance{rt: rt, platform: p, svc: s, waf: newWAF()}
			rt.mu.Lock()
			rt.instances[key(p.Slug, s.Slug)] = in
			rt.mu.Unlock()
			if err := in.listen(); err != nil {
				return fmt.Errorf("%s/%s: %w", p.Slug, s.Slug, err)
			}
		}
	}
	return nil
}

// Stop cierra todos los listeners.
func (rt *Runtime) Stop() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for _, in := range rt.instances {
		in.close()
	}
}

// Sync alinea el listener de un servicio con sus fallas: "refused" lo cierra,
// y al retirarla lo vuelve a abrir. Lo llama el motor de fallas en cada cambio.
func (rt *Runtime) Sync(platform, service string) {
	rt.mu.Lock()
	in := rt.instances[key(platform, service)]
	rt.mu.Unlock()
	if in == nil {
		return
	}
	if rt.Faults.EffectFor(platform, service).Refused {
		in.close()
		return
	}
	_ = in.listen()
}

func (in *instance) listen() error {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.srv != nil {
		return nil
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", in.rt.Host, in.svc.Port))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: in.handler(), ReadHeaderTimeout: 5 * time.Second}
	in.srv = srv
	go func() { _ = srv.Serve(ln) }()
	return nil
}

func (in *instance) close() {
	in.mu.Lock()
	srv := in.srv
	in.srv = nil
	in.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

func (in *instance) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", in.health)
	mux.HandleFunc("/health/deep", in.healthDeep)
	mux.HandleFunc("/v1/op", in.operation)
	mux.HandleFunc("POST /v1/attack", in.attack)
	if in.svc.Slug == in.platform.Entry {
		mux.HandleFunc("/v1/action/{name}", in.operation)
	}
	return mux
}

/* ── Salud ──────────────────────────────────────────────────────────────── */

func (in *instance) effect() faults.Effect {
	return in.rt.Faults.EffectFor(in.platform.Slug, in.svc.Slug)
}

func sleep(ctx context.Context, ms, jitter int) {
	if ms <= 0 {
		return
	}
	if jitter > 0 {
		ms += mrand.IntN(2*jitter+1) - jitter
	}
	select {
	case <-time.After(time.Duration(max(ms, 0)) * time.Millisecond):
	case <-ctx.Done():
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// health responde por el propio servicio. Sondear no se registra: el Poller
// consulta cada pocos segundos y llenaría el log de ruido.
func (in *instance) health(w http.ResponseWriter, r *http.Request) {
	ef := in.effect()
	sleep(r.Context(), ef.LatencyMs, ef.JitterMs)
	if ef.Down {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "down", "service": in.svc.Slug})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": in.svc.Slug})
}

// healthDeep recorre las dependencias: es el monitoring_url del BFF. Devuelve
// 503 si el servicio o alguna dependencia (transitiva) falla, y tarda lo que
// tarde la cadena, de modo que una degradación aguas abajo se vea como latencia.
func (in *instance) healthDeep(w http.ResponseWriter, r *http.Request) {
	ef := in.effect()
	sleep(r.Context(), ef.LatencyMs, ef.JitterMs)
	var failing []string
	if ef.Down {
		failing = append(failing, in.svc.Slug)
	}
	for _, dep := range in.svc.DependsOn {
		d, _ := in.platform.Service(dep)
		sleep(r.Context(), ef.DepLatencyMs[dep], 0)
		ctx, cancel := context.WithTimeout(r.Context(), healthTimeout)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(d)+"/health/deep", nil)
		res, err := in.rt.client.Do(req)
		cancel()
		if err != nil {
			failing = append(failing, dep)
			continue
		}
		var body struct {
			Failing []string `json:"failing"`
		}
		_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			if len(body.Failing) > 0 {
				failing = append(failing, body.Failing...)
			} else {
				failing = append(failing, dep)
			}
		}
	}
	if len(failing) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "down", "service": in.svc.Slug, "failing": failing})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": in.svc.Slug})
}

/* ── Operaciones de negocio ─────────────────────────────────────────────── */

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// parseTraceparent lee "00-<trace 32>-<span 16>-01".
func parseTraceparent(h string) string {
	parts := strings.Split(h, "-")
	if len(parts) == 4 && len(parts[1]) == 32 {
		return parts[1]
	}
	return ""
}

// failure describe por qué una operación no se completó.
type failure struct {
	Code      int
	Kind      string
	Message   string
	Component string // causa raíz
	// RootKind es el Kind del servicio donde nació la falla (p. ej. saturation).
	// Viaja en la respuesta para que el sitio diga «hay mucha demanda»; el log
	// de cada salto conserva su propio Kind.
	RootKind string
}

func (in *instance) operation(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	trace := parseTraceparent(r.Header.Get("traceparent"))
	if trace == "" {
		trace = randHex(16)
	}
	span := randHex(8)
	action := r.PathValue("name")
	if action == "" {
		action = r.Header.Get("X-Demo-Action")
	}

	status, body := in.run(r.Context(), trace, span, action)
	latency := time.Since(start).Milliseconds()
	body["service"], body["trace_id"] = in.svc.Slug, trace

	rec := logs.Record{
		Platform: in.platform.Slug, Service: in.svc.Slug, TraceID: trace, SpanID: span, Format: in.svc.LogFormat,
		HTTP: &logs.HTTPInfo{Method: r.Method, Route: r.URL.Path, Status: status}, LatencyMs: latency,
	}
	switch {
	case status >= 500:
		rec.Level, rec.Message, rec.ErrorKind = logs.Error, body["error"].(string), body["kind"].(string)
	case latency > slowThreshold:
		rec.Level, rec.Message = logs.Warning, fmt.Sprintf("respuesta lenta en %s: %d ms", in.svc.Op, latency)
	default:
		rec.Level, rec.Message = logs.Info, fmt.Sprintf("%s: operación completada en %d ms", capitalize(in.svc.Op), latency)
	}
	if status >= 500 && in.svc.Slug == in.platform.Entry {
		rec.Level = logs.Critical // el usuario final recibió el error
	}
	in.rt.Logger.Emit(rec)
	writeJSON(w, status, body)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// run ejecuta la operación y devuelve el código y el cuerpo de la respuesta.
func (in *instance) run(ctx context.Context, trace, span, action string) (int, map[string]any) {
	ef := in.effect()
	n := in.inflight.Add(1)
	defer in.inflight.Add(-1)

	switch {
	case ef.MaxConcurrency > 0 && int(n) > ef.MaxConcurrency:
		return fail(in, failure{Code: 503, Kind: "saturation",
			Message: fmt.Sprintf("pool de conexiones agotado en %s: %d solicitudes en curso (límite %d)", in.svc.Slug, n, ef.MaxConcurrency)})
	case ef.Down:
		return fail(in, failure{Code: 503, Kind: "service_down",
			Message: fmt.Sprintf("servicio no disponible: %s rechazada en %s", in.svc.Op, in.svc.Slug)})
	}
	sleep(ctx, ef.LatencyMs, ef.JitterMs)
	if ef.ErrorRate > 0 && mrand.Float64() < ef.ErrorRate {
		return fail(in, failure{Code: 500, Kind: "injected_error",
			Message: fmt.Sprintf("error interno al procesar %s en %s", in.svc.Op, in.svc.Slug)})
	}

	for _, dep := range in.dependencies(action) {
		sleep(ctx, ef.DepLatencyMs[dep], 0)
		if f := in.call(ctx, dep, trace, span, action); f != nil {
			return fail(in, *f)
		}
	}
	return http.StatusOK, map[string]any{"ok": true, "action": action, "receipt": randHex(6)}
}

func fail(in *instance, f failure) (int, map[string]any) {
	if f.Component == "" {
		f.Component = in.svc.Slug
	}
	if f.RootKind == "" {
		f.RootKind = f.Kind
	}
	return f.Code, map[string]any{"ok": false, "error": f.Message, "kind": f.Kind, "component": f.Component, "root_kind": f.RootKind}
}

// dependencies devuelve a quiénes llama esta operación. El BFF delega al
// objetivo de la acción; los demás llaman a todo lo que declaran, menos lo que
// la acción no necesita.
func (in *instance) dependencies(action string) []string {
	act, _ := in.platform.Action(action)
	if in.svc.Slug == in.platform.Entry {
		if act.Target == "" {
			return nil
		}
		return []string{act.Target}
	}
	var out []string
	for _, d := range in.svc.DependsOn {
		if !contains(act.Skip, d) {
			out = append(out, d)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// hopTimeout es cuánto espera este servicio a una dependencia: depTimeout más
// hopMargin por cada nivel que ella tiene debajo. El salto más profundo espera
// justo depTimeout.
func (in *instance) hopTimeout(dep string) time.Duration {
	return depTimeout + time.Duration(levelsBelow(in.platform, dep, 0))*hopMargin
}

// levelsBelow cuenta los niveles de dependencias bajo un servicio (0 si no
// llama a nadie). depth corta la recursión si el catálogo tuviera un ciclo.
func levelsBelow(p catalog.Platform, slug string, depth int) int {
	s, ok := p.Service(slug)
	if !ok || depth > len(p.Services) {
		return 0
	}
	most := 0
	for _, d := range s.DependsOn {
		most = max(most, 1+levelsBelow(p, d, depth+1))
	}
	return most
}

// call invoca /v1/op de una dependencia propagando la traza. Un error aguas
// abajo se traduce a un mensaje que nombra la causa raíz, para que los logs de
// toda la cascada compartan vocabulario.
func (in *instance) call(ctx context.Context, dep, trace, span, action string) *failure {
	d, _ := in.platform.Service(dep)
	wait := in.hopTimeout(dep)
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, BaseURL(d)+"/v1/op", nil)
	req.Header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", trace, span))
	req.Header.Set("X-Demo-Action", action)
	res, err := in.rt.client.Do(req)
	if err != nil {
		kind, cause := "upstream_unavailable", "connection refused"
		code := http.StatusServiceUnavailable
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			kind, cause = "upstream_timeout", fmt.Sprintf("timeout tras %d ms", wait.Milliseconds())
			code = http.StatusGatewayTimeout
		}
		return &failure{Code: code, Kind: kind, Component: dep,
			Message: fmt.Sprintf("no se pudo completar %s: upstream %s: %s", in.svc.Op, dep, cause)}
	}
	defer res.Body.Close()
	if res.StatusCode < 500 {
		return nil
	}
	var body struct {
		Component string `json:"component"`
		Kind      string `json:"kind"`
		RootKind  string `json:"root_kind"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&body)
	root := body.Component
	if root == "" {
		root = dep
	}
	rootKind := body.RootKind
	if rootKind == "" {
		rootKind = body.Kind
	}
	msg := fmt.Sprintf("no se pudo completar %s: upstream %s respondió HTTP %d", in.svc.Op, dep, res.StatusCode)
	if root != dep {
		msg += " (causa raíz: " + root + ")"
	}
	// El código llega al sitio tal como nació, que es lo que debe leer el
	// visitante: 504 si un salto se agotó («tardó demasiado») y 500 si fue un
	// error inesperado (errores intermitentes). Todo lo demás es 503.
	code, kind := http.StatusServiceUnavailable, "upstream_error"
	switch res.StatusCode {
	case http.StatusGatewayTimeout:
		code, kind = http.StatusGatewayTimeout, "upstream_timeout"
	case http.StatusInternalServerError:
		code = http.StatusInternalServerError
	}
	return &failure{Code: code, Kind: kind, Component: root, RootKind: rootKind, Message: msg}
}
