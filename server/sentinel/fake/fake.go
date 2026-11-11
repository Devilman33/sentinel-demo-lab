// Package fake simula la API pública de Sentinel en memoria, con el mismo
// contrato que usa el conector (incluido el autodescubrimiento de componentes
// al llegar un log y la revocación del token anterior). Sirve para probar el
// módulo sin levantar Sentinel.
package fake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Sentinel struct {
	*httptest.Server
	mu sync.Mutex

	Email, Password string
	Folders         map[string]string // id -> nombre
	Projects        map[string]map[string]any
	Components      map[string]map[string]string // id -> {project, name, criticality, health_url, status}
	Deps            map[string]bool
	Tokens          map[string]string // token -> proyecto (solo vigentes)
	TokensIssued    int
	ComponentPuts   int // PATCH a /api/v1/components/{id}
	ProjectPuts     int // PUT a /api/projects/{id}
	Ingested        int
	Alerts          []map[string]any          // id, project_id, status, category
	Probes          []map[string]any          // project_id, status_code
	Channels        map[string]map[string]any // proyecto -> cuerpo del último PUT
	SLAs            map[string]map[string]any // proyecto -> cuerpo del último PUT
	SLAPuts         int
	Thresholds      map[string]map[string]any // proyecto -> cuerpo del último PUT
	Customers       map[string]string         // id -> nombre
	Contacts        map[string][]map[string]any
	// Portal del cliente (F13): usuarios activados, tokens de activación y
	// tokens de sesión del rol Cliente.
	PortalUsers      map[string]string // email -> contraseña
	PortalProjects   map[string]string // email -> proyecto
	ActivationTokens map[string]string // token -> email
	ClientTokens     map[string]string // token de sesión -> email
	Granted          map[string]bool   // clientID/contactID ya vinculado
	// Down hace que todo responda 503, como un Sentinel caído o reiniciándose.
	Down atomic.Bool
	// Delay hace que cada respuesta tarde ese tiempo (en nanosegundos), como un Sentinel lento.
	Delay   atomic.Int64
	seq     int
	revoked map[string]string
}

func New(email, password string) *Sentinel {
	s := &Sentinel{
		Email: email, Password: password,
		Folders: map[string]string{}, Projects: map[string]map[string]any{},
		Components: map[string]map[string]string{}, Deps: map[string]bool{}, Tokens: map[string]string{},
		Alerts: []map[string]any{}, Probes: []map[string]any{}, Channels: map[string]map[string]any{},
	}
	s.clearPolicy()
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

func (s *Sentinel) id(prefix string) string { s.seq++; return fmt.Sprintf("%s-%d", prefix, s.seq) }

// Counts dice cuántos proyectos y tokens vigentes tiene.
func (s *Sentinel) Counts() (projects, tokens int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Projects), len(s.Tokens)
}

// RevokeTokens invalida los tokens de ingesta sin borrar nada más, como si
// alguien los hubiera emitido de nuevo; RestoreTokens los devuelve.
func (s *Sentinel) RevokeTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked, s.Tokens = s.Tokens, map[string]string{}
}

func (s *Sentinel) RestoreTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for t, p := range s.revoked {
		s.Tokens[t] = p
	}
	s.revoked = nil
}

// Reset borra todo lo creado, como un Sentinel recién instalado: los tokens y
// los proyectos que el módulo recordaba dejan de existir.
func (s *Sentinel) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Folders, s.Projects = map[string]string{}, map[string]map[string]any{}
	s.Components, s.Deps, s.Tokens = map[string]map[string]string{}, map[string]bool{}, map[string]string{}
	s.Channels, s.Alerts, s.Probes = map[string]map[string]any{}, []map[string]any{}, []map[string]any{}
	s.clearPolicy()
}

func (s *Sentinel) clearPolicy() {
	s.SLAs, s.SLAPuts, s.Thresholds = map[string]map[string]any{}, 0, map[string]map[string]any{}
	s.Customers, s.Contacts = map[string]string{}, map[string][]map[string]any{}
	s.PortalUsers, s.PortalProjects = map[string]string{}, map[string]string{}
	s.ActivationTokens, s.ClientTokens, s.Granted = map[string]string{}, map[string]string{}, map[string]bool{}
}

// AddAlert abre un incidente mientras el módulo consulta en paralelo.
func (s *Sentinel) AddAlert(id, project string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Alerts = append(s.Alerts, map[string]any{"id": id, "project_id": project, "status": "activo", "category": "Sin Clasificar"})
}

