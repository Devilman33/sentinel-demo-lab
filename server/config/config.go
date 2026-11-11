// Package config arma el perfil de conexión (local o aws). Puede venir de un
// archivo JSON o solo de variables de entorno (la forma de la imagen Docker).
// Los secretos llegan siempre por variables de entorno, nunca por el archivo.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DefaultProfile es el archivo que se usa si no hay variables ni flag.
const DefaultProfile = "config/profiles/local.json"

type Profile struct {
	Name string `json:"name"` // local | aws
	// SentinelURL es la dirección de api-go vista desde este módulo.
	SentinelURL string `json:"sentinel_url"`
	// ServicesURL es la base pública del módulo: la URL con la que SENTINEL lo
	// alcanza para sondear /salud/{plataforma}. Incluye el puerto.
	ServicesURL     string  `json:"services_url"`
	PollIntervalSec int     `json:"poll_interval_seconds"`
	AllowDBBackfill bool    `json:"allow_db_backfill"`
	StateDir        string  `json:"state_dir"`
	TrafficRPS      float64 `json:"traffic_rps"` // usuarios por segundo con que arrancan las plataformas
	Email           string  `json:"-"`
	Password        string  `json:"-"`
	// DBDSN es la base de Sentinel para el historial sintético (solo local).
	DBDSN string `json:"-"`
	// Webhooks de los canales de cada servicio de Sentinel. Son secretos y son
	// opcionales: sin ellos el aprovisionamiento no toca los canales.
	DiscordWebhook string `json:"-"`
	SlackWebhook   string `json:"-"`
	// Portal del cliente (F13): clave con la que se activa el usuario Cliente de
	// cada plataforma y, solo en aws, casilla real con alias +<plataforma>.
	PortalPassword string `json:"-"`
	PortalEmail    string `json:"-"`
	// SentinelWebURL es el sitio del cliente de Sentinel, para enlazar /portal.
	SentinelWebURL string `json:"-"`
	// Acceso (A6). Sin ConsoleKey todo queda abierto, como en la prueba local;
	// en aws las dos son obligatorias. Ver el paquete access.
	ConsoleKey   string `json:"-"`
	SitePassword string `json:"-"`
	// Ventana de monitoreo de los proyectos, en UTC (T3.1). Vacía es 24/7.
	WindowStart, WindowEnd string `json:"-"`
	// Intervalo del sondeo fuera de la ventana (D-23). 0 usa PollIntervalSec.
	IdlePollIntervalSec int `json:"-"`
	// Severidades que llegan por correo (T3.2, D-22). nil deja el canal como
	// está; vacío lo apaga.
	EmailSeverities []string `json:"-"`
}

// Load lee el perfil desde path (JSON). El formato es JSON y no YAML para que
// el módulo no tenga dependencias externas.
func Load(path string) (Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		return Profile{}, fmt.Errorf("perfil %s: %w", path, err)
	}
	return p.finish("perfil " + path)
}

// FromEnv arma el perfil solo con variables de entorno. dataDir es la carpeta
// de datos del módulo; el estado de aprovisionamiento va en dataDir/state.
func FromEnv(dataDir string) (Profile, error) {
	p := Profile{
		Name:            env("DEMO_PROFILE", "local"),
		SentinelURL:     os.Getenv("DEMO_SENTINEL_URL"),
		ServicesURL:     os.Getenv("DEMO_PUBLIC_URL"),
		AllowDBBackfill: os.Getenv("DEMO_ALLOW_DB_BACKFILL") == "true",
		StateDir:        filepath.Join(dataDir, "state"),
	}
	var err error
	if p.PollIntervalSec, err = envInt("DEMO_POLL_INTERVAL"); err != nil {
		return Profile{}, err
	}
	if p.TrafficRPS, err = envFloat("DEMO_TRAFFIC_RPS"); err != nil {
		return Profile{}, err
	}
	return p.finish("variables de entorno")
}

// Resolve elige el origen del perfil: el archivo pedido; si no hay, las
// variables DEMO_*; si tampoco, el archivo local por omisión.
func Resolve(path, dataDir string) (Profile, error) {
	switch {
	case path != "":
		return Load(path)
	case os.Getenv("DEMO_SENTINEL_URL") != "":
		return FromEnv(dataDir)
	}
	if _, err := os.Stat(DefaultProfile); err == nil {
		return Load(DefaultProfile)
	}
	return Profile{}, errors.New("sin configuración: define DEMO_SENTINEL_URL y DEMO_PUBLIC_URL (ver .env.example) o pasa -profile")
}

