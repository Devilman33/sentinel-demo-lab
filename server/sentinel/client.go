// Package sentinel es el cliente de la API pública de Sentinel. Solo usa
// interfaces que cualquier empresa cliente tiene: sesión, proyectos, carpetas,
// grafo, tokens de ingesta e ingesta de logs. No conoce la base de Sentinel.
package sentinel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var ErrConflict = errors.New("sentinel: ya existe")

// HTTPError es una respuesta no 2xx de Sentinel. El envío de logs distingue
// los 4xx (no se reintentan) de los 5xx y la red (sí).
type HTTPError struct {
	Method, Path string
	Code         int
	Body         string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("sentinel %s %s: %d %s", e.Method, e.Path, e.Code, e.Body)
}

type Client struct {
	base string
	http *http.Client

	mu    sync.RWMutex // el token lo renueva el login mientras otros hilos lo usan
	token string
}

func (c *Client) tok() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

func (c *Client) setTok(t string) {
	c.mu.Lock()
	c.token = t
	c.mu.Unlock()
}

func NewClient(baseURL string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: 15 * time.Second}}
}

type Folder struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Project struct {
	ID                        string `json:"id"`
	Name                      string `json:"name"`
	MonitoringURL             string `json:"monitoring_url"`
	MonitoringIntervalSeconds int    `json:"monitoring_interval_seconds"`
	// Ventana horaria en UTC ("HH:MM"); vacía es 24/7. El PUT la reemplaza:
	// mandarla vacía la borra.
	MonitoringWindowStart string `json:"monitoring_window_start"`
	MonitoringWindowEnd   string `json:"monitoring_window_end"`
	FolderID              string `json:"folder_id,omitempty"`
}

type Component struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Criticality string `json:"criticality"`
	HealthURL   string `json:"health_url"`
	Status      string `json:"status"` // ok | critical | unknown, según el último sondeo
}

// Event es el RawEvent de /api/ingest/batch.
type Event struct {
	Source      string `json:"source"`
	SourceID    string `json:"source_id,omitempty"`
	ComponentID string `json:"component_id,omitempty"`
	Format      string `json:"format"`
	Payload     string `json:"payload"`
}

// IngestResult es el estado con que Sentinel normalizó cada evento del lote.
type IngestResult struct {
	Status string `json:"status"` // processed | incomplete | discarded
}

// do ejecuta la petición y decodifica la respuesta en out (si no es nil). Un
// 409 se devuelve como ErrConflict; cualquier otro código no 2xx, como error
// con el cuerpo de Sentinel para que la consola lo muestre.
func (c *Client) do(ctx context.Context, method, path, bearer string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode == http.StatusConflict {
		return ErrConflict
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &HTTPError{Method: method, Path: path, Code: res.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (c *Client) Login(ctx context.Context, email, password string) error {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/auth/login", "", map[string]string{"email": email, "password": password}, &out); err != nil {
		return err
	}
	if out.Token == "" {
		return errors.New("sentinel: el login no devolvió token")
	}
	c.setTok(out.Token)
	return nil
}

func (c *Client) Folders(ctx context.Context) ([]Folder, error) {
	var out []Folder
	return out, c.do(ctx, http.MethodGet, "/api/v1/project-folders", c.tok(), nil, &out)
}

func (c *Client) CreateFolder(ctx context.Context, name string) (Folder, error) {
	var out Folder
	return out, c.do(ctx, http.MethodPost, "/api/v1/project-folders", c.tok(), map[string]string{"name": name}, &out)
}

func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	return out, c.do(ctx, http.MethodGet, "/api/projects", c.tok(), nil, &out)
}

func (c *Client) CreateProject(ctx context.Context, p Project) (Project, error) {
	var out Project
	return out, c.do(ctx, http.MethodPost, "/api/projects", c.tok(), p, &out)
}

func (c *Client) UpdateProject(ctx context.Context, p Project) (Project, error) {
	var out Project
	return out, c.do(ctx, http.MethodPut, "/api/projects/"+p.ID, c.tok(), p, &out)
}

func (c *Client) AssignFolder(ctx context.Context, projectID, folderID string) error {
	return c.do(ctx, http.MethodPut, "/api/v1/projects/"+projectID+"/folder", c.tok(), map[string]string{"folder_id": folderID}, nil)
}

// IssueIngestToken emite el token de ingesta. Sentinel revoca el anterior del
// proyecto, así que quien llama debe guardar el resultado.
func (c *Client) IssueIngestToken(ctx context.Context, projectID, name string) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	err := c.do(ctx, http.MethodPost, "/api/v1/projects/"+projectID+"/ingest-tokens", c.tok(), map[string]string{"name": name}, &out)
	return out.Token, err
}

func (c *Client) Components(ctx context.Context, projectID string) ([]Component, error) {
	var out []Component
	return out, c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/components", c.tok(), nil, &out)
}

// UpdateComponent cambia solo los campos que vienen en el mapa (criticality y
// health_url): Sentinel rechaza un PATCH que no traiga ninguno.
func (c *Client) UpdateComponent(ctx context.Context, componentID string, fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	return c.do(ctx, http.MethodPatch, "/api/v1/components/"+componentID, c.tok(), fields, nil)
}

// Declare crea la dependencia source -> target; si ya existe devuelve nil.
func (c *Client) Declare(ctx context.Context, sourceID, targetID string) error {
	err := c.do(ctx, http.MethodPost, "/api/v1/components/dependencies", c.tok(),
		map[string]string{"source_component_id": sourceID, "target_component_id": targetID}, nil)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	return err
}

