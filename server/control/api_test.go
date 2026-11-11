package control

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/config"
	"demo-lab/server/logs"
	"demo-lab/server/runtime"
	"demo-lab/server/sentinel"
	"demo-lab/server/sentinel/fake"
	"demo-lab/server/traffic"
)

func newAPI(t *testing.T, fk *fake.Sentinel) http.Handler {
	t.Helper()
	return New(config.Profile{
		Name: "local", SentinelURL: fk.URL, ServicesURL: "http://demo-lab", PollIntervalSec: 5,
		StateDir: t.TempDir(), Email: "demo@x.cl", Password: "clave",
	}).Handler()
}

func call(t *testing.T, h http.Handler, method, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader("")))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func totales() (servicios, aristas int) {
	for _, p := range catalog.All() {
		servicios += len(p.Services)
		aristas += len(p.Edges())
	}
	return
}

// TC-01
func TestProvision_EsIdempotenteYCreaElGrafoPorAPI(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	h := newAPI(t, fk)
	servicios, aristas := totales()

	for i := 1; i <= 2; i++ {
		if code, body := call(t, h, "POST", "/control/v1/provision"); code != 200 {
			t.Fatalf("provision #%d: %d %v", i, code, body)
		}
		if len(fk.Folders) != 3 || len(fk.Projects) != 3 || len(fk.Components) != servicios || len(fk.Deps) != aristas {
			t.Fatalf("tras #%d: carpetas=%d proyectos=%d componentes=%d/%d dependencias=%d/%d", i,
				len(fk.Folders), len(fk.Projects), len(fk.Components), servicios, len(fk.Deps), aristas)
		}
		// El token se emite una sola vez por proyecto: reemitirlo revocaría el guardado.
		if fk.TokensIssued != 3 || len(fk.Tokens) != 3 {
			t.Fatalf("tras #%d: tokens emitidos=%d vigentes=%d, quiero 3 y 3", i, fk.TokensIssued, len(fk.Tokens))
		}
	}
	for _, c := range fk.Components {
		if c["name"] == "pasarela-pagos" && c["criticality"] != "critica" {
			t.Errorf("pasarela-pagos quedó con criticidad %q", c["criticality"])
		}
	}
	for _, p := range fk.Projects {
		if !strings.Contains(p["monitoring_url"].(string), "/salud/") || p["folder_id"] == nil {
			t.Errorf("proyecto mal aprovisionado: %v", p)
		}
	}
	_, st := call(t, h, "GET", "/control/v1/status")
	for _, p := range st["platforms"].([]any) {
		if p.(map[string]any)["provisioned"] != true {
			t.Errorf("status no ve aprovisionada: %v", p)
		}
	}
}

// TC-02
func TestVerify_InformaCadaInterfazPorSeparado(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	h := newAPI(t, fk)

	_, antes := call(t, h, "POST", "/control/v1/connection/verify")
	if antes["login"].(map[string]any)["ok"] != true || antes["ingest"].(map[string]any)["ok"] != false {
		t.Errorf("sin aprovisionar: login ok e ingesta no lista; obtuve %v", antes)
	}
	call(t, h, "POST", "/control/v1/provision")
	_, despues := call(t, h, "POST", "/control/v1/connection/verify")
	for _, k := range []string{"reachable", "login", "projects", "ingest"} {
		if despues[k].(map[string]any)["ok"] != true {
			t.Errorf("%s debería estar ok: %v", k, despues[k])
		}
	}
}

func TestVerify_ClaveIncorrectaNoIntentaLoDemas(t *testing.T) {
	fk := fake.New("demo@x.cl", "otra")
	defer fk.Close()
	_, out := call(t, newAPI(t, fk), "POST", "/control/v1/connection/verify")
	if out["login"].(map[string]any)["ok"] != false {
		t.Errorf("login debería fallar: %v", out)
	}
	if _, hay := out["projects"]; hay {
		t.Errorf("sin sesión no se prueban los proyectos")
	}
}

