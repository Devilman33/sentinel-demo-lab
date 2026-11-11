package control

import (
	"testing"
	"time"

	"demo-lab/server/attacks"
	"demo-lab/server/faults"
	"demo-lab/server/history"
	"demo-lab/server/sentinel/fake"
)

func alertaConTitulo(fk *fake.Sentinel, id, project, status, category, title string) map[string]any {
	a := map[string]any{"id": id, "project_id": project, "status": status, "category": category, "title": title}
	fk.Alerts = append(fk.Alerts, a)
	return a
}

// D-17 (1): el incidente que nombra al servicio caído se cierra con la nota de
// esa falla, no con la genérica del reinicio.
func TestReset_UsaLaNotaDeLaFallaQueNombraElIncidente(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	alerta := alertaConTitulo(fk, "a1", api.projectIDs()["mercado"], "activo", "Operativo", "Incidente en db-transaccional")
	if _, err := api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "503"}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, h, "POST", "/control/v1/reset"); code != 200 {
		t.Fatalf("reset: %d %v", code, out)
	}

	want := history.ClosingNote("mercado", "db-transaccional", "Operativo")
	if want == "" {
		t.Fatal("el catálogo no tiene nota para db-transaccional")
	}
	if alerta["resolution_notes"] != want {
		t.Errorf("nota %q, quiero la de la base %q", alerta["resolution_notes"], want)
	}
}

// D-17 (2): si la cascada nombra a otro servicio, la única falla activa de la
// plataforma es la causa y aporta la nota.
func TestReset_UsaLaUnicaFallaDeLaCascada(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	alerta := alertaConTitulo(fk, "a1", api.projectIDs()["mercado"], "activo", "Operativo", "Incidente en servicio-inventario")
	if _, err := api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "refused"}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	call(t, h, "POST", "/control/v1/reset")

	want := history.ClosingNote("mercado", "db-transaccional", "Operativo")
	if alerta["resolution_notes"] != want {
		t.Errorf("nota %q, quiero la de la causa raíz %q", alerta["resolution_notes"], want)
	}
}

// D-17 (1) para Seguridad: un ataque activo contra el servicio que nombra el
// incidente deja la nota del tipo de ataque.
func TestReset_UsaLaNotaDelAtaqueActivo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Attacks = attacks.New()
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	alerta := alertaConTitulo(fk, "a1", api.projectIDs()["banco"], "activo", "Seguridad", "Incidente en auth-banca")
	if _, err := api.Attacks.Launch("banco", "auth-banca", "brute_force", 1, 60, "test"); err != nil {
		t.Fatal(err)
	}
	call(t, h, "POST", "/control/v1/reset")

	want := attacks.ClosingNote("brute_force")
	if want == "" {
		t.Fatal("el catálogo no tiene nota para fuerza bruta")
	}
	if alerta["resolution_notes"] != want {
		t.Errorf("nota %q, quiero la del ataque %q", alerta["resolution_notes"], want)
	}
}

// D-17 (3): sin fallas ni ataques, la nota sale del escenario del servicio.
func TestReset_SinFallasUsaElEscenarioDelServicio(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	alerta := alertaConTitulo(fk, "a1", api.projectIDs()["mercado"], "activo", "Operativo", "Incidente en api-catalogo")
	call(t, h, "POST", "/control/v1/reset")

	want := history.ClosingNote("mercado", "api-catalogo", "Operativo")
	if want == "" {
		t.Fatal("el catálogo no tiene nota para api-catalogo")
	}
	if alerta["resolution_notes"] != want {
		t.Errorf("nota %q, quiero la del escenario %q", alerta["resolution_notes"], want)
	}
}

// D-17: lo que llega tarde conserva el snapshot del reinicio y se cierra con la
// misma nota de la causa, no con la neutra.
func TestReset_LoQueLlegaTardeUsaLaNotaDelReinicio(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.settleEvery, api.settleFor = 5*time.Millisecond, 3*time.Second
	h := api.Handler()
	call(t, h, "POST", "/control/v1/provision")

	if _, err := api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "503"}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	call(t, h, "POST", "/control/v1/reset")

	tarde := alertaConTitulo(fk, "tarde", api.projectIDs()["mercado"], "activo", "Operativo", "Incidente en servicio-inventario")
	for end := time.Now().Add(2 * time.Second); tarde["status"] != "resuelto"; {
		if time.Now().After(end) {
			t.Fatal("el incidente tardío quedó abierto")
		}
		time.Sleep(5 * time.Millisecond)
	}
	want := history.ClosingNote("mercado", "db-transaccional", "Operativo")
	if tarde["resolution_notes"] != want {
		t.Errorf("nota tardía %q, quiero la de la causa %q", tarde["resolution_notes"], want)
	}
}
