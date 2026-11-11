package control

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/runtime"
)

// siteProbeClient espera 3 s al /health del BFF: lo mismo que tolera el sitio
// antes de mostrar la página de error. Es una variable para poder acortarla en
// las pruebas.
var siteProbeClient = &http.Client{Timeout: 3 * time.Second}

// siteGate dice si el sitio puede servirse. 502 es una conexión rechazada, 503
// un BFF que responde 503 y 504 un tiempo agotado; en esos casos la puerta
// entrega la página de error de la marca con el código real (D-14).
func (a *API) siteGate(platform string) (int, bool) {
	pl, ok := catalog.BySlug(platform)
	if !ok || a.Runtime == nil {
		return http.StatusOK, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), siteProbeClient.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runtime.BaseURL(pl.EntryService())+"/health", nil)
	if err != nil {
		return http.StatusServiceUnavailable, false
	}
	res, err := siteProbeClient.Do(req)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return http.StatusGatewayTimeout, false
		}
		return http.StatusBadGateway, false
	}
	defer res.Body.Close()
	if res.StatusCode >= 500 {
		if res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusGatewayTimeout {
			return res.StatusCode, false
		}
		return http.StatusServiceUnavailable, false
	}
	return http.StatusOK, true
}