func nuevaAPI(t *testing.T, fk *fake.Sentinel) *API {
	t.Helper()
	api := New(config.Profile{Name: "local", SentinelURL: fk.URL, ServicesURL: "http://x", StateDir: t.TempDir(), Email: "demo@x.cl", Password: "clave"})
	l := logs.New("")
	api.Runtime, api.Logger, api.Journal = runtime.New("127.0.0.1", l), l, NewJournal("")
	return api
}

// simularSondeosDeServicios imita al Poller: un sondeo por proyecto y uno por
// componente, ya con la URL de salud que dejó el aprovisionamiento.
func simularSondeosDeServicios(fk *fake.Sentinel, api *API) {
	for _, pl := range catalog.All() {
		ps := api.platformState(pl.Slug)
		fk.Probes = append(fk.Probes, map[string]any{"project_id": ps.ProjectID, "status_code": 200})
		for _, s := range pl.Services {
			id := ps.Components[s.Slug]
			fk.Probes = append(fk.Probes, map[string]any{"project_id": ps.ProjectID, "component_id": id, "status_code": 200})
			if c := fk.Components[id]; c != nil {
				c["status"] = "ok"
			}
		}
	}
}

// TC-02: verify prueba el sondeo y los canales, no solo la sesión y la ingesta.
func TestVerify_InformaSondeoYCanales(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	estado := func(k string) map[string]any {
		_, out := call(t, h, "POST", "/control/v1/connection/verify")
		return out[k].(map[string]any)
	}

	if c := estado("probe"); c["ok"] != false {
		t.Errorf("sin aprovisionar el sondeo no puede estar ok: %v", c)
	}
	call(t, h, "POST", "/control/v1/provision")
	if c := estado("probe"); c["ok"] != false || !strings.Contains(c["detail"].(string), "banco") {
		t.Errorf("sin sondeos debe nombrar las plataformas: %v", c)
	}
	if c := estado("nodes"); c["ok"] != false || !strings.Contains(c["detail"].(string), "mercado/storefront-web") {
		t.Errorf("sin sondeos los nodos deben seguir en unknown: %v", c)
	}
	if c := estado("notifications"); c["ok"] != false {
		t.Errorf("solo con correo no está listo para el stand: %v", c)
	}

	// Un 503 también prueba que el sondeo llegó; 0 es que nunca respondió.
	ids := api.projectIDs()
	fk.Probes = append(fk.Probes,
		map[string]any{"project_id": ids["mercado"], "status_code": 503},
		map[string]any{"project_id": ids["banco"], "status_code": 200},
		map[string]any{"project_id": ids["clinica"], "status_code": 0})
	if c := estado("probe"); c["ok"] != false || !strings.Contains(c["detail"].(string), "clinica") {
		t.Errorf("solo clinica no fue alcanzada: %v", c)
	}
	// Con los tres sitios alcanzados todavía falta el sondeo propio de cada
	// servicio: el mapa seguiría en gris.
	fk.Probes[2]["status_code"] = 200
	if c := estado("probe"); c["ok"] != false || !strings.Contains(c["detail"].(string), "mercado/storefront-web") {
		t.Errorf("falta el sondeo por servicio: %v", c)
	}
	simularSondeosDeServicios(fk, api)
	if c := estado("probe"); c["ok"] != true {
		t.Errorf("con sondeos de sitio y de servicio debe estar ok: %v", c)
	}
	if c := estado("nodes"); c["ok"] != true {
		t.Errorf("con status ok en los componentes no debe quedar ningún nodo en gris: %v", c)
	}
}

// El GET entrega el webhook enmascarado: reenviarlo haría que Sentinel lo rechace.
func TestProvision_ActivaLosWebhooksSinReenviarLaMascara(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.DiscordWebhook = "https://discord.com/api/webhooks/1/abc"
	h := api.Handler()

	if code, body := call(t, h, "POST", "/control/v1/provision"); code != 200 {
		t.Fatalf("provision: %d %v", code, body)
	}
	id := api.projectIDs()["mercado"]
	fk.Channels[id]["discord"].(map[string]any)["webhook"] = "https://discord.com/…/abc"
	fk.Channels[id]["slack"].(map[string]any)["webhook"] = "https://hooks.slack.com/…/xyz"

	if code, body := call(t, h, "POST", "/control/v1/provision"); code != 200 {
		t.Fatalf("segunda provision: %d %v", code, body)
	}
	if ch := fk.Channels[id]["discord"].(map[string]any); ch["active"] != true || ch["webhook"] != api.Profile.DiscordWebhook {
		t.Errorf("discord quedó %v", ch)
	}
	if ch := fk.Channels[id]["slack"].(map[string]any); ch["webhook"] != "" {
		t.Errorf("slack debe viajar vacío para conservar el guardado: %v", ch)
	}
	_, out := call(t, h, "POST", "/control/v1/connection/verify")
	if c := out["notifications"].(map[string]any); c["ok"] != true {
		t.Errorf("con discord activo debe estar ok: %v", c)
	}
}