// finish aplica secretos, valores por omisión y validaciones comunes a ambos orígenes.
func (p Profile) finish(origin string) (Profile, error) {
	p.Email, p.Password = os.Getenv("DEMO_SENTINEL_EMAIL"), os.Getenv("DEMO_SENTINEL_PASSWORD")
	p.DBDSN = os.Getenv("DEMO_DB_DSN")
	p.DiscordWebhook = strings.TrimSpace(os.Getenv("DEMO_DISCORD_WEBHOOK"))
	p.SlackWebhook = strings.TrimSpace(os.Getenv("DEMO_SLACK_WEBHOOK"))
	p.PortalPassword = os.Getenv("DEMO_PORTAL_PASSWORD")
	p.SentinelWebURL = strings.TrimRight(strings.TrimSpace(os.Getenv("DEMO_SENTINEL_WEB_URL")), "/")
	p.PortalEmail = strings.TrimSpace(os.Getenv("DEMO_PORTAL_EMAIL"))
	if p.Name != "aws" {
		// En local el contacto conserva su correo de example.com y Mailpit lo captura.
		p.PortalEmail = ""
	}
	p.ConsoleKey = strings.TrimSpace(os.Getenv("DEMO_CONSOLE_KEY"))
	p.SitePassword = strings.TrimSpace(os.Getenv("DEMO_SITE_PASSWORD"))
	var err error
	if p.WindowStart, p.WindowEnd, err = parseWindow(os.Getenv("DEMO_MONITORING_WINDOW")); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", origin, err)
	}
	if p.IdlePollIntervalSec, err = envInt("DEMO_POLL_INTERVAL_IDLE"); err != nil {
		return Profile{}, err
	}
	if p.EmailSeverities, err = emailSeverities(os.Getenv("DEMO_EMAIL_MIN_SEVERITY")); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", origin, err)
	}
	if p.PollIntervalSec < 5 {
		p.PollIntervalSec = 5 // mínimo de api-go
	}
	if p.IdlePollIntervalSec != 0 && p.IdlePollIntervalSec < 5 {
		p.IdlePollIntervalSec = 5
	}
	if p.StateDir == "" {
		p.StateDir = ".state"
	}
	p.SentinelURL = strings.TrimRight(strings.TrimSpace(p.SentinelURL), "/")
	p.ServicesURL = strings.TrimRight(strings.TrimSpace(p.ServicesURL), "/")
	switch {
	case p.Name != "local" && p.Name != "aws":
		return Profile{}, fmt.Errorf("%s: name debe ser local o aws", origin)
	case p.SentinelURL == "" || p.ServicesURL == "":
		return Profile{}, fmt.Errorf("%s: faltan sentinel_url o services_url (DEMO_SENTINEL_URL y DEMO_PUBLIC_URL)", origin)
	case p.TrafficRPS < 0:
		return Profile{}, fmt.Errorf("%s: traffic_rps no puede ser negativo", origin)
	case p.Name == "aws" && p.AllowDBBackfill:
		return Profile{}, fmt.Errorf("%s: allow_db_backfill no está permitido en aws", origin)
	}
	if err := p.checkPortal(); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", origin, err)
	}
	if err := p.checkAccess(); err != nil {
		return Profile{}, fmt.Errorf("%s: %w", origin, err)
	}
	return p, nil
}

// checkAccess exige las claves de A6 en aws: ahí el módulo queda en internet y
// sin ellas cualquiera podría tirar los servicios o abrir un banco ficticio.
func (p Profile) checkAccess() error {
	const minLen = 12 // el mismo de access.MinKeyLen
	switch {
	case p.Name == "aws" && (p.ConsoleKey == "" || p.SitePassword == ""):
		return fmt.Errorf("en aws el módulo queda abierto a internet: define DEMO_CONSOLE_KEY y DEMO_SITE_PASSWORD (al menos %d caracteres cada una)", minLen)
	case p.ConsoleKey == "" && p.SitePassword != "":
		return errors.New("DEMO_SITE_PASSWORD sin DEMO_CONSOLE_KEY no protege nada: define también la clave de la consola")
	case p.ConsoleKey != "" && len(p.ConsoleKey) < minLen:
		return fmt.Errorf("DEMO_CONSOLE_KEY debe tener al menos %d caracteres", minLen)
	case p.SitePassword != "" && len(p.SitePassword) < minLen:
		return fmt.Errorf("DEMO_SITE_PASSWORD debe tener al menos %d caracteres", minLen)
	case p.SitePassword != "" && p.SitePassword == p.ConsoleKey:
		return errors.New("DEMO_SITE_PASSWORD y DEMO_CONSOLE_KEY deben ser distintas: la de los sitios no debe abrir la consola")
	}
	return nil
}

