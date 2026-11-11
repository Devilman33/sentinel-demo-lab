package access

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	consoleKey = "clave-de-consola-123"
	siteKey    = "clave-de-sitios-456"
)

// ok responde 200 en cualquier ruta: así la prueba mide solo a la guardia.
var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func do(t *testing.T, h http.Handler, method, target string, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for _, m := range mod {
		m(req)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func withCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func login(t *testing.T, h http.Handler, key, next string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"clave": {key}, "next": {next}}
	req := httptest.NewRequest(http.MethodPost, "/acceso", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func sessionCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName {
			return c
		}
	}
	t.Fatalf("el inicio de sesión no dejó la cookie %s (código %d)", cookieName, rec.Code)
	return nil
}

// R1: sin credenciales solo responden /salud, /healthz y /robots.txt.
func TestSinCredencialesSoloRespondenLasRutasPublicas(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/healthz", 200},
		{"GET", "/salud/mercado", 200},
		{"GET", "/salud/banco/auth-banca", 200},
		{"GET", "/robots.txt", 200},
		{"GET", "/acceso", 200},
		{"GET", "/", 303},
		{"GET", "/console/console.js", 303},
		{"GET", "/sitios/", 303},
		{"GET", "/sitios/mercado/", 303},
		{"GET", "/sitios/shared/common.js", 303},
		{"POST", "/sitios/api/mercado/pay", 401},
		{"GET", "/control/v1/status", 401},
		{"POST", "/control/v1/faults", 401},
		{"POST", "/control/v1/reset", 401},
		{"DELETE", "/control/v1/marathon", 401},
		// La ruta se limpia antes de decidir: no se cuela por /salud.
		{"POST", "/salud/../control/v1/faults", 401},
		{"GET", "/salud/%2e%2e/control/v1/status", 401},
	}
	for _, c := range cases {
		if rec := do(t, h, c.method, c.path); rec.Code != c.want {
			t.Errorf("%s %s = %d, se esperaba %d", c.method, c.path, rec.Code, c.want)
		}
	}
	rec := do(t, h, "GET", "/sitios/mercado/?x=1")
	if loc := rec.Header().Get("Location"); loc != "/acceso?next=%2Fsitios%2Fmercado%2F%3Fx%3D1" {
		t.Errorf("la redirección debe volver a la página pedida; Location = %q", loc)
	}
}

func TestTodasLasRespuestasLlevanNoindex(t *testing.T) {
	for _, g := range []*Guard{New("", ""), New(consoleKey, siteKey)} {
		h := g.Wrap(ok)
		for _, p := range []string{"/healthz", "/", "/sitios/mercado/", "/acceso"} {
			if v := do(t, h, "GET", p).Header().Get("X-Robots-Tag"); !strings.Contains(v, "noindex") {
				t.Errorf("activa=%v %s sin noindex (%q)", g.Enabled(), p, v)
			}
		}
		rec := do(t, h, "GET", "/robots.txt")
		if !strings.Contains(rec.Body.String(), "Disallow: /") {
			t.Errorf("robots.txt debe bloquear todo: %q", rec.Body.String())
		}
	}
}

func TestSinClaveDeConsolaTodoQuedaAbierto(t *testing.T) {
	h := New("", "").Wrap(ok)
	for _, p := range []string{"/", "/control/v1/status", "/sitios/mercado/"} {
		if rec := do(t, h, "GET", p); rec.Code != 200 {
			t.Errorf("sin clave %s = %d, se esperaba 200 como antes de A6", p, rec.Code)
		}
	}
}

func TestClaveDeSitiosAbreLosSitiosPeroNoLaConsola(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	rec := login(t, h, siteKey, "/sitios/banco/")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sitios/banco/" {
		t.Fatalf("login con la clave de sitios = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("la cookie debe ser HttpOnly y SameSite=Lax: %+v", c)
	}
	for _, p := range []string{"/sitios/", "/sitios/banco/", "/sitios/shared/common.js"} {
		if code := do(t, h, "GET", p, withCookie(c)).Code; code != 200 {
			t.Errorf("con sesión de sitios %s = %d", p, code)
		}
	}
	if code := do(t, h, "POST", "/sitios/api/banco/login", withCookie(c)).Code; code != 200 {
		t.Errorf("las acciones del sitio deben funcionar con la clave de sitios: %d", code)
	}
	// El Modo Director usa /control/v1: con la clave de sitios no dispara fallas.
	if code := do(t, h, "POST", "/control/v1/faults", withCookie(c)).Code; code != http.StatusForbidden {
		t.Errorf("POST /control/v1/faults con sesión de sitios = %d, se esperaba 403", code)
	}
	rec = do(t, h, "GET", "/", withCookie(c))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "nivel=consola") {
		t.Errorf("la consola con sesión de sitios debe pedir la otra clave: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// Si next pide más de lo que da la clave, se va a los sitios en vez de rebotar.
	if loc := login(t, h, siteKey, "/control/v1/status").Header().Get("Location"); loc != "/sitios/" {
		t.Errorf("next de consola con clave de sitios → %q, se esperaba /sitios/", loc)
	}
}

func TestClaveDeConsolaAbreTodo(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	rec := login(t, h, consoleKey, "")
	if rec.Header().Get("Location") != "/" {
		t.Errorf("sin next la consola vuelve a /: %q", rec.Header().Get("Location"))
	}
	c := sessionCookie(t, rec)
	for _, r := range [][2]string{{"GET", "/"}, {"POST", "/control/v1/faults"}, {"GET", "/sitios/clinica/"}, {"POST", "/sitios/api/clinica/book"}} {
		if code := do(t, h, r[0], r[1], withCookie(c)).Code; code != 200 {
			t.Errorf("con sesión de consola %s %s = %d", r[0], r[1], code)
		}
	}
}

func TestBearerParaScriptsYPruebas(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	bearer := func(k string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+k) }
	}
	if code := do(t, h, "POST", "/control/v1/reset", bearer(consoleKey)).Code; code != 200 {
		t.Errorf("Bearer con la clave de consola = %d", code)
	}
	if code := do(t, h, "POST", "/control/v1/reset", bearer(siteKey)).Code; code != http.StatusForbidden {
		t.Errorf("Bearer con la clave de sitios en control = %d, se esperaba 403", code)
	}
	if code := do(t, h, "POST", "/control/v1/reset", bearer("otra-clave-cualquiera")).Code; code != http.StatusUnauthorized {
		t.Errorf("Bearer con clave falsa = %d, se esperaba 401", code)
	}
}

