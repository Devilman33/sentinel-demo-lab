package sites

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Marcas, dominios y eslóganes de los sitios de referencia (todos de empresas
// reales). Ninguno puede aparecer en los sitios ficticios (spec §2.12, R4).
var bloqueadas = []string{
	"jumbo", "cencosud", "bancochile", "banco de chile", "banchile", "mibanco", "clinicaalemana", "clínica alemana",
	"clinica alemana", "redsalud", "red salud", "miredsalud", "minerva", "falabella", "unimarc", "lider.cl",
}

// TC-19 (parte automática): los sitios no contienen nombres ni recursos de los de referencia.
func TestSitiosNoContienenMarcasDeReferencia(t *testing.T) {
	var archivos int
	err := fs.WalkDir(static, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		archivos++
		// R4 (decisión 2026-11-01): se permiten las tipografías de licencia libre (OFL) bajadas
		// de su fuente oficial, solo dentro de shared/fonts y junto a su licencia. Son binarios:
		// no se les busca nombres.
		if strings.HasSuffix(path, ".woff2") && strings.HasPrefix(path, "static/shared/fonts/") {
			return nil
		}
		// Fotografías de licencia libre (dominio público, CC0 o CC BY): solo en shared/photos y con su página de créditos.
		if strings.HasSuffix(path, ".jpg") && strings.HasPrefix(path, "static/shared/photos/") {
			return nil
		}
		raw, _ := static.ReadFile(path)
		texto := strings.ToLower(string(raw))
		for _, b := range bloqueadas {
			if strings.Contains(texto, b) {
				t.Errorf("%s contiene el nombre bloqueado %q", path, b)
			}
		}
		// Solo texto y vectores: nada de imágenes ni logos copiados, ni fuentes fuera de shared/fonts.
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff", ".woff2", ".ttf"} {
			if strings.HasSuffix(path, ext) {
				t.Errorf("%s: los sitios no deben incluir imágenes ni fuentes fuera de shared/fonts", path)
			}
		}
		return nil
	})
	if err != nil || archivos < 10 {
		t.Fatalf("recorrido de estáticos: %v (%d archivos)", err, archivos)
	}
}

// TC-20: cada sitio carga la base común, que dibuja la franja de «sitio ficticio».
func TestCadaSitioMuestraLaFranjaDeSitioFicticio(t *testing.T) {
	for _, site := range []string{"mercado", "banco", "clinica"} {
		js, _ := static.ReadFile("static/" + site + "/app.js")
		if !strings.Contains(string(js), "boot(PLATFORM)") || !strings.Contains(string(js), "../shared/common.js") {
			t.Errorf("%s no inicia la base común (franja y Modo Director)", site)
		}
	}
	common, _ := static.ReadFile("static/shared/common.js")
	if !strings.Contains(string(common), "Sitio ficticio") {
		t.Errorf("la franja de sitio ficticio no está en common.js")
	}
}

// Cada fotografía embebida figura en la página de créditos con su autor y su licencia (R4).
func TestCadaFotoTieneCreditos(t *testing.T) {
	entradas, err := fs.ReadDir(static, "static/shared/photos")
	if err != nil {
		t.Skipf("sin fotografías embebidas: %v", err)
	}
	creditos, err := static.ReadFile("static/shared/photos/creditos.html")
	if err != nil {
		t.Fatalf("faltan los créditos de las fotografías: %v", err)
	}
	for _, e := range entradas {
		if strings.HasSuffix(e.Name(), ".jpg") && !strings.Contains(string(creditos), e.Name()) {
			t.Errorf("la foto %s no figura en creditos.html", e.Name())
		}
	}
}

// Cada familia tipográfica embebida lleva su licencia OFL al lado (R4).
func TestCadaTipografiaIncluyeSuLicencia(t *testing.T) {
	entradas, err := fs.ReadDir(static, "static/shared/fonts")
	if err != nil {
		t.Fatalf("no se puede leer shared/fonts: %v", err)
	}
	var familias, licencias []string
	for _, e := range entradas {
		n := e.Name()
		if strings.HasPrefix(n, "LICENSE-") {
			licencias = append(licencias, strings.TrimSuffix(strings.TrimPrefix(n, "LICENSE-"), ".txt"))
			continue
		}
		if i := strings.Index(n, "-latin-"); i > 0 {
			familias = append(familias, n[:i])
		}
	}
	for _, f := range familias {
		ok := false
		for _, l := range licencias {
			ok = ok || l == f
		}
		if !ok {
			t.Errorf("la familia %q no tiene su LICENSE-%s.txt", f, f)
		}
	}
}

