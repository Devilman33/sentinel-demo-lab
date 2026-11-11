// Package control expone la API de control del módulo (/control/v1): lo que usa
// la consola del Demo Day.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"demo-lab/server/access"
	"demo-lab/server/attacks"
	"demo-lab/server/catalog"
	"demo-lab/server/config"
	"demo-lab/server/faults"
	"demo-lab/server/logs"
	"demo-lab/server/marathon"
	"demo-lab/server/runtime"
	"demo-lab/server/scripts"
	"demo-lab/server/sentinel"
	"demo-lab/server/sites"
	"demo-lab/server/traffic"
)

type API struct {
	Profile  config.Profile
	Runtime  *runtime.Runtime
	Logger   *logs.Logger
	Shipper  *logs.Shipper
	Traffic  *traffic.Generator
	Attacks  *attacks.Generator
	Scripts  *scripts.Runner
	Marathon *marathon.Runner

	Journal *Journal

	mu     sync.Mutex // protege state y boot; nunca se sostiene durante una llamada a Sentinel
	client *sentinel.Client
	state  sentinel.State

	provMu sync.Mutex // serializa el aprovisionamiento: dos corridas a la vez duplicarían trabajo
	boot   BootStatus

	// Cada cuánto y durante cuánto se siguen cerrando incidentes tras un reinicio.
	settleEvery, settleFor time.Duration
	// Cada cuánto reintenta conectarse y cada cuánto comprueba que Sentinel siga igual.
	bootEvery, syncEvery time.Duration

	// Acceso (A6): pide clave a los sitios y a la consola si el perfil la trae.
	access *access.Guard
	// now es el reloj de la ventana y de la pausa del portal; las pruebas lo fijan.
	now func() time.Time
	// portalHold pausa la verificación del portal en aws tras un fallo (T3.4).
	portalMu   sync.Mutex
	portalHold time.Time
	portalLast check
	portalAt   time.Time
}

// New arma la API. rt, logger, shipper y tg pueden ser nil en pruebas que solo
// ejercitan el aprovisionamiento.
func New(p config.Profile) *API {
	st, _ := sentinel.LoadState(p.StateDir, p.Name)
	return &API{Profile: p, client: sentinel.NewClient(p.SentinelURL), state: st, settleEvery: 5 * time.Second, settleFor: 90 * time.Second,
		bootEvery: 5 * time.Second, syncEvery: time.Minute, boot: BootStatus{State: BootStarting},
		access: access.New(p.ConsoleKey, p.SitePassword), now: time.Now}
}

// Target entrega el destino de logs de una plataforma (para el Shipper).
func (a *API) Target(platform string) (logs.Target, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ps, ok := a.state.Platforms[platform]
	if !ok || ps.IngestToken == "" {
		return logs.Target{}, false
	}
	return logs.Target{Token: ps.IngestToken, Components: ps.Components}, true
}

