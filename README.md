# Dublyobase

Dublyobase is an open-source Supabase Alternative, Postgres-backed backend for building apps with a
PocketBase-style developer experience. It provides a control panel, projects,
collections, REST APIs, email/password auth, file storage, SMTP settings, cron
jobs, backups, and scoped remote MCP access from one Go backend and one embedded
admin UI.

The runtime is intentionally small:

- One Go process on one port, default `:8080`
- External PostgreSQL, with deployment templates for Postgres 16, 17, or 18
- Embedded Next/Tailwind admin panel
- GHCR image: `ghcr.io/dublyo/dublyobase`
- No Redis, no nginx sidecar, no separate admin service

Current public release: `v0.10.6`. Realtime subscriptions are not included in
this release.

## Features

### Projects and Postgres

- Create isolated app projects from the admin panel.
- Each project gets its own Postgres schema and roles.
- Collection rules are enforced with Postgres row-level security.
- Collections create and update real Postgres tables.
- Admin SQL runner for project-scoped inspection and maintenance.
- Collection schema export/import for moving definitions between projects.

### Collections and Records

- Base, auth, and view collection types.
- REST endpoints for collection and record CRUD.
- Field types: text, rich editor, password, number, bool, date, autodate, email,
  URL, select, JSON, relation, and file.
- Required, hidden, presentable, help text, validation options, and rule-driven
  access.
- Query support for list filters, Directus-style JSON filters, selected-field
  search, sorting, field projection, and pagination.

### App Auth

- Every project can use an auth collection for application users.
- Email/password signup and login.
- Access tokens, refresh sessions, rotation, logout, and logout-all.
- Email verification and password reset flows.
- SMTP delivery for verification and reset emails.
- Service API keys for backend-to-backend access.

### Files and Storage

- Local file storage by default at `/data/storage`.
- S3-compatible storage support for providers such as Cloudflare R2, Backblaze
  B2, MinIO, and AWS S3.
- Runtime storage settings in the admin panel.
- Multipart file uploads.
- Resumable chunk uploads.
- Single-file fields by default, optional multi-file fields.
- Stored JSONB file metadata.
- Short-lived protected file tokens.
- Cached thumbnails for image downloads.

### Mail, Cron, Backups, and MCP

- Runtime SMTP settings with a test-email action.
- Native HTTP cron jobs with schedules, headers, retry settings, run-now, and
  run logs.
- Full-instance and per-project `pg_dump` backup jobs.
- Backup output is written to the configured storage backend.
- Remote HTTP MCP endpoint at `POST /mcp`.
- Scoped MCP tokens with tool allowlists and audit logs.
- MCP tools can manage projects, collections, records, users, files, SMTP,
  storage, cron jobs, and backups according to the token scope.

### Admin UI

- Embedded admin panel served from `/_/`.
- Root `/` returns a generic restricted-service warning page.
- Fixed empty-install bootstrap admin: `admin@example.com` / `dublyo`.
- First login must change the bootstrap password before admin access is allowed.
- Project creation and API key management.
- PocketBase-inspired collection editor and record editor.
- Settings screens for SMTP, storage, cron, backups, and MCP tokens.
- Audit log with secret-like values redacted.

## Deploy

### Recommended: Dublyo PaaS one-click deploy

The repository includes a Dublyo PaaS template at
[`deploy/dublyo.template.yml`](deploy/dublyo.template.yml).

