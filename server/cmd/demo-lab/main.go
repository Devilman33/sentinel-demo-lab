// Comando demo-lab: simulador de plataformas cliente para el stand del Demo Day.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"demo-lab/server/attacks"
	"demo-lab/server/catalog"
	"demo-lab/server/config"
	"demo-lab/server/control"
	"demo-lab/server/logs"
	"demo-lab/server/runtime"
	"demo-lab/server/scripts"
	"demo-lab/server/traffic"
)

// listenAddr elige dónde escucha el puerto público: DEMO_LISTEN si existe; si
// no, $PORT en todas las interfaces (así lo exige Render); si no, loopback.
func listenAddr() string {
	if v := os.Getenv("DEMO_LISTEN"); v != "" {
		return v
	}
	if v := os.Getenv("PORT"); v != "" {
		return ":" + v
	}
	return "127.0.0.1:7000"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	profile := flag.String("profile", "", "perfil JSON; vacío usa las variables DEMO_* (o config/profiles/local.json si existe)")
	addr := flag.String("addr", listenAddr(), "puerto público: sitios, consola y API de control (DEMO_LISTEN o PORT)")
	dataDir := flag.String("data", envOr("DEMO_DATA_DIR", "data"), "carpeta de estado, logs y bitácora (DEMO_DATA_DIR)")
	scriptsDir := flag.String("scripts", os.Getenv("DEMO_SCRIPTS_DIR"), "carpeta con guiones extra (JSON), además de los incluidos")
	flag.Parse()

	p, err := config.Resolve(*profile, *dataDir)
	if err != nil {
		log.Fatalf("configuración: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := logs.New(*dataDir + "/logs")
	defer logger.Close()
	// Los servicios simulados escuchan solo en loopback: hacia afuera hay un
	// único puerto, y Sentinel sondea /salud/{plataforma} en él.
	rt := runtime.New("127.0.0.1", logger)
	api := control.New(p)
	shipper := logs.NewShipper(api.Client(), api.Target, *dataDir+"/dead-letter")
	logger.Subscribe(shipper.Offer)
	api.Runtime, api.Logger, api.Shipper, api.Traffic = rt, logger, shipper, traffic.New()
	api.Journal = control.NewJournal(*dataDir + "/journal.ndjson")
	api.Attacks = attacks.New()
	list, errs := scripts.Library(*scriptsDir)
	for _, e := range errs {
		log.Printf("guion omitido: %v", e)
	}
	api.Scripts = scripts.NewRunner(api.ScriptActions(), list)
	api.Marathon = api.NewMarathon()

	if err := rt.Start(); err != nil {
		log.Fatalf("servicios: %v", err)
	}
	defer rt.Stop()
	go shipper.Run(ctx)
	// El tráfico de base espera a la primera conexión: antes, sus logs no tendrían
	// a dónde ir. Los sitios y la consola ya funcionan.
	go api.Bootstrap(ctx, func() { startTraffic(api.Traffic, p.TrafficRPS) })

	srv := &http.Server{Addr: *addr, Handler: api.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	acceso := "sin clave: consola y sitios abiertos a quien llegue al puerto"
	if p.ConsoleKey != "" {
		acceso = "con clave en /acceso; /salud y /healthz abiertas"
	}
	ventana := "24/7"
	if p.WindowStart != "" {
		ventana = p.WindowStart + "-" + p.WindowEnd + " UTC"
	}
	log.Printf("demo-lab · perfil %s · Sentinel %s · sondeo en %s · escuchando en %s (sitios en /sitios/, consola en /) · acceso %s · ventana %s",
		p.Name, p.SentinelURL, p.ServicesURL, *addr, acceso, ventana)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Println(err)
		os.Exit(1)
	}
}

func startTraffic(g *traffic.Generator, rps float64) {
	if rps <= 0 {
		return
	}
	for _, pl := range catalog.All() {
		if err := g.Set(pl.Slug, rps); err != nil {
			log.Printf("tráfico de base en %s: %v", pl.Slug, err)
			return
		}
	}
	log.Printf("tráfico de base: %g usuarios/s por plataforma", rps)
}