// Ingest envía un lote a /api/ingest/batch con el token del proyecto (no el de
// sesión) y devuelve el estado de cada evento, en el mismo orden.
func (c *Client) Ingest(ctx context.Context, ingestToken string, events []Event) ([]IngestResult, error) {
	var out []IngestResult
	return out, c.do(ctx, http.MethodPost, "/api/ingest/batch", ingestToken, events, &out)
}

// Reachable dice si Sentinel responde en /api/status, sin necesitar sesión.
func (c *Client) Reachable(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/api/status", "", nil, nil)
}

// Alert es una Alerta Maestra del listado de la empresa.
type Alert struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Title     string    `json:"title"`
	RootCause string    `json:"root_cause"`
	Status    string    `json:"status"`
	Severity  string    `json:"severity"`
	Category  string    `json:"category"`
	StartedAt time.Time `json:"started_at"`
}

// Alerts lista las Alertas Maestras de la empresa (GET /api/v1/alerts).
func (c *Client) Alerts(ctx context.Context) ([]Alert, error) {
	var out []Alert
	return out, c.do(ctx, http.MethodGet, "/api/v1/alerts", c.tok(), nil, &out)
}

// Assign reemplaza los responsables de una alerta.
func (c *Client) Assign(ctx context.Context, alertID string, userIDs []string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/incidents/"+alertID+"/assign", c.tok(), map[string]any{"assignee_ids": userIDs}, nil)
}

// Resolve cierra un incidente con el formulario de cierre (notas y categoría).
func (c *Client) Resolve(ctx context.Context, alertID, notes, category string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/incidents/"+alertID+"/resolve", c.tok(), map[string]string{"resolution_notes": notes, "category": category}, nil)
}

// Probe es el último sondeo de un proyecto (GET /api/v1/monitor/status).
// StatusCode 0 significa que el Poller no recibió respuesta.
type Probe struct {
	ProjectID   string `json:"project_id"`
	ComponentID string `json:"component_id"`
	Healthy     bool   `json:"healthy"`
	StatusCode  int    `json:"status_code"`
}

// MonitorStatus lista los últimos sondeos de los proyectos de la empresa.
func (c *Client) MonitorStatus(ctx context.Context) ([]Probe, error) {
	var out []Probe
	return out, c.do(ctx, http.MethodGet, "/api/v1/monitor/status", c.tok(), nil, &out)
}

// Channel es un canal de aviso de un servicio. El GET entrega el webhook
// enmascarado y el PUT rechaza la máscara: vacío conserva el guardado.
type Channel struct {
	Active          bool     `json:"active"`
	Webhook         string   `json:"webhook"`
	CategoryFilter  []string `json:"category_filter"`
	SeverityFilter  []string `json:"severity_filter"`
	EventTypeFilter []string `json:"event_type_filter"`
	StateChanges    bool     `json:"notify_state_changes"`
}

// Channels son los tres canales de un servicio; el PUT los exige todos.
type Channels struct {
	Email   Channel `json:"email"`
	Discord Channel `json:"discord"`
	Slack   Channel `json:"slack"`
}

func (c *Client) Channels(ctx context.Context, projectID string) (Channels, error) {
	var out Channels
	return out, c.do(ctx, http.MethodGet, "/api/v1/projects/"+projectID+"/notification-channels", c.tok(), nil, &out)
}

// SaveChannels guarda los canales. Sentinel guarda los válidos y responde 200
// con el motivo de los demás, así que un 200 todavía puede traer rechazos.
func (c *Client) SaveChannels(ctx context.Context, projectID string, ch Channels) error {
	var out struct {
		Errors map[string]string `json:"errors"`
	}
	if err := c.do(ctx, http.MethodPut, "/api/v1/projects/"+projectID+"/notification-channels", c.tok(), ch, &out); err != nil {
		return err
	}
	if len(out.Errors) > 0 {
		return fmt.Errorf("sentinel rechazó canales: %v", out.Errors)
	}
	return nil
}

// UpdateContactEmail cambia el correo de un contacto (F13, solo aws: se usa una
// casilla real con +<plataforma> para que el portal no escriba a example.com).
func (c *Client) UpdateContactEmail(ctx context.Context, clientID, contactID, email string) error {
	return c.do(ctx, http.MethodPatch, "/api/v1/clients/"+clientID+"/contacts/"+contactID, c.tok(), map[string]string{"email": email}, nil)
}

// Activation es el enlace de activación que Sentinel entrega una sola vez al
// dar acceso a un contacto.
type Activation struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

// GrantClientAccess vincula el contacto con su servicio y devuelve el enlace de
// activación solo la primera vez; después devuelve nil.
func (c *Client) GrantClientAccess(ctx context.Context, clientID, contactID string) (*Activation, error) {
	var out struct {
		Activation *Activation `json:"activation"`
	}
	err := c.do(ctx, http.MethodPut, "/api/v1/clients/"+clientID+"/contacts/"+contactID+"/access",
		c.tok(), map[string]bool{"enabled": true}, &out)
	return out.Activation, err
}

// ActivateClient fija la clave del usuario Cliente con el token de activación.
func (c *Client) ActivateClient(ctx context.Context, token, password string) error {
	return c.do(ctx, http.MethodPost, "/api/auth/activate-client", "",
		map[string]string{"token": token, "new_password": password}, nil)
}

// PortalService es un servicio tal como lo ve el usuario Cliente.
type PortalService struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
}

// PortalServices lista los servicios del usuario Cliente autenticado.
func (c *Client) PortalServices(ctx context.Context) ([]PortalService, error) {
	var out []PortalService
	return out, c.do(ctx, http.MethodGet, "/api/v1/portal/services", c.tok(), nil, &out)
}

// UserID es el usuario del token actual (claim "uid"), o "" si no se puede leer.
func (c *Client) UserID() string {
	parts := strings.Split(c.tok(), ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		UID string `json:"uid"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.UID
}
