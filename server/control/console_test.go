package control

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"demo-lab/server/attacks"
	"demo-lab/server/config"
	"demo-lab/server/scripts"
)

func consoleHandler(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	New(config.Profile{Name: "local", SentinelURL: "http://x", ServicesURL: "http://y", StateDir: t.TempDir()}).Handler().
		ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	return rec
}

// C1: la consola vive en archivos separados y se sirven embebidos, sin depender
// de una carpeta externa ni de un paso de compilación.
func TestConsola_SirveLaPaginaYLosArchivosSeparados(t *testing.T) {
	rec := consoleHandler(t)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("consola: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`href="/console/console.css"`,
		`src="/console/console.js"`,
		`id="tab-mercado"`,
		`id="tab-actividad"`,
		`id="tab-datos"`,
		`id="guide"`,
		`data-platform-host="banco"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("la página no contiene %q", want)
		}
	}
	if strings.Contains(body, sitesMarker) {
		t.Error("quedó el marcador de los sitios sin reemplazar")
	}

	handler := New(config.Profile{Name: "local", SentinelURL: "http://x", ServicesURL: "http://y", StateDir: t.TempDir()}).Handler()
	for path, contentType := range map[string]string{
		"/console/console.css": "text/css",
		"/console/console.js":  "text/javascript",
		"/console/guide.js":    "text/javascript",
	} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 {
			t.Errorf("%s responde %d", path, r.Code)
			continue
		}
		if got := r.Header().Get("Content-Type"); !strings.HasPrefix(got, contentType) {
			t.Errorf("%s: Content-Type %q, quiero %s", path, got, contentType)
		}
		if r.Body.Len() == 0 {
			t.Errorf("%s está vacío", path)
		}
	}

	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest("GET", "/console/loquesea.js", nil))
	if r.Code != 404 {
		t.Errorf("un archivo desconocido debe dar 404, dio %d", r.Code)
	}
}

var (
	dataGuideRef = regexp.MustCompile(`data-guide="([^"]+)"`)
	guideKey     = regexp.MustCompile(`(?m)^\s{2}"([a-z0-9_-]+)":\s*\{`)
	guideValue   = regexp.MustCompile(`guide:\s*"([a-z0-9_-]+)"`)
)

func guideKeys(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := consoleFS.ReadFile("console/guide.js")
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, m := range guideKey.FindAllStringSubmatch(string(raw), -1) {
		keys[m[1]] = true
	}
	if len(keys) == 0 {
		t.Fatal("no se encontró ninguna entrada en GUIDE")
	}
	return keys
}

func referencedGuides(t *testing.T) map[string]bool {
	t.Helper()
	refs := map[string]bool{}
	add := func(raw string) {
		for _, m := range dataGuideRef.FindAllStringSubmatch(raw, -1) {
			if !strings.Contains(m[1], "${") {
				refs[m[1]] = true
			}
		}
	}

	// La página ya armada incluye los accesos a los sitios que agrega console.go.
	add(consoleHandler(t).Body.String())

	raw, err := consoleFS.ReadFile("console/console.js")
	if err != nil {
		t.Fatal(err)
	}
	add(string(raw))
	// Las cinco fallas declaran su id de guía en la tabla FAULTS.
	for _, m := range guideValue.FindAllStringSubmatch(string(raw), -1) {
		refs[m[1]] = true
	}
	return refs
}

// C3: cada control que declara data-guide tiene una entrada en GUIDE.
func TestConsola_CadaControlTieneEntradaEnLaGuia(t *testing.T) {
	keys := guideKeys(t)
	for id := range referencedGuides(t) {
		if !keys[id] {
			t.Errorf("data-guide=%q no tiene entrada en GUIDE", id)
		}
	}
}

// C3: un tipo de ataque o un guion nuevos no pueden quedar sin explicación.
func TestConsola_LaGuiaCubreAtaquesYGuiones(t *testing.T) {
	keys := guideKeys(t)
	for _, kind := range attacks.Kinds() {
		if id := "attack-" + kind.Name; !keys[id] {
			t.Errorf("el ataque %s no tiene entrada %s en GUIDE", kind.Name, id)
		}
	}
	list, errs := scripts.Builtin()
	if len(errs) > 0 {
		t.Fatalf("guiones incluidos: %v", errs)
	}
	for _, s := range list {
		if id := "script-" + s.ID; !keys[id] {
			t.Errorf("el guion %s no tiene entrada %s en GUIDE", s.ID, id)
		}
	}
}
