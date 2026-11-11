// Textos de la guía de la consola. Una sola tabla (GUIDE) alimenta el panel y
// las explicaciones que se ven junto a cada grupo de botones, para no mantener
// dos textos. Cada entrada responde: qué hace, qué se ve en el sitio, qué se ve
// en Sentinel y cómo se deshace.
//
// Los ids siguen el patrón que usa console.js:
//   general-*   controles de la barra, Inicio y acciones generales
//   platform-*  pestaña de una plataforma
//   group-*     encabezado de un grupo de controles
//   fault-*     botón de falla
//   attack-*    tipo de ataque (attack-<name> del catálogo)
//   script-*    guion incluido (script-<id> de scripts/library)
//   user-*      acciones de usuario
//   activity-*  pestaña Actividad
//   data-*      pestaña Datos y envío

export const GROUPS = [
  "Generales",
  "Pestaña de plataforma",
  "Fallas",
  "Ataques",
  "Guiones",
  "Actividad",
  "Datos y envío",
];

// Pestaña desde la que se abre la guía: la sección que se muestra primero.
export const TAB_GROUP = {
  inicio: "Generales",
  mercado: "Pestaña de plataforma",
  banco: "Pestaña de plataforma",
  clinica: "Pestaña de plataforma",
  actividad: "Actividad",
  datos: "Datos y envío",
};

// Cómo operar el stand. Resume la §14 del plan sin repetirla entera.
export const OPERATION = [
  {
    title: "Preparar el stand",
    items: [
      "Sentinel arriba y la consola en verde: la lista «Antes de abrir» de Inicio lo comprueba sola.",
      "Pulsa «Reiniciar escenario» para dejar todo limpio; el tráfico vuelve al valor base (2 usuarios/s por omisión).",
      "Espera 10 minutos con tráfico continuo para calentar la ventana de sondeos. La lista muestra el tiempo medido desde que esta consola vio tráfico en las tres plataformas.",
      "En una pantalla deja el sitio; en la otra, el dashboard de Sentinel con la carpeta de la plataforma abierta.",
    ],
  },
  {
    title: "Recorrido con un visitante",
    items: [
      "El visitante usa el sitio: una compra, una transferencia o una reserva.",
      "Provoca una falla desde la pestaña de la plataforma o ejecuta un guion.",
      "Repite la operación: el sitio muestra el error de negocio con el código y el servicio causante.",
      "En Sentinel aparecen el aviso, la alerta que agrupa la cascada y, poco después, la caída de disponibilidad del sondeo.",
      "Revisa el mapa de dependencias, la causa raíz, los incidentes similares y la guía del Copiloto.",
      "Restaura el servicio, cierra el ticket y muestra el MTTR y el SLA.",
    ],
  },
  {
    title: "Entre visitantes",
    items: [
      "Pulsa «Reiniciar escenario»: retira fallas, detiene guiones y ataques, deja el tráfico base y cierra los incidentes abiertos de los tres proyectos demo.",
      "Sigue cerrando durante 90 segundos lo que llegue tarde desde la cola de Sentinel. Se detiene antes si aparece una falla o un ataque nuevo.",
      "Si no hay nadie, la maratón puede rotar escenarios para que el dashboard siga teniendo actividad.",
    ],
  },
];

