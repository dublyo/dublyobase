# dublyobase

**An open-source, Postgres-backed BaaS** in the spirit of PocketBase — auth, realtime,
storage, and an admin UI, served from **one Go binary on one port**. It connects to a
Postgres you provide (`DATABASE_URL`), runs migrations on boot, and ships as a single
`<30 MB` container to `ghcr.io/dublyo/dublyobase`. One-click deployable on Dublyo.

> Status: **v0.9.1 / M8 UI polish complete.** Control-plane auth, project provisioning,
> collections metadata, schema sync, records CRUD, API keys, RLS-backed rules, and
> email/password app auth, local file storage, and resumable chunk uploads are
> implemented, including SMTP delivery for verification/reset emails and an
> embedded admin panel with structured collection controls, runtime SMTP settings,
> and local or S3-compatible storage settings; realtime is still upcoming. See
> [dublyobase-dev.md](dublyobase-dev.md) for the full roadmap.

## What makes it different

- **One process, external Postgres.** No bundled DB, no nginx, no Redis. Pick your
  Postgres major (16/17/18) at the infra level; dublyobase just connects.
- **Security enforced in Postgres.** Per-project roles + `SET LOCAL ROLE` +
  `FORCE ROW LEVEL SECURITY`; hashed API keys; encrypted secrets. Deny → `403 rls_denied`.
- **Project-scoped app auth.** Automatic `users` auth collection, bcrypt passwords,
  1h access JWTs, 7d hashed refresh sessions, rotation, logout-all, reset/verify
  tokens, and optional SMTP delivery.
- **Protected file storage.** Local volume by default, or S3-compatible providers
  such as R2, Backblaze B2, MinIO, and AWS S3. Streamed multipart uploads,
  resumable chunks,
  JSONB file metadata, short-lived file tokens, and cached thumbnails on the
  active provider.
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
| `GET /admin/api/settings` | Runtime SMTP/storage settings with secrets masked |
| `PUT /admin/api/settings/smtp` | Save runtime SMTP settings |
| `POST /admin/api/settings/smtp/test` | Send an SMTP test email |
| `PUT /admin/api/settings/storage` | Save local or S3-compatible storage settings |
| `POST /admin/api/settings/storage/test` | Write/read/delete a storage test object |
| `GET/POST /admin/api/projects` | List/create projects |
| `GET /admin/api/projects/{slug}` | Project detail |
| `GET/POST /admin/api/projects/{slug}/api-keys` | List/create project API keys |
| `DELETE /admin/api/projects/{slug}/api-keys/{id}` | Revoke project API key |
| `GET /admin/api/projects/{slug}/collections/export` | Export collection schema JSON for the selected project |
| `POST /admin/api/projects/{slug}/collections/import` | Preview or apply collection schema imports |
| `POST /admin/api/projects/{slug}/sql` | Execute admin SQL in the selected project schema |
| `GET /admin/api/audit-log` | Newest-first audit log, with secret-like data redacted |
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
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}` | Multipart upload using form field `file`; `?mode=replace\|append` |
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/uploads` | Create a resumable upload session |
| `PUT /api/projects/{slug}/files/uploads/{uploadId}/chunks/{index}` | Upload one raw chunk; optional `X-Checksum-SHA256` |
| `POST /api/projects/{slug}/files/uploads/{uploadId}/complete` | Assemble chunks and update the record file field |
| `DELETE /api/projects/{slug}/files/uploads/{uploadId}` | Cancel a resumable upload and remove temp chunks |
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/token` | Mint a short-lived protected file token after `view` rules pass |
| `GET /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}?token=...` | Download original file; add `thumb=WxH` for cached JPEG thumbnail |
| `GET /` | Embedded admin UI |

## License

MIT
