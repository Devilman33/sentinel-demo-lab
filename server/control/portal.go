package control

import (
	"context"
	"fmt"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/sentinel"
)

// portalEmail es el correo con el que inicia sesión el usuario Cliente. En
// local es el contacto ficticio de example.com (Mailpit lo captura); en aws es
// la casilla real con el alias de la plataforma (equipo+mercado@dominio).
func (a *API) portalEmail(pl catalog.Platform) string {
	pol := pl.Policy()
	if len(pol.Clients) == 0 || len(pol.Clients[0].Contacts) == 0 {
		return ""
	}
	if a.Profile.PortalEmail != "" {
		return sentinel.PortalAlias(a.Profile.PortalEmail, pl.Slug)
	}
	return pol.Clients[0].Contacts[0].Email
}

// En aws «Verificar» no repite los inicios de sesión del portal a cada rato:
// Sentinel deja pasar 10 por minuto por IP y bloquea al usuario 15 min tras
// cinco claves fallidas (T3.4). Un resultado bueno vale portalReuse; tras un
// fallo la comprobación se pausa portalPause.
const (
	portalReuse = 5 * time.Minute
	portalPause = 15 * time.Minute
)

// checkPortal comprueba el portal del cliente. En local siempre de nuevo; en
// aws con las pausas de arriba.
func (a *API) checkPortal(ctx context.Context) check {
	if a.Profile.PortalPassword == "" || a.Profile.Name != "aws" {
		return a.probePortal(ctx)
	}
	a.portalMu.Lock()
	defer a.portalMu.Unlock()
	now := a.now()
	if now.Before(a.portalHold) {
		return check{Detail: fmt.Sprintf("en pausa hasta las %s UTC para no bloquear al usuario Cliente; último error: %s",
			a.portalHold.UTC().Format("15:04"), a.portalLast.Detail)}
	}
	if a.portalLast.OK && now.Sub(a.portalAt) < portalReuse {
		return check{OK: true, Detail: "comprobado a las " + a.portalAt.UTC().Format("15:04") + " UTC"}
	}
	c := a.probePortal(ctx)
	a.portalLast, a.portalAt = c, now
	if !c.OK {
		a.portalHold = now.Add(portalPause)
	}
	return c
}

// probePortal inicia sesión como el usuario Cliente de cada plataforma y
// comprueba que el portal le devuelva su servicio (F13.4). Sin
// DEMO_PORTAL_PASSWORD la comprobación queda desactivada y no rompe la
// verificación general. Se detiene en el primer fallo.
func (a *API) probePortal(ctx context.Context) check {
	if a.Profile.PortalPassword == "" {
		return check{OK: true, Detail: "desactivado: define DEMO_PORTAL_PASSWORD para probar el portal"}
	}
	ids := a.projectIDs()
	if len(ids) == 0 {
		return check{Detail: sinProyectos}
	}
	for _, pl := range catalog.All() {
		email := a.portalEmail(pl)
		c := sentinel.NewClient(a.Profile.SentinelURL)
		if err := c.Login(ctx, email, a.Profile.PortalPassword); err != nil {
			return check{Detail: fmt.Sprintf("%s: no inicia sesión (%v)", email, err)}
		}
		servicios, err := c.PortalServices(ctx)
		if err != nil {
			return check{Detail: fmt.Sprintf("%s: %v", email, err)}
		}
		found := false
		for _, s := range servicios {
			if s.ProjectID == ids[pl.Slug] {
				found = true
			}
		}
		if !found {
			return check{Detail: fmt.Sprintf("%s no ve el servicio de %s", email, pl.Slug)}
		}
	}
	return check{OK: true}
}
