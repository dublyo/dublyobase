# Dublyobase

Dublyobase is an open-source Supabase Alternative, Postgres-backed backend for building apps with a
PocketBase-style developer experience. It provides a control panel, projects,
collections, REST APIs, SSE/WebSocket realtime, email/password auth, file
storage, SMTP settings, cron jobs, backups, and scoped remote MCP access from
one Go backend and one embedded admin UI.

The runtime is intentionally small:

- One Go process on one port, default `:8080`
- External PostgreSQL, with deployment templates for Postgres 16, 17, or 18
- Embedded Next/Tailwind admin panel
- GHCR image: `ghcr.io/dublyo/dublyobase`
- No Redis, no nginx sidecar, no separate admin service

Current default deploy image: `ghcr.io/dublyo/dublyobase:main`. Semver tags are
published for fixed releases, but the templates default to `:main` so new
installs get the latest tested main build.

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
- Field types: text, rich editor, password, number, decimal, bool, date,
  autodate, email, URL, select, JSON, relation, and file.
- `decimal` fields are real `numeric(precision,scale)` columns carried over the
  wire as JSON strings, so money and quantities stay exact. Use them instead of
  `number`, which is `double precision` and will drift when summed.
- Required, hidden, presentable, help text, validation options, and rule-driven
  access.
- Query support for list filters, Directus-style JSON filters, selected-field
  search, sorting, field projection, and pagination.

### Realtime

- Server-Sent Events and WebSocket endpoints for record `create`, `update`, and
  `delete`.
- Project-scoped and collection-filtered subscriptions.
- Bearer auth, API keys, app-user JWTs, and anonymous access use the same record
  auth path as REST APIs.
- Create/update payloads are checked through collection view rules before they
  are sent to a subscriber.
- Delete events are service-subscriber only in this release to avoid leaking
  private tombstones before a durable visibility cache exists.
- Record events are persisted in Postgres, published with `LISTEN/NOTIFY`, and
  relayed to local SSE/WebSocket subscribers by each app replica.
- WebSocket channels support presence heartbeats and broadcast messages with
  persisted fanout.

### App Auth

- Every project can use an auth collection for application users.
- Email/password signup and login.
- Access tokens, refresh sessions, rotation, logout, and logout-all.
- Email verification and password reset flows.
- App-user email change flow with confirmation email.
- Project-level token duration and email template settings.
- Email one-time password login.
- OAuth login runtime for Google, GitHub, Facebook, and configurable OIDC
  providers.
- TOTP multi-factor auth enrollment, login challenges, and recovery codes.
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
- Empty installs generate a one-time `admin@example.com` bootstrap password in
  the container logs.
- First login must change generated bootstrap passwords before admin access is
  allowed.
- Project creation and API key management.
- PocketBase-inspired collection editor and record editor.
- Settings screens for SMTP, storage, cron, backups, and MCP tokens.
- PocketBase-style API Preview with REST, auth, file upload, realtime, batch,
  SDK/fetch, curl, query parameter, and response-shape examples.
- Audit and request logs with pagination, filters, detail drawers, JSON export,
  metadata, and retention controls.

## Deploy

### Recommended: Dublyo PaaS one-click deploy

The repository includes a Dublyo PaaS template at
[`deploy/dublyo.template.yml`](deploy/dublyo.template.yml).

