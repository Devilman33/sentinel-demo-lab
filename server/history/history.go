// Package history genera historial sintético de incidentes resueltos para los
// tableros e informes (analíticas, MTTR, SLA, corpus del Copiloto). Como la API
// pública siempre registra con la hora actual, esto escribe directo en la
// Postgres de Sentinel, y por eso solo corre en el perfil local con
// allow_db_backfill y una base en localhost (spec §2.9, TC-16 y TC-17).
package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/url"
	"time"

	_ "github.com/lib/pq"

	"demo-lab/server/catalog"
	"demo-lab/server/config"
)

// Marker identifica en la descripción todo lo que crea este paquete; el reinicio
// y la repetición borran por él, así que nunca tocan incidentes reales.
const Marker = "[demo-lab:historial]"

// KnownSchema son las versiones de schema_migrations contra las que se probó.
var KnownSchema = map[int64]bool{32: true}

var ErrNotAllowed = errors.New("el historial sintético no está permitido")

func notAllowed(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrNotAllowed, fmt.Sprintf(format, a...))
}

// Guard comprueba, antes de abrir ninguna conexión, que el perfil y el DSN
// permitan escribir en la base de Sentinel.
func Guard(p config.Profile, dsn string) error {
	if p.Name != "local" {
		return notAllowed("solo existe en el perfil local (este es %q)", p.Name)
	}
	if !p.AllowDBBackfill {
		return notAllowed("el perfil no tiene allow_db_backfill")
	}
	if dsn == "" {
		return notAllowed("falta DEMO_DB_DSN")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return notAllowed("DEMO_DB_DSN no es una URL válida")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return notAllowed("la base debe estar en localhost (es %q)", u.Hostname())
	}
	return nil
}

// Options controla la generación.
type Options struct {
	Days          int // días hacia atrás (1-365)
	PerDay        int // incidentes por día, en promedio (1-20)
	Seed          uint64
	AssigneeEmail string // usuario que figura como responsable y actor
	MttrTrend     string // "mejora" (por omisión) | "empeora"
	SecurityShare float64
	Now           time.Time
}

type Result struct {
	Created    int            `json:"created"`
	ByPlatform map[string]int `json:"by_platform"`
	Security   int            `json:"security"`
	Cleared    int            `json:"cleared"`
	From       time.Time      `json:"from"`
	To         time.Time      `json:"to"`
}

// Open abre la base tras pasar Guard y comprobar la versión del esquema.
func Open(ctx context.Context, p config.Profile, dsn string) (*sql.DB, error) {
	if err := Guard(p, dsn); err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	var version int64
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		db.Close()
		return nil, fmt.Errorf("no se pudo leer schema_migrations: %w", err)
	}
	if dirty || !KnownSchema[version] {
		db.Close()
		return nil, notAllowed("la versión del esquema (%d) no es una de las probadas %v: revisa el módulo tras traer cambios de dev", version, known())
	}
	return db, nil
}

func known() []int64 {
	var out []int64
	for v := range KnownSchema {
		out = append(out, v)
	}
	return out
}

type target struct {
	platform  string
	projectID string
	svc       catalog.Service
	component string
}

// Clear borra lo generado antes en los proyectos dados.
func Clear(ctx context.Context, db *sql.DB, projectIDs []string) (int, error) {
	n := 0
	for _, id := range projectIDs {
		res, err := db.ExecContext(ctx, `DELETE FROM master_incidents WHERE project_id = $1 AND description LIKE '%' || $2 || '%'`, id, Marker)
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += int(c)
	}
	return n, nil
}

