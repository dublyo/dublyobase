# dublyobase

**An open-source, Postgres-backed BaaS** in the spirit of PocketBase — auth, realtime,
storage, and an admin UI, served from **one Go binary on one port**. It connects to a
Postgres you provide (`DATABASE_URL`), runs migrations on boot, and ships as a single
`<30 MB` container to `ghcr.io/dublyo/dublyobase`. One-click deployable on Dublyo.

> Status: **early development.** See [dublyobase-dev.md](../dublyobase-dev.md) for the
> full architecture and milestone roadmap.

## What makes it different

- **One process, external Postgres.** No bundled DB, no nginx, no Redis. Pick your
  Postgres major (16/17/18) at the infra level; dublyobase just connects.
- **Security enforced in Postgres.** Per-project roles + `SET LOCAL ROLE` +
  `FORCE ROW LEVEL SECURITY`; hashed API keys; encrypted secrets. Deny → `403 rls_denied`.
- **Realtime over `LISTEN/NOTIFY`** — no Redis. **Collections** model with auto REST + RLS.

## Quick start (local)

```bash
docker compose up -d          # starts postgres + dublyobase
# open http://localhost:8080  (GET /health -> 200)
```

Or run the binary against any Postgres:

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/db?sslmode=disable"
export APP_URL="http://localhost:8080"
export JWT_SECRET="$(openssl rand -base64 32)"   # >= 32 chars, required
export ADMIN_EMAIL="admin@example.com" ADMIN_PASSWORD="change-me"
go run .            # connects, migrates, seeds admin, serves on :8080
```

## Configuration

All configuration is via environment variables (the Dublyo template contract). Required:
`DATABASE_URL`, `APP_URL`, `JWT_SECRET`. See
[core/config.go](core/config.go) for the full list (storage, SMTP, CORS, logging,
pgvector, proxy headers, …). Missing a required var → the process exits `1` with a
clear message rather than failing mysteriously later.

## Endpoints

| Route | Description |
|---|---|
| `GET /health` | `200 {status, db, storage, version}` / `503 degraded` |
| `GET /ready` | `503 {status: migrating}` until boot completes, then `200` |
| `GET /` | Embedded admin UI |

## License

MIT
