# syntax=docker/dockerfile:1

# ---- build: static Go binary (embeds ui/dist) ----
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/dublyobase .

# ---- runtime: tiny alpine, non-root, one process ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget \
 && adduser -D -u 1001 dublyo \
 && mkdir -p /data/storage \
 && chown -R 1001:1001 /data

COPY --from=build /out/dublyobase /usr/local/bin/dublyobase

USER 1001
WORKDIR /data
VOLUME /data
EXPOSE 8080

ENV HOST=0.0.0.0 \
    PORT=8080 \
    STORAGE_TYPE=local \
    STORAGE_LOCAL_PATH=/data/storage \
    MIGRATE_ON_START=true \
    TRUST_PROXY_HEADERS=true \
    LOG_FORMAT=json

# Use 127.0.0.1 (not localhost): busybox wget prefers IPv6 ::1, which fails
# against an IPv4-only bind.
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health >/dev/null 2>&1 || exit 1

ENTRYPOINT ["dublyobase"]
CMD ["serve"]
