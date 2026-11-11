package control

import (
	"strings"
	"testing"

	"demo-lab/server/sentinel/fake"
)

// F13.4: la verificación inicia sesión como el Cliente de cada plataforma y
// comprueba que el portal le devuelva su servicio. Una clave que no coincide se
// informa sin romper las demás comprobaciones.
func TestVerify_PortalIniciaSesionYVeSuServicio(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.PortalPassword = "Portal-2026!"
	api.Profile.SentinelWebURL = "http://localhost:5173"
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	_, out := call(t, h, "POST", "/control/v1/connection/verify")
	if c, ok := out["portal"].(map[string]any); !ok || c["ok"] != true {
		t.Fatalf("el portal debe verificar: %v", out["portal"])
	}

	_, st := call(t, h, "GET", "/control/v1/status")
	for _, p := range st["platforms"].([]any) {
		item := p.(map[string]any)
		if item["portal_email"] == "" || item["portal_url"] == "" {
			t.Errorf("status debe mostrar el correo y el enlace del portal: %v", item)
		}
	}

	api.Profile.PortalPassword = "otra-clave"
	_, out = call(t, h, "POST", "/control/v1/connection/verify")
	c := out["portal"].(map[string]any)
	if c["ok"] != false || !strings.Contains(c["detail"].(string), "no inicia sesión") {
		t.Errorf("con otra clave el portal debe informar la falla: %v", c)
	}
}

// Sin DEMO_PORTAL_PASSWORD la comprobación queda desactivada y no mancha el
// resto de la verificación.
func TestVerify_PortalDesactivadoNoFalla(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	_, out := call(t, nuevaAPI(t, fk).Handler(), "POST", "/control/v1/connection/verify")
	c := out["portal"].(map[string]any)
	if c["ok"] != true || !strings.Contains(c["detail"].(string), "desactivado") {
		t.Errorf("sin clave el portal debe quedar desactivado: %v", c)
	}
}
