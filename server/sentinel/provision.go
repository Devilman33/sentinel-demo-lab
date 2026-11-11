package sentinel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"demo-lab/server/catalog"
)

// PlatformState es lo que el módulo recuerda de una plataforma ya aprovisionada.
type PlatformState struct {
	FolderID    string            `json:"folder_id"`
	ProjectID   string            `json:"project_id"`
	IngestToken string            `json:"ingest_token"`
	Components  map[string]string `json:"components"` // slug -> id en Sentinel
}

// State es el mapeo guardado en .state/<perfil>.json (fuera de git: tiene tokens).
type State struct {
	Platforms map[string]PlatformState `json:"platforms"`
}

func LoadState(dir, profile string) (State, error) {
	st := State{Platforms: map[string]PlatformState{}}
	raw, err := os.ReadFile(filepath.Join(dir, profile+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return st, err
	}
	if st.Platforms == nil {
		st.Platforms = map[string]PlatformState{}
	}
	return st, nil
}

// Clone copia el estado entero, mapas incluidos, para que quien lo modifica no
// compita con quien lo lee.
func (s State) Clone() State {
	out := State{Platforms: make(map[string]PlatformState, len(s.Platforms))}
	for slug, ps := range s.Platforms {
		comps := make(map[string]string, len(ps.Components))
		for k, v := range ps.Components {
			comps[k] = v
		}
		ps.Components = comps
		out.Platforms[slug] = ps
	}
	return out
}

func SaveState(dir, profile string, st State) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, profile+".json"), raw, 0o600)
}

// Provisioner crea en Sentinel, por su API pública, lo que el escenario necesita.
type Provisioner struct {
	Client      *Client
	ServicesURL string // base con la que Sentinel alcanza los servicios
	IntervalSec int
	// Webhooks que se activan en cada servicio; vacío deja el canal como está.
	Discord, Slack string
	// Portal del cliente (F13). Sin PortalPassword no se toca el acceso.
	PortalPassword, PortalEmail string
	// Ventana de monitoreo en UTC (T3.1); vacía es 24/7.
	WindowStart, WindowEnd string
	// Severidades del canal correo (T3.2): nil lo deja como está; vacío lo apaga.
	EmailSeverities []string
}

// ProjectInSync dice si el proyecto ya tiene el sondeo que pide el perfil.
func (p *Provisioner) ProjectInSync(pl catalog.Platform, cur Project) bool {
	return cur.MonitoringURL == p.HealthURL(pl) && cur.MonitoringIntervalSeconds == p.IntervalSec &&
		cur.MonitoringWindowStart == p.WindowStart && cur.MonitoringWindowEnd == p.WindowEnd
}

// HealthURL es el monitoring_url del proyecto: /salud/{plataforma} en el puerto
// público del módulo, que reenvía al /health/deep del BFF. ServicesURL ya trae
// el puerto, así que el módulo no necesita publicar los de sus servicios.
func (p *Provisioner) HealthURL(pl catalog.Platform) string {
	return fmt.Sprintf("%s/salud/%s", strings.TrimRight(p.ServicesURL, "/"), pl.Slug)
}

// ComponentHealthURL es el health_url de un servicio: la ruta pública que
// reenvía a su /health superficial. Sentinel sondea cada nodo del mapa por
// separado, así solo se pinta el que falla (V-15).
func (p *Provisioner) ComponentHealthURL(platform, service string) string {
	return fmt.Sprintf("%s/salud/%s/%s", strings.TrimRight(p.ServicesURL, "/"), platform, service)
}

// Provision deja Sentinel listo para todas las plataformas. Es idempotente:
// correrlo otra vez reutiliza lo que existe y solo completa lo que falta. Un
// token de ingesta ya guardado no se vuelve a emitir (emitir revoca el anterior).
func (p *Provisioner) Provision(ctx context.Context, st State) (State, error) {
	for _, pl := range catalog.All() {
		ps, err := p.provisionPlatform(ctx, pl, st.Platforms[pl.Slug])
		st.Platforms[pl.Slug] = ps // se conserva el avance parcial aunque falle
		if err != nil {
			return st, fmt.Errorf("%s: %w", pl.Slug, err)
		}
	}
	return st, nil
}