// parseWindow lee "HH:MM-HH:MM" (UTC). Igual que en Sentinel, el fin puede ser
// anterior al inicio: la ventana cruza la medianoche.
func parseWindow(v string) (start, end string, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", "", nil
	}
	a, b, ok := strings.Cut(v, "-")
	if !ok {
		return "", "", fmt.Errorf("DEMO_MONITORING_WINDOW debe ser HH:MM-HH:MM en UTC (es %q)", v)
	}
	sa, errA := minutes(a)
	sb, errB := minutes(b)
	switch {
	case errA != nil || errB != nil:
		return "", "", fmt.Errorf("DEMO_MONITORING_WINDOW debe ser HH:MM-HH:MM en UTC (es %q)", v)
	case sa == sb:
		return "", "", errors.New("DEMO_MONITORING_WINDOW: el inicio y el fin no pueden ser iguales")
	}
	return hhmm(sa), hhmm(sb), nil
}

func minutes(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	hh, errH := strconv.Atoi(h)
	mm, errM := strconv.Atoi(m)
	if !ok || errH != nil || errM != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("hora inválida %q", s)
	}
	return hh*60 + mm, nil
}

func hhmm(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

// InWindow dice si t cae en la ventana [inicio, fin), con la misma regla que
// el Poller de Sentinel. Sin ventana, siempre.
func (p Profile) InWindow(t time.Time) bool {
	if p.WindowStart == "" {
		return true
	}
	a, _ := minutes(p.WindowStart)
	b, _ := minutes(p.WindowEnd)
	now := t.UTC().Hour()*60 + t.UTC().Minute()
	if a <= b {
		return now >= a && now < b
	}
	return now >= a || now < b
}

// IntervalAt es el intervalo de sondeo que corresponde a t: el normal dentro
// de la ventana y el de reposo fuera de ella (D-23).
func (p Profile) IntervalAt(t time.Time) int {
	if p.IdlePollIntervalSec > 0 && !p.InWindow(t) {
		return p.IdlePollIntervalSec
	}
	return p.PollIntervalSec
}

// severityOrder va de la más grave a la menos, con los nombres de Sentinel.
var severityOrder = []string{"critica", "alta", "media", "baja"}

// emailSeverities traduce DEMO_EMAIL_MIN_SEVERITY: "alta" es crítica y alta;
// "off" apaga el correo; vacío no toca el canal.
func emailSeverities(v string) ([]string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "":
		return nil, nil
	case "off", "apagado":
		return []string{}, nil
	case "crítica":
		v = "critica"
	}
	for i, s := range severityOrder {
		if s == v {
			return append([]string(nil), severityOrder[:i+1]...), nil
		}
	}
	return nil, fmt.Errorf("DEMO_EMAIL_MIN_SEVERITY debe ser critica, alta, media, baja u off (es %q)", v)
}

// checkPortal valida las variables del portal antes de tocar Sentinel. Una
// clave que Sentinel rechaza se descubriría tarde: el enlace de activación se
// entrega una sola vez y el usuario quedaría sin clave usable.
func (p Profile) checkPortal() error {
	if p.PortalPassword == "" {
		return nil
	}
	if n := len(p.PortalPassword); n < 8 || n > 72 {
		return errors.New("DEMO_PORTAL_PASSWORD debe tener entre 8 y 72 caracteres, como exige Sentinel")
	}
	if p.Name != "aws" {
		return nil
	}
	at := strings.Index(p.PortalEmail, "@")
	if at <= 0 || at != strings.LastIndex(p.PortalEmail, "@") || at == len(p.PortalEmail)-1 {
		return errors.New("en aws el portal exige DEMO_PORTAL_EMAIL, una casilla real (equipo@dominio.cl): el contacto pasa a equipo+<plataforma>@dominio.cl")
	}
	return nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser un número entero (es %q)", key, v)
	}
	return n, nil
}

func envFloat(key string) (float64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser un número (es %q)", key, v)
	}
	return n, nil
}