func TestClaveIncorrectaYCupoDeIntentos(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	rec := login(t, h, "no-es-la-clave", "/")
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("clave incorrecta = %d con %d cookies", rec.Code, len(rec.Result().Cookies()))
	}
	if !strings.Contains(rec.Body.String(), "Clave incorrecta") {
		t.Error("la página debe decir que la clave es incorrecta")
	}
	for i := 2; i <= loginBurst; i++ {
		login(t, h, "no-es-la-clave", "/")
	}
	// Agotado el cupo, ni la clave correcta pasa: no se sigue comparando.
	if code := login(t, h, consoleKey, "/").Code; code != http.StatusTooManyRequests {
		t.Errorf("tras %d fallos la clave correcta = %d, se esperaba 429", loginBurst, code)
	}
	// Otra IP no queda bloqueada por la primera.
	other := func(r *http.Request) { r.RemoteAddr = "203.0.113.9:4000" }
	if code := do(t, h, "GET", "/control/v1/status", other, func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+consoleKey)
	}).Code; code != 200 {
		t.Errorf("otra IP con la clave correcta = %d", code)
	}
}

func TestSesionVencidaOAlterada(t *testing.T) {
	g := New(consoleKey, siteKey)
	h := g.Wrap(ok)
	c := sessionCookie(t, login(t, h, siteKey, "/sitios/"))

	forged := *c
	forged.Value = strings.Replace(c.Value, "v1.1.", "v1.2.", 1) // sitios → consola sin firma válida
	if code := do(t, h, "POST", "/control/v1/faults", withCookie(&forged)).Code; code != http.StatusUnauthorized {
		t.Errorf("cookie alterada = %d, se esperaba 401", code)
	}
	// Otra clave de consola firma distinto: cambiarla cierra las sesiones.
	if code := do(t, New("otra-clave-consola-9", siteKey).Wrap(ok), "GET", "/sitios/", withCookie(c)).Code; code != http.StatusSeeOther {
		t.Errorf("sesión firmada con otras claves = %d, se esperaba 303", code)
	}
	g.now = func() time.Time { return time.Now().Add(sessionTTL + time.Minute) }
	if code := do(t, h, "GET", "/sitios/", withCookie(c)).Code; code != http.StatusSeeOther {
		t.Errorf("sesión vencida = %d, se esperaba 303", code)
	}
}

func TestCookieSeguraDetrasDeHTTPS(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	form := url.Values{"clave": {consoleKey}}
	req := httptest.NewRequest(http.MethodPost, "/acceso", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if c := sessionCookie(t, rec); !c.Secure {
		t.Error("detrás del proxy HTTPS de Render la cookie debe ser Secure")
	}
}

func TestNextSoloAceptaRutasPropias(t *testing.T) {
	for in, want := range map[string]string{
		"/sitios/mercado/":   "/sitios/mercado/",
		"/?x=1":              "/?x=1",
		"//otro.cl/":         "",
		"/\\otro.cl":         "",
		"https://otro.cl/":   "",
		"javascript:alert()": "",
		"":                   "",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, se esperaba %q", in, got, want)
		}
	}
}

func TestLaPaginaDeAccesoEscapaNext(t *testing.T) {
	h := New(consoleKey, siteKey).Wrap(ok)
	body := do(t, h, "GET", `/acceso?next=/x"><script>alert(1)</script>`).Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Error("next debe ir escapado en el formulario")
	}
}

func TestCupoDeRutasPublicas(t *testing.T) {
	g := New(consoleKey, siteKey)
	h := g.Wrap(ok)
	codes := map[int]int{}
	for i := 0; i < publicBurst+5; i++ {
		codes[do(t, h, "GET", "/salud/mercado").Code]++
	}
	if codes[200] != publicBurst || codes[http.StatusTooManyRequests] != 5 {
		t.Errorf("ráfaga de %d a /salud: %v", publicBurst+5, codes)
	}
}

func TestLimiterAcotaLaMemoria(t *testing.T) {
	l := newLimiter(1, 2, time.Now)
	for i := 0; i < maxBuckets+50; i++ {
		l.consume(strings.Repeat("x", i%7) + string(rune('a'+i%26)) + time.Duration(i).String())
	}
	if n := len(l.buckets); n > maxBuckets+1 {
		t.Errorf("el limitador guarda %d baldes, el tope es %d más el común", n, maxBuckets)
	}
}
