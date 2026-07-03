# syntax=docker/dockerfile:1

# ---- Stage 1: build the admin UI (placeholder until the Vite app lands) ----
# FROM node:22-bookworm-slim AS ui
# WORKDIR /ui
# COPY ui/package.json ui/package-lock.json* ./
# RUN npm ci
# COPY ui/ ./
# RUN npm run build            # -> /ui/dist

# ---- Stage 2: build the Go binary (embeds ui/dist) ----
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# COPY --from=ui /ui/dist ./ui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dublyobase .

# ---- Stage 3: runtime = Debian + PGDG Postgres 16/17/18 ----
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl gnupg \
 && install -d /usr/share/postgresql-common/pgdg \
 && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
      -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc \
 && echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" \
      > /etc/apt/sources.list.d/pgdg.list \
 && apt-get update \
 && apt-get install -y --no-install-recommends \
      postgresql-16 postgresql-17 postgresql-18 \
 && rm -rf /var/lib/apt/lists/*

# Postgres refuses to run as root — run everything as a dedicated user.
RUN useradd --create-home --uid 10001 dublyo \
 && mkdir -p /data && chown dublyo:dublyo /data

COPY --from=build /out/dublyobase /usr/local/bin/dublyobase

ENV PGSUPER_PG16_BINDIR=/usr/lib/postgresql/16/bin \
    PGSUPER_PG17_BINDIR=/usr/lib/postgresql/17/bin \
    PGSUPER_PG18_BINDIR=/usr/lib/postgresql/18/bin

USER dublyo
VOLUME /data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=3s --start-period=20s \
  CMD ["/usr/local/bin/dublyobase", "cluster", "list"]
ENTRYPOINT ["dublyobase"]
CMD ["serve", "--dir", "/data", "--http", "0.0.0.0:8090"]
