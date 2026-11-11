package control

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"demo-lab/server/faults"
	"demo-lab/server/sentinel"
	"demo-lab/server/sentinel/fake"
)

// arrancar lanza Bootstrap con tiempos cortos y devuelve cuántas veces avisó "listo".
func arrancar(t *testing.T, api *API) *atomic.Int32 {
	t.Helper()
	api.bootEvery, api.syncEvery = 10*time.Millisecond, 20*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var listos atomic.Int32
	go api.Bootstrap(ctx, func() { listos.Add(1) })
	return &listos
}

func esperar(t *testing.T, motivo string, cond func() bool) {
	t.Helper()
	for fin := time.Now().Add(5 * time.Second); !cond(); {
		if time.Now().After(fin) {
			t.Fatalf("se agotó la espera: %s", motivo)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Criterio 1: con solo las credenciales, el módulo se conecta y aprovisiona solo.
func TestBootstrap_SeConectaYAprovisionaSolo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	listos := arrancar(t, api)

	esperar(t, "estado listo", func() bool { return api.BootStatus().State == BootReady })
	servicios, aristas := totales()
	if len(fk.Projects) != 3 || len(fk.Components) != servicios || len(fk.Deps) != aristas {
		t.Errorf("proyectos=%d componentes=%d/%d dependencias=%d/%d", len(fk.Projects), len(fk.Components), servicios, len(fk.Deps), aristas)
	}
	// Con Sentinel estable no vuelve a aprovisionar ni a avisar: reemitiría los tokens.
	time.Sleep(150 * time.Millisecond)
	if fk.TokensIssued != 3 || listos.Load() != 1 {
		t.Errorf("tokens emitidos=%d (quiero 3), avisos de listo=%d (quiero 1)", fk.TokensIssued, listos.Load())
	}
}

// Criterio 2 y 3: sin Sentinel el módulo no se cae, informa y se recupera cuando vuelve.
func TestBootstrap_SinSentinelReintentaYSeRecupera(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	fk.Down.Store(true)
	api := nuevaAPI(t, fk)
	listos := arrancar(t, api)

	esperar(t, "estado conectando", func() bool { return api.BootStatus().State == BootConnecting })
	if b := api.BootStatus(); b.LastError == "" || listos.Load() != 0 {
		t.Errorf("debe informar el error y no avisar listo: %+v", b)
	}
	// Mientras tanto la API sigue respondiendo.
	if code, _ := call(t, api.Handler(), "GET", "/healthz"); code != 200 {
		t.Errorf("healthz con Sentinel caído: %d", code)
	}

	fk.Down.Store(false)
	esperar(t, "estado listo tras volver Sentinel", func() bool { return api.BootStatus().State == BootReady })
	if b := api.BootStatus(); b.Attempts != 0 || b.LastError != "" {
		t.Errorf("tras recuperarse no debe quedar error: %+v", b)
	}
	if listos.Load() != 1 || len(fk.Projects) != 3 {
		t.Errorf("avisos=%d proyectos=%d", listos.Load(), len(fk.Projects))
	}
}

// Perder Sentinel en marcha y recuperarlo con los mismos datos no reaprovisiona.
func TestBootstrap_UnCorteNoReaprovisiona(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	arrancar(t, api)
	esperar(t, "listo", func() bool { return api.BootStatus().State == BootReady })

	fk.Down.Store(true)
	esperar(t, "detecta el corte", func() bool { return api.BootStatus().State == BootConnecting })
	fk.Down.Store(false)
	esperar(t, "recupera", func() bool { return api.BootStatus().State == BootReady })
	if fk.TokensIssued != 3 {
		t.Errorf("un corte no debe reemitir tokens: %d", fk.TokensIssued)
	}
}

// Sentinel recreado de cero (el módulo recuerda ids y tokens que ya no existen).
func TestBootstrap_SentinelRecreadoSeAprovisionaDeNuevo(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	arrancar(t, api)
	esperar(t, "listo", func() bool { return api.BootStatus().State == BootReady })
	viejos := api.projectIDs()

	fk.Reset()
	// El estado guardado se reemplaza al terminar el aprovisionamiento: se espera a eso.
	esperar(t, "reaprovisionado", func() bool {
		nuevos := api.projectIDs()
		for slug, id := range viejos {
			if nuevos[slug] == id {
				return false
			}
		}
		return len(nuevos) == len(viejos)
	})
	if p, tk := fk.Counts(); p != 3 || tk != 3 {
		t.Errorf("Sentinel quedó con %d proyectos y %d tokens, quiero 3 y 3", p, tk)
	}
	tgt, _ := api.Target("mercado")
	if _, err := api.client.Ingest(context.Background(), tgt.Token, nil); err == nil || !strings.Contains(err.Error(), "400") {
		t.Errorf("el token guardado debe ser el nuevo (lote vacío → 400): %v", err)
	}
}

func TestBootstrap_SinCredencialesLoDiceYNoReintenta(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	api.Profile.Email = ""
	arrancar(t, api)

	esperar(t, "sin credenciales", func() bool { return api.BootStatus().State == BootNoCredentials })
	time.Sleep(50 * time.Millisecond)
	if len(fk.Projects) != 0 {
		t.Errorf("sin credenciales no debe aprovisionar")
	}
}

// El estado guardado sobrevive a un reinicio del módulo: no reemite tokens.
func TestBootstrap_ReiniciarElModuloReusaLoGuardado(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	primera := nuevaAPI(t, fk)
	arrancar(t, primera)
	esperar(t, "listo", func() bool { return primera.BootStatus().State == BootReady })

	segunda := nuevaAPI(t, fk)
	segunda.Profile.StateDir = primera.Profile.StateDir
	segunda.state, _ = sentinel.LoadState(segunda.Profile.StateDir, segunda.Profile.Name)
	arrancar(t, segunda)
	esperar(t, "listo tras reiniciar", func() bool { return segunda.BootStatus().State == BootReady })
	if fk.TokensIssued != 3 {
		t.Errorf("reiniciar el módulo reemitió tokens: %d", fk.TokensIssued)
	}
}

// El tráfico de base y el resto de la API no deben esperar a Sentinel: Target
// no puede quedar bloqueado mientras se aprovisiona.
func TestDoProvision_NoBloqueaLaLecturaDelEstado(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	fk.Delay.Store(int64(30 * time.Millisecond))
	api := nuevaAPI(t, fk)

	hecho := make(chan struct{})
	go func() { _, _ = api.doProvision(context.Background()); close(hecho) }()
	time.Sleep(20 * time.Millisecond)
	leido := make(chan struct{})
	go func() { api.Target("mercado"); api.BootStatus(); close(leido) }()
	select {
	case <-leido:
	case <-time.After(time.Second):
		t.Fatal("Target quedó esperando al aprovisionamiento")
	}
	<-hecho
}

// /salud entrega lo mismo que el /health/deep del BFF y lo ve caído si su puerto está cerrado.
func TestSalud_ReflejaElEstadoDelBFF(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	if err := api.Runtime.Start(); err != nil {
		t.Skipf("puertos 7101-7125 ocupados (¿hay un demo-lab encendido?): %v", err)
	}
	defer api.Runtime.Stop()
	h := api.Handler()

	if code, out := call(t, h, "GET", "/salud/mercado"); code != 200 || out["status"] != "ok" {
		t.Errorf("sana: %d %v", code, out)
	}
	if code, _ := call(t, h, "GET", "/salud/nada"); code != 404 {
		t.Errorf("plataforma desconocida: %d", code)
	}

	// Una dependencia caída: el BFF responde 503 y /salud lo reenvía con la causa.
	if _, err := api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "refused"}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, h, "GET", "/salud/mercado"); code != 503 || out["failing"] == nil {
		t.Errorf("con la base caída: %d %v", code, out)
	}
	// El BFF mismo con la conexión rechazada: hacia afuera, la plataforma está caída.
	if _, err := api.Runtime.Faults.Apply("mercado", "storefront-web", "down", faults.Params{Mode: "refused"}, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, h, "GET", "/salud/mercado"); code != 503 || out["status"] != "down" {
		t.Errorf("con el BFF cerrado: %d %v", code, out)
	}
	// El chequeo del contenedor no se entera de las fallas simuladas.
	if code, _ := call(t, h, "GET", "/healthz"); code != 200 {
		t.Errorf("healthz con fallas activas: %d", code)
	}
}

