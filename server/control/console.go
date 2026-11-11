package control

import (
	"embed"
	"fmt"
	"html"
	"net/http"
	"strings"

	"demo-lab/server/catalog"
)

// La consola se sirve como archivos separados (sin compilación): la página, su
// CSS, la lógica y la guía. Se embeben en el binario para que el módulo funcione
// sin carpetas sueltas y para poder proteger toda la consola bajo una sola ruta
// cuando se exponga fuera de loopback (A6).
//
//go:embed console/*
var consoleFS embed.FS

// sitesMarker es el lugar de console/index.html donde van los accesos a los sitios.
const sitesMarker = "<!--SITIOS-->"

// consoleHTML es la consola con los accesos a los sitios ya armados desde el
// catálogo: aparecen al instante y siguen las plataformas que existan, sin
// depender del JavaScript ni de que el estado haya cargado.
var consoleHTML = []byte(strings.Replace(mustReadEmbedded("console/index.html"), sitesMarker, sitesSection(), 1))

// consoleAssets solo sirve los archivos conocidos: así no se puede pedir una
// ruta arbitraria del sistema de archivos embebido.
var consoleAssets = map[string]string{
	"console.css": "text/css; charset=utf-8",
	"console.js":  "text/javascript; charset=utf-8",
	"guide.js":    "text/javascript; charset=utf-8",
}

func mustReadEmbedded(name string) string {
	raw, err := consoleFS.ReadFile(name)
	if err != nil {
		panic("demo-lab: falta el archivo embebido " + name + ": " + err.Error())
	}
	return string(raw)
}

func sitesSection() string {
	var b strings.Builder
	b.WriteString(`<section id="sec-sites"><h2>Sitios de las plataformas</h2><div class="row">`)
	for _, p := range catalog.All() {
		fmt.Fprintf(&b, `<a class="btn site" href="/sitios/%s/" target="_blank" rel="noopener" data-guide="general-open-site">%s ↗</a>`,
			html.EscapeString(p.Slug), html.EscapeString(p.Name))
	}
	b.WriteString(`<a class="btn" href="/sitios/" target="_blank" rel="noopener" data-guide="general-open-site">Todos los sitios ↗</a></div>`)
	b.WriteString(`<p class="muted small" id="sites-hint" style="margin:10px 0 0"></p></section>`)
	return b.String()
}

func (a *API) console(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(consoleHTML)
}

// consoleAsset sirve el CSS, la lógica y la guía de la consola.
func (a *API) consoleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	contentType, ok := consoleAssets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	raw, err := consoleFS.ReadFile("console/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}
