package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"demo-lab/server/faults"
	"demo-lab/server/marathon"
	"demo-lab/server/sentinel"
)

// marathonDeps conecta la maratón con el módulo y con la API pública de Sentinel.
type marathonDeps struct{ a *API }

func (d marathonDeps) ApplyFault(platform, service, typ string, p faults.Params) error {
	f, err := d.a.Runtime.Faults.Apply(platform, service, typ, p, 0, "marathon")
	if err == nil {
		d.a.Journal.Add("falla", platform, fmt.Sprintf("%s en %s (maratón)", typ, service), f.ID)
	}
	return err
}

func (d marathonDeps) Attack(platform, target, kind string, intensity, durationS int) error {
	if d.a.Attacks == nil {
		return errors.New("el generador de ataques no está activo")
	}
	at, err := d.a.Attacks.Launch(platform, target, kind, intensity, durationS, "marathon")
	if err == nil {
		d.a.Journal.Add("ataque", platform, fmt.Sprintf("%s contra %s (maratón)", kind, target), at.ID)
	}
	return err
}

func (d marathonDeps) SetTraffic(platform string, rps float64) error {
	if d.a.Traffic == nil {
		return errors.New("el generador de tráfico no está activo")
	}
	return d.a.Traffic.Set(platform, rps)
}

func (d marathonDeps) RestorePlatform(platform string) {
	d.a.Runtime.Faults.RestoreAll(platform)
	if d.a.Attacks != nil {
		d.a.Attacks.StopAll(platform)
	}
}

func (d marathonDeps) ProjectID(platform string) string { return d.a.projectIDs()[platform] }

// retry renueva la sesión una vez si Sentinel responde 401 (la maratón puede durar más que el token).
func (d marathonDeps) retry(ctx context.Context, fn func() error) error {
	err := fn()
	var he *sentinel.HTTPError
	if errors.As(err, &he) && he.Code == http.StatusUnauthorized {
		if lerr := d.a.client.Login(ctx, d.a.Profile.Email, d.a.Profile.Password); lerr != nil {
			return lerr
		}
		return fn()
	}
	return err
}

func (d marathonDeps) Alerts(ctx context.Context) (out []sentinel.Alert, err error) {
	err = d.retry(ctx, func() (e error) { out, e = d.a.client.Alerts(ctx); return })
	return
}

func (d marathonDeps) Assign(ctx context.Context, alertID string) error {
	uid := d.a.client.UserID()
	if uid == "" {
		return errors.New("no se pudo leer el usuario del token")
	}
	return d.retry(ctx, func() error { return d.a.client.Assign(ctx, alertID, []string{uid}) })
}

func (d marathonDeps) Resolve(ctx context.Context, alertID, notes, category string) error {
	return d.retry(ctx, func() error { return d.a.client.Resolve(ctx, alertID, notes, category) })
}

// NewMarathon arma la maratón sobre este módulo.
func (a *API) NewMarathon() *marathon.Runner { return marathon.New(marathonDeps{a}) }

func (a *API) startMarathon(w http.ResponseWriter, r *http.Request) {
	if a.Marathon == nil || !a.needRuntime(w) {
		if a.Marathon == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("la maratón no está activa"))
		}
		return
	}
	var in struct {
		Hours      float64 `json:"hours"`
		CadenceMin float64 `json:"cadence_min"`
		MttrMinS   int     `json:"mttr_min_s"`
		MttrMaxS   int     `json:"mttr_max_s"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("cuerpo inválido"))
		return
	}
	if a.Profile.Email == "" || a.Profile.Password == "" {
		writeErr(w, http.StatusBadRequest, errors.New("faltan DEMO_SENTINEL_EMAIL o DEMO_SENTINEL_PASSWORD"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := a.client.Login(ctx, a.Profile.Email, a.Profile.Password); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	if !a.needIngest(w, r) {
		return
	}
	cfg := marathon.Config{
		Hours: in.Hours, CadenceMin: in.CadenceMin,
		MttrMin: time.Duration(in.MttrMinS) * time.Second, MttrMax: time.Duration(in.MttrMaxS) * time.Second,
	}
	switch err := a.Marathon.Start(cfg); {
	case errors.Is(err, marathon.ErrRunning):
		writeErr(w, http.StatusConflict, err)
		return
	case err != nil:
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	a.Journal.Add("maraton", "", fmt.Sprintf("iniciar maratón de %.1f h, un ciclo cada %.1f min", in.Hours, in.CadenceMin), "")
	writeJSON(w, http.StatusCreated, a.Marathon.Status())
}

func (a *API) marathonStatus(w http.ResponseWriter, r *http.Request) {
	if a.Marathon == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("la maratón no está activa"))
		return
	}
	writeJSON(w, http.StatusOK, a.Marathon.Status())
}

func (a *API) stopMarathon(w http.ResponseWriter, r *http.Request) {
	if a.Marathon == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("la maratón no está activa"))
		return
	}
	a.Marathon.Stop()
	a.Journal.Add("maraton", "", "detener maratón", "")
	writeJSON(w, http.StatusOK, a.Marathon.Status())
}
