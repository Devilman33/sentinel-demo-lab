package runtime

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/faults"
	"demo-lab/server/logs"
)

// Los servicios usan los puertos fijos del catálogo, así que estas pruebas no
// pueden correr a la vez que un demo-lab real ni en paralelo entre sí.
func start(t *testing.T) (*Runtime, *logs.Logger) {
	t.Helper()
	l := logs.New("")
	rt := New("127.0.0.1", l)
	if err := rt.Start(); err != nil {
		t.Fatalf("no se pudo iniciar el runtime (¿hay un demo-lab corriendo?): %v", err)
	}
	t.Cleanup(rt.Stop)
	return rt, l
}

func action(t *testing.T, platform, name string) (int, map[string]any) {
	t.Helper()
	p, _ := catalog.BySlug(platform)
	res, err := http.Post(BaseURL(p.EntryService())+"/v1/action/"+name, "application/json", nil)
	if err != nil {
		t.Fatalf("%s/%s: %v", platform, name, err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

func deep(t *testing.T, platform string) int {
	t.Helper()
	p, _ := catalog.BySlug(platform)
	res, err := http.Get(BaseURL(p.EntryService()) + "/health/deep")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func down(t *testing.T, rt *Runtime, platform, service, mode string) {
	t.Helper()
	if _, err := rt.Faults.Apply(platform, service, faults.TypeDown, faults.Params{Mode: mode}, 0, "test"); err != nil {
		t.Fatal(err)
	}
}

// TC-03 y TC-06
func TestOperacionSanaEncadenaLaTrazaPorTodosLosServicios(t *testing.T) {
	rt, l := start(t)
	code, body := action(t, "mercado", "checkout")
	if code != 200 || body["ok"] != true || body["receipt"] == "" {
		t.Fatalf("checkout sano: %d %v", code, body)
	}
	trace := body["trace_id"].(string)

	porServicio := map[string]logs.Record{}
	for _, r := range l.Recent(logs.Filter{}) {
		if r.TraceID == trace {
			porServicio[r.Service] = r
		}
	}
	for _, svc := range []string{"storefront-web", "pasarela-pagos", "servicio-inventario", "db-transaccional"} {
		r, ok := porServicio[svc]
		if !ok {
			t.Errorf("%s no escribió log con la traza %s", svc, trace)
			continue
		}
		if r.Level == "" || r.Service == "" || r.Time.IsZero() {
			t.Errorf("%s: faltan severity, service o timestamp: %+v", svc, r)
		}
	}
	if porServicio["db-transaccional"].Format != "xml" {
		t.Errorf("db-transaccional debe escribir XML (legacy)")
	}
	// Una acción que no toca la cadena de pagos no pasa por ella.
	_, b2 := action(t, "mercado", "browse")
	for _, r := range l.Recent(logs.Filter{}) {
		if r.TraceID == b2["trace_id"] && r.Service == "pasarela-pagos" {
			t.Errorf("browse no debería llegar a pasarela-pagos")
		}
	}
	_ = rt
}

// TC-07, TC-08 y TC-14
func TestCaidaDeLaBasePropagaErroresYSeRecupera(t *testing.T) {
	rt, l := start(t)
	if deep(t, "mercado") != 200 {
		t.Fatal("la plataforma debería partir sana")
	}
	down(t, rt, "mercado", "db-transaccional", "refused")

	code, body := action(t, "mercado", "checkout")
	if code != 503 || body["component"] != "db-transaccional" {
		t.Fatalf("checkout con la BD caída: %d %v", code, body)
	}
	if deep(t, "mercado") != 503 {
		t.Errorf("/health/deep del BFF debería ser 503")
	}
	// Los errores de toda la cadena nombran la causa raíz.
	trace := body["trace_id"].(string)
	conError := map[string]bool{}
	for _, r := range l.Recent(logs.Filter{MinLevel: logs.Error}) {
		if r.TraceID == trace {
			conError[r.Service] = true
			if !strings.Contains(r.Message, "db-transaccional") {
				t.Errorf("%s: el error no nombra la causa raíz: %q", r.Service, r.Message)
			}
		}
	}
	for _, svc := range []string{"storefront-web", "pasarela-pagos", "servicio-inventario"} {
		if !conError[svc] {
			t.Errorf("%s no registró el error aguas arriba", svc)
		}
	}
	// El usuario recibe el error en el BFF: queda como crítico.
	for _, r := range l.Recent(logs.Filter{MinLevel: logs.Critical}) {
		if r.TraceID == trace && r.Service != "storefront-web" {
			t.Errorf("solo el BFF debería registrar el error crítico, no %s", r.Service)
		}
	}

	if n := rt.Faults.RestoreAll("mercado"); n != 1 {
		t.Fatalf("RestoreAll retiró %d fallas", n)
	}
	if code, _ := action(t, "mercado", "checkout"); code != 200 {
		t.Errorf("checkout tras restaurar: %d", code)
	}
	if deep(t, "mercado") != 200 {
		t.Errorf("/health/deep tras restaurar debería ser 200")
	}
}

func TestCaidaEn503ResponeSinCerrarElPuerto(t *testing.T) {
	rt, _ := start(t)
	down(t, rt, "banco", "motor-antifraude", "503")
	if code, body := action(t, "banco", "transfer"); code != 503 || body["component"] != "motor-antifraude" {
		t.Errorf("transfer con antifraude en 503: %d %v", code, body)
	}
	// Consultar el saldo no pasa por el antifraude.
	if code, _ := action(t, "banco", "balance"); code != 200 {
		t.Errorf("balance debería seguir funcionando: %d", code)
	}
}

// F12.1: un salto que se agota responde 504, no 503, para que el sitio muestre
// «tardó demasiado». Las caídas siguen siendo 503.
func TestTiempoAgotadoResponde504(t *testing.T) {
	rt, _ := start(t)
	if _, err := rt.Faults.Apply("mercado", "servicio-inventario", faults.TypeDegraded, faults.Params{LatencyMs: 3500}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	inicio := time.Now()
	code, body := action(t, "mercado", "checkout")
	espera := time.Since(inicio)
	if code != 504 {
		t.Errorf("checkout con la reserva en 3.500 ms: %d %v", code, body)
	}
	// Cada salto espera un poco más que el de abajo, así que se agota primero
	// la pasarela esperando a la reserva y el 504 nombra al servicio lento.
	if body["kind"] != "upstream_timeout" || body["component"] != "servicio-inventario" {
		t.Errorf("el 504 debe nombrar a servicio-inventario, no al primer salto: %v", body)
	}
	if espera < 2500*time.Millisecond || espera > 5*time.Second {
		t.Errorf("el 504 debe llegar cerca de los 3 s, tardó %s", espera)
	}
	// Una caída sigue siendo 503 (no se confunde con un tiempo agotado).
	rt.Faults.RestoreAll("mercado")
	down(t, rt, "mercado", "servicio-inventario", "503")
	if code, body := action(t, "mercado", "checkout"); code != 503 {
		t.Errorf("una caída debe seguir dando 503: %d %v", code, body)
	}
}

// Regla 4 de la matriz de errores: un error inesperado aguas abajo llega al
// sitio como 500 («ocurrió un error inesperado»), no como 503.
func TestErrorInesperadoLlegaAlSitioComo500(t *testing.T) {
	rt, _ := start(t)
	for _, c := range []struct{ service, action string }{
		{"api-catalogo", "browse"},
		{"servicio-inventario", "checkout"},
	} {
		if _, err := rt.Faults.Apply("mercado", c.service, faults.TypeErrors, faults.Params{ErrorRate: 1}, 0, "test"); err != nil {
			t.Fatal(err)
		}
		code, body := action(t, "mercado", c.action)
		if code != 500 || body["component"] != c.service || body["root_kind"] != "injected_error" {
			t.Errorf("errores al 100 %% en %s: %d %v", c.service, code, body)
		}
		rt.Faults.RestoreAll("mercado")
	}
}

// La saturación de un servicio interno sigue siendo 503, pero el sitio debe
// poder decir «hay mucha demanda»: root_kind viaja hasta el BFF.
func TestSaturacionInternaLlegaConSuTipo(t *testing.T) {
	rt, _ := start(t)
	if _, err := rt.Faults.Apply("mercado", "api-catalogo", faults.TypeSaturation, faults.Params{MaxConcurrency: 1}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Faults.Apply("mercado", "api-catalogo", faults.TypeDegraded, faults.Params{LatencyMs: 400}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	// action llama a t.Fatalf, que no se puede usar fuera de la gorrutina de la
	// prueba: aquí cada compra hace su propio POST.
	p, _ := catalog.BySlug("mercado")
	out := make(chan map[string]any, 3)
	for range 3 {
		go func() {
			var body map[string]any
			if res, err := http.Post(BaseURL(p.EntryService())+"/v1/action/browse", "application/json", nil); err == nil {
				_ = json.NewDecoder(res.Body).Decode(&body)
				res.Body.Close()
			}
			out <- body
		}()
	}
	saturado := false
	for range 3 {
		if b := <-out; b["root_kind"] == "saturation" && b["component"] == "api-catalogo" {
			saturado = true
		}
	}
	if !saturado {
		t.Error("con el pool en 1 y tres compras a la vez, una debe llegar como saturation")
	}
}

// F12.2: las acciones nuevas dependen del servicio correcto. Agregar al carro
// no pasa por la pasarela; buscar horas no toca la ficha; los exámenes sí.
func TestAccionesNuevasLleganAlServicioCorrecto(t *testing.T) {
	rt, l := start(t)
	down(t, rt, "mercado", "pasarela-pagos", "503")
	code, body := action(t, "mercado", "cart_add")
	if code != 200 {
		t.Fatalf("cart_add con la pasarela caída: %d %v", code, body)
	}
	trace := body["trace_id"].(string)
	for _, r := range l.Recent(logs.Filter{}) {
		if r.TraceID == trace && r.Service == "pasarela-pagos" {
			t.Errorf("cart_add no debe llamar a pasarela-pagos")
		}
	}
	rt.Faults.RestoreAll("mercado")

	down(t, rt, "clinica", "ficha-clinica-db", "503")
	if code, body := action(t, "clinica", "search"); code != 200 {
		t.Errorf("search no necesita la ficha: %d %v", code, body)
	}
	code, body = action(t, "clinica", "list_exams")
	if code != 503 || body["component"] != "ficha-clinica-db" {
		t.Errorf("list_exams con la ficha caída: %d %v", code, body)
	}
}

// TC-10
func TestErroresIntermitentesRespetanLaTasa(t *testing.T) {
	rt, _ := start(t)
	if _, err := rt.Faults.Apply("mercado", "api-catalogo", faults.TypeErrors, faults.Params{ErrorRate: 0.3}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	fallos, total := 0, 600
	for i := 0; i < total; i++ {
		if code, _ := action(t, "mercado", "browse"); code >= 500 {
			fallos++
		}
	}
	if tasa := float64(fallos) / float64(total); tasa < 0.22 || tasa > 0.38 {
		t.Errorf("tasa observada %.2f, quiero cerca de 0,30", tasa)
	}
}

func TestFallasInvalidasYExpiracion(t *testing.T) {
	rt, _ := start(t)
	for _, c := range []struct {
		svc, typ string
		p        faults.Params
	}{
		{"nope", faults.TypeDown, faults.Params{Mode: "503"}},
		{"api-catalogo", faults.TypeDown, faults.Params{Mode: "rara"}},
		{"api-catalogo", faults.TypeErrors, faults.Params{ErrorRate: 1.5}},
		{"api-catalogo", faults.TypeDegraded, faults.Params{LatencyMs: 0}},
		{"storefront-web", faults.TypeSlowDep, faults.Params{Target: "no-existe", LatencyMs: 100}},
		{"api-catalogo", "inventada", faults.Params{}},
	} {
		if _, err := rt.Faults.Apply("mercado", c.svc, c.typ, c.p, 0, "test"); err == nil {
			t.Errorf("%s/%s debería rechazarse", c.svc, c.typ)
		}
	}
	f, err := rt.Faults.Apply("mercado", "api-catalogo", faults.TypeDown, faults.Params{Mode: "refused"}, 1, "test")
	if err != nil || f.ExpiresAt == nil {
		t.Fatalf("falla con duración: %v %+v", err, f)
	}
}
