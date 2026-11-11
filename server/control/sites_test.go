package control

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"demo-lab/server/faults"
	"demo-lab/server/sentinel/fake"
)

// F12.3: la portada del sitio consulta al BFF. Si está caído, la puerta
// responde el código real con la página de error de la marca; los estáticos
// siguen disponibles para que la página se vea y el Modo Director funcione.
func TestSitios_LaPuertaRespondeLaPaginaDeError(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	if err := api.Runtime.Start(); err != nil {
		t.Skipf("puertos 7101-7125 ocupados (¿hay un demo-lab encendido?): %v", err)
	}
	defer api.Runtime.Stop()
	h := api.Handler()

	pide := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	if rec := pide("/sitios/mercado/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>Mercado") {
		t.Errorf("sitio sano: %d", rec.Code)
	}
	if rec := pide("/sitios/mercado/index.html"); rec.Code != 200 && rec.Code != 301 {
		t.Errorf("index sano: %d", rec.Code)
	}

	// Responder 503 en el BFF: página de error con ese código.
	f, err := api.Runtime.Faults.Apply("mercado", "storefront-web", "down", faults.Params{Mode: "503"}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	rec := pide("/sitios/mercado/")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "Error 503") || !strings.Contains(rec.Body.String(), "Mercado Don Federico") {
		t.Errorf("con el BFF en 503: %d", rec.Code)
	}
	if rec := pide("/sitios/mercado/style.css"); rec.Code != 200 {
		t.Errorf("los estáticos deben seguir: %d", rec.Code)
	}
	api.Runtime.Faults.Remove(f.ID)

	// Cerrar el puerto: conexión rechazada, 502.
	f, err = api.Runtime.Faults.Apply("mercado", "storefront-web", "down", faults.Params{Mode: "refused"}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if rec := pide("/sitios/mercado/"); rec.Code != 502 || !strings.Contains(rec.Body.String(), "Error 502") {
		t.Errorf("con el BFF cerrado: %d", rec.Code)
	}
	api.Runtime.Faults.Remove(f.ID)

	// Tiempo agotado: 504. Se acorta el cliente de la puerta para no esperar 3 s.
	original := siteProbeClient
	siteProbeClient = &http.Client{Timeout: 20 * time.Millisecond}
	t.Cleanup(func() { siteProbeClient = original })
	f, err = api.Runtime.Faults.Apply("mercado", "storefront-web", "degraded", faults.Params{LatencyMs: 200}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if rec := pide("/sitios/mercado/"); rec.Code != 504 || !strings.Contains(rec.Body.String(), "Error 504") {
		t.Errorf("con el BFF lento: %d", rec.Code)
	}
	api.Runtime.Faults.Remove(f.ID)

	if rec := pide("/sitios/nada/"); rec.Code != 404 {
		t.Errorf("plataforma inexistente: %d", rec.Code)
	}
}

// F12.3: la ruta pública de acciones es la misma que usa la consola y no exige
// sesión de consola.
func TestSitios_AccionPublicaDevuelveElTraceID(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	if err := api.Runtime.Start(); err != nil {
		t.Skipf("puertos 7101-7125 ocupados (¿hay un demo-lab encendido?): %v", err)
	}
	defer api.Runtime.Stop()
	h := api.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/sitios/api/mercado/cart_add", strings.NewReader("")))
	if code := rec.Code; code != 200 {
		t.Fatalf("cart_add público: %d %s", code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"trace_id"`) || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("la respuesta debe traer ok y trace_id: %s", rec.Body.String())
	}
}