// F12.4: la base común habla por la ruta pública y unifica los cuatro estados
// con el formato de error que pide el plan.
func TestBaseComunUsaLaRutaPublicaYElFormatoDeError(t *testing.T) {
	raw, err := static.ReadFile("static/shared/common.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(raw)
	for _, want := range []string{"/sitios/api/", "traceId", "Ref. ", "errorText", "skeleton", "emptyState", "errorState"} {
		if !strings.Contains(js, want) {
			t.Errorf("common.js no contiene %q", want)
		}
	}
	css, _ := static.ReadFile("static/shared/common.css")
	for _, want := range []string{".skeleton", ".state-error", ".state-empty"} {
		if !strings.Contains(string(css), want) {
			t.Errorf("common.css no contiene %q", want)
		}
	}
}

// F12.5 a F12.7: cada sitio pide sus datos al servicio antes de dibujar y usa
// los estados comunes; no hay productos ni exámenes guardados si la llamada falla.
func TestSitiosPidenLosDatosAntesDeDibujar(t *testing.T) {
	casos := map[string][]string{
		"mercado": {"userAction(PLATFORM, 'browse')", "userAction(PLATFORM, 'cart_add')", "errorState", "showShelves", "showProduct"},
		"banco":   {"loadHomeBalance", "loadTransferBalance", "errorState", "disabled>Continuar"},
		"clinica": {"loadPros", "userAction(PLATFORM, 'search')", "userAction(PLATFORM, 'list_exams')", "errorState"},
	}
	for site, wants := range casos {
		raw, _ := static.ReadFile("static/" + site + "/app.js")
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Errorf("%s/app.js no contiene %q", site, want)
			}
		}
	}
	mercado, _ := static.ReadFile("static/mercado/app.js")
	if strings.Contains(string(mercado), "catalog-banner") {
		t.Error("el Mercado debe mostrar el error en cada parte, no en la franja del catálogo")
	}
}

func TestSeSirvenLasPaginas(t *testing.T) {
	h := Handler()
	for _, p := range []string{"/sitios/", "/sitios/mercado/", "/sitios/banco/", "/sitios/clinica/", "/sitios/shared/common.js", "/sitios/mercado/app.js"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != 200 || rec.Body.Len() == 0 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

// F12.3: con la puerta cerrada, la portada responde el código real con la marca
// y los estáticos se siguen sirviendo para que la página se vea.
func TestLaPuertaMuestraLaPaginaDeError(t *testing.T) {
	h := Handler(func(string) (int, bool) { return 502, false })
	for _, p := range []string{"/sitios/mercado/", "/sitios/mercado/index.html"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		cuerpo := rec.Body.String()
		if rec.Code != 502 || !strings.Contains(cuerpo, "Error 502") || !strings.Contains(cuerpo, "Mercado Don Federico") {
			t.Errorf("%s: %d", p, rec.Code)
		}
		if !strings.Contains(cuerpo, "Reintentar ahora") || !strings.Contains(cuerpo, "shared/common.js") {
			t.Errorf("%s: la página no trae reintento y Modo Director", p)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitios/mercado/style.css", nil))
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Errorf("los estáticos deben seguir disponibles con la puerta cerrada: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitios/nada/", nil))
	if rec.Code != 404 {
		t.Errorf("una plataforma inexistente debe dar 404: %d", rec.Code)
	}
	// Sin la barra final, las rutas relativas de la página de error no
	// resolverían: primero se redirige a la portada con barra.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitios/mercado?x=1", nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/sitios/mercado/?x=1" {
		t.Errorf("/sitios/mercado debe redirigir a /sitios/mercado/: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	sano := Handler(func(string) (int, bool) { return 200, true })
	rec = httptest.NewRecorder()
	sano.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sitios/mercado/", nil))
	if rec.Code != 200 {
		t.Errorf("con la puerta abierta: %d", rec.Code)
	}
}