func (p *Provisioner) provisionPlatform(ctx context.Context, pl catalog.Platform, ps PlatformState) (PlatformState, error) {
	folderID, err := p.ensureFolder(ctx, pl.Folder)
	if err != nil {
		return ps, fmt.Errorf("carpeta: %w", err)
	}
	ps.FolderID = folderID

	proj, err := p.ensureProject(ctx, pl, folderID)
	if err != nil {
		return ps, fmt.Errorf("proyecto: %w", err)
	}
	// Si el proyecto no es el que se recordaba, Sentinel se recreó: el token y los
	// componentes guardados ya no valen, y reusarlos enviaría logs a ninguna parte.
	if ps.ProjectID != "" && ps.ProjectID != proj.ID {
		ps.IngestToken, ps.Components = "", nil
	}
	ps.ProjectID = proj.ID

	if ps.IngestToken == "" {
		if ps.IngestToken, err = p.Client.IssueIngestToken(ctx, proj.ID, "demo-lab-"+pl.Slug); err != nil {
			return ps, fmt.Errorf("token de ingesta: %w", err)
		}
	}

	if ps.Components, err = p.ensureComponents(ctx, pl, ps); err != nil {
		return ps, fmt.Errorf("componentes: %w", err)
	}
	for _, e := range pl.Edges() {
		if err := p.Client.Declare(ctx, ps.Components[e[0]], ps.Components[e[1]]); err != nil {
			return ps, fmt.Errorf("dependencia %s -> %s: %w", e[0], e[1], err)
		}
	}
	if err := p.ensureChannels(ctx, ps.ProjectID); err != nil {
		return ps, fmt.Errorf("canales: %w", err)
	}
	if err := p.ensurePolicy(ctx, pl, ps.ProjectID); err != nil {
		return ps, err
	}
	return ps, nil
}

// ensureChannels activa los webhooks del perfil en el servicio y aplica el
// filtro del correo. Los avisos se configuran por servicio desde TASK-016; el
// resto de la configuración se conserva, porque el PUT reemplaza los tres canales.
func (p *Provisioner) ensureChannels(ctx context.Context, projectID string) error {
	if p.Discord == "" && p.Slack == "" && p.EmailSeverities == nil {
		return nil
	}
	ch, err := p.Client.Channels(ctx, projectID)
	if err != nil {
		return err
	}
	ch.Email.Webhook, ch.Discord.Webhook, ch.Slack.Webhook = "", "", ""
	// En producción cada incidente le escribe a la casilla de la empresa demo
	// por el SMTP de Sentinel: el filtro cuida esa cuota (D-22).
	if p.EmailSeverities != nil {
		ch.Email.Active = len(p.EmailSeverities) > 0
		if ch.Email.Active {
			ch.Email.SeverityFilter = append([]string(nil), p.EmailSeverities...)
		}
	}
	if p.Discord != "" {
		ch.Discord.Active, ch.Discord.Webhook = true, p.Discord
	}
	if p.Slack != "" {
		ch.Slack.Active, ch.Slack.Webhook = true, p.Slack
	}
	return p.Client.SaveChannels(ctx, projectID, ch)
}

func (p *Provisioner) ensureFolder(ctx context.Context, name string) (string, error) {
	folders, err := p.Client.Folders(ctx)
	if err != nil {
		return "", err
	}
	for _, f := range folders {
		if f.Name == name {
			return f.ID, nil
		}
	}
	f, err := p.Client.CreateFolder(ctx, name)
	return f.ID, err
}

