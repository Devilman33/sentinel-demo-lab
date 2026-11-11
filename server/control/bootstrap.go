package control

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/sentinel"
)

// Estados del arranque autónomo. Los sitios y la consola funcionan en todos:
// solo cambia si lo que se envía a Sentinel llega a alguna parte.
const (
	BootStarting      = "iniciando"
	BootConnecting    = "conectando"
	BootReady         = "listo"
	BootNoCredentials = "sin_credenciales"
)

// BootStatus es lo que el módulo informa de su conexión con Sentinel.
type BootStatus struct {
	State     string    `json:"state"`
	Attempts  int       `json:"attempts"` // intentos fallidos desde el último éxito
	LastError string    `json:"last_error,omitempty"`
	Since     time.Time `json:"since"`
}

func (a *API) BootStatus() BootStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.boot
}

func (a *API) setBoot(state string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if state != a.boot.State {
		a.boot.Since = time.Now()
	}
	a.boot.State = state
	if err != nil {
		a.boot.Attempts++
		a.boot.LastError = err.Error()
		return
	}
	a.boot.Attempts, a.boot.LastError = 0, ""
}

// Bootstrap deja el módulo conectado a Sentinel sin intervención: inicia sesión,
// aprovisiona lo que falte y sigue comprobando que Sentinel no haya cambiado.
// onReady se llama una sola vez, al primer éxito. Corre hasta que ctx termina.
//
// Si el estado guardado sigue valiendo no vuelve a aprovisionar, para no
// pisar lo que alguien haya cambiado a mano en Sentinel (p. ej. los canales).
func (a *API) Bootstrap(ctx context.Context, onReady func()) {
	if a.Profile.Email == "" || a.Profile.Password == "" {
		a.setBoot(BootNoCredentials, errors.New("faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD"))
		log.Println("sin credenciales de Sentinel: los sitios funcionan, pero nada se envía")
		return
	}
	delay, announced, ready := a.bootEvery, "", false
	for {
		err := a.reconcile(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.setBoot(BootConnecting, err)
			if msg := err.Error(); msg != announced {
				log.Printf("sin conexión con Sentinel (reintenta): %s", msg)
				announced = msg
			}
			delay = min(delay*2, 6*a.bootEvery)
		} else {
			a.setBoot(BootReady, nil)
			if !ready {
				ready = true
				log.Printf("conectado a Sentinel (%s)", a.Profile.SentinelURL)
				if onReady != nil {
					onReady()
				}
			} else if announced != "" {
				log.Println("conexión con Sentinel recuperada")
			}
			announced, delay = "", a.syncEvery
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// reconcile aprovisiona solo si lo guardado ya no coincide con Sentinel.
func (a *API) reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ok, err := a.inSync(ctx)
	if err != nil || ok {
		return err
	}
	if _, err := a.doProvision(ctx); err != nil {
		return err
	}
	a.Journal.Add("provision", "", "aprovisionar escenario en Sentinel (automático)", "")
	return nil
}

// inSync comprueba, con pocas lecturas, que cada plataforma siga existiendo en
// Sentinel con el sondeo correcto y que su token de ingesta valga. false sin
// error significa "hay que aprovisionar"; un error es que Sentinel no respondió.
func (a *API) inSync(ctx context.Context) (bool, error) {
	ids := a.projectIDs()
	if len(ids) != len(catalog.All()) {
		return false, nil
	}
	var projects []sentinel.Project
	err := marathonDeps{a}.retry(ctx, func() (e error) { projects, e = a.client.Projects(ctx); return })
	if err != nil {
		return false, err
	}
	byID := make(map[string]sentinel.Project, len(projects))
	for _, p := range projects {
		byID[p.ID] = p
	}
	prov := a.provisioner()
	for _, pl := range catalog.All() {
		p, found := byID[ids[pl.Slug]]
		// Cada minuto: así el intervalo cambia solo al abrir y cerrar la ventana.
		if !found || !prov.ProjectInSync(pl, p) {
			return false, nil
		}
		tgt, found := a.Target(pl.Slug)
		if !found || len(tgt.Components) != len(pl.Services) {
			return false, nil
		}
		// La salud por servicio se corrige sola si cambió DEMO_PUBLIC_URL: los
		// nodos del mapa dejarían de tener sondeo propio y quedarían en gris.
		comps, err := a.client.Components(ctx, ids[pl.Slug])
		if err != nil {
			return false, err
		}
		salud := make(map[string]string, len(comps))
		for _, c := range comps {
			salud[c.Name] = c.HealthURL
		}
		for _, s := range pl.Services {
			if salud[s.Slug] != prov.ComponentHealthURL(pl.Slug, s.Slug) {
				return false, nil
			}
		}
		// Un lote vacío no escribe nada: 400 es "token válido" y 401, "inválido".
		_, err = a.client.Ingest(ctx, tgt.Token, nil)
		var he *sentinel.HTTPError
		switch {
		case errors.As(err, &he) && he.Code == http.StatusUnauthorized:
			return false, nil
		case errors.As(err, &he) && he.Code == http.StatusBadRequest:
		case err != nil:
			return false, err
		}
	}
	return true, nil
}
