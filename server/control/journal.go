package control

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry es una acción del director, para reconstruir el ensayo después.
type Entry struct {
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
	Platform string    `json:"platform,omitempty"`
	Summary  string    `json:"summary"`
	Detail   string    `json:"detail,omitempty"`
}

// Journal es la bitácora de la sesión: en memoria y, si tiene ruta, en un NDJSON.
type Journal struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

func NewJournal(path string) *Journal { return &Journal{path: path} }

// Add anota una acción. Es seguro llamarlo sobre un *Journal nil (pruebas).
func (j *Journal) Add(kind, platform, summary, detail string) {
	if j == nil {
		return
	}
	e := Entry{Time: time.Now().UTC(), Kind: kind, Platform: platform, Summary: summary, Detail: detail}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, e)
	if j.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(j.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	raw, _ := json.Marshal(e)
	_, _ = f.Write(append(raw, '\n'))
}

func (j *Journal) All() []Entry {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]Entry{}, j.entries...)
}

// journal devuelve la bitácora en JSON, o en CSV con ?format=csv.
func (a *API) journal(w http.ResponseWriter, r *http.Request) {
	entries := a.Journal.All()
	if entries == nil {
		entries = []Entry{}
	}
	if r.URL.Query().Get("format") != "csv" {
		writeJSON(w, http.StatusOK, entries)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="bitacora-demo-lab.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"hora_utc", "tipo", "plataforma", "resumen", "detalle"})
	for _, e := range entries {
		_ = cw.Write([]string{e.Time.Format(time.RFC3339), e.Kind, e.Platform, e.Summary, e.Detail})
	}
	cw.Flush()
}
