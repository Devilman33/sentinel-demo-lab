package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"demo-lab/server/attacks"
	"demo-lab/server/catalog"
	"demo-lab/server/faults"
	"demo-lab/server/history"
	"demo-lab/server/runtime"
	"demo-lab/server/scripts"
	"demo-lab/server/sentinel"
)

/* ── Ataques ────────────────────────────────────────────────────────────── */

func (a *API) listAttacks(w http.ResponseWriter, r *http.Request) {
	if a.Attacks == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el generador de ataques no está activo"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kinds": attacks.Kinds(), "attacks": a.Attacks.List()})
}

func (a *API) launchAttack(w http.ResponseWriter, r *http.Request) {
	if a.Attacks == nil || !a.needRuntime(w) {
		if a.Attacks == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("el generador de ataques no está activo"))
		}
		return
	}
	var in struct {
		Platform  string `json:"platform"`
		Target    string `json:"target"`
		Kind      string `json:"kind"`
		Intensity int    `json:"intensity"`
		DurationS int    `json:"duration_s"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	if !a.needIngest(w, r, in.Platform) {
		return
	}
	at, err := a.Attacks.Launch(in.Platform, in.Target, in.Kind, in.Intensity, in.DurationS, "console")
	if errors.Is(err, attacks.ErrInvalid) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("ataque", in.Platform, fmt.Sprintf("%s contra %s", in.Kind, in.Target), at.ID)
	writeJSON(w, http.StatusCreated, at)
}

func (a *API) stopAttack(w http.ResponseWriter, r *http.Request) {
	if a.Attacks == nil || !a.Attacks.Stop(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, errors.New("no existe ese ataque"))
		return
	}
	a.Journal.Add("ataque", "", "detener ataque", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

/* ── Guiones ────────────────────────────────────────────────────────────── */

func (a *API) listScripts(w http.ResponseWriter, r *http.Request) {
	if a.Scripts == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el intérprete de guiones no está activo"))
		return
	}
	type item struct {
		scripts.Script
		DurationS int `json:"duration_s"`
	}
	var out []item
	for _, s := range a.Scripts.Scripts() {
		out = append(out, item{s, int(s.Duration().Seconds())})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scripts": out, "runs": a.Scripts.List()})
}

func (a *API) runScript(w http.ResponseWriter, r *http.Request) {
	if a.Scripts == nil || !a.needRuntime(w) {
		if a.Scripts == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("el intérprete de guiones no está activo"))
		}
		return
	}
	if !a.needIngest(w, r) {
		return
	}
	run, err := a.Scripts.Start(r.PathValue("id"))
	if err != nil {
		writeErr(w, scriptStatus(err), err)
		return
	}
	a.Journal.Add("guion", run.Platform, "iniciar «"+run.Title+"»", run.ID)
	writeJSON(w, http.StatusCreated, run)
}

func scriptStatus(err error) int {
	switch {
	case errors.Is(err, scripts.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, scripts.ErrState):
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func (a *API) runControl(w http.ResponseWriter, r *http.Request) {
	if a.Scripts == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("el intérprete de guiones no está activo"))
		return
	}
	id, op := r.PathValue("id"), r.PathValue("op")
	var (
		run scripts.Run
		err error
	)
	switch op {
	case "pause":
		run, err = a.Scripts.Pause(id)
	case "resume":
		run, err = a.Scripts.Resume(id)
	case "abort":
		run, err = a.Scripts.Abort(id)
	default:
		writeErr(w, http.StatusNotFound, errors.New("operación desconocida"))
		return
	}
	if err != nil {
		writeErr(w, scriptStatus(err), err)
		return
	}
	a.Journal.Add("guion", run.Platform, op+" «"+run.Title+"»", id)
	writeJSON(w, http.StatusOK, run)
}

// ScriptActions entrega lo que un guion puede hacer sobre el módulo.
func (a *API) ScriptActions() scripts.Actions { return scriptActions{a} }

// scriptActions deja que un guion actúe sobre el módulo sin duplicar la lógica de la API.
type scriptActions struct{ a *API }

func (s scriptActions) SetTraffic(platform string, rps float64) error {
	if s.a.Traffic == nil {
		return errors.New("el generador de tráfico no está activo")
	}
	s.a.Journal.Add("trafico", platform, fmt.Sprintf("%.1f usuarios/s (guion)", rps), "")
	return s.a.Traffic.Set(platform, rps)
}

func (s scriptActions) ApplyFault(platform, service, typ string, p faults.Params, durationS int) error {
	f, err := s.a.Runtime.Faults.Apply(platform, service, typ, p, durationS, "script")
	if err == nil {
		s.a.Journal.Add("falla", platform, fmt.Sprintf("%s en %s (guion)", typ, service), f.ID)
	}
	return err
}

func (s scriptActions) Attack(platform, target, kind string, intensity, durationS int) error {
	if s.a.Attacks == nil {
		return errors.New("el generador de ataques no está activo")
	}
	at, err := s.a.Attacks.Launch(platform, target, kind, intensity, durationS, "script")
	if err == nil {
		s.a.Journal.Add("ataque", platform, fmt.Sprintf("%s contra %s (guion)", kind, target), at.ID)
	}
	return err
}

func (s scriptActions) UserAction(platform, action string, times int) {
	p, ok := catalog.BySlug(platform)
	if !ok {
		return
	}
	go func() {
		for i := 0; i < times; i++ {
			_, _, _ = s.a.doUserAction(context.Background(), p, action)
			time.Sleep(300 * time.Millisecond)
		}
	}()
}

func (s scriptActions) RestoreAll(platform string) {
	n := s.a.Runtime.Faults.RestoreAll(platform)
	if s.a.Attacks != nil {
		s.a.Attacks.StopAll(platform)
	}
	s.a.Journal.Add("restaurar", platform, "restaurar plataforma (guion)", fmt.Sprintf("%d fallas retiradas", n))
}

// doUserAction ejecuta una acción de usuario final contra el BFF, como lo haría el sitio.
func (a *API) doUserAction(ctx context.Context, p catalog.Platform, action string) (int, map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, runtime.BaseURL(p.EntryService())+"/v1/action/"+action, nil)
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body, nil
}

/* ── Reinicio ───────────────────────────────────────────────────────────── */

const resetNotes = "Cierre automático al reiniciar el escenario de demo-lab."

// resetSnapshot guarda lo que estaba activo cuando se pulsó «Reiniciar». El
// fallo o ataque se retira enseguida, pero sus incidentes pueden llegar a
// Sentinel durante los 90 s siguientes; con esto se cierran con la nota de la
// causa real y no con la genérica (V-21, D-17).
type resetSnapshot struct {
	faults  []faults.Fault
	attacks []attacks.Attack
}

func (a *API) snapshotActive() resetSnapshot {
	var snap resetSnapshot
	if a.Runtime != nil {
		snap.faults = a.Runtime.Faults.List("")
	}
	if a.Attacks != nil {
		for _, at := range a.Attacks.List() {
			if !at.Done {
				snap.attacks = append(snap.attacks, at)
			}
		}
	}
	return snap
}

// reset deja el escenario como al inicio de un ensayo: sin fallas, ataques ni
// guiones, con el tráfico de vuelta en el valor base (DEMO_TRAFFIC_RPS) y con
// los incidentes de los proyectos demo cerrados, para que el ensayo siguiente
// no se sume al anterior (ventana de 900 s de api-ia). Dejar el tráfico en 0
// apagaría los sitios y el dashboard entre visitantes. Si Sentinel no responde,
// lo local queda igual de reiniciado y el error se informa en incidents_error.
func (a *API) reset(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	var out struct {
		Runs           int    `json:"runs"`
		Attacks        int    `json:"attacks"`
		Faults         int    `json:"faults"`
		Incidents      int    `json:"incidents"`
		IncidentsError string `json:"incidents_error,omitempty"`
	}
	snap := a.snapshotActive()
	if a.Marathon != nil {
		a.Marathon.Stop()
	}
	if a.Scripts != nil {
		out.Runs = a.Scripts.AbortAll()
	}
	if a.Attacks != nil {
		out.Attacks = a.Attacks.StopAll("")
	}
	base := a.Profile.TrafficRPS
	if base < 0 {
		base = 0
	}
	for _, p := range catalog.All() {
		out.Faults += a.Runtime.Faults.RestoreAll(p.Slug)
		if a.Traffic != nil {
			_ = a.Traffic.Set(p.Slug, base)
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	closed, err := a.closeIncidents(ctx, snap)
	out.Incidents = closed
	if err != nil {
		out.IncidentsError = err.Error()
	}
	a.Journal.Add("reinicio", "", "reiniciar el escenario", fmt.Sprintf("%+v", out))
	go a.settleIncidents(snap)
	writeJSON(w, http.StatusOK, out)
}

// settleIncidents repite el cierre durante un rato: lo que ya estaba en la cola
// de Sentinel llega después del reinicio y abre incidentes nuevos. Se detiene si
// empieza otro ensayo, porque desde ahí un incidente nuevo es legítimo. Conserva
// el snapshot del reinicio para que esos cierres también expliquen la causa.
func (a *API) settleIncidents(snap resetSnapshot) {
	for end := time.Now().Add(a.settleFor); time.Now().Before(end); {
		time.Sleep(a.settleEvery)
		if a.rehearsing() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		closed, _ := a.closeIncidents(ctx, snap)
		cancel()
		if closed > 0 {
			a.Journal.Add("reinicio", "", "cerrar incidentes tardíos", strconv.Itoa(closed))
		}
	}
}

// rehearsing dice si hay fallas o ataques en curso. El tráfico no cuenta: tras
// el reinicio queda en el valor base y debe seguir cerrándose lo que llegue
// tarde desde la cola de Sentinel.
func (a *API) rehearsing() bool {
	if len(a.Runtime.Faults.List("")) > 0 {
		return true
	}
	if a.Attacks != nil {
		for _, at := range a.Attacks.List() {
			if !at.Done {
				return true
			}
		}
	}
	return false
}

// closeIncidents cierra por la API los incidentes abiertos de los proyectos
// demo. Sentinel solo admite cerrar como Operativo o Seguridad. La nota sale
// del snapshot del reinicio: la falla o ataque activo, el escenario del servicio
// o, como último recurso, la nota neutra.
func (a *API) closeIncidents(ctx context.Context, snap resetSnapshot) (int, error) {
	platforms := map[string]catalog.Platform{}
	for slug, id := range a.projectIDs() {
		if pl, ok := catalog.BySlug(slug); ok {
			platforms[id] = pl
		}
	}
	if len(platforms) == 0 {
		return 0, nil
	}
	d := marathonDeps{a}
	alerts, err := d.Alerts(ctx)
	if err != nil {
		return 0, err
	}
	closed := 0
	var errs []error
	for _, al := range alerts {
		pl, ok := platforms[al.ProjectID]
		if !ok || al.Status == "resuelto" {
			continue
		}
		category := "Operativo"
		if al.Category == "Seguridad" {
			category = al.Category
		}
		err := d.Resolve(ctx, al.ID, snap.closingNote(pl, al, category), category)
		// El incidente del sondeo se cierra solo al recuperarse la plataforma y
		// puede ganarle a este cierre.
		if err != nil && strings.Contains(err.Error(), "ya está resuelto") {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		closed++
	}
	return closed, errors.Join(errs...)
}

// closingNote elige la nota según D-17: (1) la falla o ataque activo en el
// servicio que nombra el incidente; (2) la única falla o ataque de la
// plataforma, que es la causa de una cascada; (3) el escenario de ese servicio;
// (4) la nota neutra.
func (snap resetSnapshot) closingNote(pl catalog.Platform, al sentinel.Alert, category string) string {
	svc := namedService(pl, al)
	if svc != "" {
		if category == "Seguridad" {
			if n := snap.attackNote(pl.Slug, svc); n != "" {
				return n
			}
			if n := snap.faultNote(pl.Slug, svc); n != "" {
				return n
			}
		} else {
			if n := snap.faultNote(pl.Slug, svc); n != "" {
				return n
			}
			if n := snap.attackNote(pl.Slug, svc); n != "" {
				return n
			}
		}
	}

	// La única falla o el único ataque de la plataforma explica una cascada que
	// nombra a otro servicio. Con dos o más no se puede atribuir sin adivinar.
	var unica []string
	for _, f := range snap.faults {
		if f.Platform == pl.Slug {
			if n := faultClosingNote(f); n != "" {
				unica = append(unica, n)
			}
		}
	}
	for _, at := range snap.attacks {
		if at.Platform == pl.Slug {
			if n := attacks.ClosingNote(at.Kind); n != "" {
				unica = append(unica, n)
			}
		}
	}
	if len(unica) == 1 {
		return unica[0]
	}
	if svc != "" {
		if n := history.ClosingNote(pl.Slug, svc, category); n != "" {
			return n
		}
	}
	return resetNotes
}

// namedService encuentra el servicio del proyecto que aparece en el título o en
// la causa raíz del incidente. El título manda: la causa raíz puede nombrar
// varios servicios de la cascada.
func namedService(pl catalog.Platform, al sentinel.Alert) string {
	for _, text := range []string{al.Title, al.RootCause} {
		lower := strings.ToLower(text)
		for _, s := range pl.Services {
			if strings.Contains(lower, s.Slug) {
				return s.Slug
			}
		}
	}
	return ""
}

func (snap resetSnapshot) faultNote(platform, service string) string {
	for _, f := range snap.faults {
		if f.Platform == platform && f.Service == service {
			return faultClosingNote(f)
		}
	}
	return ""
}

func (snap resetSnapshot) attackNote(platform, service string) string {
	for _, at := range snap.attacks {
		if at.Platform == platform && at.Target == service {
			return attacks.ClosingNote(at.Kind)
		}
	}
	return ""
}

// faultClosingNote usa la nota del escenario operativo del servicio afectado.
// Todos los servicios del catálogo tienen uno, incluidos los BFF.
func faultClosingNote(f faults.Fault) string {
	return history.ClosingNote(f.Platform, f.Service, "Operativo")
}