// Client expone el cliente de Sentinel para el envío de logs.
func (a *API) Client() *sentinel.Client { return a.client }

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /salud/{platform}", a.salud)
	mux.HandleFunc("GET /salud/{platform}/{service}", a.saludServicio)
	mux.HandleFunc("GET /control/v1/status", a.status)
	mux.HandleFunc("POST /control/v1/connection/verify", a.verify)
	mux.HandleFunc("POST /control/v1/provision", a.provision)

	mux.HandleFunc("GET /control/v1/faults", a.listFaults)
	mux.HandleFunc("POST /control/v1/faults", a.applyFault)
	mux.HandleFunc("DELETE /control/v1/faults/{id}", a.removeFault)
	mux.HandleFunc("POST /control/v1/platforms/{platform}/restore", a.restore)
	mux.HandleFunc("POST /control/v1/platforms/{platform}/actions/{action}", a.userAction)
	mux.HandleFunc("PUT /control/v1/traffic/{platform}", a.setTraffic)

	mux.HandleFunc("GET /control/v1/attacks", a.listAttacks)
	mux.HandleFunc("POST /control/v1/attacks", a.launchAttack)
	mux.HandleFunc("DELETE /control/v1/attacks/{id}", a.stopAttack)
	mux.HandleFunc("GET /control/v1/scripts", a.listScripts)
	mux.HandleFunc("POST /control/v1/scripts/{id}/run", a.runScript)
	mux.HandleFunc("POST /control/v1/runs/{id}/{op}", a.runControl)
	mux.HandleFunc("POST /control/v1/reset", a.reset)
	mux.HandleFunc("POST /control/v1/history", a.generateHistory)
	mux.HandleFunc("GET /control/v1/marathon", a.marathonStatus)
	mux.HandleFunc("POST /control/v1/marathon", a.startMarathon)
	mux.HandleFunc("DELETE /control/v1/marathon", a.stopMarathon)
	mux.HandleFunc("DELETE /control/v1/history", a.clearHistory)

	mux.HandleFunc("GET /control/v1/platforms", a.platforms)
	mux.HandleFunc("GET /control/v1/journal", a.journal)
	mux.HandleFunc("GET /{$}", a.console)
	mux.HandleFunc("GET /console/{file}", a.consoleAsset)
	mux.Handle("GET /sitios/", sites.Handler(a.siteGate))
	mux.HandleFunc("POST /sitios/api/{platform}/{action}", a.userAction)
	mux.HandleFunc("GET /control/v1/logs", a.listLogs)
	mux.HandleFunc("GET /control/v1/shipping", a.getShipping)
	mux.HandleFunc("PUT /control/v1/shipping", a.putShipping)
	mux.HandleFunc("POST /control/v1/logs/ship", a.shipLogs)
	return a.access.Wrap(mux)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	st := a.state
	a.mu.Unlock()
	platforms := []map[string]any{}
	for _, pl := range catalog.All() {
		ps, ok := st.Platforms[pl.Slug]
		item := map[string]any{
			"platform": pl.Slug, "name": pl.Name, "services": len(pl.Services),
			"provisioned": ok && ps.ProjectID != "" && len(ps.Components) == len(pl.Services), "project_id": ps.ProjectID,
		}
		if a.Traffic != nil {
			item["traffic_rps"] = a.Traffic.RPS(pl.Slug)
		}
		if a.Runtime != nil {
			item["active_faults"] = len(a.Runtime.Faults.List(pl.Slug))
		}
		if a.Profile.PortalPassword != "" {
			item["portal_email"] = a.portalEmail(pl)
			if a.Profile.SentinelWebURL != "" {
				item["portal_url"] = a.Profile.SentinelWebURL + "/portal"
			}
		}
		platforms = append(platforms, item)
	}
	out := map[string]any{"profile": a.Profile.Name, "sentinel_url": a.Profile.SentinelURL, "platforms": platforms, "bootstrap": a.BootStatus()}
	if a.Shipper != nil {
		out["shipping"] = map[string]any{"policy": a.Shipper.Policy(), "metrics": a.Shipper.Metrics()}
	}
	writeJSON(w, http.StatusOK, out)
}

type check struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func result(err error) check {
	if err != nil {
		return check{Detail: err.Error()}
	}
	return check{OK: true}
}

// verify prueba cada interfaz por separado, sin efectos secundarios: la
// ingesta se comprueba con un lote vacío (Sentinel valida el token antes que
// el lote: 400 = token válido, 401 = inválido).
func (a *API) verify(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out := map[string]check{}

	out["reachable"] = result(a.client.Reachable(ctx))
	if !out["reachable"].OK {
		writeJSON(w, http.StatusOK, out)
		return
	}
	if a.Profile.Email == "" || a.Profile.Password == "" {
		out["login"] = check{Detail: "faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD"}
	} else {
		out["login"] = result(a.client.Login(ctx, a.Profile.Email, a.Profile.Password))
	}
	if out["login"].OK {
		_, err := a.client.Projects(ctx)
		out["projects"] = result(err)
		_, err = a.client.Alerts(ctx)
		out["alerts"] = result(err)
		out["probe"] = a.checkProbes(ctx)
		out["nodes"] = a.checkNodes(ctx)
		out["portal"] = a.checkPortal(ctx)
		out["notifications"] = a.checkChannels(ctx)
	}

	ing := check{Detail: "sin token: ejecuta «Preparar escenario»"}
	for _, pl := range catalog.All() {
		tgt, ok := a.Target(pl.Slug)
		if !ok {
			continue
		}
		_, err := a.client.Ingest(ctx, tgt.Token, nil)
		// Lote vacío: 400 significa que el token fue aceptado.
		var he *sentinel.HTTPError
		if errors.As(err, &he) && he.Code == http.StatusBadRequest {
			err = nil
		}
		ing = result(err)
		break
	}
	out["ingest"] = ing
	writeJSON(w, http.StatusOK, out)
}

