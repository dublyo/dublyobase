# M1 Spec: Control Plane and Admin Auth

Status: implemented in v0.2.0
Depends on: v0.1.2

This spec supersedes the short M1 roadmap note for admin-auth internals: M1 uses opaque, hashed admin session tokens instead of stateless admin JWTs. Stateless app-user access JWTs remain planned for M4.

## Goal

Build the secure control-plane foundation for dublyobase:

- First admin setup.
- Admin login/logout/me.
- Auth middleware on every `/admin/api/*` route except setup/login.
- Project creation and listing.
- Per-project Postgres schema and NOLOGIN roles.
- Audit logging for control-plane actions.

M1 must close the class of bug found in postbase: dashboard UI gating without API enforcement.

## Scope

In scope:

- `POST /setup` self-closing first-admin creation.
- `POST /admin/api/auth/login`.
- `POST /admin/api/auth/logout`.
- `GET /admin/api/me`.
- `GET /admin/api/projects`.
- `POST /admin/api/projects`.
- `GET /admin/api/projects/{slug}`.
- Admin auth middleware for `/admin/api/*`.
- Rate limiting for setup and login.
- `_dbo` migration for admin sessions, audit log, API keys, and project metadata hardening.
- Tests against real Postgres via `TEST_DATABASE_URL`.

Out of scope:

- Full Svelte admin panel.
- App-user auth.
- Collections and schema sync.
- Records API.
- RLS policy generation.
- File upload.
- SMTP email sending.
- Hard project deletion or schema dropping.
- Public anon/service API key usage by data APIs.

## User Flow

1. Operator deploys dublyobase with `DATABASE_URL`, `APP_URL`, `JWT_SECRET`, and optionally `ADMIN_EMAIL` / `ADMIN_PASSWORD`.
2. If admin env vars are present and no admin exists, boot seed creates the first admin.
3. If no admin exists, operator calls `POST /setup` once with email/password.
4. Admin calls `POST /admin/api/auth/login` and receives an opaque bearer token.
5. Admin calls `GET /admin/api/me` to verify the session.
6. Admin calls `POST /admin/api/projects` with a slug/name.
7. dublyobase creates project metadata, `proj_<slug>` schema, and project roles in one transaction.
8. Admin lists projects with `GET /admin/api/projects`.
9. Admin logs out with `POST /admin/api/auth/logout`; the current session is revoked.

## Requirements

- Every response uses the common JSON error envelope:
  `{"error":"machine_slug","message":"human text","details":{...}}`.
- All `/admin/api/*` routes require admin auth except `/admin/api/auth/login`.
- `POST /setup` is outside `/admin/api/*` but must be rate-limited and self-closing.
- Setup returns `410` forever once any admin exists.
- Login failure response is generic and does not reveal whether the email exists.
- Admin tokens are opaque random values, shown once, stored only as SHA-256 hashes.
- Admin sessions expire after 24 hours by default.
- Logout revokes only the current session.
- Project slugs match `^[a-z][a-z0-9_]{2,30}$`.
- Reserved slugs/prefixes are rejected: `_dbo`, `pg_`, `information_schema`, `public`.
- Project creation is idempotency-safe: duplicate slug returns `409`, not a partial DB state.
- All DDL identifiers are validated and double-quoted.
- Control-plane actions write audit rows.

## Frontend

M1 does not ship the full admin panel. The embedded UI remains a placeholder.

Allowed frontend changes:

- Update placeholder copy if needed.
- Keep SPA fallback behavior from v0.1.2.
- Do not introduce Node/Vite/Svelte runtime work in M1 unless the backend API is complete and tested first.

## Backend

Add packages/files only where they remove real duplication:

- `core/auth.go`: password checks, opaque token generation, token hashing.
- `core/audit.go`: audit event helper.
- `core/projects.go`: project slug validation and provisioning.
- `apis/admin_auth.go`: setup/login/logout/me handlers.
- `apis/admin_projects.go`: project handlers.
- `apis/auth_middleware.go`: bearer auth middleware.
- `apis/rate_limit.go`: small in-process token bucket for setup/login.