// Generate reemplaza el historial sintético de los proyectos por uno nuevo y
// determinista (misma semilla, mismos incidentes). Todo ocurre en una transacción.
func Generate(ctx context.Context, db *sql.DB, projects map[string]string, o Options) (Result, error) {
	if o.Days < 1 || o.Days > 365 || o.PerDay < 1 || o.PerDay > 20 {
		return Result{}, errors.New("days debe estar entre 1 y 365 e incidents_per_day entre 1 y 20")
	}
	if o.MttrTrend == "" {
		o.MttrTrend = "mejora"
	}
	if o.MttrTrend != "mejora" && o.MttrTrend != "empeora" {
		return Result{}, errors.New("mttr_trend debe ser mejora o empeora")
	}
	if o.SecurityShare == 0 {
		o.SecurityShare = 0.15
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	o.Now = o.Now.UTC()
	if len(projects) == 0 {
		return Result{}, errors.New("no hay proyectos aprovisionados: ejecuta «Preparar escenario»")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()

	var ids []string
	for _, id := range projects {
		ids = append(ids, id)
	}
	cleared, err := clearTx(ctx, tx, ids)
	if err != nil {
		return Result{}, err
	}

	var assignee, assigneeErr = lookupUser(ctx, tx, o.AssigneeEmail)
	_ = assigneeErr // sin usuario el historial queda sin responsable

	comps, err := loadComponents(ctx, tx, projects)
	if err != nil {
		return Result{}, err
	}
	rng := rand.New(rand.NewPCG(o.Seed, o.Seed^0x9e3779b97f4a7c15))
	res := Result{ByPlatform: map[string]int{}, Cleared: cleared, From: o.Now.AddDate(0, 0, -o.Days), To: o.Now}

	for day := o.Days; day >= 1; day-- {
		count := poisson(rng, float64(o.PerDay))
		for i := 0; i < count; i++ {
			sc := pickScenario(rng, o.SecurityShare)
			tgt, ok := comps[sc.platform+"/"+sc.service]
			if !ok {
				continue
			}
			start := o.Now.AddDate(0, 0, -day).Truncate(24 * time.Hour).Add(businessHour(rng))
			mttr := mttrSeconds(rng, sc, day, o.Days, o.MttrTrend)
			end := start.Add(time.Duration(mttr) * time.Second)
			if end.After(o.Now) {
				continue
			}
			if err := insertIncident(ctx, tx, rng, tgt, comps, sc, start, end, mttr, assignee); err != nil {
				return Result{}, err
			}
			res.Created++
			res.ByPlatform[sc.platform]++
			if sc.category == "Seguridad" {
				res.Security++
			}
		}
	}
	return res, tx.Commit()
}

func clearTx(ctx context.Context, tx *sql.Tx, ids []string) (int, error) {
	n := 0
	for _, id := range ids {
		r, err := tx.ExecContext(ctx, `DELETE FROM master_incidents WHERE project_id = $1 AND description LIKE '%' || $2 || '%'`, id, Marker)
		if err != nil {
			return n, err
		}
		c, _ := r.RowsAffected()
		n += int(c)
	}
	return n, nil
}

func lookupUser(ctx context.Context, tx *sql.Tx, email string) (string, error) {
	if email == "" {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE lower(email) = lower($1)`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

type compRef struct {
	target
	id string
}

func loadComponents(ctx context.Context, tx *sql.Tx, projects map[string]string) (map[string]compRef, error) {
	out := map[string]compRef{}
	for platform, projectID := range projects {
		rows, err := tx.QueryContext(ctx, `SELECT id, name FROM components WHERE project_id = $1`, projectID)
		if err != nil {
			return nil, err
		}
		p, _ := catalog.BySlug(platform)
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, err
			}
			if s, ok := p.Service(name); ok {
				out[platform+"/"+name] = compRef{target: target{platform: platform, projectID: projectID, svc: s, component: id}, id: id}
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func insertIncident(ctx context.Context, tx *sql.Tx, rng *rand.Rand, t compRef, comps map[string]compRef, sc scenario, start, end time.Time, mttr int, assignee string) error {
	notes := sc.notes[rng.IntN(len(sc.notes))]
	firstResp := start.Add(time.Duration(2+rng.IntN(13)) * time.Minute)
	if firstResp.After(end) {
		firstResp = start.Add(time.Duration(mttr/2) * time.Second)
	}
	var id string
	err := tx.QueryRowContext(ctx,
		`INSERT INTO master_incidents (project_id, title, description, status, severity, category, resolution_notes,
		                               started_at, resolved_at, first_response_at, mttr_seconds, assignee_id)
		 VALUES ($1, $2, $3, 'resuelto', $4::severity, $5::incident_category, $6, $7, $8, $9, $10, NULLIF($11, '')::uuid)
		 RETURNING id`,
		t.projectID, sc.title, sc.description+" "+Marker, sc.severity, sc.category, notes, start, end, firstResp, mttr, assignee,
	).Scan(&id)
	if err != nil {
		return err
	}
	actor := nullable(assignee)
	steps := []struct {
		kind, from, to string
		at             time.Time
	}{
		{"creacion", "", "activo", start},
		{"asignacion", "", "en_progreso", firstResp},
		{"cambio_estado", "en_progreso", "resuelto", end},
	}
	for _, s := range steps {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO incident_timeline (incident_id, actor_user_id, kind, from_value, to_value, created_at)
			 VALUES ($1, $2::uuid, $3::timeline_kind, NULLIF($4, ''), $5, $6)`,
			id, actor, s.kind, s.from, s.to, s.at); err != nil {
			return err
		}
	}
	if assignee != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO incident_assignees (incident_id, user_id, assigned_by, assigned_at) VALUES ($1, $2, $2, $3)
			 ON CONFLICT DO NOTHING`, id, assignee, firstResp); err != nil {
			return err
		}
	}
	// Impacto: el servicio origen y los que dependen de él (aguas arriba) como afectados.
	p, _ := catalog.BySlug(t.platform)
	affected := upstream(p, sc.service)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO incident_impacts (incident_id, component_id, role, propagation_depth, detected_at)
		 VALUES ($1, $2, 'origen', 0, $3)`, id, t.id, start); err != nil {
		return err
	}
	for i, svc := range affected {
		c, ok := comps[t.platform+"/"+svc]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO incident_impacts (incident_id, component_id, role, propagation_depth, detected_at)
			 VALUES ($1, $2, 'afectado', $3, $4) ON CONFLICT DO NOTHING`, id, c.id, i+1, start.Add(time.Duration(i+1)*20*time.Second)); err != nil {
			return err
		}
	}
	// Ventana de indisponibilidad del origen: alimenta la disponibilidad/SLA por servicio.
	if sc.downtime {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO downtime_windows (component_id, incident_id, started_at, ended_at, seconds, planned)
			 VALUES ($1, $2, $3, $4, $5, false)`, t.id, id, start, end, mttr)
	}
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// upstream devuelve los servicios que dependen, directa o indirectamente, de svc.
func upstream(p catalog.Platform, svc string) []string {
	var out []string
	seen := map[string]bool{svc: true}
	frontier := []string{svc}
	for len(frontier) > 0 {
		var next []string
		for _, cur := range frontier {
			for _, s := range p.Services {
				if seen[s.Slug] {
					continue
				}
				for _, d := range s.DependsOn {
					if d == cur {
						seen[s.Slug] = true
						out = append(out, s.Slug)
						next = append(next, s.Slug)
						break
					}
				}
			}
		}
		frontier = next
	}
	return out
}