func TestProvision_SinWebhooksNoTocaLosCanales(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	h := nuevaAPI(t, fk).Handler()
	if code, body := call(t, h, "POST", "/control/v1/provision"); code != 200 {
		t.Fatalf("provision: %d %v", code, body)
	}
	if len(fk.Channels) != 0 {
		t.Errorf("no debió guardar canales: %v", fk.Channels)
	}
}

// TC-15
func TestReset_CierraLosIncidentesAbiertosDeLosProyectosDemo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	ids := api.projectIDs()
	alerta := func(id, project, status, category string) map[string]any {
		a := map[string]any{"id": id, "project_id": project, "status": status, "category": category}
		fk.Alerts = append(fk.Alerts, a)
		return a
	}
	sinClasificar := alerta("a1", ids["mercado"], "activo", "Sin Clasificar")
	seguridad := alerta("a2", ids["banco"], "en_progreso", "Seguridad")
	ajena := alerta("a3", "otro-proyecto", "activo", "Operativo")
	alerta("a4", ids["clinica"], "resuelto", "Operativo")

	// Un proceso recién arrancado no tiene sesión: el reinicio debe abrirla.
	api.client = sentinel.NewClient(fk.URL)
	code, out := call(t, h, "POST", "/control/v1/reset")
	if code != 200 || out["incidents"] != float64(2) || out["incidents_error"] != nil {
		t.Fatalf("reset: %d %v", code, out)
	}
	if sinClasificar["status"] != "resuelto" || sinClasificar["category"] != "Operativo" {
		t.Errorf("Sin Clasificar debe cerrarse como Operativo: %v", sinClasificar)
	}
	if seguridad["status"] != "resuelto" || seguridad["category"] != "Seguridad" {
		t.Errorf("no debe perder la categoría Seguridad: %v", seguridad)
	}
	if ajena["status"] != "activo" {
		t.Errorf("no debe tocar incidentes de otros proyectos: %v", ajena)
	}
}

