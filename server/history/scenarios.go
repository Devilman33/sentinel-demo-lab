package history

import "math/rand/v2"

// scenario es un tipo de incidente del catálogo: de él salen los títulos, las
// notas de resolución y la duración típica. Las notas son texto propio, escrito
// como lo haría un equipo de operaciones; alimentan el corpus del Copiloto.
type scenario struct {
	platform, service string
	title             string
	description       string
	severity          string
	category          string
	medianMin         int
	downtime          bool
	weight            int
	notes             []string
}

func ops(platform, service, title, desc, sev string, median, weight int, notes ...string) scenario {
	return scenario{platform, service, title, desc, sev, "Operativo", median, true, weight, notes}
}

func sec(platform, service, title, desc string, median, weight int, notes ...string) scenario {
	return scenario{platform, service, title, desc, "alta", "Seguridad", median, false, weight, notes}
}

var scenarios = []scenario{
	ops("mercado", "db-transaccional", "Caída de la base transaccional de pedidos", "La base de pedidos dejó de aceptar conexiones y los pagos fallaron en cascada.", "critica", 55, 3,
		"Se reinició la instancia de la base tras agotarse el espacio del volumen de transacciones. Se amplió el disco y se agregó una alerta al 80 % de uso.",
		"El motor entró en recuperación tras un apagado inesperado. Se esperó el replay del WAL, se validó la integridad de los pedidos y se reabrió el tráfico gradualmente."),
	ops("mercado", "pasarela-pagos", "Degradación de la pasarela de pagos", "El cobro de pedidos superó el segundo de latencia y aumentaron los timeouts en el checkout.", "alta", 35, 4,
		"El proveedor de cobro tenía un incidente propio. Se activó el reintento con backoff y se bajó el tiempo máximo de espera hasta que el proveedor se normalizó.",
		"El pool de conexiones hacia el proveedor estaba agotado por un pico de campaña. Se duplicó el pool y se limitó la concurrencia por cliente."),
	ops("mercado", "servicio-inventario", "Errores en la reserva de stock", "La reserva de stock devolvió errores 5xx intermitentes durante la hora punta.", "alta", 40, 3,
		"Un bloqueo largo en la tabla de reservas frenaba las escrituras. Se canceló la consulta de reporte que lo provocaba y se movió a la réplica de lectura.",
		"Se desplegó una versión con un índice faltante. Se hizo rollback del despliegue y se creó el índice antes de volver a publicar."),
	ops("mercado", "api-catalogo", "Búsqueda de productos lenta", "El catálogo respondía sobre 2 s y la búsqueda del sitio mostraba resultados incompletos.", "media", 25, 4,
		"El índice de búsqueda se había desactualizado tras una carga masiva de precios. Se reconstruyó el índice y se programó la carga fuera del horario comercial."),
	ops("mercado", "auth-clientes", "Fallas de inicio de sesión de clientes", "Los clientes no podían iniciar sesión y recibían errores 500.", "alta", 30, 2,
		"El almacén de sesiones se quedó sin memoria. Se aumentó el tamaño, se ajustó la expiración de sesiones y se vació la caché corrupta."),
	// Los BFF no entran al historial sintético (peso 0): solo aportan su nota
	// de cierre al reinicio, que no debe cerrar con la nota genérica.
	ops("mercado", "storefront-web", "Caída del BFF del Mercado", "El sitio no cargó porque el BFF dejó de aceptar conexiones.", "alta", 30, 0,
		"El BFF agotó sus hilos por un pico de tráfico. Se reinició de forma progresiva, se amplió el pool de workers y se limitó la concurrencia por cliente."),
	ops("banco", "banca-en-linea", "Caída de la banca en línea", "El sitio del banco no respondió y los clientes no pudieron operar.", "alta", 30, 0,
		"La banca en línea quedó sin conexiones disponibles tras una actualización. Se restauró la versión anterior, se validó el ingreso y se añadió una prueba de humo al despliegue."),
	ops("clinica", "portal-paciente", "Caída del portal de pacientes", "El portal dejó de responder y no se pudo reservar ni ver exámenes.", "alta", 30, 0,
		"El portal devolvió errores por una configuración de caché inválida. Se limpió la caché, se reinició el servicio por partes y se verificaron reservas y descargas."),
	ops("banco", "core-bancario", "Indisponibilidad del core bancario", "El core no respondió y se rechazaron transferencias y consultas de saldo.", "critica", 70, 3,
		"La conmutación a la réplica falló por un desfase de replicación. Se promovió la réplica manualmente, se verificó la conciliación contable y se reconstruyó el nodo caído.",
		"Un proceso de cierre de lote retuvo bloqueos sobre las cuentas. Se terminó el proceso, se reintentó el cierre en ventana controlada y se agregó un límite de duración."),
	ops("banco", "api-transferencias", "Transferencias rechazadas por error interno", "El servicio de transferencias devolvió errores 500 en un porcentaje alto de operaciones.", "critica", 45, 3,
		"Una dependencia del antifraude respondía con timeouts y el servicio no degradaba bien. Se activó el modo de contingencia con límites reducidos y se corrigió el timeout.",
		"Se detectó un despliegue con una configuración de conexión equivocada. Se restauró la configuración anterior y se añadió una validación previa al despliegue."),
	ops("banco", "motor-antifraude", "Latencia alta del motor antifraude", "El scoring de operaciones tardaba sobre 3 s y retrasaba las transferencias.", "alta", 30, 3,
		"El modelo se recargaba en cada petición tras un cambio de configuración. Se corrigió el caché del modelo y se reinició el servicio de forma progresiva."),
	ops("banco", "auth-banca", "Fallas en la clave dinámica", "Los clientes no recibían o no validaban la clave dinámica al iniciar sesión.", "alta", 35, 2,
		"El proveedor de mensajería tenía una demora. Se cambió al proveedor de respaldo y se normalizó la cola de envíos pendientes."),
	ops("clinica", "ficha-clinica-db", "Caída de la ficha clínica", "La base de fichas no respondió y se detuvieron reservas y consultas de exámenes.", "critica", 60, 3,
		"El volumen de datos alcanzó el límite y la base pasó a solo lectura. Se amplió el almacenamiento, se archivaron registros antiguos y se verificó la integridad.",
		"Una migración de esquema dejó bloqueada la tabla de pacientes. Se canceló la migración, se liberaron los bloqueos y se reprogramó con la técnica de cambio en línea."),
	ops("clinica", "agenda-medica", "Errores al reservar horas médicas", "La agenda devolvió errores y los pacientes no pudieron confirmar su hora.", "alta", 40, 4,
		"El servicio de agenda quedó sin hilos disponibles tras un pico de consultas. Se escaló horizontalmente y se limitó la frecuencia de consulta por usuario.",
		"Un cambio en el calendario de feriados dejó horas duplicadas. Se corrigió el dato maestro y se reconstruyó la disponibilidad de la semana."),
	ops("clinica", "api-laboratorio", "Exámenes no disponibles para descarga", "Los pacientes no podían descargar sus resultados de laboratorio.", "media", 30, 3,
		"El almacenamiento de documentos devolvía errores de permisos tras una rotación de credenciales. Se actualizó la credencial del servicio y se verificaron las descargas."),
	ops("clinica", "auth-pacientes", "Inicio de sesión de pacientes intermitente", "Los pacientes experimentaron errores al iniciar sesión en el portal.", "media", 25, 2,
		"La caché de tokens se saturó. Se aumentó su capacidad y se redujo la vida útil de los tokens inactivos."),
	sec("banco", "auth-banca", "Ataque de fuerza bruta contra el inicio de sesión", "Se detectó una ráfaga de inicios de sesión fallidos desde pocas direcciones.", 20, 3,
		"Se bloquearon las direcciones origen en el WAF, se forzó el restablecimiento de clave de las cuentas atacadas y se activó la limitación por IP y por cuenta."),
	sec("mercado", "auth-clientes", "Credential stuffing contra clientes", "Muchas cuentas distintas fueron probadas desde pocas IP con credenciales filtradas.", 25, 2,
		"Se activó el desafío de verificación para las IP sospechosas, se bloquearon los rangos origen y se notificó a los clientes con intentos exitosos para cambiar su clave."),
	sec("clinica", "ficha-clinica-db", "Lectura masiva de fichas fuera de horario", "Una cuenta de servicio leyó un volumen anómalo de fichas durante la madrugada.", 50, 2,
		"Se revocaron las credenciales de la cuenta, se revisó el registro de accesos para dimensionar el alcance y se rotaron las llaves. No hubo evidencia de salida de datos hacia el exterior."),
	sec("mercado", "api-catalogo", "Intento de inyección SQL en el catálogo", "El WAF detectó parámetros con cargas de inyección SQL en las búsquedas.", 15, 2,
		"Se confirmó que las consultas estaban parametrizadas, se endureció la regla del WAF y se bloquearon las direcciones origen."),
}