const sinProyectos = "sin proyectos: ejecuta «Preparar escenario»"

// checkProbes dice si el Poller de Sentinel llega al /health/deep de cada
// plataforma y al /health de cada servicio. Importa que responda, no que esté
// sano: con una falla activa el 503 también prueba que el sondeo llegó.
func (a *API) checkProbes(ctx context.Context) check {
	ids := a.projectIDs()
	if len(ids) == 0 {
		return check{Detail: sinProyectos}
	}
	probes, err := a.client.MonitorStatus(ctx)
	if err != nil {
		return result(err)
	}
	reached := map[string]bool{}
	porServicio := map[string]bool{}
	for _, p := range probes {
		if p.StatusCode == 0 {
			continue
		}
		if p.ComponentID == "" {
			reached[p.ProjectID] = true
		} else {
			porServicio[p.ComponentID] = true
		}
	}
	var sinProyecto, sinServicio []string
	for _, pl := range catalog.All() {
		if !reached[ids[pl.Slug]] {
			sinProyecto = append(sinProyecto, pl.Slug)
		}
		comps := a.platformState(pl.Slug).Components
		for _, s := range pl.Services {
			if id := comps[s.Slug]; id == "" || !porServicio[id] {
				sinServicio = append(sinServicio, pl.Slug+"/"+s.Slug)
			}
		}
	}
	sort.Strings(sinProyecto)
	sort.Strings(sinServicio)
	switch {
	case len(sinProyecto) > 0 && len(sinServicio) > 0:
		return check{Detail: "sin sondeo del sitio en " + strings.Join(sinProyecto, ", ") +
			"; sin sondeo por servicio en " + strings.Join(sinServicio, ", ")}
	case len(sinProyecto) > 0:
		return check{Detail: "sin sondeo del sitio en " + strings.Join(sinProyecto, ", ") + ": revisa DEMO_PUBLIC_URL (services_url)"}
	case len(sinServicio) > 0:
		return check{Detail: "sin sondeo por servicio en " + strings.Join(sinServicio, ", ") + ": espera el primer sondeo o revisa DEMO_PUBLIC_URL"}
	}
	return check{OK: true}
}

