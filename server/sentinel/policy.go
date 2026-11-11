package sentinel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"demo-lab/server/catalog"
)

type slaTarget struct {
	Severity             string `json:"severity"`
	FirstResponseMinutes int    `json:"firstResponseMinutes"`
	ResolutionMinutes    int    `json:"resolutionMinutes"`
}

type slaHours struct {
	Days  []string `json:"days"`
	Start string   `json:"start"`
	End   string   `json:"end"`
}

type slaConfig struct {
	Targets       []slaTarget `json:"targets"`
	Timezone      string      `json:"timezone"`
	BusinessHours *slaHours   `json:"businessHours"`
	NotifyPct     int         `json:"notifyPct"`
	RiskPct       int         `json:"riskPct"`
	HourlyCost    int         `json:"hourlyCost"`
}

// HasSLA dice si el proyecto ya tiene un SLA vigente. Cada PUT crea una versión
// nueva, así que el aprovisionamiento solo lo escribe cuando falta.
func (c *Client) HasSLA(ctx context.Context, projectID string) (bool, error) {
	err := c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/sla", c.tok(), nil, nil)
	var he *HTTPError
	if errors.As(err, &he) && he.Code == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) SaveSLA(ctx context.Context, projectID string, cfg slaConfig) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+projectID+"/sla", c.tok(), cfg, nil)
}

func (c *Client) SaveHealthThresholds(ctx context.Context, projectID string, h catalog.HealthThresholds) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+projectID+"/health-thresholds", c.tok(), map[string]any{
		"availability_warn_pct": h.AvailabilityWarnPct,
		"mttr_warn_seconds":     h.MttrWarnSeconds,
		"latency_warn_ms":       h.LatencyWarnMs,
	}, nil)
}

// ClientInfo es un cliente de la empresa (quien contrata el servicio), no el
// cliente HTTP de este paquete.
type ClientInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type contact struct {
	ID        string `json:"id,omitempty"`
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	JobTitle  string `json:"job_title"`
}

func (c *Client) Customers(ctx context.Context) ([]ClientInfo, error) {
	var out []ClientInfo
	return out, c.do(ctx, http.MethodGet, "/api/v1/clients", c.tok(), nil, &out)
}

func (c *Client) CreateCustomer(ctx context.Context, name string) (ClientInfo, error) {
	var out ClientInfo
	return out, c.do(ctx, http.MethodPost, "/api/v1/clients", c.tok(), map[string]string{"name": name}, &out)
}

func (c *Client) contacts(ctx context.Context, clientID, projectID string) ([]contact, error) {
	var out []contact
	return out, c.do(ctx, http.MethodGet, "/api/v1/clients/"+clientID+"/contacts?project_id="+projectID, c.tok(), nil, &out)
}

func (c *Client) addContact(ctx context.Context, clientID string, ct contact) error {
	return c.do(ctx, http.MethodPost, "/api/v1/clients/"+clientID+"/contacts", c.tok(), ct, nil)
}

// ensurePolicy deja en el proyecto lo que una empresa real configura: SLA,
// umbrales de salud y clientes con su contacto en el servicio.
func (p *Provisioner) ensurePolicy(ctx context.Context, pl catalog.Platform, projectID string) error {
	pol := pl.Policy()

	has, err := p.Client.HasSLA(ctx, projectID)
	if err != nil {
		return fmt.Errorf("SLA: %w", err)
	}
	if !has {
		cfg := slaConfig{Timezone: "America/Santiago", NotifyPct: pol.Notify, RiskPct: pol.Risk, HourlyCost: pol.Hourly}
		for _, t := range pol.SLA {
			cfg.Targets = append(cfg.Targets, slaTarget(t))
		}
		if h := pol.Hours; h != nil {
			cfg.BusinessHours = &slaHours{Days: h.Days, Start: h.Start, End: h.End}
		}
		if err := p.Client.SaveSLA(ctx, projectID, cfg); err != nil {
			return fmt.Errorf("SLA: %w", err)
		}
	}

	if err := p.Client.SaveHealthThresholds(ctx, projectID, pol.Health); err != nil {
		return fmt.Errorf("umbrales de salud: %w", err)
	}

	if err := p.ensureCustomers(ctx, pl.Slug, pol, projectID); err != nil {
		return err
	}
	return p.ensurePortalAccess(ctx, pl, projectID)
}

