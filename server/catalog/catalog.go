// Package catalog describe las plataformas ficticias del Demo Day: sus servicios,
// criticidades, dependencias y acciones de usuario final. Es solo datos; ni el
// runtime ni el conector de Sentinel conocen los detalles de una plataforma.
package catalog

// Brand es la marca ficticia común de las tres plataformas.
const Brand = "Don Federico"

type Service struct {
	Slug        string
	Role        string
	Criticality string // critica | alta | media | baja
	Port        int
	// LogFormat: json | text | xml. Los dos últimos ejercitan la normalización
	// de formatos legacy de Sentinel (FEAT-001).
	LogFormat string
	// Op describe, para los mensajes de log, lo que hace el servicio
	// ("el cobro del pedido").
	Op string
	// DependsOn lista los slugs de los servicios que este llama.
	DependsOn []string
}

// Action es una operación de usuario final que entra por el BFF.
type Action struct {
	Name string
	// Target es el servicio al que el BFF delega. Vacío = lo resuelve el propio BFF.
	Target string
	// Skip son dependencias que esta operación no necesita aunque el servicio
	// las tenga declaradas (p. ej. consultar saldo no pasa por el antifraude).
	Skip []string
	// Label es lo que ve el usuario ("pagar el pedido").
	Label string
}

type Platform struct {
	Slug     string
	Name     string // nombre visible de la marca
	Folder   string // carpeta de Sentinel
	Project  string // proyecto de Sentinel (uno por plataforma)
	Entry    string // BFF: su /health/deep es el monitoring_url del proyecto
	Services []Service
	Actions  []Action
}

func svc(slug, role, crit string, port int, format, op string, deps ...string) Service {
	return Service{Slug: slug, Role: role, Criticality: crit, Port: port, LogFormat: format, Op: op, DependsOn: deps}
}

// All devuelve el catálogo completo. Es una función y no una variable para que
// ningún llamador pueda mutar el catálogo compartido.
func All() []Platform {
	return []Platform{
		{
			Slug: "mercado", Name: "Mercado " + Brand, Folder: "Mercado", Project: "Mercado " + Brand, Entry: "storefront-web",
			Services: []Service{
				svc("storefront-web", "BFF del sitio", "alta", 7101, "json", "la atención del cliente", "auth-clientes", "api-catalogo", "pasarela-pagos"),
				svc("auth-clientes", "Inicio de sesión de clientes", "alta", 7102, "json", "la autenticación del cliente"),
				svc("api-catalogo", "Catálogo y búsqueda", "media", 7103, "json", "la búsqueda de productos"),
				svc("pasarela-pagos", "Cobro del pedido", "critica", 7104, "json", "el cobro del pedido", "servicio-inventario"),
				svc("servicio-inventario", "Reserva de stock", "alta", 7105, "json", "la reserva de stock", "db-transaccional"),
				svc("db-transaccional", "Base de pedidos", "critica", 7106, "xml", "el registro del pedido"),
			},
			Actions: []Action{
				{Name: "browse", Target: "api-catalogo", Label: "buscar productos"},
				{Name: "cart_add", Target: "api-catalogo", Label: "agregar al carro"},
				{Name: "login", Target: "auth-clientes", Label: "iniciar sesión"},
				{Name: "checkout", Target: "pasarela-pagos", Label: "pagar el pedido"},
			},
		},
		{
			Slug: "banco", Name: "Banco " + Brand, Folder: "Banco", Project: "Banco " + Brand, Entry: "banca-en-linea",
			Services: []Service{
				svc("banca-en-linea", "BFF del sitio", "alta", 7111, "json", "la atención del cliente", "auth-banca", "api-transferencias"),
				svc("auth-banca", "Clave y segundo factor", "critica", 7112, "json", "la validación de clave y segundo factor"),
				svc("api-transferencias", "Transferencias TEF", "critica", 7113, "json", "la transferencia de fondos", "motor-antifraude", "core-bancario"),
				svc("motor-antifraude", "Scoring de operaciones", "alta", 7114, "json", "la evaluación antifraude"),
				svc("core-bancario", "Saldos y movimientos", "critica", 7115, "xml", "el registro contable"),
			},
			Actions: []Action{
				{Name: "login", Target: "auth-banca", Label: "iniciar sesión"},
				{Name: "balance", Target: "api-transferencias", Skip: []string{"motor-antifraude"}, Label: "consultar el saldo"},
				{Name: "transfer", Target: "api-transferencias", Label: "transferir fondos"},
			},
		},
		{
			Slug: "clinica", Name: "Clínica " + Brand, Folder: "Clínica", Project: "Clínica " + Brand, Entry: "portal-paciente",
			Services: []Service{
				svc("portal-paciente", "BFF del sitio", "alta", 7121, "json", "la atención del paciente", "auth-pacientes", "agenda-medica", "api-laboratorio"),
				svc("auth-pacientes", "Inicio de sesión de pacientes", "alta", 7122, "json", "la autenticación del paciente"),
				svc("agenda-medica", "Reserva de horas", "alta", 7123, "json", "la reserva de hora médica", "ficha-clinica-db"),
				svc("api-laboratorio", "Resultados de exámenes", "media", 7124, "json", "la entrega de resultados de examen", "ficha-clinica-db"),
				svc("ficha-clinica-db", "Fichas clínicas", "critica", 7125, "xml", "la consulta de la ficha clínica"),
			},
			Actions: []Action{
				{Name: "search", Target: "agenda-medica", Skip: []string{"ficha-clinica-db"}, Label: "buscar especialidad"},
				{Name: "login", Target: "auth-pacientes", Label: "iniciar sesión"},
				{Name: "book_appointment", Target: "agenda-medica", Label: "reservar una hora"},
				{Name: "list_exams", Target: "api-laboratorio", Label: "ver mis exámenes"},
				{Name: "download_exam", Target: "api-laboratorio", Label: "descargar un examen"},
			},
		},
	}
}

func BySlug(slug string) (Platform, bool) {
	for _, p := range All() {
		if p.Slug == slug {
			return p, true
		}
	}
	return Platform{}, false
}

func (p Platform) EntryService() Service { s, _ := p.Service(p.Entry); return s }

func (p Platform) Service(slug string) (Service, bool) {
	for _, s := range p.Services {
		if s.Slug == slug {
			return s, true
		}
	}
	return Service{}, false
}

func (p Platform) Action(name string) (Action, bool) {
	for _, a := range p.Actions {
		if a.Name == name {
			return a, true
		}
	}
	return Action{}, false
}

// Edges devuelve las dependencias como pares {origen, destino}.
func (p Platform) Edges() [][2]string {
	var out [][2]string
	for _, s := range p.Services {
		for _, d := range s.DependsOn {
			out = append(out, [2]string{s.Slug, d})
		}
	}
	return out
}
