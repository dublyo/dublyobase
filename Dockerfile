# syntax=docker/dockerfile:1

# ---- build: static admin UI export ----
FROM node:22-alpine AS ui-build
WORKDIR /src/ui/admin
COPY ui/admin/package.json ui/admin/package-lock.json ./
RUN npm ci
COPY ui/admin ./
RUN npm run build

# ---- build: static Go binary (embeds ui/dist) ----
FROM golang:1.25-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui-build /src/ui/dist ./ui/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X github.com/dublyo/dublyobase/core.Version=${VERSION}" \
      -o /out/dublyobase .

# ---- runtime: tiny alpine, non-root, one process ----
FROM alpine:3.23
RUN apk add --no-cache ca-certificates postgresql18-client wget \
 && adduser -D -u 1001 dublyo \
 && mkdir -p /data/storage \
 && chown -R 1001:1001 /data

COPY --from=build /out/dublyobase /usr/local/bin/dublyobase

USER 1001
WORKDIR /data
EXPOSE 8080

ENV HOST=0.0.0.0 \
    PORT=8080 \
    STORAGE_TYPE=local \
    STORAGE_LOCAL_PATH=/data/storage \
    MIGRATE_ON_START=true \
    TRUST_PROXY_HEADERS=false \
    LOG_FORMAT=json

# Use 127.0.0.1 (not localhost): busybox wget prefers IPv6 ::1, which fails
# against an IPv4-only bind. Shell form so ${PORT} tracks the env override.
HEALTHCHECK --interval=15s --timeout=3s --start-period=30s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT:-8080}/health" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["dublyobase"]
CMD ["serve"]
