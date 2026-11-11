package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"demo-lab/server/history"
)

// projectIDs devuelve los proyectos aprovisionados por plataforma.
func (a *API) projectIDs() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]string{}
	for slug, ps := range a.state.Platforms {
		if ps.ProjectID != "" {
			out[slug] = ps.ProjectID
		}
	}
	return out
}

// openHistoryDB aplica las condiciones del perfil antes de abrir la base; si
// alguna falla responde 409 y no se conecta.
func (a *API) openHistoryDB(w http.ResponseWriter, r *http.Request) (*sql.DB, bool) {
	db, err := history.Open(r.Context(), a.Profile, a.Profile.DBDSN)
	if errors.Is(err, history.ErrNotAllowed) {
		writeErr(w, http.StatusConflict, err)
		return nil, false
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return nil, false
	}
	return db, true
}

func (a *API) generateHistory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days            int    `json:"days"`
		IncidentsPerDay int    `json:"incidents_per_day"`
		MttrTrend       string `json:"mttr_profile"`
		Seed            uint64 `json:"seed"`
	}
	in.Days, in.IncidentsPerDay = 30, 3
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
			return
		}
	}
	db, ok := a.openHistoryDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if in.Seed == 0 {
		in.Seed = 20261028
	}
	res, err := history.Generate(ctx, db, a.projectIDs(), history.Options{
		Days: in.Days, PerDay: in.IncidentsPerDay, MttrTrend: in.MttrTrend, Seed: in.Seed, AssigneeEmail: a.Profile.Email,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("historial", "", fmt.Sprintf("%d incidentes resueltos en %d días", res.Created, in.Days), "")
	writeJSON(w, http.StatusCreated, res)
}

func (a *API) clearHistory(w http.ResponseWriter, r *http.Request) {
	db, ok := a.openHistoryDB(w, r)
	if !ok {
		return
	}
	defer db.Close()
	var ids []string
	for _, id := range a.projectIDs() {
		ids = append(ids, id)
	}
	n, err := history.Clear(r.Context(), db, ids)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	a.Journal.Add("historial", "", fmt.Sprintf("historial borrado (%d incidentes)", n), "")
	writeJSON(w, http.StatusOK, map[string]int{"deleted": n})
}