// ensurePortalAccess da acceso al primer contacto del primer cliente de la
// plataforma y activa su usuario con DEMO_PORTAL_PASSWORD (F13, D-18). Es
// idempotente: si el contacto ya tiene usuario, Sentinel devuelve activation
// nulo y no se vuelve a activar nada.
func (p *Provisioner) ensurePortalAccess(ctx context.Context, pl catalog.Platform, projectID string) error {
	if p.PortalPassword == "" {
		return nil
	}
	pol := pl.Policy()
	if len(pol.Clients) == 0 || len(pol.Clients[0].Contacts) == 0 {
		return nil
	}
	spec, contacto := pol.Clients[0], pol.Clients[0].Contacts[0]
	customers, err := p.Client.Customers(ctx)
	if err != nil {
		return fmt.Errorf("portal: %w", err)
	}
	var clientID string
	for _, c := range customers {
		if c.Name == spec.Name {
			clientID = c.ID
		}
	}
	if clientID == "" {
		return fmt.Errorf("portal: no existe el cliente %s", spec.Name)
	}
	email := p.portalContactEmail(pl.Slug, contacto.Email)
	contacts, err := p.Client.contacts(ctx, clientID, projectID)
	if err != nil {
		return fmt.Errorf("portal: %w", err)
	}
	var contactID string
	for _, ct := range contacts {
		if ct.Email == email {
			contactID = ct.ID
		}
	}
	if contactID == "" {
		for _, ct := range contacts {
			if ct.Email == contacto.Email {
				contactID = ct.ID
			}
		}
		if contactID == "" {
			return fmt.Errorf("portal: no existe el contacto %s", email)
		}
		if email != contacto.Email {
			if err := p.Client.UpdateContactEmail(ctx, clientID, contactID, email); err != nil {
				return fmt.Errorf("portal: correo del contacto: %w", err)
			}
		}
	}
	act, err := p.Client.GrantClientAccess(ctx, clientID, contactID)
	if err != nil {
		return fmt.Errorf("portal: acceso de %s: %w", email, err)
	}
	if act != nil {
		if err := p.Client.ActivateClient(ctx, act.Token, p.PortalPassword); err != nil {
			return fmt.Errorf("portal: activación de %s: %w", email, err)
		}
	}
	return nil
}

// PortalAlias arma la casilla del usuario Cliente de una plataforma en aws: el
// alias va antes de la arroba (equipo+mercado@dominio.cl), así cada plataforma
// tiene su propio usuario y todos los correos llegan a la misma casilla.
func PortalAlias(mailbox, platform string) string {
	at := strings.LastIndex(mailbox, "@")
	if at <= 0 {
		return mailbox
	}
	return mailbox[:at] + "+" + platform + mailbox[at:]
}

// portalContactEmail es el correo que debe tener el contacto del portal: en
// aws (con DEMO_PORTAL_EMAIL) la casilla real con el alias de la plataforma; si
// no, el del catálogo, que en local captura Mailpit.
func (p *Provisioner) portalContactEmail(platform, catalogEmail string) string {
	if p.PortalPassword == "" || p.PortalEmail == "" {
		return catalogEmail
	}
	return PortalAlias(p.PortalEmail, platform)
}

func (p *Provisioner) ensureCustomers(ctx context.Context, platform string, pol catalog.Policy, projectID string) error {
	existing, err := p.Client.Customers(ctx)
	if err != nil {
		return fmt.Errorf("clientes: %w", err)
	}
	byName := make(map[string]string, len(existing))
	for _, c := range existing {
		byName[c.Name] = c.ID
	}
	for i, spec := range pol.Clients {
		id, found := byName[spec.Name]
		if !found {
			created, err := p.Client.CreateCustomer(ctx, spec.Name)
			if err != nil {
				return fmt.Errorf("cliente %s: %w", spec.Name, err)
			}
			id = created.ID
		}
		have, err := p.Client.contacts(ctx, id, projectID)
		if err != nil {
			return fmt.Errorf("contactos de %s: %w", spec.Name, err)
		}
		emails := make(map[string]bool, len(have))
		for _, ct := range have {
			emails[ct.Email] = true
		}
		for j, ct := range spec.Contacts {
			// En aws el contacto del portal ya pasó a la casilla real: buscarlo
			// solo por su correo del catálogo lo volvería a crear duplicado.
			if emails[ct.Email] || (i == 0 && j == 0 && emails[p.portalContactEmail(platform, ct.Email)]) {
				continue
			}
			err := p.Client.addContact(ctx, id, contact{ProjectID: projectID, Name: ct.Name, Email: ct.Email, JobTitle: ct.JobTitle})
			if err != nil && !errors.Is(err, ErrConflict) {
				return fmt.Errorf("contacto %s: %w", ct.Email, err)
			}
		}
	}
	return nil
}
