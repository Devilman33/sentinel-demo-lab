// Package access separa lo público del control (A6). Con DEMO_CONSOLE_KEY
// definida, los sitios y la consola piden una clave en /acceso y recuerdan la
// sesión en una cookie firmada. El sondeo de Sentinel (/salud/*) y /healthz
// siguen abiertos, porque el Poller no manda credenciales. Sin clave, que solo
// se permite en el perfil local, todo queda abierto como antes.
package access

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// Level es lo que permite una sesión: cada nivel incluye a los anteriores.
type Level int

const (
	Public  Level = iota // /salud, /healthz, /robots.txt y /acceso
	Sites                // /sitios/*, incluida su API de acciones de usuario
	Console              // la consola, /control/v1/* y el Modo Director
)

const (
	// MinKeyLen es el largo mínimo de cada clave.
	MinKeyLen  = 12
	cookieName = "demo_acceso"
	sessionTTL = 12 * time.Hour
	// Intentos fallidos de clave por IP: 10 seguidos y luego uno cada 6 s.
	loginBurst, loginPerSec = 10, 10.0 / 60
	// Rutas abiertas por IP. El Poller de Sentinel pide 19 URLs cada 5 s.
	publicBurst, publicPerSec = 60, 20.0
)

// Guard aplica el control de acceso delante de todas las rutas del módulo.
type Guard struct {
	consoleKey, siteKey string
	macKey              []byte
	now                 func() time.Time
	login, public       *limiter
}

// New arma la guardia. Sin consoleKey queda desactivada: solo agrega noindex y
// robots.txt. La firma de las sesiones sale de las dos claves, así que cambiar
// cualquiera de ellas cierra todas las sesiones abiertas.
func New(consoleKey, siteKey string) *Guard {
	sum := sha256.Sum256([]byte("demo-lab/acceso\x00" + consoleKey + "\x00" + siteKey))
	return &Guard{
		consoleKey: consoleKey, siteKey: siteKey, macKey: sum[:], now: time.Now,
		login:  newLimiter(loginPerSec, loginBurst, time.Now),
		public: newLimiter(publicPerSec, publicBurst, time.Now),
	}
}

// Enabled dice si el módulo pide clave.
func (g *Guard) Enabled() bool { return g.consoleKey != "" }

// Required dice qué nivel pide una ruta. Se mira la ruta ya limpia, la misma
// que usa el enrutador, para que /salud/../control no cuente como pública.
func Required(p string) Level {
	switch {
	case p == "/healthz", p == "/robots.txt", p == "/favicon.ico", p == "/acceso",
		strings.HasPrefix(p, "/salud/"):
		return Public
	case p == "/sitios" || strings.HasPrefix(p, "/sitios/"):
		return Sites
	}
	return Console
}

func cleanPath(p string) string {
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	return path.Clean(p)
}

// Wrap pone la guardia delante de next.
func (g *Guard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Los sitios son ficticios: que ningún buscador los indexe.
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		p := cleanPath(r.URL.Path)
		if p == "/robots.txt" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
			return
		}
		if !g.Enabled() {
			if p == "/acceso" {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		if p == "/acceso" {
			g.serveLogin(w, r, ip)
			return
		}
		need := Required(p)
		if need == Public {
			if !g.public.allow(ip) {
				writeJSON(w, http.StatusTooManyRequests, "demasiadas peticiones: espera unos segundos")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		have, bearer := g.level(r, ip)
		if bearer == errTooMany {
			writeJSON(w, http.StatusTooManyRequests, "demasiados intentos con clave incorrecta: espera un minuto")
			return
		}
		if have >= need {
			next.ServeHTTP(w, r)
			return
		}
		g.deny(w, r, p, need, have)
	})
}

type bearerResult int

const (
	noBearer bearerResult = iota
	bearerOK
	bearerBad
	errTooMany
)

// level resuelve la sesión de la petición: primero una cabecera
// Authorization: Bearer <clave> (para curl y las pruebas), después la cookie.
func (g *Guard) level(r *http.Request, ip string) (Level, bearerResult) {
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		// Con el cupo agotado no se compara: así no se puede seguir probando claves.
		if g.login.blocked(ip) {
			return Public, errTooMany
		}
		if l := g.keyLevel(tok); l != Public {
			return l, bearerOK
		}
		g.login.consume(ip)
		return Public, bearerBad
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return Public, noBearer
	}
	return g.verify(c.Value), noBearer
}

func (g *Guard) keyLevel(k string) Level {
	k = strings.TrimSpace(k)
	switch {
	case k == "":
		return Public
	case equal(k, g.consoleKey):
		return Console
	case g.siteKey != "" && equal(k, g.siteKey):
		return Sites
	}
	return Public
}

// equal compara los resúmenes: así el tiempo tampoco depende del largo.
func equal(a, b string) bool {
	ha, hb := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

// sign arma el valor de la cookie: v1.<nivel>.<vence>.<firma>.
func (g *Guard) sign(l Level, exp time.Time) string {
	payload := fmt.Sprintf("v1.%d.%d", l, exp.Unix())
	return payload + "." + base64.RawURLEncoding.EncodeToString(g.mac(payload))
}

func (g *Guard) mac(payload string) []byte {
	m := hmac.New(sha256.New, g.macKey)
	m.Write([]byte(payload))
	return m.Sum(nil)
}

func (g *Guard) verify(v string) Level {
	i := strings.LastIndexByte(v, '.')
	if i < 0 {
		return Public
	}
	payload := v[:i]
	sig, err := base64.RawURLEncoding.DecodeString(v[i+1:])
	if err != nil || !hmac.Equal(sig, g.mac(payload)) {
		return Public
	}
	parts := strings.Split(payload, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return Public
	}
	l, err := strconv.Atoi(parts[1])
	if err != nil || (Level(l) != Sites && Level(l) != Console) {
		return Public
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || g.now().Unix() >= exp {
		return Public
	}
	return Level(l)
}

// deny manda a /acceso a quien navega y responde JSON a quien llama a la API.
func (g *Guard) deny(w http.ResponseWriter, r *http.Request, p string, need, have Level) {
	if wantsPage(r, p) {
		q := url.Values{"next": {r.URL.RequestURI()}}
		if have == Sites && need == Console {
			q.Set("nivel", "consola")
		}
		http.Redirect(w, r, "/acceso?"+q.Encode(), http.StatusSeeOther)
		return
	}
	if have == Sites {
		writeJSON(w, http.StatusForbidden, "esta acción pide la clave de la consola: entra de nuevo en /acceso")
		return
	}
	writeJSON(w, http.StatusUnauthorized, "sesión vencida o sin clave: entra en /acceso")
}

func wantsPage(r *http.Request, p string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return !strings.HasPrefix(p, "/control/") && !strings.HasPrefix(p, "/sitios/api/")
}

func writeJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// safeNext solo acepta rutas de este mismo sitio: /x sí; //otro.cl y /\otro.cl no.
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return ""
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return ""
	}
	return next
}

// clientIP usa la primera dirección de X-Forwarded-For, que pone el proxy de
// Render. Un cliente puede falsearla, pero solo cambia en qué cupo cae: las
// claves tienen al menos 12 caracteres, así que adivinarlas sigue sin ser viable.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
