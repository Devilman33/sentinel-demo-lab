package control

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"demo-lab/server/sentinel/fake"
)

// T3.1 y D-23: la ventana se escribe en los tres proyectos, el intervalo cambia
// solo al abrir y cerrar la ventana, y aprovisionar dos veces no repite escrituras.
func TestProvision_VentanaEIntervaloPorHorario(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.PollIntervalSec, api.Profile.IdlePollIntervalSec = 5, 30
	api.Profile.WindowStart, api.Profile.WindowEnd = "12:00", "22:00"
	reloj := time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC) // dentro de la ventana
	api.now = func() time.Time { return reloj }
	ctx := context.Background()

	if _, err := api.doProvision(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range fk.Projects {
		if p["monitoring_window_start"] != "12:00" || p["monitoring_window_end"] != "22:00" || p["monitoring_interval_seconds"] != float64(5) {
			t.Fatalf("proyecto sin la ventana o el intervalo de prueba: %v", p)
		}
	}
	puts := fk.ProjectPuts
	if _, err := api.doProvision(ctx); err != nil {
		t.Fatal(err)
	}
	if fk.ProjectPuts != puts {
		t.Errorf("aprovisionar otra vez reescribió %d proyectos", fk.ProjectPuts-puts)
	}
	if ok, err := api.inSync(ctx); err != nil || !ok {
		t.Fatalf("dentro de la ventana debe estar en sync: %v %v", ok, err)
	}

	reloj = time.Date(2026, 11, 20, 23, 0, 0, 0, time.UTC) // ya cerró
	if ok, _ := api.inSync(ctx); ok {
		t.Fatal("al cerrar la ventana inSync debe pedir el intervalo de reposo")
	}
	if _, err := api.doProvision(ctx); err != nil {
		t.Fatal(err)
	}
	for _, p := range fk.Projects {
		if p["monitoring_interval_seconds"] != float64(30) || p["monitoring_window_start"] != "12:00" {
			t.Errorf("fuera de la ventana: %v", p)
		}
	}
}

// T3.2 y D-22: el correo queda con alta y crítica; "off" lo apaga.
func TestProvision_FiltroDelCorreo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.EmailSeverities = []string{"critica", "alta"}
	if _, err := api.doProvision(context.Background()); err != nil {
		t.Fatal(err)
	}
	for id, ch := range fk.Channels {
		email := ch["email"].(map[string]any)
		sev, _ := email["severity_filter"].([]any)
		if email["active"] != true || len(sev) != 2 || sev[0] != "critica" || sev[1] != "alta" {
			t.Errorf("%s: correo %v", id, email)
		}
	}
	if len(fk.Channels) != 3 {
		t.Errorf("se esperaban los canales de 3 servicios, hay %d", len(fk.Channels))
	}

	api.Profile.EmailSeverities = []string{}
	if _, err := api.doProvision(context.Background()); err != nil {
		t.Fatal(err)
	}
	for id, ch := range fk.Channels {
		if email := ch["email"].(map[string]any); email["active"] != false {
			t.Errorf("%s: con off el correo debe quedar apagado: %v", id, email)
		}
	}
}

// T3.4: en aws la verificación del portal reutiliza un resultado bueno y, tras
// un fallo, se pausa en vez de seguir probando la clave del usuario Cliente.
func TestVerify_PortalEnAWSNoInsiste(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.Name = "aws"
	api.Profile.PortalPassword, api.Profile.PortalEmail = "Portal-2026!", "equipo@demo.cl"
	reloj := time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)
	api.now = func() time.Time { return reloj }
	h := api.Handler()
	if code, body := call(t, h, "POST", "/control/v1/provision"); code != 200 {
		t.Fatalf("provision: %d %v", code, body)
	}
	portal := func() map[string]any {
		_, out := call(t, h, "POST", "/control/v1/connection/verify")
		return out["portal"].(map[string]any)
	}
	if c := portal(); c["ok"] != true {
		t.Fatalf("el portal debe verificar: %v", c)
	}

	api.Profile.PortalPassword = "otra-clave-x" // si probara de nuevo, fallaría
	reloj = reloj.Add(2 * time.Minute)
	if c := portal(); c["ok"] != true || !strings.Contains(c["detail"].(string), "comprobado a las 15:00") {
		t.Errorf("dentro de %v debe reutilizar el resultado: %v", portalReuse, c)
	}
	reloj = reloj.Add(portalReuse)
	if c := portal(); c["ok"] != false || !strings.Contains(c["detail"].(string), "no inicia sesión") {
		t.Fatalf("pasado el plazo debe probar y fallar: %v", c)
	}
	api.Profile.PortalPassword = "Portal-2026!"
	reloj = reloj.Add(time.Minute)
	if c := portal(); c["ok"] != false || !strings.Contains(c["detail"].(string), "en pausa hasta") {
		t.Errorf("tras un fallo debe quedar en pausa aunque la clave ya sea buena: %v", c)
	}
	reloj = reloj.Add(portalPause)
	if c := portal(); c["ok"] != true {
		t.Errorf("pasada la pausa debe volver a verificar: %v", c)
	}
}

// A6 dentro del módulo completo: con las claves, el sondeo sigue abierto y la
// consola, la API de control y los sitios piden sesión.
func TestHandler_ConClavesSoloElSondeoQuedaAbierto(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.ConsoleKey, api.Profile.SitePassword = "clave-de-consola-123", "clave-de-sitios-456"
	api = New(api.Profile) // la guardia se arma con el perfil
	h := api.Handler()
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/healthz", 200},
		{"GET", "/robots.txt", 200},
		{"GET", "/", 303},
		{"GET", "/sitios/mercado/", 303},
		{"GET", "/control/v1/status", 401},
		{"POST", "/control/v1/faults", 401},
		{"POST", "/sitios/api/mercado/browse", 401},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, se esperaba %d", c.method, c.path, rec.Code, c.want)
		}
	}
	req := httptest.NewRequest("GET", "/control/v1/status", nil)
	req.Header.Set("Authorization", "Bearer clave-de-consola-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("con la clave de consola /control/v1/status = %d", rec.Code)
	}
}
