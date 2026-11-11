package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// limpiar deja sin definir todas las variables DEMO_* para que la prueba no
// dependa de la máquina.
func limpiar(t *testing.T) {
	t.Helper()
	for _, k := range []string{"DEMO_PROFILE", "DEMO_SENTINEL_URL", "DEMO_PUBLIC_URL", "DEMO_POLL_INTERVAL",
		"DEMO_ALLOW_DB_BACKFILL", "DEMO_TRAFFIC_RPS", "DEMO_SENTINEL_EMAIL", "DEMO_SENTINEL_PASSWORD",
		"DEMO_DB_DSN", "DEMO_DISCORD_WEBHOOK", "DEMO_SLACK_WEBHOOK", "DEMO_PORTAL_PASSWORD", "DEMO_PORTAL_EMAIL",
		"DEMO_SENTINEL_WEB_URL", "DEMO_CONSOLE_KEY", "DEMO_SITE_PASSWORD", "DEMO_MONITORING_WINDOW",
		"DEMO_POLL_INTERVAL_IDLE", "DEMO_EMAIL_MIN_SEVERITY"} {
		t.Setenv(k, "")
	}
}

func TestFromEnv_ArmaElPerfilSoloConVariables(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://api-go:8080/")
	t.Setenv("DEMO_PUBLIC_URL", "http://demo-lab:7000")
	t.Setenv("DEMO_SENTINEL_EMAIL", "admin@demo.cl")
	t.Setenv("DEMO_SENTINEL_PASSWORD", "clave")
	t.Setenv("DEMO_TRAFFIC_RPS", "2.5")
	t.Setenv("DEMO_DISCORD_WEBHOOK", "  https://discord.com/api/webhooks/1/x  ")

	p, err := FromEnv("/data")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "local" || p.SentinelURL != "http://api-go:8080" || p.ServicesURL != "http://demo-lab:7000" {
		t.Errorf("perfil mal armado: %+v", p)
	}
	if p.StateDir != filepath.Join("/data", "state") || p.PollIntervalSec != 5 || p.TrafficRPS != 2.5 {
		t.Errorf("valores por omisión: %+v", p)
	}
	if p.Email != "admin@demo.cl" || p.DiscordWebhook != "https://discord.com/api/webhooks/1/x" || p.AllowDBBackfill {
		t.Errorf("secretos u opciones: %+v", p)
	}
}