func (p *Provisioner) ensureProject(ctx context.Context, pl catalog.Platform, folderID string) (Project, error) {
	want := Project{Name: pl.Project, MonitoringURL: p.HealthURL(pl), MonitoringIntervalSeconds: p.IntervalSec,
		MonitoringWindowStart: p.WindowStart, MonitoringWindowEnd: p.WindowEnd}
	projects, err := p.Client.Projects(ctx)
	if err != nil {
		return Project{}, err
	}
	var cur Project
	for _, x := range projects {
		if x.Name == pl.Project {
			cur = x
		}
	}
	if cur.ID == "" {
		if cur, err = p.Client.CreateProject(ctx, want); err != nil {
			return Project{}, err
		}
	} else if !p.ProjectInSync(pl, cur) {
		// El perfil cambió (p. ej. de local a aws) o la ventana pasó de abierta a
		// cerrada (D-23): se corrige el destino, el intervalo y la ventana.
		want.ID = cur.ID
		if cur, err = p.Client.UpdateProject(ctx, want); err != nil {
			return Project{}, err
		}
	}
	if cur.FolderID != folderID {
		if err := p.Client.AssignFolder(ctx, cur.ID, folderID); err != nil {
			return Project{}, err
		}
		cur.FolderID = folderID
	}
	return cur, nil
}

// ensureComponents hace que cada servicio exista como componente. Sentinel no
// tiene alta directa: el componente nace al llegar el primer log con
// service=<slug> (autodescubrimiento). Por eso se envía un log informativo de
// arranque por servicio faltante, y luego se fija la criticidad por API.
func (p *Provisioner) ensureComponents(ctx context.Context, pl catalog.Platform, ps PlatformState) (map[string]string, error) {
	have, err := p.componentIDs(ctx, ps.ProjectID)
	if err != nil {
		return nil, err
	}
	var boot []Event
	for _, s := range pl.Services {
		if _, ok := have[s.Slug]; !ok {
			boot = append(boot, bootEvent(pl, s))
		}
	}
	if len(boot) > 0 {
		if _, err := p.Client.Ingest(ctx, ps.IngestToken, boot); err != nil {
			return nil, fmt.Errorf("log de arranque: %w", err)
		}
		if have, err = p.componentIDs(ctx, ps.ProjectID); err != nil {
			return nil, err
		}
	}
	comps, err := p.Client.Components(ctx, ps.ProjectID)
	if err != nil {
		return nil, err
	}
	porNombre := make(map[string]Component, len(comps))
	for _, c := range comps {
		porNombre[c.Name] = c
	}
	for _, s := range pl.Services {
		id, ok := have[s.Slug]
		if !ok {
			return nil, fmt.Errorf("el componente %s no apareció tras el log de arranque", s.Slug)
		}
		// Un solo PATCH con lo que cambió: la criticidad y la URL de salud que
		// sondea el Poller. Si el perfil cambió, la URL se corrige sola.
		patch := map[string]string{}
		if cur := porNombre[s.Slug]; cur.Criticality != s.Criticality {
			patch["criticality"] = s.Criticality
		}
		want := p.ComponentHealthURL(pl.Slug, s.Slug)
		if porNombre[s.Slug].HealthURL != want {
			patch["health_url"] = want
		}
		if len(patch) > 0 {
			if err := p.Client.UpdateComponent(ctx, id, patch); err != nil {
				return nil, fmt.Errorf("configuración de %s: %w", s.Slug, err)
			}
		}
	}
	return have, nil
}

func (p *Provisioner) componentIDs(ctx context.Context, projectID string) (map[string]string, error) {
	comps, err := p.Client.Components(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]string, len(comps))
	for _, c := range comps {
		ids[c.Name] = c.ID
	}
	return ids, nil
}

// bootEvent solo existe para que Sentinel autodescubra el componente. Va sin
// severity a propósito: queda "incomplete" y api-ia no lo correlaciona. Con
// severity se vectorizaría y abriría un incidente "Incidente en <servicio>"
// por cada servicio al aprovisionar.
func bootEvent(pl catalog.Platform, s catalog.Service) Event {
	payload, _ := json.Marshal(map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"service":   s.Slug,
		"platform":  pl.Slug,
		"message":   "servicio iniciado",
	})
	return Event{Source: "cloud", SourceID: "demo-lab/" + pl.Slug + "/" + s.Slug, Format: "json", Payload: string(payload)}
}
