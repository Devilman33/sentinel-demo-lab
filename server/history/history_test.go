package history

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"os"
	"strings"
	"testing"

	"demo-lab/server/config"
)

func TestGuardRechazaTodoLoQueNoSeaLocalConPermiso(t *testing.T) {
	ok := config.Profile{Name: "local", AllowDBBackfill: true}
	const local = "postgres://admin:x@localhost:5433/monitoring_db?sslmode=disable"
	cases := map[string]struct {
		p   config.Profile
		dsn string
	}{
		"perfil aws":            {config.Profile{Name: "aws", AllowDBBackfill: true}, local},
		"sin allow_db_backfill": {config.Profile{Name: "local"}, local},
		"sin DSN":               {ok, ""},
		"base remota":           {ok, "postgres://admin:x@db.example.com:5432/monitoring_db"},
		"DSN inválido":          {ok, "::::"},
	}
	for name, c := range cases {
		if err := Guard(c.p, c.dsn); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: esperaba ErrNotAllowed, fue %v", name, err)
		}
	}
	for _, dsn := range []string{local, "postgres://a:b@127.0.0.1/x", "postgres://a:b@[::1]:5433/x"} {
		if err := Guard(ok, dsn); err != nil {
			t.Errorf("%s debía permitirse: %v", dsn, err)
		}
	}
}

// TC-17: con el perfil aws, Open se niega antes de abrir ninguna conexión.
func TestOpenNoSeConectaFueraDeLocal(t *testing.T) {
	_, err := Open(context.Background(), config.Profile{Name: "aws", AllowDBBackfill: true}, "postgres://admin:x@localhost:5433/monitoring_db")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("esperaba ErrNotAllowed, fue %v", err)
	}
}

func newRNG(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed)) }

func TestPoissonYMttrSonRazonables(t *testing.T) {
	sc := scenarios[0]
	for _, trend := range []string{"mejora", "empeora"} {
		recent := mttrSeconds(newRNG(1), sc, 1, 30, trend)
		old := mttrSeconds(newRNG(1), sc, 30, 30, trend)
		if (trend == "mejora") != (recent < old) {
			t.Errorf("%s: reciente %d, antiguo %d", trend, recent, old)
		}
	}
}

// TC-16: contra una Postgres de Sentinel real (DEMO_DB_DSN) con el escenario ya aprovisionado.
// Se omite si no hay base. Repetir la generación no duplica datos.
func TestGenerarEsIdempotenteContraPostgres(t *testing.T) {
	dsn := os.Getenv("DEMO_DB_DSN")
	if dsn == "" {
		t.Skip("sin DEMO_DB_DSN")
	}
	p := config.Profile{Name: "local", AllowDBBackfill: true}
	db, err := Open(context.Background(), p, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() }) // se registra primero: corre al final, después de la limpieza
	// Una sola empresa: la base local puede tener varias empresas demo con los mismos nombres.
	projects := map[string]string{}
	rows, err := db.Query(`SELECT p.id, p.name FROM projects p WHERE p.name LIKE '%Don Federico'
	                        AND p.company_id = (SELECT company_id FROM projects WHERE name LIKE 'Mercado%Don Federico' ORDER BY created_at DESC LIMIT 1)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, name string
		_ = rows.Scan(&id, &name)
		switch {
		case strings.HasPrefix(name, "Mercado"):
			projects["mercado"] = id
		case strings.HasPrefix(name, "Banco"):
			projects["banco"] = id
		case strings.HasPrefix(name, "Cl"):
			projects["clinica"] = id
		}
	}
	rows.Close()
	if len(projects) != 3 {
		t.Skipf("el escenario no está aprovisionado (%d proyectos)", len(projects))
	}
	count := func() int {
		n := 0
		for _, id := range projects {
			var c int
			if err := db.QueryRow(`SELECT count(*) FROM master_incidents WHERE project_id=$1 AND description LIKE '%'||$2||'%'`, id, Marker).Scan(&c); err != nil {
				t.Fatal(err)
			}
			n += c
		}
		return n
	}
	t.Cleanup(func() {
		if _, err := Clear(context.Background(), db, values(projects)); err != nil {
			t.Errorf("limpieza: %v", err)
		}
	})
	o := Options{Days: 30, PerDay: 3, Seed: 7}
	r1, err := Generate(context.Background(), db, projects, o)
	if err != nil {
		t.Fatal(err)
	}
	n1 := count()
	r2, err := Generate(context.Background(), db, projects, o)
	if err != nil {
		t.Fatal(err)
	}
	if n1 != r1.Created || count() != n1 || r2.Created != r1.Created || r2.Cleared != n1 {
		t.Fatalf("repetir debía dar lo mismo: primera %d (en base %d), segunda %d (en base %d, borrados %d)", r1.Created, n1, r2.Created, count(), r2.Cleared)
	}
	if r1.Created < 30 {
		t.Errorf("pocos incidentes para 30 días: %d", r1.Created)
	}
	var withoutMttr int
	_ = db.QueryRow(`SELECT count(*) FROM master_incidents WHERE description LIKE '%'||$1||'%' AND (mttr_seconds IS NULL OR resolved_at <= started_at)`, Marker).Scan(&withoutMttr)
	if withoutMttr != 0 {
		t.Errorf("%d incidentes con MTTR inválido", withoutMttr)
	}
	var _ sql.NullString
}

// Los BFF del reinicio no entran al historial ni a la maratón, pero sí aportan
// su nota de cierre.
func TestScenarios_NoSorteaLosBFFPeroSiTieneSuNota(t *testing.T) {
	bff := map[string]bool{"storefront-web": true, "banca-en-linea": true, "portal-paciente": true}
	for _, s := range Scenarios() {
		if bff[s.Service] {
			t.Errorf("la maratón no debe sortear el BFF %s", s.Service)
		}
	}
	for platform, service := range map[string]string{"mercado": "storefront-web", "banco": "banca-en-linea", "clinica": "portal-paciente"} {
		if ClosingNote(platform, service, "Operativo") == "" {
			t.Errorf("%s/%s no tiene nota de cierre", platform, service)
		}
	}
}

func values(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
