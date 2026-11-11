package control

import (
	"net/http"
	"time"

	"demo-lab/server/catalog"
	"demo-lab/server/logs"
)

type serviceView struct {
	Slug        string   `json:"slug"`
	Role        string   `json:"role"`
	Criticality string   `json:"criticality"`
	LogFormat   string   `json:"log_format"`
	DependsOn   []string `json:"depends_on"`
	State       string   `json:"state"` // ok | degradado | caido
	Faults      []any    `json:"faults"`
	Requests    int      `json:"requests_last_min"`
	Errors      int      `json:"errors_last_min"`
}

type activity struct{ requests, errors int }

// lastMinute cuenta peticiones y errores 5xx por servicio según su propio log.
func (a *API) lastMinute() map[string]activity {
	out := map[string]activity{}
	if a.Logger == nil {
		return out
	}
	for _, rec := range a.Logger.Recent(logs.Filter{Since: time.Now().Add(-time.Minute)}) {
		if rec.HTTP == nil {
			continue
		}
		k := rec.Platform + "/" + rec.Service
		v := out[k]
		v.requests++
		if rec.HTTP.Status >= 500 {
			v.errors++
		}
		out[k] = v
	}
	return out
}

// platforms da el estado de cada servicio: el que dejan las fallas activas y
// la actividad del último minuto.
func (a *API) platforms(w http.ResponseWriter, r *http.Request) {
	if !a.needRuntime(w) {
		return
	}
	recent := a.lastMinute()
	active := a.Runtime.Faults.List("")

	out := []map[string]any{}
	for _, pl := range catalog.All() {
		views := []serviceView{}
		worst := "ok"
		for _, s := range pl.Services {
			v := serviceView{Slug: s.Slug, Role: s.Role, Criticality: s.Criticality, LogFormat: s.LogFormat, DependsOn: s.DependsOn, State: "ok", Faults: []any{}}
			ef := a.Runtime.Faults.EffectFor(pl.Slug, s.Slug)
			switch {
			case ef.Down:
				v.State = "caido"
			case ef.LatencyMs > 0 || ef.ErrorRate > 0 || ef.MaxConcurrency > 0 || len(ef.DepLatencyMs) > 0:
				v.State = "degradado"
			}
			for _, f := range active {
				if f.Platform == pl.Slug && f.Service == s.Slug {
					v.Faults = append(v.Faults, f)
				}
			}
			act := recent[pl.Slug+"/"+s.Slug]
			v.Requests, v.Errors = act.requests, act.errors
			if v.State == "caido" || (v.State == "degradado" && worst == "ok") {
				worst = v.State
			}
			views = append(views, v)
		}
		actions := []string{}
		for _, ac := range pl.Actions {
			actions = append(actions, ac.Name)
		}
		item := map[string]any{"platform": pl.Slug, "name": pl.Name, "state": worst, "services": views, "actions": actions}
		if a.Traffic != nil {
			item["traffic_rps"] = a.Traffic.RPS(pl.Slug)
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}