Use [Dublyo PaaS](https://dublyo.com) for the simplest production path:

1. Choose the Dublyobase app template.
2. Pick PostgreSQL `16`, `17`, or `18`.
3. Set your domain.
4. Let Dublyo generate `JWT_SECRET` and the database password.
5. Deploy the stack and open `https://your-domain/_/`.
6. Log in with `admin@example.com` / `dublyo` and set a new admin password.

The template runs two services: Postgres and Dublyobase. TLS and routing are
handled by the Dublyo platform.

### Docker Compose

For a local or self-hosted install:

```bash
git clone https://github.com/dublyo/dublyobase.git
cd dublyobase
```

Before starting the stack, edit `docker-compose.yml` and replace:

- `POSTGRES_PASSWORD`
- `DATABASE_URL`
- `JWT_SECRET`
- `APP_URL`

The sample `JWT_SECRET` is intentionally invalid so unchanged public installs
fail closed. The compose file also binds the app to `127.0.0.1:8080` and keeps
`TRUST_PROXY_HEADERS=false` by default; put Dublyobase behind a trusted proxy
before exposing it publicly.

Then start it:

```bash
docker compose up -d
```

Open [http://localhost:8080/_/](http://localhost:8080/_/) for the admin panel.
Health should return `200`:

```bash
curl http://localhost:8080/health
```

Use a pinned image tag in production:

```yaml
image: ghcr.io/dublyo/dublyobase:v0.10.6
```

### Existing Postgres

You can run Dublyobase against any reachable PostgreSQL database:

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require" \
  -e APP_URL="https://dublyobase.example.com" \
  -e JWT_SECRET="$(openssl rand -base64 32)" \
  -v dublyobase-storage:/data/storage \
  ghcr.io/dublyo/dublyobase:v0.10.6
```

`DATABASE_URL`, `APP_URL`, and `JWT_SECRET` are required. On an empty install,
Dublyobase seeds `admin@example.com` / `dublyo` and forces a password change
before the control panel or admin APIs can be used.

Advanced deploys may set `ADMIN_EMAIL` and `ADMIN_PASSWORD` together to seed a
custom first admin instead. Custom admin passwords must be at least 12
characters.

## Configuration

All runtime configuration is environment-based. Runtime SMTP and storage settings
can also be managed from the admin panel after setup.

| Variable | Required | Default | Description |
|---|---:|---|---|
| `DATABASE_URL` | Yes |  | Postgres connection string. |
| `APP_URL` | Yes |  | Public URL used for auth links and generated URLs. |
| `JWT_SECRET` | Yes |  | At least 32 characters; used for signing and secret encryption. |
| `HOST` | No | `0.0.0.0` | HTTP bind host. |
| `PORT` | No | `8080` | HTTP bind port. |
| `ADMIN_EMAIL` | No | `admin@example.com` | Optional override for the first admin email. |
| `ADMIN_PASSWORD` | No | `dublyo` | Optional override for the first admin password. Custom values require 12+ characters. |
| `BCRYPT_COST` | No | `10` | Password hashing cost. |
| `AUTH_DEV_TOKENS` | No | `false` | Returns auth action tokens in responses for development tests. |
| `MAX_UPLOAD_MB` | No | `64` | Upload limit, 1 to 1024 MB. |
| `STORAGE_TYPE` | No | `local` | `local` or `s3`. |
| `STORAGE_LOCAL_PATH` | No | `/data/storage` | Local storage path. |
| `S3_ENDPOINT` | For S3 |  | S3-compatible endpoint. |
| `S3_BUCKET` | For S3 |  | Storage bucket. |
| `S3_ACCESS_KEY` | For S3 |  | Access key. |
| `S3_SECRET_KEY` | For S3 |  | Secret key. |
| `S3_REGION` | No | `us-east-1` | S3 region. |
| `S3_PREFIX` | No |  | Optional object key prefix. |
| `S3_USE_SSL` | No | `true` | Use HTTPS for S3 endpoint. |
| `S3_FORCE_PATH_STYLE` | No | `true` | Path-style S3 requests. |
| `SMTP_HOST` | No |  | SMTP host. |
| `SMTP_PORT` | No | `587` | SMTP port. |
| `SMTP_USER` | No |  | SMTP username. |
| `SMTP_PASSWORD` | No |  | SMTP password. |
| `SMTP_FROM` | No |  | Sender address. |
| `MIGRATE_ON_START` | No | `true` | Run migrations at startup. |
| `TRUST_PROXY_HEADERS` | No | `false` | Respect proxy headers from trusted proxies. Enable only behind a trusted proxy. |
| `TRUSTED_PROXY_CIDRS` | No | private ranges | Comma-separated trusted proxy CIDRs. |
| `CORS_ORIGINS` | No | `*` | Comma-separated allowed origins. |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error`. |
| `LOG_FORMAT` | No | `json` | `json` or `text`. |
| `ENABLE_PGVECTOR` | No | `false` | Enables pgvector-related migrations when configured. |

## API Overview

### System and Admin

| Route | Description |
|---|---|
| `GET /health` | Health, database, storage, and version status. |
| `GET /ready` | Readiness status during boot and migration. |
| `POST /setup` | Create the first admin while no admin exists. |
| `POST /admin/api/auth/login` | Admin login. |
| `POST /admin/api/auth/change-password` | Change the current admin password and unlock forced-change sessions. |
| `POST /admin/api/auth/logout` | Revoke the current admin session. |
| `GET /admin/api/me` | Current admin session. |
| `GET /admin/api/audit-log` | Newest-first admin audit log. |

### Projects

| Route | Description |
|---|---|
| `GET /admin/api/projects` | List projects. |
| `POST /admin/api/projects` | Create a project. |
| `GET /admin/api/projects/{slug}` | Project detail. |
| `GET /admin/api/projects/{slug}/api-keys` | List project API keys. |
| `POST /admin/api/projects/{slug}/api-keys` | Create a project API key. |
| `DELETE /admin/api/projects/{slug}/api-keys/{id}` | Revoke an API key. |
| `POST /admin/api/projects/{slug}/sql` | Run project-scoped admin SQL. |

### Collections and Records

| Route | Description |
|---|---|
| `GET /api/projects/{slug}/collections` | List collections. |
| `POST /api/projects/{slug}/collections` | Create a collection and table. |
| `GET /api/projects/{slug}/collections/{name}` | Collection detail. |
| `PATCH /api/projects/{slug}/collections/{name}` | Update fields and rules. |
| `DELETE /api/projects/{slug}/collections/{name}` | Delete a collection. |
| `GET /api/projects/{slug}/collections/{name}/records` | List records. |
| `POST /api/projects/{slug}/collections/{name}/records` | Create a record. |
| `GET /api/projects/{slug}/collections/{name}/records/{id}` | Get a record. |
| `PATCH /api/projects/{slug}/collections/{name}/records/{id}` | Update a record. |
| `DELETE /api/projects/{slug}/collections/{name}/records/{id}` | Delete a record. |
| `GET /admin/api/projects/{slug}/collections/export` | Export collection schema JSON. |
| `POST /admin/api/projects/{slug}/collections/import` | Preview or apply collection schema imports. |

Record list APIs support `page`, `perPage` (`10`, `25`, `100`, `250`, or `500`),
`offset`, `sort`, `fields`, `search`, PocketBase-style string `filter`, and
Directus-style JSON filters such as:

```text
GET /api/projects/app/collections/posts/records?search=hello&perPage=25
GET /api/projects/app/collections/posts/records?filter={"title":{"_icontains":"hello"}}
GET /api/projects/app/collections/posts/records?filter[title][_icontains]=hello
```

The `search` parameter only scans fields marked `searchable` in the collection
editor.

### App Auth

| Route | Description |
|---|---|
| `POST /api/projects/{slug}/auth/signup` | Create an app user. |
| `POST /api/projects/{slug}/auth/login` | Login an app user. |
| `POST /api/projects/{slug}/auth/refresh` | Rotate a refresh token. |
| `POST /api/projects/{slug}/auth/logout` | Revoke the current refresh token. |
| `POST /api/projects/{slug}/auth/logout-all` | Revoke all sessions for the user. |
| `GET /api/projects/{slug}/auth/me` | Current app user. |
| `POST /api/projects/{slug}/auth/request-verification` | Request verification email. |
| `POST /api/projects/{slug}/auth/confirm-verification` | Confirm verification token. |
| `POST /api/projects/{slug}/auth/request-password-reset` | Request password reset email. |
| `POST /api/projects/{slug}/auth/confirm-password-reset` | Confirm reset token and set a new password. |

### Files

| Route | Description |
|---|---|
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}` | Multipart upload using form field `file`. |
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/uploads` | Create a resumable upload session. |
| `PUT /api/projects/{slug}/files/uploads/{uploadId}/chunks/{index}` | Upload a raw chunk. |
| `POST /api/projects/{slug}/files/uploads/{uploadId}/complete` | Assemble chunks and update the record field. |
| `DELETE /api/projects/{slug}/files/uploads/{uploadId}` | Cancel a resumable upload. |
| `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/token` | Mint a short-lived protected file token. |
| `GET /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}` | Download a file. Add `?token=...` for protected files and `thumb=WxH` for thumbnails. |

### Settings, Cron, Backups, and MCP

| Route | Description |
|---|---|
| `GET /admin/api/settings` | Runtime SMTP and storage settings with secrets masked. |
| `PUT /admin/api/settings/smtp` | Save SMTP settings. |
| `POST /admin/api/settings/smtp/test` | Send a test email. |
| `PUT /admin/api/settings/storage` | Save local or S3-compatible storage settings. |
| `POST /admin/api/settings/storage/test` | Test storage write/read/delete. |
| `GET /admin/api/cron-jobs` | List cron jobs. |
| `POST /admin/api/cron-jobs` | Create a cron job. |
| `GET /admin/api/cron-jobs/{id}/runs` | List cron runs. |
| `POST /admin/api/cron-jobs/{id}/run` | Run a cron job immediately. |
| `GET /admin/api/backups` | List backup jobs. |
| `POST /admin/api/backups` | Create a backup job. |
| `GET /admin/api/backups/{id}/runs` | List backup runs. |
| `POST /admin/api/backups/{id}/run` | Run a backup job immediately. |
| `GET /admin/api/mcp/tokens` | List MCP tokens. |
| `POST /admin/api/mcp/tokens` | Create a scoped MCP token. |
| `DELETE /admin/api/mcp/tokens/{id}` | Revoke an MCP token. |
| `POST /mcp` | Remote HTTP MCP endpoint using bearer MCP tokens. |

## Security Notes

- Use a strong `JWT_SECRET`; it signs tokens and encrypts stored runtime secrets.
- Change the bootstrap `admin@example.com` / `dublyo` password immediately after
  first login. Forced-change sessions cannot access admin APIs until the
  password is changed.
- Replace all sample passwords before exposing an instance.
- Keep `AUTH_DEV_TOKENS=false` outside development.
- Use HTTPS in production and set `APP_URL` to the public HTTPS URL.
- Scope MCP tokens narrowly and revoke them when no longer needed.
- Prefer project-scoped backups for app tenants and full backups for instance
  administrators.
- S3 and SMTP secrets are masked in API responses and audit logs.

## Local Development

Requirements:

- Go 1.25+
- Node.js 22+
- PostgreSQL 16+

Build and test:

```bash
npm ci --prefix ui/admin
npm run --prefix ui/admin typecheck
npm run --prefix ui/admin build
go test ./...
go build ./...
```

Run locally against your own database:

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/dublyobase?sslmode=disable"
export APP_URL="http://localhost:8080"
export JWT_SECRET="$(openssl rand -base64 32)"
go run . serve
```

## License

MIT