// Las señales que seguían en la cola de Sentinel abren incidentes después del cierre.
func TestReset_SigueCerrandoLoQueLlegaTarde(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.settleEvery, api.settleFor = 5*time.Millisecond, 3*time.Second
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	call(t, h, "POST", "/control/v1/reset")
	fk.AddAlert("tarde", api.projectIDs()["mercado"])
	for end := time.Now().Add(2 * time.Second); fk.AlertStatus("tarde") != "resuelto"; {
		if time.Now().After(end) {
			t.Fatal("el incidente tardío quedó abierto")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// D-C1: entre visitantes el reinicio deja el tráfico base encendido, no en 0.
// Los valores son diminutos a propósito: el ticker del generador tarda cientos
// de segundos en disparar y la prueba no interfiere con los puertos de runtime.
func TestReset_VuelveAlTraficoBase(t *testing.T) {
	const base = 0.001
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.TrafficRPS = base
	api.Traffic = traffic.New()
	defer api.Traffic.StopAll()
	for _, slug := range []string{"mercado", "banco", "clinica"} {
		if err := api.Traffic.Set(slug, 0.002); err != nil {
			t.Fatal(err)
		}
	}

	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")
	if code, out := call(t, h, "POST", "/control/v1/reset"); code != 200 {
		t.Fatalf("reset: %d %v", code, out)
	}
	for _, slug := range []string{"mercado", "banco", "clinica"} {
		if got := api.Traffic.RPS(slug); got != base {
			t.Errorf("%s quedó en %g usuarios/s, quiero el tráfico base %g", slug, got, base)
		}
	}
}

// El tráfico base no debe cortar el cierre de lo que llega tarde desde la cola:
// solo una falla o un ataque nuevos marcan el inicio de otro ensayo.
func TestReset_ConTraficoBaseSigueCerrandoTarde(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.TrafficRPS = 0.001
	api.Traffic = traffic.New()
	defer api.Traffic.StopAll()
	api.settleEvery, api.settleFor = 5*time.Millisecond, 3*time.Second

	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")
	call(t, h, "POST", "/control/v1/reset")
	fk.AddAlert("tarde-con-trafico", api.projectIDs()["mercado"])
	for end := time.Now().Add(2 * time.Second); fk.AlertStatus("tarde-con-trafico") != "resuelto"; {
		if time.Now().After(end) {
			t.Fatal("el incidente tardío quedó abierto con el tráfico base encendido")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestReset_NoCierraLosIncidentesDeUnEnsayoNuevo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.settleEvery, api.settleFor = 20*time.Millisecond, 3*time.Second
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	call(t, h, "POST", "/control/v1/reset")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/control/v1/faults", strings.NewReader(`{"platform":"mercado","service":"pasarela-pagos","type":"degraded","params":{"latency_ms":950}}`)))
	if rec.Code != 201 {
		t.Fatalf("falla: %d", rec.Code)
	}
	fk.AddAlert("nuevo", api.projectIDs()["mercado"])
	time.Sleep(100 * time.Millisecond)
	if got := fk.AlertStatus("nuevo"); got != "activo" {
		t.Errorf("cerró un incidente del ensayo nuevo: %v", got)
	}
}

func TestReset_ReiniciaLoLocalAunqueSentinelNoResponda(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/control/v1/faults", strings.NewReader(`{"platform":"mercado","service":"pasarela-pagos","type":"degraded","params":{"latency_ms":950}}`)))
	if rec.Code != 201 {
		t.Fatalf("falla: %d", rec.Code)
	}
	fk.Close()

	code, out := call(t, h, "POST", "/control/v1/reset")
	if code != 200 || out["faults"] != float64(1) || out["incidents_error"] == nil {
		t.Errorf("reset: %d %v", code, out)
	}
}

// TC-07 (lado del módulo): el estado por servicio refleja las fallas y la bitácora las anota.
func TestPlataformas_ReflejanFallasYQuedanEnLaBitacora(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	post := func(path, body string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return rec.Code
	}
	if c := post("/control/v1/faults", `{"platform":"mercado","service":"pasarela-pagos","type":"degraded","params":{"latency_ms":950,"jitter_ms":50}}`); c != 201 {
		t.Fatalf("falla: %d", c)
	}
	if c := post("/control/v1/faults", `{"platform":"mercado","service":"nope","type":"down","params":{"mode":"503"}}`); c != 400 {
		t.Errorf("servicio inexistente: %d, quiero 400", c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/control/v1/platforms", nil))
	var out []struct {
		Platform string
		State    string
		Services []struct{ Slug, State string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out) != 3 {
		t.Fatalf("platforms: %v %s", err, rec.Body.String())
	}
	if out[0].Platform != "mercado" || out[0].State != "degradado" {
		t.Errorf("mercado debería verse degradado: %+v", out[0])
	}
	for _, s := range out[0].Services {
		if (s.Slug == "pasarela-pagos") != (s.State == "degradado") {
			t.Errorf("%s: estado %s", s.Slug, s.State)
		}
	}
	if out[1].State != "ok" {
		t.Errorf("banco no tiene fallas")
	}
	fallas := 0
	for _, e := range api.Journal.All() {
		if e.Kind == "falla" {
			fallas++
		}
	}
	if fallas != 1 {
		t.Errorf("la bitácora debe tener la falla válida (y no la rechazada): %d fallas", fallas)
	}
	csv := httptest.NewRecorder()
	h.ServeHTTP(csv, httptest.NewRequest("GET", "/control/v1/journal?format=csv", nil))
	if !strings.Contains(csv.Body.String(), "hora_utc") || !strings.Contains(csv.Body.String(), "degraded en pasarela-pagos") {
		t.Errorf("csv: %s", csv.Body.String())
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Consola Demo Day") {
		t.Errorf("la consola no se sirve: %d", page.Code)
	}
}

// TC-17: fuera del perfil local el historial se rechaza con 409 sin abrir la base.
func TestHistorial_SeRechazaFueraDelPerfilLocal(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	for name, p := range map[string]config.Profile{
		"aws":                   {Name: "aws", SentinelURL: fk.URL, ServicesURL: "http://x", DBDSN: "postgres://a:b@localhost/x"},
		"local sin permiso":     {Name: "local", SentinelURL: fk.URL, ServicesURL: "http://x", DBDSN: "postgres://a:b@localhost/x"},
		"local con base remota": {Name: "local", AllowDBBackfill: true, SentinelURL: fk.URL, ServicesURL: "http://x", DBDSN: "postgres://a:b@db.example.com/x"},
	} {
		p.StateDir = t.TempDir()
		h := New(p).Handler()
		for _, method := range []string{"POST", "DELETE"} {
			if code, body := call(t, h, method, "/control/v1/history"); code != 409 {
				t.Errorf("%s %s: esperaba 409, fue %d %v", name, method, code, body)
			}
		}
	}
}

// La consola ofrece un botón por plataforma para abrir su sitio, y el de todos.
func TestConsola_TieneAccesoAlSitioDeCadaPlataforma(t *testing.T) {
	rec := httptest.NewRecorder()
	New(config.Profile{Name: "local", SentinelURL: "http://x", ServicesURL: "http://y", StateDir: t.TempDir()}).Handler().
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("consola: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	if strings.Contains(body, sitesMarker) {
		t.Error("quedó el marcador sin reemplazar")
	}
	for _, p := range catalog.All() {
		if !strings.Contains(body, `href="/sitios/`+p.Slug+`/" target="_blank"`) || !strings.Contains(body, p.Name+" ↗") {
			t.Errorf("falta el botón del sitio de %s", p.Slug)
		}
	}
	if !strings.Contains(body, `href="/sitios/" target="_blank"`) {
		t.Error("falta el acceso a todos los sitios")
	}
	// Cada ruta que enlaza la consola debe existir.
	for _, ruta := range []string{"/sitios/", "/sitios/mercado/", "/sitios/banco/", "/sitios/clinica/"} {
		r := httptest.NewRecorder()
		New(config.Profile{Name: "local", SentinelURL: "http://x", ServicesURL: "http://y", StateDir: t.TempDir()}).Handler().
			ServeHTTP(r, httptest.NewRequest("GET", ruta, nil))
		if r.Code != 200 {
			t.Errorf("%s responde %d", ruta, r.Code)
		}
	}
}

// TC-02: sin ingesta que responda no se puede provocar nada, porque el incidente no se abriría.
func TestFallas_NoSePuedenDispararSinIngesta(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	falla := func() (int, map[string]any) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("POST", "/control/v1/faults", strings.NewReader(`{"platform":"mercado","service":"pasarela-pagos","type":"degraded","params":{"latency_ms":950}}`)))
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	if code, out := falla(); code != 409 || !strings.Contains(out["error"].(string), "Preparar escenario") {
		t.Errorf("sin aprovisionar: %d %v", code, out)
	}
	call(t, h, "POST", "/control/v1/provision")

	fk.Down.Store(true)
	if code, out := falla(); code != 503 || !strings.Contains(out["error"].(string), "ingesta") {
		t.Errorf("Sentinel caído: %d %v", code, out)
	}
	fk.Down.Store(false)

	fk.RevokeTokens()
	if code, out := falla(); code != 503 || !strings.Contains(out["error"].(string), "rechazó el token") {
		t.Errorf("token revocado: %d %v", code, out)
	}
	if got := len(api.Runtime.Faults.List("")); got != 0 {
		t.Errorf("se aplicaron %d fallas pese al bloqueo", got)
	}

	fk.RestoreTokens()
	if code, out := falla(); code != 201 {
		t.Errorf("con la ingesta sana: %d %v", code, out)
	}
}
