# demo-lab

Simulador de plataformas cliente para el stand del Demo Day de Sentinel: **Mercado**, **Banco** y **Clínica Don Federico** (marca ficticia). Es un módulo independiente que le entrega todo a Sentinel por sus pipelines oficiales (ingesta, API pública y sondeo), como lo haría una empresa cliente real; la única excepción, por ahora, es el historial sintético. No comparte código con Sentinel y **no se despliega en AWS ni se mergea a `dev`/`main`**.

Se levanta por su cuenta, en Docker, sin el código de Sentinel (ver [Con Docker](#con-docker)).

Spec: [TASK-018](../specs/TASK-018-mockup-para-interactuar-con-servicios.sdd.md) · Plan: [plan-mockup-servicios-demoday.md](../docs/plan-mockup-servicios-demoday.md) · Empaquetado: [plan-demo-lab-autonomo.md](../docs/plan-demo-lab-autonomo.md) · Render: [plan-prueba-render-produccion.md](../docs/plan-prueba-render-produccion.md)

> En el repo público `sentinel-demo-lab` esos enlaces no abren: los documentos viven en el repo de Sentinel. Este repo es una copia de la carpeta `demo-lab/`, sin historial, para desplegarla en Render.

## Estado

| Fase | Estado |
|---|---|
| F0 · Desacople | hecha: Sentinel sin cambios; lo reutilizable de la v1 está en `_v1-referencia/` (las vistas React se reemplazaron en F4) |
| F1 · Esqueleto, conector y aprovisionamiento | hecha y verificada contra un `api-go` local |
| F2 · Servicios simulados, logs y envío | hecha en el módulo y verificada en vivo contra `api-go` y `api-ia` (Bedrock) |
| F3 · Consola y fallas | consola rediseñada por pestañas, con guía y adaptación a tablet (`http://localhost:7000/`), alerta preventiva verificada (TC-09); falta el ensayo completo en el stand (TC-08) |
| F4 · Sitios | hecha: `http://localhost:7000/sitios/{mercado,banco,clinica}/`; prueba E2E con Playwright |
| F5 · Ataques y guiones | hecha: WAF simulado, ataques con lista permitida, guiones JSON (`scripts/`), reinicio. Verificado: Sentinel clasifica como **Seguridad** |
| F6 · Datos para informes | hecha: historial sintético (solo local) y maratón por la API pública |
| A6 · Acceso | hecha: con `DEMO_CONSOLE_KEY` y `DEMO_SITE_PASSWORD`, consola y sitios piden clave en `/acceso`; obligatorias en el perfil `aws` |
| F7 en adelante | pendiente (plantillas desde URL, ensayo); prueba en Render contra producción en curso |

Sin implementar: la falla `crash loop` y la mezcla de acciones del tráfico (`mix`).

## Carpetas

- `server/` · Go (`go.mod` propio). `catalog/` plataformas, servicios y acciones; `runtime/` los 16 servicios simulados; `faults/` motor de fallas; `attacks/` generador de ataques; `scripts/` intérprete de guiones, con los incluidos en `scripts/library/`; `logs/` logger, formatos y envío a Sentinel; `traffic/` usuarios sintéticos; `sentinel/` cliente de la API pública y aprovisionador; `sentinel/fake/` Sentinel falso para pruebas; `control/` API de control; `config/` perfiles.
- `server/sites/static/` · los tres sitios y su base común (franja de sitio ficticio, Modo Director). Sin compilación.
- `Dockerfile`, `docker-compose.yml`, `docker-compose.sentinel-local.yml`, `.env.example` · empaquetado.
- `e2e/` · prueba de extremo a extremo de los sitios con Playwright.
- `.referencias_sitios/` · capturas de portales reales usadas solo como referencia de estructura. **Fuera de git; no se copia nada de ahí a los sitios.**
- `_v1-referencia/` · código de la primera versión (endpoints dentro de `api-go`). Solo referencia para los incidentes históricos de F6; Go lo ignora (prefijo `_`).
- `config/profiles/` · `local.example.json` y `aws.example.json`. Copia uno a `local.json` / `aws.json` (ignorados por git).
- `.state/` y `data/` · mapeo con tokens de ingesta, logs y bitácoras. **Fuera de git.**

## Con Docker

Es la forma recomendada: no hace falta Go ni el código de Sentinel. Solo Docker y una empresa demo ya registrada y verificada en Sentinel, con su Administrador. Guía corta: [README-DOCKER.md](README-DOCKER.md).

```bash
cd demo-lab
cp .env.example .env          # completa DEMO_SENTINEL_EMAIL y DEMO_SENTINEL_PASSWORD
docker compose up -d --build
```

Con el Sentinel de Docker en esta misma máquina, suma el segundo archivo para que ambos se vean por nombre en su red:

```bash
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml up -d --build
```

En unos segundos `http://localhost:7000/` muestra la consola y `http://localhost:7000/sitios/` los tres sitios. El módulo se conecta solo, prepara el escenario en Sentinel y enciende el tráfico de base (`DEMO_TRAFFIC_RPS`, 2 por omisión). Si Sentinel no responde, los sitios siguen funcionando y reintenta; el estado se ve en `GET /control/v1/status` (`bootstrap`).

- **Estado y logs** viven en el volumen `demo-lab-data`: reiniciar el contenedor conserva el escenario; `docker compose down -v` lo borra y el siguiente arranque lo prepara de nuevo.
- **Otros equipos del stand:** pon `DEMO_BIND=0.0.0.0` en `.env` y abre `http://<IP de esta máquina>:7000/sitios/`. Sin `DEMO_CONSOLE_KEY` también quedan abiertas la consola y la API de control.
- **Claves de acceso (A6):** con `DEMO_CONSOLE_KEY` la consola, el Modo Director y `/control/v1/*` piden esa clave en `/acceso`; `DEMO_SITE_PASSWORD` abre solo los sitios. `/salud/*`, `/healthz` y `/robots.txt` siguen abiertas para el sondeo de Sentinel. La sesión dura 12 h en una cookie firmada; para curl, `Authorization: Bearer <DEMO_CONSOLE_KEY>`. En el perfil `aws` las dos claves son obligatorias.
- **Sentinel en AWS:** `DEMO_SENTINEL_URL` con la URL de CloudFront y `DEMO_PUBLIC_URL` con una dirección pública que Sentinel pueda abrir (hoy, un túnel hacia el puerto 7000).
- **Historial sintético:** escribe en la base de Sentinel y no funciona desde el contenedor. Se carga con `go run` desde la máquina que tiene Sentinel (sección siguiente).
- **Variables:** ver `.env.example`.

## Uso con `go run` (perfil local)

Requisitos: Go 1.26 (con acceso a internet la primera vez, para bajar `lib/pq`) y un Sentinel local arriba (`docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d`) con las migraciones al día (el historial exige la versión 32), los roles de login de api-go y api-ia creados (`DB_API_GO_PASSWORD` y `DB_API_IA_PASSWORD` en el `.env` raíz y `devbox run db-roles`, ver `docs/COMANDOS-DEV.md` del repo de Sentinel) y una empresa demo con su Administrador ya registrados.

```bash
cd demo-lab
cp config/profiles/local.example.json config/profiles/local.json
export DEMO_SENTINEL_EMAIL=admin@empresa-demo.cl
export DEMO_SENTINEL_PASSWORD=...
export DEMO_DISCORD_WEBHOOK=https://discord.com/api/webhooks/...   # opcional; también DEMO_SLACK_WEBHOOK
go run ./server/cmd/demo-lab -profile config/profiles/local.json -addr 0.0.0.0:7000

curl -X POST localhost:7000/control/v1/connection/verify   # login, proyectos, alertas, ingesta, sondeo y canales
curl -X POST localhost:7000/control/v1/provision           # carpetas, proyectos, grafo, tokens (idempotente)
curl localhost:7000/control/v1/status

# actividad, falla y recuperación (el rps y los parámetros son ejemplos)
curl -X PUT  localhost:7000/control/v1/traffic/mercado -d '{"rps":2}'
curl -X POST localhost:7000/control/v1/faults -d '{"platform":"mercado","service":"db-transaccional","type":"down","params":{"mode":"refused"}}'
curl -X POST localhost:7000/control/v1/platforms/mercado/actions/checkout     # 503 con la causa raíz
curl -X POST localhost:7000/control/v1/platforms/mercado/restore
curl -X PUT  localhost:7000/control/v1/shipping -d '{"level_min":"info","info_sample_rate":1,"budget_per_min":600}'
```

El perfil de ejemplo trae `"services_url": "http://host.docker.internal:7000"`, la dirección con la que Sentinel alcanza el módulo; con Sentinel en Docker y el módulo en tu máquina, `-addr 0.0.0.0:7000` hace que la alcance. Sentinel sondea `/salud/{plataforma}` en ese puerto: los servicios simulados escuchan solo en `127.0.0.1`. Sus sondeos empiezan unos segundos después de preparar el escenario: hasta entonces la verificación marca `probe` en rojo.

Al conectarse, el módulo también deja en Sentinel lo que configura una empresa real: SLA por plataforma, umbrales de salud y seis clientes con su contacto (`catalog/policy.go`). Si ya existen, no los toca.

Desde TASK-016 Sentinel avisa por canales de cada servicio. Si defines `DEMO_DISCORD_WEBHOOK` o `DEMO_SLACK_WEBHOOK`, «Preparar escenario» los activa en los tres proyectos y conserva sus filtros; sin ellas no toca los canales y solo queda el correo, que `notifications` marca en rojo porque el aviso que se ve en un teléfono no llegaría. Las pruebas de `runtime` usan los puertos 7101-7125 de esta máquina: no las corras con un `demo-lab` encendido fuera de Docker.

## Consola

Abre `http://localhost:7000/` con el módulo en marcha. La barra fija muestra el estado de Sentinel, el perfil y dos accesos: `? Guía` (explica cada botón) y el menú `⋯` con «Restaurar todo» y «Reiniciar escenario» (pide confirmación). La pestaña activa se guarda en la URL (`#banco`).

- **Inicio:** lista «Antes de abrir» que se comprueba sola, resumen de las tres plataformas, fallas y ataques activos y accesos a los sitios.
- **Mercado, Banco, Clínica:** guiones, servicios con filas desplegables y sus cinco fallas, acciones de usuario y ataques, todo filtrado a esa plataforma. La pestaña muestra el estado y un contador de fallas y ataques activos.
- **Actividad:** logs en vivo y bitácora (CSV). Solo se consultan con la pestaña abierta.
- **Datos y envío:** política y métricas del envío, reenvío manual, maratón e historial sintético.

La consola se puede operar en una tablet vertical: bajo 900 px las filas pasan a tarjetas, las pestañas se desplazan y la guía ocupa todo el ancho. Los sondeos actualizan textos y clases; no reemplazan el HTML, así que no se borra lo que estés escribiendo ni se cierra la fila abierta.

Para que `api-ia` use Bedrock sin tocar el `.env`: `AWS_PROFILE=sentinel docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d api-ia` (con sesión activa de `aws login --profile sentinel`).

## Ataques y guiones

Desde la pestaña de cada plataforma (secciones «Guiones» y «Ataques») o por API:

```bash
curl -X POST localhost:7000/control/v1/attacks -d '{"platform":"banco","target":"auth-banca","kind":"brute_force","intensity":2,"duration_s":40}'
curl -X POST localhost:7000/control/v1/scripts/fuerza-bruta-banco/run     # devuelve el id de la ejecución
curl -X POST localhost:7000/control/v1/runs/r1/pause     # también resume y abort
curl -X POST localhost:7000/control/v1/reset             # sin fallas, ataques ni guiones; tráfico base y sin incidentes abiertos
```

Tipos: `brute_force`, `credential_stuffing`, `mfa_bypass`, `sqli`, `exfiltration`, `scraping`. Los guiones son JSON en `scripts/` (formato en el spec, §2.11).

El reinicio deja el tráfico en `DEMO_TRAFFIC_RPS` (2 por omisión), no en 0, para que los sitios y el dashboard sigan activos entre visitantes. Cierra por la API los incidentes abiertos de los proyectos demo y sigue cerrando durante 90 s lo que llegue tarde desde la cola de Sentinel. Se detiene antes solo si aplicas una falla o lanzas un ataque; el tráfico base no cuenta como ensayo nuevo. Si la cola tiene más retraso que eso, espera a que `alert.raw.received.v2` llegue a 0.

## Datos para informes

```bash
# Historial sintético: solo perfil local con "allow_db_backfill": true y DEMO_DB_DSN a localhost.
# Con el usuario maestro (admin), no con sentinel_api_go: este no puede leer schema_migrations.
export DEMO_DB_DSN="postgres://admin:password@localhost:5433/monitoring_db?sslmode=disable"
curl -X POST   localhost:7000/control/v1/history -d '{"days":30,"incidents_per_day":3,"mttr_profile":"mejora"}'
curl -X DELETE localhost:7000/control/v1/history

# Maratón (local o AWS): una falla o ataque cada 10 min durante 1 h; cierra lo que Sentinel abre
curl -X POST   localhost:7000/control/v1/marathon -d '{"hours":1,"cadence_min":10}'
curl           localhost:7000/control/v1/marathon      # ciclos
curl -X DELETE localhost:7000/control/v1/marathon
```

El historial se marca con `[demo-lab:historial]` en la descripción y nunca toca incidentes reales. Con otro perfil, otra versión del esquema o una base que no sea de localhost responde `409` sin conectarse. La maratón usa el usuario de `DEMO_SENTINEL_EMAIL` como responsable.

## Sitios

Con el módulo en marcha: `http://localhost:7000/sitios/` (índice), `/sitios/mercado/`, `/sitios/banco/` y `/sitios/clinica/`. En los tres, la tecla **D** abre el Modo Director (responder 503, cerrar el puerto, lentitud o errores, y restaurar). Credenciales de demostración: `cliente@demo.cl` / `demo1234` (mercado) y RUT `11.111.111-1` / `demo1234` (banco y clínica).

## Pruebas

```bash
go vet ./... && go test ./...      # unitarias y de integración (los servicios usan los puertos 7101-7125)
node e2e/sites.cjs                 # sitios de extremo a extremo; requiere demo-lab corriendo y Playwright
node e2e/console.cjs               # consola: pestañas, guía, redibujo y confirmación del reinicio
```

`console.cjs` prueba también una falla desde la consola cuando el escenario está preparado; si no lo está, esa parte se omite y el resto corre igual.

Las pruebas de Go corren contra `sentinel/fake`, sin Sentinel real. Las de Playwright necesitan el módulo encendido; las fallas necesitan además el escenario preparado.