Avoid adding a framework. Continue with stdlib `net/http` and existing middleware style.

## Database

Create migration `core/migrations/0002_control_plane_auth.sql`.

Tables and changes:

- `_dbo.admins`
  - add `updated_at timestamptz not null default now()`
  - add `disabled_at timestamptz null`

- `_dbo.admin_sessions`
  - `id uuid primary key default gen_random_uuid()`
  - `admin_id uuid not null references _dbo.admins(id) on delete cascade`
  - `token_hash text unique not null`
  - `user_agent text not null default ''`
  - `ip text not null default ''`
  - `created_at timestamptz not null default now()`
  - `last_seen_at timestamptz not null default now()`
  - `expires_at timestamptz not null`
  - `revoked_at timestamptz null`

- `_dbo.audit_log`
  - `id uuid primary key default gen_random_uuid()`
  - `admin_id uuid null references _dbo.admins(id) on delete set null`
  - `action text not null`
  - `target_type text not null`
  - `target_id text not null default ''`
  - `ip text not null default ''`
  - `user_agent text not null default ''`
  - `data jsonb not null default '{}'::jsonb`
  - `created_at timestamptz not null default now()`

- `_dbo.projects`
  - add `updated_at timestamptz not null default now()`
  - add `disabled_at timestamptz null`

- `_dbo.api_keys`
  - `id uuid primary key default gen_random_uuid()`
  - `project_id uuid not null references _dbo.projects(id) on delete cascade`
  - `name text not null`
  - `type text not null check (type in ('anon','service'))`
  - `key_hash text unique not null`
  - `prefix text not null`
  - `created_at timestamptz not null default now()`
  - `revoked_at timestamptz null`

Project provisioning transaction:

1. Insert `_dbo.projects`.
2. `CREATE SCHEMA "proj_<slug>"`.
3. `CREATE ROLE "<slug>_anon" NOLOGIN`.
4. `CREATE ROLE "<slug>_authenticated" NOLOGIN`.
5. `CREATE ROLE "<slug>_service" NOLOGIN`.
6. Grant those roles to the app login role.
7. Revoke broad public privileges on the project schema.
8. Insert audit row.

M1 does not create collection tables or RLS policies yet.

## APIs

### POST /setup

Body:

```json
{"email":"admin@example.com","password":"long-password"}
```

Responses:

- `201 {"admin":{"id":"...","email":"..."}}`
- `400 invalid_json`
- `422 validation_failed`
- `410 setup_closed`
- `429 rate_limited`

Rules:

- Only works when `_dbo.admins` is empty.
- Password minimum: 12 characters.
- Email is normalized with trim + lowercase.
- Password stored with bcrypt.

### POST /admin/api/auth/login

Body:

```json
{"email":"admin@example.com","password":"long-password"}
```

Response:

```json
{
  "token": "dbo_admin_...",
  "expiresAt": "2026-07-04T12:00:00Z",
  "admin": {"id": "...", "email": "admin@example.com"}
}
```

Failures:

- `401 invalid_credentials`
- `403 admin_disabled`
- `429 rate_limited`

### POST /admin/api/auth/logout

Auth: `Authorization: Bearer dbo_admin_...`

Response:

- `204 No Content`

### GET /admin/api/me

Auth required.

Response:

```json
{
  "admin": {"id": "...", "email": "admin@example.com"},
  "session": {"id": "...", "expiresAt": "..."}
}
```

### GET /admin/api/projects

Auth required.

Response:

```json
{"items":[{"id":"...","slug":"demo","name":"Demo","schemaName":"proj_demo"}]}
```

### POST /admin/api/projects

Auth required.

Body:

```json
{"slug":"demo","name":"Demo"}
```

Response:

```json
{
  "id": "...",
  "slug": "demo",
  "name": "Demo",
  "schemaName": "proj_demo",
  "roles": {
    "anon": "demo_anon",
    "authenticated": "demo_authenticated",
    "service": "demo_service"
  }
}
```

Failures:

- `401 unauthorized`
- `409 project_exists`
- `422 validation_failed`
- `500 provisioning_failed`

### GET /admin/api/projects/{slug}

Auth required. Returns one project or `404 project_not_found`.

## UI/UX

M1 is API-first. Operator experience is validated through curl/API tests, not a polished panel.

The future panel should map directly to these concepts:

- Setup screen when no admin exists.
- Login screen.
- Projects list.
- Create project dialog with slug validation.
- Project detail showing schema and role names.
- Audit log table.

No visible UI text should claim collections, records, storage, SMTP, or realtime are implemented in M1.

## Edge Cases

- Missing admin env vars and no setup call: app remains healthy but has no admin until `POST /setup`.
- Admin env vars set after an admin already exists: no overwrite and no second admin.
- Two replicas call `POST /setup` concurrently: one succeeds, one returns `410` or `409`.
- Two replicas create the same project concurrently: one succeeds, one returns `409`.
- DDL failure during project creation: transaction rolls back metadata; no partial project row.
- Existing schema/role from manual operator action: return `409 provisioning_conflict` with generic detail.
- Expired session token: `401 session_expired`.
- Revoked session token: `401 unauthorized`.
- Disabled admin: existing sessions stop working.
- Malformed bearer token: `401 unauthorized`.

## Security

- Hash session tokens with SHA-256 before storage.
- Generate tokens with `crypto/rand`; minimum 32 random bytes before encoding.
- Never log plaintext tokens or passwords.
- Bcrypt admin passwords with current default cost.
- Use generic login errors.
- Add rate limiting to setup and login by client IP.
- Store only validated identifiers; quote every DDL identifier.
- Wrap project DDL in one transaction.
- Auth middleware must run before every `/admin/api/*` route except login.
- Audit successful setup, login, logout, and project creation.
- Audit failed login attempts without storing passwords.

## Testing

Unit tests:

- Email normalization.
- Password validation.
- Slug validation and reserved names.
- Token generation format.
- Token hash comparison.
- Auth middleware rejects missing/malformed/expired/revoked tokens.
- Rate limiter returns 429 after threshold.

Integration tests with `TEST_DATABASE_URL`:

- `POST /setup` creates first admin and then returns 410.
- Env-seeded admin can log in.
- Login returns a token whose hash is stored, not plaintext.
- `GET /admin/api/me` works with token and fails after logout.
- Unauthenticated `/admin/api/projects` returns 401.
- `POST /admin/api/projects` creates project metadata, schema, roles, grants, and audit row.
- Duplicate project slug returns 409 and leaves one row.
- Concurrent project creation returns one success and one conflict.
- Disabled admin cannot use an existing session.

Validation commands:

```bash
go test ./...
TEST_DATABASE_URL="postgres://..." go test -v ./...
go vet ./...
git diff --check
```

## Implementation Steps

1. Add migration `0002_control_plane_auth.sql`.
2. Add shared JSON error helper so all new APIs use the same envelope.
3. Add admin session/token helpers.
4. Add setup handler with rate limiting and race-safe insert.
5. Add login/logout/me handlers.
6. Add admin auth middleware and apply it to `/admin/api/*`.
7. Add project validation and provisioning helpers.
8. Add project list/create/get handlers.
9. Add audit logging helper and write audit rows from setup/login/logout/project create.
10. Add unit tests for validation, auth helpers, middleware, and rate limiting.
11. Add integration tests for setup, login, logout, project provisioning, and concurrency.
12. Update `dublyobase-dev.md`, README endpoint table, and deploy notes.
13. Run real Postgres tests before tagging `v0.2.0`.

## Acceptance Criteria

- `POST /setup` is self-closing and race-safe.
- Admin login/logout/me works end to end.
- Every protected admin route returns `401` without a valid token.
- Project creation provisions metadata, schema, roles, grants, and audit log.
- Duplicate/concurrent project creation is safe.
- Tests pass with and without `TEST_DATABASE_URL`; integration coverage runs in CI.
- No secrets, SQL internals, stack traces, or plaintext tokens are returned or logged.