export const GUIDE = {
  /* ── Generales ────────────────────────────────────────────────────────── */
  "general-checklist": {
    group: "Generales",
    label: "Lista «Antes de abrir»",
    what: "Comprueba los seis puntos de la operación: Sentinel conectado, escenario preparado, nodos del mapa con sondeo, sin fallas ni ataques, tráfico encendido y calentamiento de 10 minutos. El calentamiento se mide desde que esta consola ve tráfico en las tres plataformas; si recargas la página, vuelve a contar.",
    site: "No cambia nada en los sitios.",
    sentinel: "Solo lee el estado del módulo. Si hace falta, ejecuta la verificación de conexión, que no escribe.",
    undo: "No aplica.",
  },
  "general-nodes": {
    group: "Generales",
    label: "Nodos del mapa con sondeo",
    what: "Comprueba que cada uno de los 16 servicios tenga su health_url y que el Poller ya haya recibido al menos un sondeo suyo. Mientras no llegue, Sentinel dibuja el nodo en gris (unknown), aunque el proyecto esté sano. El primer sondeo llega en unos segundos y la ventana de 10 minutos del calentamiento también alimenta la alerta preventiva de cada servicio.",
    site: "No cambia nada.",
    sentinel: "Solo lee /api/v1/monitor/status y los componentes de cada proyecto. No escribe.",
    undo: "No aplica. Si un nodo sigue en gris, revisa DEMO_PUBLIC_URL y vuelve a preparar el escenario.",
  },
  "general-portal": {
    group: "Generales",
    label: "Portal del cliente",
    what: "Muestra el correo del usuario Cliente de cada plataforma y, si DEMO_SENTINEL_WEB_URL está definida, el enlace a /portal. El aprovisionamiento da acceso al primer contacto de cada plataforma y activa su clave con DEMO_PORTAL_PASSWORD. Sin esa variable no se toca nada.",
    site: "El visitante puede entrar al portal como cliente, ver el estado de su servicio y reportar un problema; el ticket llega al dashboard.",
    sentinel: "El usuario Cliente nace por la API de acceso; el reporte crea una Alerta Maestra de categoría Correo y no se une al incidente automático.",
    undo: "Revocar el acceso desde la consola de Sentinel o volver a preparar el escenario sin DEMO_PORTAL_PASSWORD.",
  },
  "general-verify": {
    group: "Generales",
    label: "Verificar conexión",
    what: "Prueba por separado alcance, inicio de sesión, proyectos, alertas, ingesta, sondeo, nodos, portal y canales. Un lote vacío comprueba el token de ingesta sin escribir nada.",
    site: "No cambia lo que se ve; los sitios siguen igual.",
    sentinel: "Solo lecturas y un lote vacío de ingesta. No crea ni modifica nada.",
    undo: "No aplica.",
  },
  "general-provision": {
    group: "Generales",
    label: "Preparar escenario",
    what: "Crea o completa carpetas, proyectos, componentes, dependencias, token de ingesta, SLA, umbrales y clientes en Sentinel. Es idempotente: repetirlo no duplica y no reemite el token.",
    site: "Nada visible.",
    sentinel: "Deja los tres proyectos demo listos para recibir logs y sondeos.",
    undo: "No se deshace desde la consola; habría que borrar los proyectos en Sentinel.",
  },
  "general-open-site": {
    group: "Generales",
    label: "Abrir sitio",
    what: "Abre el sitio de la plataforma en otra pestaña.",
    site: "El sitio completo de la plataforma, con su Modo Director (tecla D).",
    sentinel: "Nada.",
    undo: "Cerrar la pestaña.",
  },
  "general-traffic": {
    group: "Generales",
    label: "Tráfico (usuarios/s)",
    what: "Fija los usuarios sintéticos por segundo de la plataforma. 0 detiene el tráfico; el máximo es 50. Se aplica al salir del campo o al confirmar.",
    site: "Con más tráfico las operaciones se repiten más seguido; el contenido no cambia.",
    sentinel: "Sube el volumen de logs y de señales. El muestreo de INFO y el presupuesto por minuto limitan lo que llega a Bedrock.",
    undo: "Volver al valor anterior o a 0. «Reiniciar escenario» lo devuelve al tráfico base.",
  },
  "general-restore": {
    group: "Generales",
    label: "Restaurar plataforma",
    what: "Retira todas las fallas de esa plataforma. No detiene ataques ni guiones, no cierra incidentes y no toca el tráfico.",
    site: "El sitio vuelve a funcionar si la falla era la causa.",
    sentinel: "El sondeo se recupera y la alerta de disponibilidad se cierra sola. El incidente de logs queda abierto hasta cerrarlo.",
    undo: "Volver a provocar la falla.",
  },
  "general-restore-all": {
    group: "Generales",
    label: "Restaurar todo",
    what: "Retira las fallas de las tres plataformas a la vez. No detiene ataques ni guiones, no cierra incidentes y no toca el tráfico.",
    site: "Los tres sitios vuelven a funcionar si las fallas eran la causa.",
    sentinel: "Los sondeos se recuperan y las alertas de disponibilidad se cierran solas. Los incidentes de logs quedan abiertos.",
    undo: "Volver a provocar la falla.",
  },
  "general-reset": {
    group: "Generales",
    label: "Reiniciar escenario",
    what: "Detiene la maratón, los guiones y los ataques; retira las fallas; deja el tráfico en el valor base (DEMO_TRAFFIC_RPS, 2 por omisión); cierra todos los incidentes abiertos de los proyectos demo, incluidos los de Seguridad, y sigue cerrando 90 segundos lo que llegue tarde. Pide confirmación.",
    site: "Los sitios quedan sanos y, con el tráfico base, vuelven a verse operaciones normales.",
    sentinel: "Los incidentes abiertos de los tres proyectos demo se cierran como Operativo o Seguridad. Lo que llega tarde también se cierra, salvo que empiece otro ensayo.",
    undo: "No se deshace en Sentinel: los incidentes cerrados no se reabren. Para volver a ensayar, provoca la falla otra vez.",
  },
  "general-menu": {
    group: "Generales",
    label: "Menú ⋯",
    what: "Agrupa «Restaurar todo» y «Reiniciar escenario» para tenerlos a mano desde cualquier pestaña, separados de los botones de uso normal.",
    site: "Nada por sí solo.",
    sentinel: "Nada por sí solo.",
    undo: "Cerrar el menú.",
  },

  /* ── Pestaña de plataforma ────────────────────────────────────────────── */
  "platform-tabs": {
    group: "Pestaña de plataforma",
    label: "Pestañas de plataforma",
    what: "Cada plataforma tiene su pestaña. El punto muestra su estado y el contador las fallas y ataques activos, para que una falla en Banco no quede invisible desde Mercado. La pestaña activa se guarda en la URL (#banco).",
    site: "No cambia nada.",
    sentinel: "No cambia nada.",
    undo: "No aplica.",
  },
  "group-services": {
    group: "Pestaña de plataforma",
    label: "Servicios de la plataforma",
    what: "Una fila por servicio con estado, rol, peticiones y errores del último minuto. Al desplegarla aparecen las cinco fallas, cada una con su explicación. Solo hay una fila abierta a la vez.",
    site: "El estado de la fila anticipa lo que verá el visitante si esa falla se activa.",
    sentinel: "Las peticiones y errores salen de los logs locales, los mismos que alimentan la ingesta.",
    undo: "Cerrar la fila. Las fallas se retiran con la «X» de la fila o con «Restaurar plataforma».",
  },
  "group-scripts": {
    group: "Pestaña de plataforma",
    label: "Guiones de la plataforma",
    what: "Solo aparecen los guiones de esa plataforma. Ejecutar inicia su línea de tiempo; solo puede haber una ejecución activa por plataforma.",
    site: "El guion cambia el tráfico, las fallas o los ataques según sus pasos, y el sitio reacciona.",
    sentinel: "Produce los incidentes que correspondan a esos cambios.",
    undo: "Pausar, reanudar o abortar. Abortar deja la plataforma sana.",
  },
  "group-attacks": {
    group: "Pestaña de plataforma",
    label: "Ataques de la plataforma",
    what: "Solo se ofrecen los tipos de ataque con objetivos en esta plataforma. La lista de lanzados también se filtra.",
    site: "El WAF simulado del servicio registra los eventos; la operación normal no se cae.",
    sentinel: "Los eventos llegan como logs y pueden abrir un incidente de categoría Seguridad.",
    undo: "Detener el ataque. Los eventos ya enviados quedan.",
  },
  "user-actions": {
    group: "Pestaña de plataforma",
    label: "Probar como usuario",
    what: "Ejecuta una operación real de usuario contra el BFF: inicio de sesión, consulta, compra, agregar al carro, transferencia, reserva o exámenes. Devuelve un comprobante o el error con el servicio causante.",
    site: "Equivale a hacer la operación en el sitio, sin pasar por la interfaz.",
    sentinel: "Genera logs reales en toda la cadena de servicios.",
    undo: "No aplica: cada clic es una operación nueva.",
  },
  "platform-logs-link": {
    group: "Pestaña de plataforma",
    label: "Ver logs de esta plataforma",
    what: "Abre Actividad con el filtro de esta plataforma ya puesto.",
    site: "Nada.",
    sentinel: "Muestra la copia local de los logs de la plataforma.",
    undo: "Volver a la pestaña o cambiar el filtro.",
  },

  /* ── Fallas ────────────────────────────────────────────────────────────── */
  "fault-503": {
    group: "Fallas",
    label: "Responder 503",
    what: "El servicio contesta error 503 a todas las peticiones.",
    site: "Error de negocio en las operaciones que lo usan, con el código y el servicio causante.",
    sentinel: "Incidente de logs que agrupa la cascada y «Caída de disponibilidad» del sondeo, porque la salud profunda recorre toda la cadena.",
    undo: "Retirarla con la «X» o con «Restaurar plataforma».",
  },
  "fault-refused": {
    group: "Fallas",
    label: "Cerrar puerto",
    what: "El servicio deja de aceptar conexiones.",
    site: "Igual que «Responder 503»: la operación falla con error de negocio.",
    sentinel: "Igual que «Responder 503», con error de conexión en los logs.",
    undo: "Retirarla con la «X» o con «Restaurar plataforma».",
  },
  "fault-latency": {
    group: "Fallas",
    label: "Lentitud 950 ms",
    what: "Agrega 950 ms ± 100 a cada respuesta, sin devolver error.",
    site: "Funciona, pero lento.",
    sentinel: "Alerta preventiva por σ cuando la ventana de sondeos está caliente (unos 40 s después en TC-09).",
    undo: "Retirarla con la «X» o con «Restaurar plataforma».",
  },
  "fault-errors": {
    group: "Fallas",
    label: "Errores 30 %",
    what: "Falla tres de cada diez peticiones.",
    site: "Fallos intermitentes: unas operaciones pasan y otras no.",
    sentinel: "Señales de error en el incidente. Con poco tráfico puede tardar en abrir uno: conviene lanzarla con tráfico sostenido.",
    undo: "Retirarla con la «X» o con «Restaurar plataforma».",
  },
  "fault-saturation": {
    group: "Fallas",
    label: "Saturar",
    what: "El servicio atiende solo 2 peticiones a la vez; el resto espera o se rechaza.",
    site: "Lento o con rechazos según el tráfico.",
    sentinel: "Señales de saturación. Con poco tráfico puede no abrir incidente: súbelo antes de usarla.",
    undo: "Retirarla con la «X» o con «Restaurar plataforma».",
  },
  "fault-remove": {
    group: "Fallas",
    label: "Retirar una falla",
    what: "La «X» de la fila retira esa falla en el acto, sin tocar las demás.",
    site: "El servicio se recupera en la siguiente petición; el sitio puede tardar unos segundos en reflejarlo.",
    sentinel: "El sondeo se recupera y la alerta de disponibilidad se cierra sola. El incidente de logs sigue abierto.",
    undo: "Volver a provocar la falla.",
  },

  /* ── Ataques ───────────────────────────────────────────────────────────── */
  "attack-launch": {
    group: "Ataques",
    label: "Lanzar ataque",
    what: "Envía eventos maliciosos sintéticos al servicio elegido, a la intensidad y duración indicadas. No derriba el servicio: el WAF simulado registra cada evento.",
    site: "La operación normal sigue funcionando; con mucha intensidad puede verse más lenta.",
    sentinel: "Los eventos llegan como logs y pueden abrir un incidente de categoría Seguridad (TC-11).",
    undo: "«Detener» corta el envío. Los eventos ya enviados quedan.",
  },
  "attack-brute_force": {
    group: "Ataques",
    label: "Fuerza bruta",
    what: "Ráfaga de inicios de sesión fallidos de una misma cuenta. Objetivos: auth-clientes, auth-banca y auth-pacientes.",
    site: "El inicio de sesión normal sigue disponible; el WAF registra los intentos.",
    sentinel: "Incidente de Seguridad con los eventos del WAF.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-credential_stuffing": {
    group: "Ataques",
    label: "Credential stuffing",
    what: "Inicios de sesión fallidos de muchas cuentas desde pocas IP. Objetivos: auth-clientes, auth-banca y auth-pacientes.",
    site: "El inicio de sesión normal sigue disponible; el WAF registra los intentos.",
    sentinel: "Incidente de Seguridad con los eventos del WAF.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-mfa_bypass": {
    group: "Ataques",
    label: "Bypass de segundo factor",
    what: "Reintentos de OTP y tokens reutilizados. Solo aplica a auth-banca.",
    site: "El inicio de sesión del banco sigue funcionando; el WAF registra los reintentos.",
    sentinel: "Incidente de Seguridad con los eventos del WAF.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-sqli": {
    group: "Ataques",
    label: "Inyección SQL",
    what: "Peticiones con firmas SQL contra el catálogo o un BFF. Objetivos: api-catalogo, storefront-web, banca-en-linea, portal-paciente y agenda-medica.",
    site: "La operación normal sigue; la regla WAF simulada registra cada intento.",
    sentinel: "Incidente de Seguridad con los eventos del WAF.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-exfiltration": {
    group: "Ataques",
    label: "Exfiltración de datos",
    what: "Lecturas masivas fuera de horario. Objetivos: ficha-clinica-db, core-bancario y db-transaccional.",
    site: "La operación normal sigue; el WAF registra las lecturas.",
    sentinel: "Incidente de Seguridad con los eventos del WAF.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-scraping": {
    group: "Ataques",
    label: "Bots / scraping",
    what: "Volumen anómalo por IP y user-agent. Objetivos: api-catalogo y agenda-medica.",
    site: "La operación normal sigue; el WAF registra el volumen anómalo.",
    sentinel: "Puede clasificarse como Seguridad u Operativo según el resto de las señales.",
    undo: "Detener el ataque; los eventos ya enviados quedan.",
  },
  "attack-stop": {
    group: "Ataques",
    label: "Detener ataque",
    what: "Corta el envío de eventos del ataque en curso.",
    site: "Nada; el sitio ya no recibe los eventos sintéticos.",
    sentinel: "Los eventos ya enviados quedan y el incidente se cierra por la vía normal.",
    undo: "Volver a lanzarlo.",
  },

  /* ── Guiones ───────────────────────────────────────────────────────────── */
  "script-run-control": {
    group: "Guiones",
    label: "Pausar, reanudar y abortar",
    what: "Pausar congela el reloj del guion; Reanudar sigue donde quedó; Abortar detiene la ejecución y deja la plataforma sana.",
    site: "Pausar mantiene el estado actual; Abortar devuelve el sitio a la normalidad.",
    sentinel: "Abortar retira las fallas y detiene los ataques de la plataforma. Los incidentes ya abiertos no se cierran.",
    undo: "Un guion abortado no se reanuda: hay que ejecutarlo de nuevo.",
  },
  "script-caida-bd-mercado": {
    group: "Guiones",
    label: "Caída de la base transaccional en plena compra",
    what: "Enciende tráfico a 2 usuarios/s, a los 20 s cierra db-transaccional, a los 25 s hace tres compras, a los 150 s restaura todo y a los 155 s hace dos compras de comprobación. Dura 155 s.",
    site: "Las compras fallan con error de negocio desde el segundo 20 y vuelven a funcionar al restaurar.",
    sentinel: "Abre el incidente de logs de la cascada y la caída de disponibilidad del sondeo; este último se cierra al restaurar.",
    undo: "Termina solo. «Abortar» restaura la plataforma en el acto.",
  },
  "script-degradacion-pasarela": {
    group: "Guiones",
    label: "Degradación de la pasarela de pagos",
    what: "Enciende tráfico a 2 usuarios/s, a los 10 s aplica lentitud de 950 ms ± 100 y a los 120 s restaura. Dura 120 s. Necesita unos 10 minutos de tráfico sano antes para calentar la ventana σ.",
    site: "El pago funciona, pero tarda cerca de un segundo.",
    sentinel: "Alerta preventiva de rendimiento unos 40 s después, con la ventana caliente (TC-09).",
    undo: "Termina solo. «Abortar» restaura la plataforma en el acto.",
  },
  "script-fuerza-bruta-banco": {
    group: "Guiones",
    label: "Fuerza bruta contra el inicio de sesión del banco",
    what: "Enciende tráfico a 1 usuario/s, a los 10 s lanza fuerza bruta contra auth-banca (2 eventos/s durante 40 s) y a los 70 s restaura. Dura 70 s.",
    site: "El inicio de sesión sigue funcionando; el WAF registra los intentos.",
    sentinel: "Incidente de Seguridad con los eventos del WAF (TC-11).",
    undo: "Termina solo. «Abortar» detiene el ataque y restaura la plataforma.",
  },

  /* ── Actividad ─────────────────────────────────────────────────────────── */
  "activity-logs": {
    group: "Actividad",
    label: "Logs en vivo",
    what: "Últimos 200 logs del módulo, filtrables por plataforma y nivel. Se actualiza cada 3 segundos solo mientras esta pestaña está abierta.",
    site: "Nada.",
    sentinel: "Son los mismos eventos que alimentan la ingesta; aquí se ve la copia local.",
    undo: "No aplica.",
  },
  "activity-journal": {
    group: "Actividad",
    label: "Bitácora",
    what: "Registro de las acciones de la sesión con hora UTC, origen y resultado. El enlace CSV la descarga para el post-mortem del ensayo.",
    site: "Nada.",
    sentinel: "Nada; es un registro local del módulo.",
    undo: "No aplica.",
  },

  /* ── Datos y envío ─────────────────────────────────────────────────────── */
  "data-shipping": {
    group: "Datos y envío",
    label: "Envío de logs",
    what: "Activa el envío, fija el nivel mínimo, el muestreo de INFO y el presupuesto por minuto. Cada evento procesado cuesta una llamada a Bedrock.",
    site: "Nada.",
    sentinel: "Controla cuántos logs llegan. Lo que no se envía queda en la copia local.",
    undo: "Volver a aplicar otros valores.",
  },
  "data-reship": {
    group: "Datos y envío",
    label: "Reenviar últimos 10 min",
    what: "Reenvía los logs guardados de los últimos 10 minutos con nivel error o superior e informa cuántos aceptó y cuántos rechazó Sentinel.",
    site: "Nada.",
    sentinel: "Puede duplicar eventos ya enviados; úsalo para recuperar un hueco.",
    undo: "No se deshace: los eventos reenviados quedan.",
  },
  "data-history": {
    group: "Datos y envío",
    label: "Generar historial sintético",
    what: "Genera N días de incidentes resueltos con MTTR, categoría, bitácora y ventanas de indisponibilidad. Solo perfil local con allow_db_backfill y DEMO_DB_DSN a localhost. No funciona desde el contenedor: se carga con go run en la máquina que tiene Sentinel. Repetirlo reemplaza el historial anterior.",
    site: "Nada.",
    sentinel: "Aparecen incidentes históricos marcados [demo-lab:historial] en analíticas e informes. No toca incidentes reales.",
    undo: "«Borrar historial» los elimina.",
  },
  "data-history-delete": {
    group: "Datos y envío",
    label: "Borrar historial",
    what: "Borra solo el historial sintético marcado [demo-lab:historial]. Pide confirmación en el mismo botón.",
    site: "Nada.",
    sentinel: "Desaparecen los incidentes históricos de los informes; los incidentes reales no se tocan.",
    undo: "Volver a generar el historial.",
  },
  "data-marathon": {
    group: "Datos y envío",
    label: "Maratón",
    what: "Durante H horas rota escenarios cada N minutos: provoca una falla o un ataque, espera el incidente, lo asigna y lo cierra por la API con notas. Es la forma de tener historia real en AWS.",
    site: "Los sitios muestran fallas y recuperaciones periódicas.",
    sentinel: "Incidentes resueltos con fechas reales, notas y responsable.",
    undo: "«Detener» corta la maratón; no cierra los incidentes que ya abrió.",
  },
  "data-marathon-stop": {
    group: "Datos y envío",
    label: "Detener maratón",
    what: "Detiene la maratón; el ciclo en curso termina y no se lanzan más.",
    site: "Los sitios dejan de recibir fallas programadas.",
    sentinel: "Los incidentes abiertos siguen su curso normal.",
    undo: "Volver a iniciarla.",
  },
};
