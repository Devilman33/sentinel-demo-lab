// Package sites sirve los sitios web ficticios de las plataformas (HTML, CSS y
// JS sin paso de compilación). Son una interfaz más del módulo: llaman a su API
// de control como cualquier cliente.
package sites

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"demo-lab/server/catalog"
)

//go:embed all:static
var static embed.FS

// Gate dice si el sitio puede servirse. Devuelve el código real de la falla
// (502, 503 o 504) y false cuando hay que mostrar la página de error.
type Gate func(platform string) (code int, up bool)

// Handler sirve /sitios/{mercado,banco,clinica}/ y /sitios/shared/. Sin Gate se
// sirve como siempre; con Gate, la portada de cada plataforma consulta primero
// al BFF y, si no está, entrega la página de error de la marca.
func Handler(gates ...Gate) http.Handler {
	sub, _ := fs.Sub(static, "static")
	files := http.FileServer(http.FS(sub))
	var gate Gate
	if len(gates) > 0 {
		gate = gates[0]
	}
	return http.StripPrefix("/sitios/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /sitios/mercado sin la barra final: la página de error usa rutas
		// relativas y ahí cargaría sin estilos ni Modo Director. Se redirige
		// antes de consultar al BFF, igual que hace el FileServer con el sitio arriba.
		if _, ok := catalog.BySlug(r.URL.Path); ok {
			to := "/sitios/" + r.URL.Path + "/"
			if r.URL.RawQuery != "" {
				to += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, to, http.StatusMovedPermanently)
			return
		}
		if gate != nil && r.Method == http.MethodGet {
			if platform, isIndex := portada(r.URL.Path); isIndex {
				if code, up := gate(platform); !up {
					writeErrorPage(w, code, platform)
					return
				}
			}
		}
		files.ServeHTTP(w, r)
	}))
}

// portada reconoce /{plataforma}/ y /{plataforma}/index.html. Cualquier otro
// archivo (CSS, JS, fuentes) se sirve sin consultar al BFF.
func portada(path string) (platform string, isIndex bool) {
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 2)
	platform = parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}
	if _, ok := catalog.BySlug(platform); !ok {
		return platform, false
	}
	return platform, rest == "" || rest == "index.html"
}

var errorPage, _ = static.ReadFile("static/shared/error.html")

// writeErrorPage responde con el código real y la página de error de la marca.
// El sitio reintenta solo cada 10 s y la barra Modo Director sigue disponible.
func writeErrorPage(w http.ResponseWriter, code int, platform string) {
	pl, ok := catalog.BySlug(platform)
	if !ok {
		http.NotFound(w, nil)
		return
	}
	if code < 400 {
		code = http.StatusServiceUnavailable
	}
	msg := "No pudimos cargar " + pl.Name
	switch code {
	case http.StatusGatewayTimeout:
		msg = pl.Name + " tardó demasiado en responder"
	case http.StatusServiceUnavailable:
		msg = pl.Name + " no está disponible en este momento"
	}
	html := strings.NewReplacer(
		"{{PLATFORM}}", pl.Slug,
		"{{NAME}}", pl.Name,
		"{{CODE}}", strconv.Itoa(code),
		"{{MESSAGE}}", msg,
	).Replace(string(errorPage))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = fmt.Fprint(w, html)
}