// checkNodes nombra los componentes que el mapa sigue mostrando en gris porque
// el primer sondeo por servicio todavía no llegó. Es lo que se mira tras el
// calentamiento de 10 min.
func (a *API) checkNodes(ctx context.Context) check {
	ids := a.projectIDs()
	if len(ids) == 0 {
		return check{Detail: sinProyectos}
	}
	var unknown []string
	for _, pl := range catalog.All() {
		comps, err := a.client.Components(ctx, ids[pl.Slug])
		if err != nil {
			return result(err)
		}
		for _, c := range comps {
			if c.Status == "" || c.Status == "unknown" {
				unknown = append(unknown, pl.Slug+"/"+c.Name)
			}
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return check{Detail: "nodos todavía en unknown (sin sondeo): " + strings.Join(unknown, ", ")}
	}
	return check{OK: true}
}

// checkChannels dice si cada plataforma avisa fuera del correo: en el stand el
// aviso se muestra en un teléfono.
func (a *API) checkChannels(ctx context.Context) check {
	ids := a.projectIDs()
	if len(ids) == 0 {
		return check{Detail: sinProyectos}
	}
	var missing []string
	for _, pl := range catalog.All() {
		ch, err := a.client.Channels(ctx, ids[pl.Slug])
		if err != nil {
			return result(err)
		}
		if !ch.Discord.Active && !ch.Slack.Active {
			missing = append(missing, pl.Slug)
		}
	}
	if len(missing) > 0 {
		return check{Detail: "solo correo en " + strings.Join(missing, ", ") + ": define DEMO_DISCORD_WEBHOOK o DEMO_SLACK_WEBHOOK y prepara el escenario"}
	}
	return check{OK: true}
}

func (a *API) provision(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if a.Profile.Email == "" || a.Profile.Password == "" {
		writeErr(w, http.StatusBadRequest, errors.New("faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD"))
		return
	}
	defer a.Journal.Add("provision", "", "aprovisionar escenario en Sentinel", "")
	n, err := a.doProvision(ctx)
	switch {
	case errors.Is(err, errSaveState):
		writeErr(w, http.StatusInternalServerError, err)
	case err != nil:
		writeErr(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, map[string]any{"provisioned": n})
	}
}

var errSaveState = errors.New("no se pudo guardar el estado")

// provisioner arma el aprovisionador con la configuración del perfil.
func (a *API) provisioner() *sentinel.Provisioner {
	return &sentinel.Provisioner{Client: a.client, ServicesURL: a.Profile.ServicesURL, IntervalSec: a.Profile.IntervalAt(a.now()),
		Discord: a.Profile.DiscordWebhook, Slack: a.Profile.SlackWebhook,
		PortalPassword: a.Profile.PortalPassword, PortalEmail: a.Profile.PortalEmail,
		WindowStart: a.Profile.WindowStart, WindowEnd: a.Profile.WindowEnd, EmailSeverities: a.Profile.EmailSeverities}
}

// doProvision inicia sesión y aprovisiona todas las plataformas. Trabaja sobre
// una copia del estado y solo toma a.mu para cambiarla: el envío de logs lee
// el estado por Target y no debe esperar a Sentinel.
func (a *API) doProvision(ctx context.Context) (int, error) {
	a.provMu.Lock()
	defer a.provMu.Unlock()
	if err := a.client.Login(ctx, a.Profile.Email, a.Profile.Password); err != nil {
		return 0, err
	}
	a.mu.Lock()
	work := a.state.Clone()
	a.mu.Unlock()

	st, perr := a.provisioner().Provision(ctx, work)
	a.mu.Lock()
	a.state = st
	a.mu.Unlock()
	// Se guarda aunque haya fallado: el token emitido no se puede recuperar.
	if err := sentinel.SaveState(a.Profile.StateDir, a.Profile.Name, st); err != nil {
		return 0, fmt.Errorf("%w: %v", errSaveState, err)
	}
	if perr != nil {
		return 0, perr
	}
	return len(st.Platforms), nil
}

/* ── Fallas, acciones y tráfico ─────────────────────────────────────────── */

func (a *API) needRuntime(w http.ResponseWriter) bool {
	if a.Runtime == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("los servicios simulados no están en ejecución"))
		return false
	}
	return true
}

// needIngest rechaza lo que provoca incidentes mientras Sentinel no reciba los
// logs de las plataformas afectadas (todas, si no se nombra ninguna): una falla
// sin ingesta no abre incidente y el ensayo se vería como si Sentinel no
// detectara nada. Un lote vacío no escribe: 400 es «token válido» y 401, «inválido».
func (a *API) needIngest(w http.ResponseWriter, r *http.Request, platforms ...string) bool {
	if len(platforms) == 0 {
		for _, pl := range catalog.All() {
			platforms = append(platforms, pl.Slug)
		}
	}
	for _, slug := range platforms {
		if _, known := catalog.BySlug(slug); !known {
			continue // una plataforma inexistente la rechaza quien la valida, con 400
		}
		tgt, ok := a.Target(slug)
		if !ok {
			writeErr(w, http.StatusConflict, fmt.Errorf("Sentinel no está preparado para %s: falta «Preparar escenario» o el token de ingesta", slug))
			return false
		}
		_, err := a.client.Ingest(r.Context(), tgt.Token, nil)
		var he *sentinel.HTTPError
		if errors.As(err, &he) && he.Code == http.StatusBadRequest {
			continue
		}
		motivo := "no responde"
		switch {
		case errors.As(err, &he) && he.Code == http.StatusUnauthorized:
			motivo = "rechazó el token de ingesta"
		case errors.As(err, &he):
			motivo = fmt.Sprintf("respondió %d", he.Code)
		}
		writeErr(w, http.StatusServiceUnavailable, fmt.Errorf("no se puede provocar una falla en %s: la ingesta de Sentinel %s", slug, motivo))
		return false
	}
	return true
}