Use [Dublyo PaaS](https://dublyo.com) for the simplest production path:

1. Choose the Dublyobase app template.
2. Pick PostgreSQL `16`, `17`, or `18`.
3. Set your domain.
4. Let Dublyo generate `JWT_SECRET`, the database password, and initial admin
   credentials.
5. Deploy the stack and open `https://your-domain/_/`.
6. Log in with the generated admin credential and set a new admin password.

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

The compose file uses the moving `:main` image tag by default so a stack
redeploy pulls the latest tested main build:

```yaml
image: ghcr.io/dublyo/dublyobase:main
```

For conservative production rollouts, pin a semver tag after selecting the
release you want and upgrade intentionally.

### Existing Postgres

You can run Dublyobase against any reachable PostgreSQL database:

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://user:pass@host:5432/db?sslmode=require" \
  -e APP_URL="https://dublyobase.example.com" \
  -e JWT_SECRET="$(openssl rand -base64 32)" \
  -v dublyobase-storage:/data/storage \
  ghcr.io/dublyo/dublyobase:main
```

`DATABASE_URL`, `APP_URL`, and `JWT_SECRET` are required. On an empty install
without `ADMIN_EMAIL`/`ADMIN_PASSWORD`, Dublyobase seeds `admin@example.com` with
a generated one-time password, writes it once to the container logs, and forces a
password change before the control panel or admin APIs can be used.

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
| `ADMIN_EMAIL` | No | generated bootstrap email `admin@example.com` | Optional override for the first admin email. |
| `ADMIN_PASSWORD` | No | generated one-time password | Optional override for the first admin password. Values require 12+ characters. |
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
| `CORS_ORIGINS` | No | `APP_URL` origin | Comma-separated allowed origins. Runtime admin and project CORS can be managed in the panel. |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error`. |
| `LOG_FORMAT` | No | `json` | `json` or `text`. |
| `ENABLE_PGVECTOR` | No | `false` | Enables pgvector-related migrations when configured. |

## API Overview

### System and Admin

| Route | Description |
|---|---|
| `GET /health` | Health, database, storage, and version status. |
| `GET /ready` | Readiness status after the public listener is available. |
| `POST /setup` | Manual first-admin creation only while no admin exists and the app is ready. Empty installs are seeded automatically. |
| `POST /admin/api/auth/login` | Admin login. |
| `POST /admin/api/auth/change-password` | Change the current admin password and unlock forced-change sessions. |
| `POST /admin/api/auth/logout` | Revoke the current admin session. |
| `GET /admin/api/me` | Current admin session. |
| `GET /admin/api/audit-log` | Newest-first admin audit log with pagination and filters. |
| `GET /admin/api/request-logs` | Newest-first request log with pagination, filters, metadata, and detail payloads. |
| `PUT /admin/api/settings/logs` | Configure audit log retention by age and row count. |

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
| `POST /api/projects/{slug}/batch` | Run up to 50 bounded record operations. Send `atomic: true` to run them all in one transaction, so a failure rolls the whole batch back. |
| `GET /api/projects/{slug}/collections/{name}/records/aggregate` | Grouped aggregates (`count`, `sum`, `avg`, `min`, `max`) under the caller's rules. |
| `GET /api/projects/{slug}/realtime` | SSE stream for record create/update/delete events. |
| `GET /api/projects/{slug}/realtime/ws` | WebSocket stream for record events, presence, and broadcast. |
| `GET /admin/api/projects/{slug}/collections/export` | Export collection schema JSON. |
| `POST /admin/api/projects/{slug}/collections/import` | Preview or apply collection schema imports. |
| `GET /admin/api/projects/{slug}/schema/versions` | List schema metadata snapshots. |
| `POST /admin/api/projects/{slug}/schema/versions` | Create a schema metadata snapshot. |
| `GET /admin/api/projects/{slug}/sdk/typescript` | Download generated TypeScript types and client helpers. |

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

Aggregates are a separate endpoint; passing `aggregate` or `groupBy` to the
record list is rejected rather than silently ignored:

```text
GET /api/projects/app/collections/deals/records/aggregate?aggregate=sum:amount,count:*&groupBy=stage
```

Sums over `decimal` fields are exact — `100.10 + 200.20` returns `"300.30"`,
not `300.29999999999995`.

Aggregates accept the same filter forms as the record list, including the
bracket form (`filter[stage][_eq]=won`). `min`/`max` are rejected on boolean,
UUID and relation fields, which have no Postgres aggregate.

### CSV export

```text
GET /api/projects/{slug}/collections/{name}/records/export
GET /api/projects/{slug}/collections/{name}/records/aggregate/export
```

Both stream CSV under the caller's role, so an export can never contain a row
the caller could not read. The record export accepts the same `filter`
(including the bracket form), `search`, `sort` and `fields` as the record list,
so the file matches the view it came from; relation columns are rendered as
readable labels, or as raw ids with `relations=id`. Files begin with a UTF-8
BOM because Excel otherwise assumes the system codepage and renders non-Latin
text as mojibake.

Add `format=xlsx` for a real Excel workbook. Numbers are written as numeric
cells so Excel can sum them; values too long to be exact as a float64, and
anything with a leading zero (phone numbers, codes), stay as text rather than
being silently rounded or reformatted.

`fields` accepts dotted paths that walk many-to-one relations up to four deep,
so a flat report can pull columns from across the schema:

```text
GET .../records/export?fields=ref,client.full_name,client.city.name,total
```

Only the many-to-one direction is supported, because walking towards one record
has a single answer. Flattening a one-to-many — a patient and all their
appointments — has several, and belongs in an aggregate rather than a column.

Prefer the aggregate export for reports. Summing across two one-to-many
relations with a flat join multiplies each row by the other side — a patient
with one 2,885 payment and four appointments totals 11,540 — and the result
looks entirely plausible. The aggregate endpoint groups in PostgreSQL, so it is
not subject to that fan-out.

### Cross-row invariants

A `CHECK` constraint only sees the row being written, so two kinds of rule need
more than one. Both are enforced by PostgreSQL inside the writing transaction,
so a second client, a direct SQL session or a batch cannot get around them.

`consistency` requires that a related record agrees with this row — a payment's
invoice must belong to the payment's patient:

```json
"consistency": [
  { "name": "invoice_patient", "field": "invoice", "remote": "patient", "local": "patient" }
]
```

`exclusions` forbid two rows that agree on `equals` from overlapping between
`from` and `to`, with an optional `where` so a cancelled booking does not block
the slot it released:

```json
"exclusions": [
  { "name": "no_double_booking", "equals": ["provider", "location"],
    "from": "starts_at", "to": "ends_at", "where": "status != \"cancelled\"" }
]
```

Exclusions need the `btree_gist` extension; if the database role cannot create
it, saving the collection fails with that reason rather than silently skipping
the rule.

### Imported collections and rules

Collection API rules are compiled into PostgreSQL row-level security on tables
Dublyobase created. Imported tables keep their existing grants and RLS —
Dublyobase does not take ownership of another schema's security model — so
setting an API rule on an imported collection is rejected rather than stored
where nothing would enforce it. Manage those policies from the SQL console.

### Organization-scoped rules

Send `X-Org-Id` with a request to act inside one organization. Membership is
verified server-side on every request, so the header is a claim by the client
and never trusted on its own; a user who is not a member of that organization
gets `403`. The active organization is then available to rules:

```text
listRule:   org = @request.auth.orgId
createRule: org = @request.auth.orgId && @request.auth.orgRole != "viewer"
```

Browser `EventSource` clients that cannot set headers may pass `org=...` in the
realtime query string instead.

`X-Org-Id` scopes app-user requests. It does **not** restrict a service API key:
service policies are unconditional by design, so a service key remains a
project-wide credential regardless of the header.

Realtime accepts `collection`, `collections`, `event`, and `events` query
parameters. Values can be repeated or comma-separated:

```text
GET /api/projects/app/realtime?collection=posts&events=create,update
```

Use `Authorization: Bearer ...` for API clients. Browser `EventSource` clients
that cannot set headers may pass `token` or `access_token` in the query string;
avoid logging those URLs.

### App Auth

| Route | Description |
|---|---|
| `POST /api/projects/{slug}/auth/signup` | Create an app user. |
| `POST /api/projects/{slug}/auth/login` | Login an app user. |
| `POST /api/projects/{slug}/auth/request-otp` | Request an email one-time login code. |
| `POST /api/projects/{slug}/auth/login-otp` | Login with an email one-time code. |
| `GET /api/projects/{slug}/auth/oauth/{provider}/start` | Start OAuth login for `google`, `github`, `facebook`, or `oidc`. |
| `GET /api/projects/{slug}/auth/oauth/{provider}/callback` | OAuth provider callback. |
| `POST /api/projects/{slug}/auth/mfa/enroll` | Start TOTP MFA enrollment. |
| `POST /api/projects/{slug}/auth/mfa/confirm` | Confirm TOTP setup and receive recovery codes. |
| `POST /api/projects/{slug}/auth/mfa/verify` | Finish login with a TOTP MFA code. |
| `POST /api/projects/{slug}/auth/mfa/recovery` | Finish login with a recovery code. |
| `POST /api/projects/{slug}/auth/mfa/disable` | Disable MFA for the current app user. |
| `POST /api/projects/{slug}/auth/refresh` | Rotate a refresh token. |
| `POST /api/projects/{slug}/auth/logout` | Revoke the current refresh token. |
| `POST /api/projects/{slug}/auth/logout-all` | Revoke all sessions for the user. |
| `GET /api/projects/{slug}/auth/me` | Current app user. |
| `POST /api/projects/{slug}/auth/request-verification` | Request verification email. |
| `POST /api/projects/{slug}/auth/confirm-verification` | Confirm verification token. |
| `POST /api/projects/{slug}/auth/request-password-reset` | Request password reset email. |
| `POST /api/projects/{slug}/auth/confirm-password-reset` | Confirm reset token and set a new password. |
| `POST /api/projects/{slug}/auth/request-email-change` | Request app-user email change confirmation. |
| `POST /api/projects/{slug}/auth/confirm-email-change` | Confirm app-user email change. |
| `GET /auth/email-change` | Browser confirmation page for app-user email changes. |

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
| `GET /admin/api/backups/{id}/runs/{runId}/download` | Download a completed backup archive from configured storage. |
| `POST /admin/api/restores` | Upload a backup archive for dry-run validation or confirmed restore. |
| `GET /admin/api/projects/{slug}/auth-settings` | Read project auth token durations, templates, MFA flags, and OAuth provider settings. |
| `PUT /admin/api/projects/{slug}/auth-settings` | Save project auth settings. |
| `GET /admin/api/projects/{slug}/ops/alerts` | List or refresh project ops alerts. |
| `POST /admin/api/projects/{slug}/ops/alerts/{id}/resolve` | Resolve an ops alert. |
| `GET /admin/api/projects/{slug}/webhooks` | List outbound webhooks. |
| `POST /admin/api/projects/{slug}/webhooks` | Create a signed outbound webhook. |
| `DELETE /admin/api/projects/{slug}/webhooks/{id}` | Delete an outbound webhook. |
| `GET /admin/api/projects/{slug}/webhooks/{id}/deliveries` | List webhook delivery attempts. |
| `GET /admin/api/mcp/tokens` | List MCP tokens. |
| `POST /admin/api/mcp/tokens` | Create a scoped MCP token. |
| `DELETE /admin/api/mcp/tokens/{id}` | Revoke an MCP token. |
| `POST /mcp` | Remote HTTP MCP endpoint using bearer MCP tokens. |

## Security Notes

- Use a strong `JWT_SECRET`; it signs tokens and encrypts stored runtime secrets.
- For empty installs, copy the generated bootstrap password from container logs
  once and change it immediately after first login. Forced-change sessions cannot
  access admin APIs until the password is changed.
- Replace all sample passwords before exposing an instance.
- Keep `AUTH_DEV_TOKENS=false` outside development.
- Use HTTPS in production and set `APP_URL` to the public HTTPS URL.
- Scope MCP tokens narrowly and revoke them when no longer needed.
- Prefer project-scoped backups for app tenants and full backups for instance
  administrators.
- S3 and SMTP secrets are masked in API responses and audit logs.
- OAuth provider secrets are encrypted at rest and masked in API responses.
- For multi-replica realtime, use the same Postgres database so persisted events
  and `LISTEN/NOTIFY` fanout are shared by all replicas.

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
