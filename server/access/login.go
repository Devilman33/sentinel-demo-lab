package access

import (
	"html/template"
	"net/http"
	"strings"
	"time"
)

// La página de acceso es neutra a propósito: no muestra ninguna de las marcas
// ficticias. En un dominio público, un formulario con la cara de un banco es lo
// que hace que un sitio parezca phishing.
var loginPage = template.Must(template.New("acceso").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>demo-lab · acceso</title>
<style>
:root{color-scheme:light dark;--bg:#f4f6f8;--card:#fff;--ink:#1d2731;--muted:#5b6875;--line:#cfd6dd;--accent:#2457c5;--bad:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#12161b;--card:#1b2128;--ink:#e6ebf0;--muted:#9aa7b4;--line:#34404c;--accent:#7aa2ff;--bad:#ff8a80}}
*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;padding:16px;background:var(--bg);color:var(--ink);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{width:100%;max-width:380px;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:28px 24px}
h1{font-size:20px;margin:0 0 4px}
p{margin:0 0 16px;color:var(--muted);font-size:14px}
label{display:block;font-weight:600;margin:0 0 6px}
input{width:100%;font:inherit;padding:10px 12px;border:1px solid var(--line);border-radius:8px;background:transparent;color:inherit}
input:focus{outline:2px solid var(--accent);outline-offset:1px}
button{margin-top:16px;width:100%;font:inherit;font-weight:600;padding:10px 12px;border:0;border-radius:8px;background:var(--accent);color:#fff;cursor:pointer}
.msg{color:var(--bad);font-weight:600}
</style>
</head>
<body>
<main>
<h1>demo-lab</h1>
<p>Simulador de plataformas ficticias para la demostración de Sentinel. Si no tienes la clave, pídela al equipo del stand.</p>
{{if .Msg}}<p class="msg" role="alert">{{.Msg}}</p>{{end}}
<form method="post" action="/acceso">
<input type="hidden" name="next" value="{{.Next}}">
<label for="clave">Clave</label>
<input id="clave" name="clave" type="password" autocomplete="current-password" required autofocus>
<button type="submit">Entrar</button>
</form>
</main>
</body>
</html>
`))

func renderLogin(w http.ResponseWriter, code int, next, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_ = loginPage.Execute(w, struct{ Next, Msg string }{next, msg})
}

// serveLogin atiende GET (formulario) y POST (clave) en /acceso.
func (g *Guard) serveLogin(w http.ResponseWriter, r *http.Request, ip string) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		q := r.URL.Query()
		msg := ""
		if q.Get("nivel") == "consola" {
			msg = "Esta parte pide la clave de la consola."
		}
		renderLogin(w, http.StatusOK, safeNext(q.Get("next")), msg)
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := r.ParseForm(); err != nil {
			renderLogin(w, http.StatusBadRequest, "", "No se pudo leer el formulario.")
			return
		}
		next := safeNext(r.PostForm.Get("next"))
		if g.login.blocked(ip) {
			renderLogin(w, http.StatusTooManyRequests, next, "Demasiados intentos. Espera un minuto.")
			return
		}
		l := g.keyLevel(r.PostForm.Get("clave"))
		if l == Public {
			g.login.consume(ip)
			renderLogin(w, http.StatusUnauthorized, next, "Clave incorrecta.")
			return
		}
		exp := g.now().Add(sessionTTL)
		// SameSite=Lax: llega en la navegación normal (un enlace desde el
		// dashboard), pero no en un POST ni en un fetch desde otro sitio, que es
		// lo que bloquea que otra página dispare fallas con la sesión del equipo.
		http.SetCookie(w, &http.Cookie{
			Name: cookieName, Value: g.sign(l, exp), Path: "/",
			Expires: exp, MaxAge: int(sessionTTL / time.Second),
			HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
		})
		if next == "" || Required(cleanPath(pathOf(next))) > l {
			next = "/"
			if l == Sites {
				next = "/sitios/"
			}
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	default:
		h.Set("Allow", "GET, POST")
		http.Error(w, "método no permitido", http.StatusMethodNotAllowed)
	}
}

// pathOf quita la consulta de una ruta relativa ya validada por safeNext.
func pathOf(next string) string {
	if i := strings.IndexAny(next, "?#"); i >= 0 {
		return next[:i]
	}
	return next
}