// AlertStatus devuelve el estado de un incidente.
func (s *Sentinel) AlertStatus(id string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.Alerts {
		if a["id"] == id {
			return a["status"]
		}
	}
	return nil
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Sentinel) serve(w http.ResponseWriter, r *http.Request) {
	if s.Down.Load() {
		reply(w, 503, map[string]string{"error": "no disponible"})
		return
	}
	if d := s.Delay.Load(); d > 0 {
		time.Sleep(time.Duration(d))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	path := r.URL.Path

	if path == "/api/ingest/batch" {
		s.ingest(w, r, bearer)
		return
	}
	var body map[string]any
	if r.Method != http.MethodGet && r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch {
	case path == "/api/status":
		reply(w, 200, map[string]string{"status": "ok"})
	case path == "/api/auth/login":
		email, password := str(body, "email"), str(body, "password")
		switch {
		case email == s.Email && password == s.Password:
			reply(w, 200, map[string]string{"token": "jwt-demo"})
		case s.PortalUsers[email] != "" && s.PortalUsers[email] == password:
			tok := s.id("jwt-cli")
			s.ClientTokens[tok] = email
			reply(w, 200, map[string]string{"token": tok})
		default:
			reply(w, 401, map[string]string{"error": "correo o contraseña incorrectos"})
		}
	case path == "/api/auth/activate-client":
		s.activate(w, body)
	case bearer == "jwt-demo":
		s.api(w, r, path, body)
	case s.ClientTokens[bearer] != "":
		s.portal(w, path, s.ClientTokens[bearer])
	default:
		reply(w, 401, map[string]string{"error": "sin sesión"})
	}
}

// activate consume el token de activación y fija la clave del usuario Cliente.
func (s *Sentinel) activate(w http.ResponseWriter, body map[string]any) {
	token, password := str(body, "token"), str(body, "new_password")
	email, ok := s.ActivationTokens[token]
	if !ok || password == "" {
		reply(w, 400, map[string]string{"error": "el enlace no es válido o venció"})
		return
	}
	s.PortalUsers[email] = password
	delete(s.ActivationTokens, token)
	reply(w, 200, map[string]string{"email": email})
}

// portal imita lo mínimo del portal Cliente: los servicios del usuario.
func (s *Sentinel) portal(w http.ResponseWriter, path, email string) {
	if path != "/api/v1/portal/services" {
		reply(w, 404, map[string]string{"error": "no existe " + path})
		return
	}
	project := s.PortalProjects[email]
	out := []map[string]string{}
	if project != "" {
		name := ""
		if p := s.Projects[project]; p != nil {
			name, _ = p["name"].(string)
		}
		out = append(out, map[string]string{"project_id": project, "name": name, "status": "operational"})
	}
	reply(w, 200, out)
}

func str(body map[string]any, k string) string { v, _ := body[k].(string); return v }

func (s *Sentinel) ingest(w http.ResponseWriter, r *http.Request, bearer string) {
	project, ok := s.Tokens[bearer]
	if !ok {
		reply(w, 401, map[string]string{"error": "token inválido"})
		return
	}
	var batch []map[string]any
	_ = json.NewDecoder(r.Body).Decode(&batch)
	if len(batch) == 0 {
		reply(w, 400, map[string]string{"error": "el lote no puede estar vacío"})
		return
	}
	for _, ev := range batch {
		s.Ingested++
		var p map[string]any
		_ = json.Unmarshal([]byte(ev["payload"].(string)), &p)
		s.discover(project, str(p, "service"))
	}
	reply(w, 202, batch)
}

func (s *Sentinel) discover(project, name string) {
	for _, c := range s.Components {
		if c["project"] == project && c["name"] == name {
			return
		}
	}
	s.Components[s.id("comp")] = map[string]string{
		"project": project, "name": name, "criticality": "media", "health_url": "", "status": "unknown",
	}
}

func (s *Sentinel) api(w http.ResponseWriter, r *http.Request, path string, body map[string]any) {
	switch {
	case path == "/api/v1/project-folders" && r.Method == http.MethodGet:
		out := []map[string]string{}
		for id, n := range s.Folders {
			out = append(out, map[string]string{"id": id, "name": n})
		}
		reply(w, 200, out)
	case path == "/api/v1/project-folders":
		id := s.id("fold")
		s.Folders[id] = str(body, "name")
		reply(w, 201, map[string]string{"id": id, "name": str(body, "name")})
	case path == "/api/projects" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, p := range s.Projects {
			out = append(out, p)
		}
		reply(w, 200, out)
	case path == "/api/projects":
		id := s.id("proj")
		p := map[string]any{"id": id, "name": str(body, "name"), "monitoring_url": str(body, "monitoring_url"),
			"monitoring_interval_seconds": body["monitoring_interval_seconds"],
			"monitoring_window_start": str(body, "monitoring_window_start"), "monitoring_window_end": str(body, "monitoring_window_end")}
		s.Projects[id] = p
		reply(w, 201, p)
	case strings.HasPrefix(path, "/api/projects/") && r.Method == http.MethodPut:
		p := s.Projects[strings.TrimPrefix(path, "/api/projects/")]
		// Como Sentinel, el PUT reemplaza la ventana: si no viene, queda 24/7.
		p["monitoring_url"], p["monitoring_interval_seconds"] = str(body, "monitoring_url"), body["monitoring_interval_seconds"]
		p["monitoring_window_start"], p["monitoring_window_end"] = str(body, "monitoring_window_start"), str(body, "monitoring_window_end")
		s.ProjectPuts++
		reply(w, 200, p)
	case strings.HasSuffix(path, "/folder"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/folder")
		s.Projects[id]["folder_id"] = str(body, "folder_id")
		reply(w, 200, s.Projects[id])
	case strings.HasSuffix(path, "/ingest-tokens"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/ingest-tokens")
		for t, p := range s.Tokens { // emitir revoca el anterior
			if p == id {
				delete(s.Tokens, t)
			}
		}
		s.TokensIssued++
		tok := fmt.Sprintf("tok-%d", s.TokensIssued)
		s.Tokens[tok] = id
		reply(w, 201, map[string]string{"token": tok, "project_id": id})
	case strings.HasSuffix(path, "/components"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/components")
		out := []map[string]string{}
		for cid, c := range s.Components {
			if c["project"] == id {
				out = append(out, map[string]string{
					"id": cid, "name": c["name"], "criticality": c["criticality"],
					"health_url": c["health_url"], "status": c["status"],
				})
			}
		}
		reply(w, 200, out)
	case path == "/api/v1/components/dependencies":
		k := str(body, "source_component_id") + ">" + str(body, "target_component_id")
		if s.Deps[k] {
			reply(w, 409, map[string]string{"error": "esa dependencia ya existe"})
			return
		}
		s.Deps[k] = true
		reply(w, 201, map[string]string{"id": k})
	case strings.HasPrefix(path, "/api/v1/components/") && r.Method == http.MethodPatch:
		c, ok := s.Components[strings.TrimPrefix(path, "/api/v1/components/")]
		if !ok {
			reply(w, 404, map[string]string{"error": "componente no encontrado"})
			return
		}
		if v, ok := body["criticality"].(string); ok {
			c["criticality"] = v
		}
		if v, ok := body["health_url"].(string); ok {
			c["health_url"] = v
		}
		s.ComponentPuts++
		reply(w, 200, c)
	case path == "/api/v1/alerts":
		reply(w, 200, s.Alerts)
	case path == "/api/v1/monitor/status":
		reply(w, 200, s.Probes)
	case strings.HasSuffix(path, "/sla"):
		s.sla(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/sla"), body)
	case strings.HasSuffix(path, "/health-thresholds") && r.Method == http.MethodPut:
		if _, ok := body["availability_warn_pct"].(float64); !ok {
			reply(w, 400, map[string]string{"error": "availability_warn_pct y mttr_warn_seconds son obligatorios"})
			return
		}
		s.Thresholds[strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/health-thresholds")] = body
		reply(w, 200, body)
	case strings.HasPrefix(path, "/api/v1/clients"):
		s.customers(w, r, strings.Split(strings.TrimPrefix(path, "/api/v1/clients"), "/"), body)
	case strings.HasSuffix(path, "/notification-channels"):
		s.channels(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/notification-channels"), body)
	case strings.HasPrefix(path, "/api/v1/incidents/") && strings.HasSuffix(path, "/resolve") && r.Method == http.MethodPatch:
		s.resolve(w, strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/incidents/"), "/resolve"), body)
	default:
		reply(w, 404, map[string]string{"error": "no existe " + path})
	}
}

// channels imita el PUT de Sentinel: exige los tres canales y rechaza por canal
// un webhook enmascarado o un canal activo sin webhook.
func (s *Sentinel) channels(w http.ResponseWriter, r *http.Request, project string, body map[string]any) {
	if r.Method == http.MethodGet {
		saved, ok := s.Channels[project]
		if !ok {
			saved = map[string]any{"email": map[string]any{"active": true}, "discord": map[string]any{}, "slack": map[string]any{}}
		}
		reply(w, 200, saved)
		return
	}
	rejected := map[string]string{}
	for _, name := range []string{"email", "discord", "slack"} {
		ch, ok := body[name].(map[string]any)
		if !ok {
			reply(w, 400, map[string]string{"error": "faltan canales: se esperan email, discord y slack"})
			return
		}
		webhook := str(ch, "webhook")
		if strings.Contains(webhook, "…") || (name != "email" && ch["active"] == true && webhook == "") {
			rejected[name] = "webhook inválido"
		}
	}
	if len(rejected) == 0 {
		s.Channels[project] = body
	}
	reply(w, 200, map[string]any{"channels": s.Channels[project], "errors": rejected})
}

// resolve aplica las reglas de cierre: notas y categoría válida, una sola vez.
func (s *Sentinel) resolve(w http.ResponseWriter, id string, body map[string]any) {
	if c := str(body, "category"); strings.TrimSpace(str(body, "resolution_notes")) == "" || (c != "Operativo" && c != "Seguridad") {
		reply(w, 400, map[string]string{"error": "faltan notas o categoría válida"})
		return
	}
	for _, a := range s.Alerts {
		if a["id"] != id {
			continue
		}
		if a["status"] == "resuelto" {
			reply(w, 400, map[string]string{"error": "el incidente ya está resuelto"})
			return
		}
		a["status"], a["category"] = "resuelto", str(body, "category")
		a["resolution_notes"] = str(body, "resolution_notes")
		reply(w, 200, a)
		return
	}
	reply(w, 404, map[string]string{"error": "incidente no encontrado"})
}

// sla imita el PUT del SLA: exige los tiempos de las tres severidades. El GET
// responde 404 mientras el proyecto no tenga SLA, como Sentinel.
func (s *Sentinel) sla(w http.ResponseWriter, r *http.Request, project string, body map[string]any) {
	if r.Method == http.MethodGet {
		saved, ok := s.SLAs[project]
		if !ok {
			reply(w, 404, map[string]string{"error": "el proyecto no tiene SLA configurado"})
			return
		}
		reply(w, 200, saved)
		return
	}
	if targets, _ := body["targets"].([]any); len(targets) != 3 {
		reply(w, 400, map[string]string{"error": "el SLA debe traer los tiempos de las severidades alta, media y baja", "field": "targets"})
		return
	}
	s.SLAs[project] = body
	s.SLAPuts++
	reply(w, 200, body)
}

// customers imita /api/v1/clients: lista, crea (409 si el nombre existe) y
// maneja los contactos de cada cliente. rest es la ruta tras /clients, partida.
func (s *Sentinel) customers(w http.ResponseWriter, r *http.Request, rest []string, body map[string]any) {
	switch {
	case len(rest) == 1 && r.Method == http.MethodGet:
		out := []map[string]string{}
		for id, name := range s.Customers {
			out = append(out, map[string]string{"id": id, "name": name})
		}
		reply(w, 200, out)
	case len(rest) == 1 && r.Method == http.MethodPost:
		name := str(body, "name")
		for _, existing := range s.Customers {
			if existing == name {
				reply(w, 409, map[string]string{"error": "ya existe un cliente con ese nombre"})
				return
			}
		}
		id := s.id("cli")
		s.Customers[id] = name
		reply(w, 201, map[string]string{"id": id, "name": name})
	case len(rest) == 3 && rest[2] == "contacts" && r.Method == http.MethodGet:
		out := []map[string]any{}
		for _, c := range s.Contacts[rest[1]] {
			if c["project_id"] == r.URL.Query().Get("project_id") {
				out = append(out, c)
			}
		}
		reply(w, 200, out)
	case len(rest) == 3 && rest[2] == "contacts" && r.Method == http.MethodPost:
		for _, c := range s.Contacts[rest[1]] {
			if c["email"] == body["email"] && c["project_id"] == body["project_id"] {
				reply(w, 409, map[string]string{"error": "ese contacto ya está en el servicio"})
				return
			}
		}
		body["id"] = s.id("ct")
		s.Contacts[rest[1]] = append(s.Contacts[rest[1]], body)
		reply(w, 201, body)
	case len(rest) == 4 && rest[2] == "contacts" && r.Method == http.MethodPatch:
		for _, c := range s.Contacts[rest[1]] {
			if c["id"] != rest[3] {
				continue
			}
			for _, k := range []string{"name", "email", "job_title"} {
				if v, ok := body[k].(string); ok {
					c[k] = v
				}
			}
			reply(w, 200, c)
			return
		}
		reply(w, 404, map[string]string{"error": "contacto no encontrado"})
	case len(rest) == 5 && rest[2] == "contacts" && rest[4] == "access" && r.Method == http.MethodPut:
		var contacto map[string]any
		for _, c := range s.Contacts[rest[1]] {
			if c["id"] == rest[3] {
				contacto = c
			}
		}
		if contacto == nil {
			reply(w, 404, map[string]string{"error": "contacto no encontrado"})
			return
		}
		key := rest[1] + "/" + rest[3]
		var activation any
		if !s.Granted[key] {
			s.Granted[key] = true
			email, _ := contacto["email"].(string)
			project, _ := contacto["project_id"].(string)
			token := s.id("act")
			s.ActivationTokens[token] = email
			s.PortalProjects[email] = project
			activation = map[string]string{"email": email, "token": token}
		}
		reply(w, 200, map[string]any{"contact": contacto, "activation": activation})
	default:
		reply(w, 404, map[string]string{"error": "no existe " + r.URL.Path})
	}
}
