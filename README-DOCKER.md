# Levantar la demo con Docker (resumen)

Requisitos: Docker con Compose, este repositorio y Sentinel local levantado.

## 1. Levantar Sentinel local

Desde la raíz del repositorio:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
```

- Frontend: http://localhost:5173
- Mailpit (correos de verificación): http://localhost:8025

## 2. Crear la cuenta en Sentinel

1. Abre http://localhost:5173/register.
2. Completa la empresa, el correo y la contraseña. Esa cuenta queda como **Administrador** de la empresa demo.
3. Abre http://localhost:8025, busca el correo de verificación e ingresa el código de 6 dígitos en Sentinel.
4. Si la cuenta ya existe, solo inicia sesión; no hace falta crearla de nuevo.

## 3. Revisar que sea la misma cuenta del `.env`

Desde `demo-lab/`, crea el `.env` si todavía no existe:

```bash
cd demo-lab
cp .env.example .env
```

En PowerShell: `Copy-Item .env.example .env`.

Edita `demo-lab/.env` y deja exactamente el correo y la contraseña de la cuenta creada:

```env
DEMO_SENTINEL_EMAIL=admin@empresa-demo.cl
DEMO_SENTINEL_PASSWORD=la-misma-clave
```

No cambies `DEMO_SENTINEL_URL` ni `DEMO_PUBLIC_URL`: el compose de Sentinel local los ajusta solo.

Para abrir la consola desde una tablet, agrega `DEMO_BIND=0.0.0.0`. Sin `DEMO_CONSOLE_KEY` eso también deja abiertas la consola y la API de control; con ella, piden clave en `/acceso` (ver `.env.example`).

## 4. Construir y correr la demo

Desde la carpeta `demo-lab/` (el compose se une a la red `sentinel_monitoring-network`, así que Sentinel debe estar arriba primero):

```bash
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml build demo-lab
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml up -d
```

En un solo paso:

```bash
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml up -d --build
```

Abre http://localhost:7000. El módulo se conecta solo, prepara el escenario y enciende el tráfico base.

```bash
# ver el arranque y los errores de conexión
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml logs -f demo-lab
```

Si la consola queda en rojo, revisa que el correo esté verificado, que las credenciales del `.env` sean las mismas y que Sentinel esté arriba.

## 5. Detener

```bash
docker compose -f docker-compose.yml -f docker-compose.sentinel-local.yml down
```

`down -v` también borra el estado y los tokens: el siguiente arranque vuelve a preparar el escenario desde cero.
