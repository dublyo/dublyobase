# dublyobase

**An open-source, Postgres-backed BaaS** in the spirit of PocketBase — auth, realtime,
storage, and an admin UI, served from **one Go binary on one port**. It connects to a
Postgres you provide (`DATABASE_URL`), runs migrations on boot, and ships as a single
`<30 MB` container to `ghcr.io/dublyo/dublyobase`. One-click deployable on Dublyo.

> Status: **v0.5.0 / M4 complete.** Control-plane auth, project provisioning,
> collections metadata, schema sync, records CRUD, API keys, RLS-backed rules, and
> email/password app auth are implemented; storage, SMTP, and realtime are still upcoming. See
> [dublyobase-dev.md](dublyobase-dev.md) for the full roadmap.

## What makes it different

- **One process, external Postgres.** No bundled DB, no nginx, no Redis. Pick your
  Postgres major (16/17/18) at the infra level; dublyobase just connects.
- **Security enforced in Postgres.** Per-project roles + `SET LOCAL ROLE` +
  `FORCE ROW LEVEL SECURITY`; hashed API keys; encrypted secrets. Deny → `403 rls_denied`.
- **Project-scoped app auth.** Automatic `users` auth collection, bcrypt passwords,
  1h access JWTs, 7d hashed refresh sessions, rotation, logout-all, reset/verify tokens.
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
pgvector, proxy headers, app-auth bcrypt/dev-token settings, …). Missing a required
var → the process exits `1` with a clear message rather than failing mysteriously later.

## Endpoints

| Route | Description |
|---|---|
| `GET /health` | `200 {status, db, storage, version}` / `503 degraded` |
| `GET /ready` | `503 {status: migrating}` until boot completes, then `200` |
| `POST /setup` | Create the first admin while `_dbo.admins` is empty |
| `POST /admin/api/auth/login` | Admin login, returns an opaque bearer token |
| `POST /admin/api/auth/logout` | Revoke the current admin session |
| `GET /admin/api/me` | Current admin/session |
| `GET/POST /admin/api/projects` | List/create projects |
| `GET /admin/api/projects/{slug}` | Project detail |
| `GET/POST /admin/api/projects/{slug}/api-keys` | List/create project API keys |
| `DELETE /admin/api/projects/{slug}/api-keys/{id}` | Revoke project API key |
| `POST /api/projects/{slug}/auth/signup` | App-user signup; returns access + refresh tokens |
| `POST /api/projects/{slug}/auth/login` | App-user login |
| `POST /api/projects/{slug}/auth/refresh` | Rotate refresh token |
| `POST /api/projects/{slug}/auth/logout` | Revoke current refresh token |
| `POST /api/projects/{slug}/auth/logout-all` | Rotate user `token_key` and revoke all sessions |
| `GET /api/projects/{slug}/auth/me` | Current app user |
| `POST /api/projects/{slug}/auth/request-verification` | Create email verification token |
| `POST /api/projects/{slug}/auth/confirm-verification` | Confirm verification token |
| `POST /api/projects/{slug}/auth/request-password-reset` | Create password reset token |
| `POST /api/projects/{slug}/auth/confirm-password-reset` | Confirm reset token and set password |
| `GET/POST /api/projects/{slug}/collections` | List/create project collections |
| `GET/PATCH/DELETE /api/projects/{slug}/collections/{name}` | Collection detail/schema sync/delete |
| `GET/POST /api/projects/{slug}/collections/{name}/records` | List/create records |
| `GET/PATCH/DELETE /api/projects/{slug}/collections/{name}/records/{id}` | Record detail/update/delete |
| `GET /` | Embedded admin UI |

## License

MIT