func pickScenario(rng *rand.Rand, securityShare float64) scenario {
	wantSec := rng.Float64() < securityShare
	total := 0
	for _, s := range scenarios {
		if (s.category == "Seguridad") == wantSec {
			total += s.weight
		}
	}
	n := rng.IntN(total)
	for _, s := range scenarios {
		if (s.category == "Seguridad") != wantSec {
			continue
		}
		if n < s.weight {
			return s
		}
		n -= s.weight
	}
	return scenarios[0]
}

// Scenario es la vista pública de un escenario, para que la maratón reutilice
// el mismo catálogo de títulos y notas de resolución.
type Scenario struct {
	Platform, Service, Title, Severity, Category string
	Notes                                        []string
}

// Scenarios devuelve el catálogo de escenarios que puede usar la maratón. Los
// de peso 0 (los BFF) solo aportan su nota al reinicio: no se sortean.
func Scenarios() []Scenario {
	out := make([]Scenario, 0, len(scenarios))
	for _, s := range scenarios {
		if s.weight == 0 {
			continue
		}
		out = append(out, Scenario{s.platform, s.service, s.title, s.severity, s.category, s.notes})
	}
	return out
}

// ClosingNote es la nota de resolución del escenario de un servicio para esa
// categoría. El reinicio la usa cuando no hay una falla o ataque activo que
// nombre la causa; "" significa que no hay nota aplicable.
func ClosingNote(platform, service, category string) string {
	for _, s := range scenarios {
		if s.platform == platform && s.service == service && s.category == category && len(s.notes) > 0 {
			return s.notes[0]
		}
	}
	return ""
}