func (a *API) listFaults(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	writeJSON(w, http.StatusOK, a.Runtime.Faults.List(r.URL.Query().Get("platform")))
}

func (a *API) applyFault(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	var in struct {
		Platform  string        `json:"platform"`
		Service   string        `json:"service"`
		Type      string        `json:"type"`
		Params    faults.Params `json:"params"`
		DurationS int           `json:"duration_s"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	if !a.needIngest(w, r, in.Platform) {
		return
	}
	f, err := a.Runtime.Faults.Apply(in.Platform, in.Service, in.Type, in.Params, in.DurationS, "console")
	if errors.Is(err, faults.ErrInvalid) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("falla", in.Platform, fmt.Sprintf("%s en %s", in.Type, in.Service), f.ID)
	writeJSON(w, http.StatusCreated, f)
}

func (a *API) removeFault(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	if !a.Runtime.Faults.Remove(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, errors.New("no existe esa falla"))
		return
	}
	a.Journal.Add("restaurar", "", "retirar falla", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restore(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	p := r.PathValue("platform")
	if _, ok := catalog.BySlug(p); !ok {
		writeErr(w, http.StatusNotFound, errors.New("plataforma desconocida"))
		return
	}
	n := a.Runtime.Faults.RestoreAll(p)
	a.Journal.Add("restaurar", p, "restaurar plataforma", fmt.Sprintf("%d fallas retiradas", n))
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

// userAction ejecuta una acción de usuario final contra el BFF, como lo haría
// el sitio: devuelve el código y el cuerpo que dio la cadena de servicios.
func (a *API) userAction(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	p, ok := catalog.BySlug(r.PathValue("platform"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("plataforma desconocida"))
		return
	}
	action := r.PathValue("action")
	if _, ok := p.Action(action); !ok {
		writeErr(w, http.StatusBadRequest, errors.New("acción inválida para la plataforma"))
		return
	}
	status, body, err := a.doUserAction(r.Context(), p, action)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, status, body)
}

func (a *API) setTraffic(w http.ResponseWriter, r *http.Request) {
	if a.Traffic == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el generador de tráfico no está activo"))
		return
	}
	var in struct {
		RPS float64 `json:"rps"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	if err := a.Traffic.Set(r.PathValue("platform"), in.RPS); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("trafico", r.PathValue("platform"), fmt.Sprintf("%.1f usuarios/s", in.RPS), "")
	writeJSON(w, http.StatusOK, map[string]float64{"rps": in.RPS})
}

/* ── Logs y envío ───────────────────────────────────────────────────────── */

func parseFilter(r *http.Request) (logs.Filter, error) {
	q := r.URL.Query()
	f := logs.Filter{Platform: q.Get("platform"), Service: q.Get("service"), MinLevel: q.Get("level")}
	for key, dst := range map[string]*time.Time{"since": &f.Since, "until": &f.Until} {
		if v := q.Get(key); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				return f, errors.New(key + " debe estar en formato RFC 3339")
			}
			*dst = t
		}
	}
	return f, nil
}

func (a *API) listLogs(w http.ResponseWriter, r *http.Request) {
	if a.Logger == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el registro de logs no está activo"))
		return
	}
	f, err := parseFilter(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	f.Limit = 200
	writeJSON(w, http.StatusOK, a.Logger.Recent(f))
}