// /salud/{plataforma}/{servicio} reenvía el /health superficial: solo se cae el
// nodo afectado, y una degradación se nota en la espera.
func TestSalud_ReflejaLaSaludDeCadaServicio(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	if err := api.Runtime.Start(); err != nil {
		t.Skipf("puertos 7101-7125 ocupados (¿hay un demo-lab encendido?): %v", err)
	}
	defer api.Runtime.Stop()
	h := api.Handler()

	if code, out := call(t, h, "GET", "/salud/mercado/db-transaccional"); code != 200 || out["service"] != "db-transaccional" {
		t.Errorf("servicio sano: %d %v", code, out)
	}
	if code, _ := call(t, h, "GET", "/salud/mercado/nada"); code != 404 {
		t.Errorf("servicio desconocido: %d", code)
	}
	if code, _ := call(t, h, "GET", "/salud/nada/db-transaccional"); code != 404 {
		t.Errorf("plataforma desconocida: %d", code)
	}

	// Responder 503: el servicio está caído sin cerrar el puerto.
	f, err := api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "503"}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, h, "GET", "/salud/mercado/db-transaccional"); code != 503 || out["status"] != "down" {
		t.Errorf("con la base en 503: %d %v", code, out)
	}
	api.Runtime.Faults.Remove(f.ID)

	// Cerrar el puerto: la conexión se rechaza y la ruta responde 503.
	f, err = api.Runtime.Faults.Apply("mercado", "db-transaccional", "down", faults.Params{Mode: "refused"}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if code, out := call(t, h, "GET", "/salud/mercado/db-transaccional"); code != 503 || out["status"] != "down" {
		t.Errorf("con el puerto cerrado: %d %v", code, out)
	}
	api.Runtime.Faults.Remove(f.ID)

	// Degradación: el sondeo espera la latencia real del servicio.
	f, err = api.Runtime.Faults.Apply("mercado", "db-transaccional", "degraded", faults.Params{LatencyMs: 300}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	inicio := time.Now()
	if code, _ := call(t, h, "GET", "/salud/mercado/db-transaccional"); code != 200 {
		t.Errorf("degradado pero sano: %d", code)
	}
	if espera := time.Since(inicio); espera < 250*time.Millisecond {
		t.Errorf("la ruta no esperó la degradación: %s", espera)
	}
	api.Runtime.Faults.Remove(f.ID)
}

// Un cambio de DEMO_PUBLIC_URL (services_url) debe hacer que inSync pida
// reaprovisionar: si no, las health_url de los nodos quedarían apuntando al
// destino viejo.
func TestInSync_DetectaElCambioDeLaURLPublica(t *testing.T) {
	fk := fake.New("demo@x.cl", "clave")
	defer fk.Close()
	api := nuevaAPI(t, fk)
	ctx := context.Background()

	if _, err := api.doProvision(ctx); err != nil {
		t.Fatal(err)
	}
	if ok, err := api.inSync(ctx); err != nil || !ok {
		t.Fatalf("recién aprovisionado debe estar en sync: ok=%v err=%v", ok, err)
	}

	api.Profile.ServicesURL = "http://otra-maquina:7000"
	if ok, err := api.inSync(ctx); err != nil || ok {
		t.Errorf("con otra URL pública inSync debe pedir aprovisionar: ok=%v err=%v", ok, err)
	}
}
