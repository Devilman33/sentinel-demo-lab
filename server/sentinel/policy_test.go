package sentinel_test

import (
	"context"
	"testing"

	"demo-lab/server/catalog"
	"demo-lab/server/sentinel"
	"demo-lab/server/sentinel/fake"
)

func aprovisionar(t *testing.T, fk *fake.Sentinel, st sentinel.State) sentinel.State {
	t.Helper()
	c := sentinel.NewClient(fk.URL)
	if err := c.Login(context.Background(), "demo@x.cl", "clave"); err != nil {
		t.Fatal(err)
	}
	p := &sentinel.Provisioner{Client: c, ServicesURL: "http://x", IntervalSec: 5}
	out, err := p.Provision(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A4: una empresa nueva queda con SLA, umbrales y clientes sin tocar la base.
func TestProvision_ConfiguraSLAUmbralesYClientes(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	st := aprovisionar(t, fk, sentinel.State{Platforms: map[string]sentinel.PlatformState{}})

	for _, pl := range catalog.All() {
		id := st.Platforms[pl.Slug].ProjectID
		if fk.SLAs[id] == nil {
			t.Errorf("%s: sin SLA", pl.Slug)
		}
		if fk.Thresholds[id]["availability_warn_pct"] != pl.Policy().Health.AvailabilityWarnPct {
			t.Errorf("%s: umbral de disponibilidad %v", pl.Slug, fk.Thresholds[id])
		}
	}
	contactos := 0
	for _, cs := range fk.Contacts {
		contactos += len(cs)
	}
	if len(fk.Customers) != 6 || contactos != 6 {
		t.Errorf("clientes=%d contactos=%d (quiero 6 y 6)", len(fk.Customers), contactos)
	}
}

// Aprovisionar otra vez no duplica nada ni crea versiones nuevas del SLA.
func TestProvision_EsIdempotenteConLaPolitica(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	st := aprovisionar(t, fk, sentinel.State{Platforms: map[string]sentinel.PlatformState{}})
	aprovisionar(t, fk, st)

	contactos := 0
	for _, cs := range fk.Contacts {
		contactos += len(cs)
	}
	if fk.SLAPuts != 3 || len(fk.Customers) != 6 || contactos != 6 {
		t.Errorf("PUT de SLA=%d (quiero 3), clientes=%d, contactos=%d", fk.SLAPuts, len(fk.Customers), contactos)
	}
}

// F11.3: cada servicio del mapa queda con su health_url y la segunda corrida no
// manda ningún PATCH (comparar antes de escribir).
func TestProvision_DeclaraLaSaludPorServicioUnaVez(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	st := aprovisionar(t, fk, sentinel.State{Platforms: map[string]sentinel.PlatformState{}})

	servicios := 0
	for _, pl := range catalog.All() {
		ps := st.Platforms[pl.Slug]
		for _, s := range pl.Services {
			servicios++
			c := fk.Components[ps.Components[s.Slug]]
			want := "http://x/salud/" + pl.Slug + "/" + s.Slug
			if c == nil || c["health_url"] != want {
				t.Errorf("%s/%s: health_url %v, quiero %q", pl.Slug, s.Slug, c["health_url"], want)
			}
		}
	}
	if fk.ComponentPuts < servicios {
		t.Errorf("PATCH de componentes=%d, quiero al menos %d (uno por health_url)", fk.ComponentPuts, servicios)
	}

	antes := fk.ComponentPuts
	aprovisionar(t, fk, st)
	if fk.ComponentPuts != antes {
		t.Errorf("la segunda corrida mandó %d PATCH más", fk.ComponentPuts-antes)
	}
}

// F13: con DEMO_PORTAL_PASSWORD se da acceso al primer contacto de cada
// plataforma una sola vez y se activa su clave; repetir no vuelve a activar.
func TestProvision_PortalDaAccesoUnaVezPorPlataforma(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	c := sentinel.NewClient(fk.URL)
	if err := c.Login(context.Background(), "demo@x.cl", "clave"); err != nil {
		t.Fatal(err)
	}
	p := &sentinel.Provisioner{Client: c, ServicesURL: "http://x", IntervalSec: 5, PortalPassword: "Portal-2026!"}
	st, err := p.Provision(context.Background(), sentinel.State{Platforms: map[string]sentinel.PlatformState{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(fk.PortalUsers) != 3 || len(fk.Granted) != 3 {
		t.Fatalf("usuarios del portal=%d accesos=%d, quiero 3 y 3", len(fk.PortalUsers), len(fk.Granted))
	}
	if len(fk.ActivationTokens) != 0 {
		t.Errorf("quedaron %d tokens de activación sin usar", len(fk.ActivationTokens))
	}
	if _, err := p.Provision(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	if len(fk.PortalUsers) != 3 || len(fk.Granted) != 3 {
		t.Errorf("repetir no debe otorgar de nuevo: usuarios=%d accesos=%d", len(fk.PortalUsers), len(fk.Granted))
	}
	// El usuario Cliente entra y ve su servicio.
	pc := sentinel.NewClient(fk.URL)
	if err := pc.Login(context.Background(), "marcela.fuentes@example.com", "Portal-2026!"); err != nil {
		t.Fatal(err)
	}
	svcs, err := pc.PortalServices(context.Background())
	if err != nil || len(svcs) != 1 || svcs[0].ProjectID == "" {
		t.Errorf("el portal debe devolver el servicio del contacto: %v %v", svcs, err)
	}
}

// F13 en aws: el contacto del portal pasa a la casilla real con el alias antes
// de la arroba, y aprovisionar otra vez no recrea su correo de example.com.
func TestProvision_PortalEnAWSUsaElAliasDeLaCasilla(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	c := sentinel.NewClient(fk.URL)
	if err := c.Login(context.Background(), "demo@x.cl", "clave"); err != nil {
		t.Fatal(err)
	}
	p := &sentinel.Provisioner{Client: c, ServicesURL: "http://x", IntervalSec: 5,
		PortalPassword: "Portal-2026!", PortalEmail: "equipo@demo.cl"}
	st, err := p.Provision(context.Background(), sentinel.State{Platforms: map[string]sentinel.PlatformState{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Provision(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	for _, pl := range catalog.All() {
		alias := "equipo+" + pl.Slug + "@demo.cl"
		original := pl.Policy().Clients[0].Contacts[0].Email
		conAlias, conOriginal := 0, 0
		for _, contactos := range fk.Contacts {
			for _, ct := range contactos {
				switch ct["email"] {
				case alias:
					conAlias++
				case original:
					conOriginal++
				}
			}
		}
		if conAlias != 1 || conOriginal != 0 {
			t.Errorf("%s: contactos con %s=%d y con %s=%d, quiero 1 y 0", pl.Slug, alias, conAlias, original, conOriginal)
		}
		pc := sentinel.NewClient(fk.URL)
		if err := pc.Login(context.Background(), alias, "Portal-2026!"); err != nil {
			t.Errorf("%s: el usuario del portal no inicia sesión con %s: %v", pl.Slug, alias, err)
		}
	}
	if got := sentinel.PortalAlias("equipo@demo.cl", "banco"); got != "equipo+banco@demo.cl" {
		t.Errorf("alias %q", got)
	}
}

// F13: sin DEMO_PORTAL_PASSWORD no se toca ningún acceso.
func TestProvision_SinClaveDePortalNoTocaAccesos(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	aprovisionar(t, fk, sentinel.State{Platforms: map[string]sentinel.PlatformState{}})
	if len(fk.PortalUsers) != 0 || len(fk.Granted) != 0 {
		t.Errorf("sin clave no debe otorgar acceso: usuarios=%d accesos=%d", len(fk.PortalUsers), len(fk.Granted))
	}
}

// Una política del catálogo que Sentinel rechazaría no debe llegar a la demo.
func TestPolitica_CumpleLasReglasDeSentinel(t *testing.T) {
	for _, pl := range catalog.All() {
		pol := pl.Policy()
		if len(pol.SLA) != 3 {
			t.Errorf("%s: el SLA lleva las tres severidades", pl.Slug)
		}
		for _, s := range pol.SLA {
			if s.FirstResponseMinutes <= 0 || s.ResolutionMinutes < s.FirstResponseMinutes {
				t.Errorf("%s/%s: plazos %d/%d", pl.Slug, s.Severity, s.FirstResponseMinutes, s.ResolutionMinutes)
			}
		}
		if pol.Notify < 1 || pol.Risk > 99 || pol.Notify >= pol.Risk {
			t.Errorf("%s: umbrales de aviso %d y riesgo %d", pl.Slug, pol.Notify, pol.Risk)
		}
		if h := pol.Health; h.AvailabilityWarnPct <= 0 || h.AvailabilityWarnPct > 100 || h.MttrWarnSeconds <= 0 || h.LatencyWarnMs <= 0 {
			t.Errorf("%s: umbrales de salud %+v", pl.Slug, h)
		}
		if len(pol.Clients) == 0 {
			t.Errorf("%s: sin clientes", pl.Slug)
		}
	}
}