func (a *API) getShipping(w http.ResponseWriter, r *http.Request) {
	if a.Shipper == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el envío de logs no está activo"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": a.Shipper.Policy(), "metrics": a.Shipper.Metrics()})
}

func (a *API) putShipping(w http.ResponseWriter, r *http.Request) {
	if a.Shipper == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el envío de logs no está activo"))
		return
	}
	p := a.Shipper.Policy() // los campos omitidos conservan su valor
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	if err := a.Shipper.SetPolicy(p); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("envio", "", "política de envío actualizada", fmt.Sprintf("%+v", a.Shipper.Policy()))
	writeJSON(w, http.StatusOK, a.Shipper.Policy())
}

// shipLogs reenvía logs ya guardados, filtrados por plataforma, servicio,
// nivel y rango; con dry_run solo cuenta.
func (a *API) shipLogs(w http.ResponseWriter, r *http.Request) {
	if a.Shipper == nil || a.Logger == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el envío de logs no está activo"))
		return
	}
	var in struct {
		Platform string `json:"platform"`
		Service  string `json:"service"`
		Level    string `json:"level_min"`
		Since    string `json:"since"`
		Until    string `json:"until"`
		DryRun   bool   `json:"dry_run"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	f := logs.Filter{Platform: in.Platform, Service: in.Service, MinLevel: in.Level}
	var err error
	if f.Since, err = parseTime(in.Since); err == nil {
		f.Until, err = parseTime(in.Until)
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("since y until deben estar en formato RFC 3339"))
		return
	}
	res, err := a.Shipper.ShipNow(r.Context(), a.Logger, f, in.DryRun)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusAccepted, res)
}

func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

/* ── Salud pública ──────────────────────────────────────────────────────── */

// healthz dice que el proceso vive. No mira a Sentinel ni las fallas activas:
// es el chequeo del contenedor, y reiniciarlo por una falla simulada sería absurdo.
func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// saludClient espera lo que tarde la cadena de un BFF degradado: la latencia
// que mide Sentinel debe ser la real.
var saludClient = &http.Client{Timeout: 15 * time.Second}

// platformState devuelve una copia de lo que se recuerda de una plataforma.
func (a *API) platformState(platform string) sentinel.PlatformState {
	a.mu.Lock()
	defer a.mu.Unlock()
	ps := a.state.Platforms[platform]
	comps := make(map[string]string, len(ps.Components))
	for slug, id := range ps.Components {
		comps[slug] = id
	}
	ps.Components = comps
	return ps
}

// salud entrega el /health/deep del BFF de una plataforma: es el monitoring_url
// que sondea Sentinel. Así el módulo expone un solo puerto, y el sondeo ve lo
// mismo que vería en el del BFF: 503 si la cadena falla o el BFF está caído, y
// la latencia real.
func (a *API) salud(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	pl, ok := catalog.BySlug(r.PathValue("platform"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("plataforma desconocida"))
		return
	}
	entry := pl.EntryService()
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, runtime.BaseURL(entry)+"/health/deep", nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := saludClient.Do(req)
	if err != nil {
		// Un BFF con la conexión rechazada es una plataforma caída hacia afuera.
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "down", "service": entry.Slug, "failing": []string{entry.Slug}})
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 1<<16))
}

// saludServicio entrega el /health superficial de un servicio concreto: es el
// health_url que sondea Sentinel por cada nodo del mapa. Una conexión rechazada
// es 503 (el servicio está caído) y un slug inexistente, 404.
func (a *API) saludServicio(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	pl, ok := catalog.BySlug(r.PathValue("platform"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("plataforma desconocida"))
		return
	}
	svc, ok := pl.Service(r.PathValue("service"))
	if !ok {
		writeErr(w, http.StatusNotFound, errors.New("servicio desconocido"))
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, runtime.BaseURL(svc)+"/health", nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	res, err := saludClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "down", "service": svc.Slug})
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 1<<16))
}