func TestFromEnv_RechazaLoQueFaltaOEstaMal(t *testing.T) {
	casos := map[string]map[string]string{
		"sin url de Sentinel":   {"DEMO_PUBLIC_URL": "http://x:7000"},
		"sin url pública":       {"DEMO_SENTINEL_URL": "http://x:8080"},
		"perfil desconocido":    {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "prod"},
		"intervalo no numérico": {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_POLL_INTERVAL": "rápido"},
		"tráfico negativo":      {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_TRAFFIC_RPS": "-1"},
		"historial en aws":      {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws", "DEMO_ALLOW_DB_BACKFILL": "true"},
		"clave de portal corta": {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PORTAL_PASSWORD": "corta"},
		"portal en aws sin casilla": {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws",
			"DEMO_PORTAL_PASSWORD": "Portal-2026!", "DEMO_CONSOLE_KEY": consola, "DEMO_SITE_PASSWORD": sitios},
		"portal en aws con casilla inválida": {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws",
			"DEMO_PORTAL_PASSWORD": "Portal-2026!", "DEMO_PORTAL_EMAIL": "equipo", "DEMO_CONSOLE_KEY": consola, "DEMO_SITE_PASSWORD": sitios},
		// A6: en aws el módulo queda en internet y no arranca sin las dos claves.
		"aws sin claves":               {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws"},
		"aws sin clave de sitios":      {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws", "DEMO_CONSOLE_KEY": consola},
		"aws sin clave de consola":     {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_PROFILE": "aws", "DEMO_SITE_PASSWORD": sitios},
		"clave de sitios sin consola":  {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_SITE_PASSWORD": sitios},
		"clave de consola corta":       {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_CONSOLE_KEY": "corta"},
		"las dos claves iguales":       {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_CONSOLE_KEY": consola, "DEMO_SITE_PASSWORD": consola},
		"ventana sin guion":            {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_MONITORING_WINDOW": "11:30"},
		"ventana con hora inválida":    {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_MONITORING_WINDOW": "11:30-25:00"},
		"ventana vacía":                {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_MONITORING_WINDOW": "10:00-10:00"},
		"severidad de correo inválida": {"DEMO_SENTINEL_URL": "http://x", "DEMO_PUBLIC_URL": "http://y", "DEMO_EMAIL_MIN_SEVERITY": "urgente"},
	}
	for nombre, vars := range casos {
		t.Run(nombre, func(t *testing.T) {
			limpiar(t)
			for k, v := range vars {
				t.Setenv(k, v)
			}
			if _, err := FromEnv("/data"); err == nil {
				t.Error("debía fallar")
			}
		})
	}
}

// F13: en aws el portal usa la casilla real; en local se ignora y el contacto
// conserva su correo de example.com.
func TestFromEnv_PortalSoloUsaLaCasillaEnAWS(t *testing.T) {
	for perfil, quiero := range map[string]string{"aws": "equipo@demo.cl", "local": ""} {
		limpiar(t)
		t.Setenv("DEMO_SENTINEL_URL", "http://x")
		t.Setenv("DEMO_PUBLIC_URL", "http://y")
		t.Setenv("DEMO_PROFILE", perfil)
		t.Setenv("DEMO_PORTAL_PASSWORD", "Portal-2026!")
		t.Setenv("DEMO_PORTAL_EMAIL", " equipo@demo.cl ")
		t.Setenv("DEMO_CONSOLE_KEY", consola)
		t.Setenv("DEMO_SITE_PASSWORD", sitios)
		p, err := FromEnv("/data")
		if err != nil {
			t.Fatalf("%s: %v", perfil, err)
		}
		if p.PortalEmail != quiero {
			t.Errorf("%s: casilla %q, quiero %q", perfil, p.PortalEmail, quiero)
		}
	}
}

const (
	consola = "clave-de-consola-123"
	sitios  = "clave-de-sitios-456"
)

// A6: en aws arranca con las dos claves; en local siguen siendo opcionales.
func TestFromEnv_ClavesDeAcceso(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://x")
	t.Setenv("DEMO_PUBLIC_URL", "http://y")
	p, err := FromEnv("/data")
	if err != nil || p.ConsoleKey != "" {
		t.Fatalf("local sin claves debe arrancar abierto: %+v %v", p, err)
	}
	t.Setenv("DEMO_PROFILE", "aws")
	t.Setenv("DEMO_CONSOLE_KEY", " "+consola+" ")
	t.Setenv("DEMO_SITE_PASSWORD", sitios)
	if p, err = FromEnv("/data"); err != nil || p.ConsoleKey != consola || p.SitePassword != sitios {
		t.Errorf("aws con claves: %+v %v", p, err)
	}
}

// T3.1 y D-23: la ventana usa la regla de Sentinel, [inicio, fin) en UTC, y
// fuera de ella rige el intervalo de reposo.
func TestFromEnv_VentanaEIntervaloDeReposo(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://x")
	t.Setenv("DEMO_PUBLIC_URL", "http://y")
	t.Setenv("DEMO_MONITORING_WINDOW", " 9:30 - 22:00 ")
	t.Setenv("DEMO_POLL_INTERVAL_IDLE", "30")
	p, err := FromEnv("/data")
	if err != nil {
		t.Fatal(err)
	}
	if p.WindowStart != "09:30" || p.WindowEnd != "22:00" {
		t.Errorf("ventana %q-%q, se esperaba 09:30-22:00", p.WindowStart, p.WindowEnd)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 20, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		t    time.Time
		in   bool
		secs int
	}{{at(9, 29), false, 30}, {at(9, 30), true, 5}, {at(21, 59), true, 5}, {at(22, 0), false, 30}} {
		if p.InWindow(c.t) != c.in || p.IntervalAt(c.t) != c.secs {
			t.Errorf("%s: en ventana %v (quiero %v), intervalo %d (quiero %d)", c.t.Format("15:04"), p.InWindow(c.t), c.in, p.IntervalAt(c.t), c.secs)
		}
	}
	// Cruza la medianoche, como admite Sentinel.
	t.Setenv("DEMO_MONITORING_WINDOW", "22:00-02:00")
	if p, err = FromEnv("/data"); err != nil || !p.InWindow(at(23, 0)) || !p.InWindow(at(1, 59)) || p.InWindow(at(2, 0)) {
		t.Errorf("ventana nocturna mal evaluada: %+v %v", p, err)
	}
	// Sin ventana siempre rige el intervalo normal.
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://x")
	t.Setenv("DEMO_PUBLIC_URL", "http://y")
	t.Setenv("DEMO_POLL_INTERVAL_IDLE", "30")
	if p, _ = FromEnv("/data"); p.IntervalAt(at(3, 0)) != 5 {
		t.Errorf("sin ventana el intervalo es %d, quiero 5", p.IntervalAt(at(3, 0)))
	}
}

// T3.2 y D-22: la severidad mínima se traduce a la lista que guarda Sentinel.
func TestFromEnv_SeveridadMinimaDelCorreo(t *testing.T) {
	for v, quiero := range map[string][]string{
		"":        nil,
		"alta":    {"critica", "alta"},
		"Crítica": {"critica"},
		"baja":    {"critica", "alta", "media", "baja"},
		"off":     {},
	} {
		limpiar(t)
		t.Setenv("DEMO_SENTINEL_URL", "http://x")
		t.Setenv("DEMO_PUBLIC_URL", "http://y")
		t.Setenv("DEMO_EMAIL_MIN_SEVERITY", v)
		p, err := FromEnv("/data")
		if err != nil {
			t.Fatalf("%q: %v", v, err)
		}
		if (p.EmailSeverities == nil) != (quiero == nil) || strings.Join(p.EmailSeverities, ",") != strings.Join(quiero, ",") {
			t.Errorf("%q → %#v, quiero %#v", v, p.EmailSeverities, quiero)
		}
	}
}

func TestFromEnv_ElIntervaloNuncaBajaDelMinimoDeSentinel(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://x")
	t.Setenv("DEMO_PUBLIC_URL", "http://y")
	t.Setenv("DEMO_POLL_INTERVAL", "1")
	p, err := FromEnv("/data")
	if err != nil || p.PollIntervalSec != 5 {
		t.Errorf("intervalo %d, error %v", p.PollIntervalSec, err)
	}
}

func TestResolve_ElArchivoPedidoGanaALasVariables(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://desde-env")
	t.Setenv("DEMO_PUBLIC_URL", "http://desde-env")
	archivo := filepath.Join(t.TempDir(), "p.json")
	if err := os.WriteFile(archivo, []byte(`{"name":"local","sentinel_url":"http://desde-archivo","services_url":"http://pub:7000"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Resolve(archivo, "/data")
	if err != nil || p.SentinelURL != "http://desde-archivo" {
		t.Errorf("%+v %v", p, err)
	}
}

func TestResolve_UsaLasVariablesSinArchivo(t *testing.T) {
	limpiar(t)
	t.Setenv("DEMO_SENTINEL_URL", "http://desde-env")
	t.Setenv("DEMO_PUBLIC_URL", "http://pub:7000")
	p, err := Resolve("", "/data")
	if err != nil || p.SentinelURL != "http://desde-env" {
		t.Errorf("%+v %v", p, err)
	}
}

func TestResolve_SinNadaExplicaQueHacer(t *testing.T) {
	limpiar(t)
	t.Chdir(t.TempDir())
	_, err := Resolve("", "/data")
	if err == nil || !strings.Contains(err.Error(), "DEMO_SENTINEL_URL") {
		t.Errorf("el error debe nombrar la variable que falta: %v", err)
	}
}