// poisson da una cuenta con media lambda (algoritmo de Knuth).
func poisson(rng *rand.Rand, lambda float64) int {
	l, k, p := math.Exp(-lambda), 0, 1.0
	for {
		k++
		p *= rng.Float64()
		if p <= l {
			return k - 1
		}
	}
}

// businessHour sesga el inicio hacia el horario laboral (8 a 20 h).
func businessHour(rng *rand.Rand) time.Duration {
	var h float64
	if rng.Float64() < 0.8 {
		h = 8 + rng.Float64()*12
	} else {
		h = rng.Float64() * 24
	}
	return time.Duration(h * float64(time.Hour))
}

// mttrSeconds sigue una distribución lognormal alrededor de la mediana del
// escenario; con tendencia "mejora" los días recientes tardan menos.
func mttrSeconds(rng *rand.Rand, sc scenario, day, days int, trend string) int {
	age := float64(day) / float64(days) // 1 = el más antiguo, 0 = el más reciente
	factor := 0.7 + 0.6*age             // mejora: 0.7x hoy, 1.3x al inicio
	if trend == "empeora" {
		factor = 1.3 - 0.6*age
	}
	v := float64(sc.medianMin*60) * factor * math.Exp(rng.NormFloat64()*0.45)
	return int(math.Max(float64(5*60), math.Min(v, 8*3600)))
}
