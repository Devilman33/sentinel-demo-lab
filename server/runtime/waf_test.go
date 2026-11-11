package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"demo-lab/server/catalog"
	"demo-lab/server/logs"
)

func attackOnce(t *testing.T, platform, service string, a Attack) int {
	t.Helper()
	p, _ := catalog.BySlug(platform)
	s, _ := p.Service(service)
	raw, _ := json.Marshal(a)
	res, err := http.Post(BaseURL(s)+"/v1/attack", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func security(l *logs.Logger, service string) (warnings, errs []logs.Record) {
	for _, r := range l.Recent(logs.Filter{Service: service}) {
		if !strings.HasPrefix(r.ErrorKind, "security_") {
			continue
		}
		if r.Level == logs.Error {
			errs = append(errs, r)
		} else {
			warnings = append(warnings, r)
		}
	}
	return
}

// TC-11 (parte del módulo): el WAF escribe los eventos de seguridad del servicio atacado.
func TestWAFRegistraEventosYBloqueaLaIP(t *testing.T) {
	_, l := start(t)
	var last int
	for i := 1; i <= wafBlockAfter+5; i++ {
		last = attackOnce(t, "banco", "auth-banca", Attack{Kind: "brute_force", IP: "203.0.113.10", Account: "cliente.demo", Seq: i})
	}
	warnings, errs := security(l, "auth-banca")
	if len(warnings) != wafBlockAfter {
		t.Errorf("advertencias %d, esperaba %d (una por petición hasta el umbral)", len(warnings), wafBlockAfter)
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Message, "bloqueó temporalmente la IP 203.0.113.10") {
		t.Fatalf("debía haber un solo evento de bloqueo: %+v", errs)
	}
	if !strings.Contains(warnings[0].Message, "fuerza bruta") || !strings.Contains(warnings[0].Message, "cliente.demo") {
		t.Errorf("mensaje poco claro: %q", warnings[0].Message)
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("con la IP bloqueada esperaba 429, fue %d", last)
	}
	if warnings[0].Platform != "banco" || warnings[0].Service != "auth-banca" {
		t.Errorf("registro mal atribuido: %+v", warnings[0])
	}
}

func TestWAFOtraIPNoHeredaElBloqueo(t *testing.T) {
	_, l := start(t)
	for i := 1; i <= wafBlockAfter+2; i++ {
		attackOnce(t, "banco", "auth-banca", Attack{Kind: "brute_force", IP: "203.0.113.10", Account: "a", Seq: i})
	}
	if code := attackOnce(t, "banco", "auth-banca", Attack{Kind: "brute_force", IP: "203.0.113.11", Account: "a", Seq: 1}); code != http.StatusForbidden {
		t.Errorf("otra IP debía recibir 403 (no bloqueada), fue %d", code)
	}
	if w, _ := security(l, "auth-banca"); len(w) != wafBlockAfter+1 {
		t.Errorf("advertencias %d", len(w))
	}
}

func TestWAFConServicioCaidoNoRegistraNada(t *testing.T) {
	rt, l := start(t)
	down(t, rt, "banco", "auth-banca", "503")
	if code := attackOnce(t, "banco", "auth-banca", Attack{Kind: "brute_force", IP: "203.0.113.10", Account: "a", Seq: 1}); code != http.StatusServiceUnavailable {
		t.Errorf("servicio caído debía responder 503, fue %d", code)
	}
	if w, e := security(l, "auth-banca"); len(w)+len(e) != 0 {
		t.Error("un servicio caído no escribe eventos")
	}
}

func TestWAFRechazaTiposDesconocidos(t *testing.T) {
	start(t)
	if code := attackOnce(t, "banco", "auth-banca", Attack{Kind: "otro", IP: "203.0.113.10"}); code != http.StatusBadRequest {
		t.Errorf("tipo desconocido debía dar 400, fue %d", code)
	}
}

func TestWAFMensajesDeCadaTipo(t *testing.T) {
	for kind := range wafRules {
		msg, ok := wafMessage(Attack{Kind: kind, IP: "203.0.113.9", Account: "x", Seq: 1, Accounts: 3, Records: 1000, Rate: 600}, "svc")
		if !ok || !strings.Contains(msg, "203.0.113.9") || !strings.Contains(msg, "regla WAF") {
			t.Errorf("%s: mensaje %q", kind, msg)
		}
	}
}
