package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"demo-lab/server/logs"
)

// WAF simulado: cada servicio reconoce en sus propias peticiones las firmas del
// catálogo de ataques (spec §2.10) y escribe el evento de seguridad. Hasta
// wafBlockAfter eventos de una misma IP y regla cada uno queda en el log; al
// superarlo la IP se bloquea (un solo evento de nivel error) y de ahí en adelante
// solo se registra una de cada wafSampleBlocked peticiones, para no inundar a
// Sentinel (cada evento cuesta un embedding).
const (
	wafWindow        = time.Minute
	wafBlockAfter    = 8
	wafSampleBlocked = 10
)

// Attack es lo que el generador de ataques envía a /v1/attack de un servicio.
type Attack struct {
	Kind    string `json:"kind"`
	IP      string `json:"ip"`
	Account string `json:"account,omitempty"`
	Seq     int    `json:"seq"`
	// Accounts es cuántas cuentas distintas ha probado la IP (credential stuffing).
	Accounts int `json:"accounts,omitempty"`
	// Records es el volumen leído en una exfiltración; Rate, las solicitudes por minuto de un bot.
	Records int `json:"records,omitempty"`
	Rate    int `json:"rate,omitempty"`
}

// Rule traduce un tipo de ataque al nombre de la regla del WAF.
var wafRules = map[string]string{
	"brute_force":         "brute_force",
	"credential_stuffing": "credential_stuffing",
	"mfa_bypass":          "mfa_bypass",
	"sqli":                "sqli",
	"exfiltration":        "exfiltration",
	"scraping":            "bot_scraping",
}

func wafMessage(a Attack, service string) (string, bool) {
	rule, ok := wafRules[a.Kind]
	if !ok {
		return "", false
	}
	switch a.Kind {
	case "brute_force":
		return fmt.Sprintf("Posible ataque de fuerza bruta: inicio de sesión fallido número %d para la cuenta %s desde la IP %s (regla WAF %s)", a.Seq, a.Account, a.IP, rule), true
	case "credential_stuffing":
		return fmt.Sprintf("Posible credential stuffing: inicio de sesión fallido para la cuenta %s desde la IP %s, que ya probó %d cuentas distintas (regla WAF %s)", a.Account, a.IP, a.Accounts, rule), true
	case "mfa_bypass":
		return fmt.Sprintf("Intento de evasión del segundo factor: código OTP inválido y token reutilizado para la cuenta %s desde la IP %s, intento %d (regla WAF %s)", a.Account, a.IP, a.Seq, rule), true
	case "sqli":
		return fmt.Sprintf("Intento de inyección SQL detectado en %s: parámetro con carga maliciosa \"' OR '1'='1' --\" desde la IP %s (regla WAF %s)", service, a.IP, rule), true
	case "exfiltration":
		return fmt.Sprintf("Posible exfiltración de datos: lectura masiva de %d registros por la cuenta %s desde la IP %s fuera de horario (regla WAF %s)", a.Records, a.Account, a.IP, rule), true
	default:
		return fmt.Sprintf("Posible scraping automatizado: %d solicitudes por minuto desde la IP %s con user-agent de bot (regla WAF %s)", a.Rate, a.IP, rule), true
	}
}

type wafKey struct{ ip, kind string }

type wafState struct {
	mu      sync.Mutex
	hits    map[wafKey][]time.Time
	blocked map[wafKey]time.Time
	skipped map[wafKey]int
}

func newWAF() *wafState {
	return &wafState{hits: map[wafKey][]time.Time{}, blocked: map[wafKey]time.Time{}, skipped: map[wafKey]int{}}
}

// observe registra una petición y decide qué se escribe: nada, un evento de
// advertencia o el evento de bloqueo.
func (s *wafState) observe(k wafKey, now time.Time) (block, record bool, count int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if until, ok := s.blocked[k]; ok {
		if now.Before(until) {
			s.skipped[k]++
			return false, s.skipped[k]%wafSampleBlocked == 0, 0
		}
		delete(s.blocked, k)
		delete(s.hits, k)
		delete(s.skipped, k)
	}
	kept := s.hits[k][:0]
	for _, t := range s.hits[k] {
		if now.Sub(t) < wafWindow {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	s.hits[k] = kept
	if len(kept) > wafBlockAfter {
		s.blocked[k] = now.Add(wafWindow)
		return true, false, len(kept)
	}
	return false, true, len(kept)
}

// attack atiende /v1/attack. Con el servicio caído responde 503 sin registrar
// nada (no hay quién lo escriba); degradado, tarda lo que tarde el servicio.
func (in *instance) attack(w http.ResponseWriter, r *http.Request) {
	ef := in.effect()
	sleep(r.Context(), ef.LatencyMs, ef.JitterMs)
	if ef.Down {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "service": in.svc.Slug})
		return
	}
	var a Attack
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<14)).Decode(&a); err != nil || a.IP == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "ataque inválido"})
		return
	}
	msg, ok := wafMessage(a, in.svc.Slug)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "tipo de ataque desconocido"})
		return
	}
	k := wafKey{ip: a.IP, kind: a.Kind}
	block, record, n := in.waf.observe(k, time.Now())
	rec := logs.Record{
		Platform: in.platform.Slug, Service: in.svc.Slug, Format: in.svc.LogFormat, Level: logs.Warning,
		Message: msg, ErrorKind: "security_" + a.Kind, HTTP: &logs.HTTPInfo{Method: r.Method, Route: r.URL.Path, Status: http.StatusForbidden},
	}
	switch {
	case block:
		rec.Level = logs.Error
		rec.Message = fmt.Sprintf("El WAF bloqueó temporalmente la IP %s por superar el umbral de la regla %s: %d eventos en %d s", a.IP, wafRules[a.Kind], n, int(wafWindow.Seconds()))
		in.rt.Logger.Emit(rec)
		writeJSON(w, http.StatusForbidden, map[string]any{"ok": false, "blocked": true, "service": in.svc.Slug})
		return
	case record:
		in.rt.Logger.Emit(rec)
	}
	code := http.StatusForbidden
	if n == 0 {
		code = http.StatusTooManyRequests // IP ya bloqueada
	}
	writeJSON(w, code, map[string]any{"ok": false, "service": in.svc.Slug, "rule": wafRules[a.Kind]})
}
