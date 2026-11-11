# demo-lab: simulador de plataformas cliente. Una imagen autocontenida: no
# necesita el código de Sentinel, solo poder alcanzarlo por HTTP.
#
#   docker compose up -d --build        (ver docker-compose.yml y .env.example)

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY server ./server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/demo-lab ./server/cmd/demo-lab

FROM alpine:3.21
# ca-certificates: Sentinel en AWS se alcanza por HTTPS.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app \
 && mkdir /data && chown app /data
COPY --from=build /out/demo-lab /usr/local/bin/demo-lab
USER app
# Escucha en $PORT en todas las interfaces: 7000 por omisión (compose) y el
# que asigne Render al arrancar. DEMO_LISTEN, si se define, lo reemplaza.
ENV PORT=7000 \
    DEMO_DATA_DIR=/data
VOLUME /data
EXPOSE 7000
# Solo mira que el proceso viva: ni Sentinel ni las fallas simuladas deben reiniciarlo.
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" >/dev/null || exit 1
ENTRYPOINT ["demo-lab"]
