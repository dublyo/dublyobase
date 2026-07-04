# M4 Spec: App Auth

Status: implemented in v0.5.0
Depends on: v0.4.1

M4 adds project-scoped app-user authentication. M3 already validates signed
authenticated JWTs and maps them to the project authenticated role; M4 creates
the users, sessions, refresh rotation, and auth endpoints that issue those JWTs.

## Goal

Let each project have a real `users` auth collection and email/password auth
without adding external services.

Success means:

- A project can create, log in, refresh, and log out app users.
- Access tokens work with existing M3 record rules such as
  `owner = @request.auth.id`.
- Refresh tokens are opaque, hashed at rest, and rotated on every refresh.
- Logout-all invalidates old access tokens by rotating the user's `token_key`.
- Password reset and email verification tokens exist now, while actual SMTP
  delivery remains M6.
- All auth data stays in the existing Postgres database.

## Scope

In scope:

- Automatic system `users` auth collection per project.
- Email/password signup and login.
- Access JWTs with 1 hour TTL.
- Refresh tokens with 7 day TTL, stored hashed in `_dbo.sessions`.
- Refresh rotation and refresh-token replay protection.
- Logout current session and logout all sessions.
- Verification and password-reset token generation/confirmation.
- Configurable bcrypt cost for app users.
- Record-auth validation that checks the user's current `token_key`.
- Tests proving authenticated JWTs unlock M3 RLS rules and old tokens die after
  logout-all.

Out of scope:

- SMTP delivery. M6 sends verification/reset emails.
- OAuth2/social login. M7.
- Admin UI user-management screens. M7.
- MFA/passkeys.
- Phone auth, magic links, anonymous users.
- Cross-project identities.
- Long-lived access tokens.

## User Flow

1. Admin creates a project.
2. M4 ensures a system `users` auth collection exists for the project.
3. App client signs up with email and password.
4. Dublyobase creates a user row, hashes the password, creates a refresh session,
   and returns an access token plus refresh token.
5. App client uses the access token against records APIs.
6. M3 resolves the JWT, verifies the user and `token_key`, then runs the request
   as the project authenticated role.
7. App client refreshes with the opaque refresh token.
8. Dublyobase revokes the old refresh token, creates a new session, and returns a
   new access/refresh pair.
9. App client logs out current session, or logs out everywhere by rotating
   `token_key` and revoking all active sessions.
10. Reset/verify flows create one-use hashed tokens; M6 later sends them by SMTP.

## Requirements

- Email is normalized with the existing `NormalizeEmail` behavior.
- Passwords must be at least 12 characters.
- App-user password hashes use bcrypt.
- `BCRYPT_COST` controls app-user bcrypt cost; default `10`; tests may lower it.
- Duplicate email returns `409 user_exists`.
- Invalid login returns `401 invalid_credentials` without revealing whether the
  email exists.
- Disabled/deleted users cannot log in, refresh, or authenticate records.
- Access token TTL is 1 hour.
- Refresh token TTL is 7 days.
- Refresh tokens use an opaque prefix such as `dbo_refresh_`.
- Refresh token plaintext is returned only on signup/login/refresh.
- Refresh token hash is unique and stored only in `_dbo.sessions`.
- Refresh rotates every time:
  - old session gets `revoked_at` and `rotated_at`;
  - new session is inserted;
  - reusing the old token returns `401 unauthorized`;
  - replay of a rotated token revokes the token family.
- Logout current session revokes only that refresh session.
- Logout-all rotates the user's `token_key` and revokes all active sessions.
- Access JWTs include:
  - `sub`: user UUID
  - `role`: `authenticated`
  - `project`: project slug
  - `collection`: `users`
  - `token_key`: current user token key
  - `exp`
- `ResolveRecordAuth` must verify `token_key` against the project `users` row
  before accepting authenticated JWTs.
- All app-auth errors use the existing JSON error envelope.
- All app-auth write actions create audit entries.
- Public auth endpoints are rate-limited by IP.

## Frontend

M4 remains API-first.

Allowed UI change:

- Update placeholder text or docs links to mention app auth APIs.

Out of scope:

- Signup/login forms in the admin panel.
- User table UI.
- Reset/verification email UI.

## Backend

Suggested files:

- `core/app_auth.go`: signup, login, refresh, logout, verify/reset core logic.
- `core/app_tokens.go`: access JWT and refresh/reset/verify token helpers.
- `core/app_users.go`: ensure users collection and hidden auth columns.
- `apis/app_auth.go`: public auth handlers.
- `core/migrations/0005_app_auth.sql`: control-plane session/token tables.

Reuse:

- `HashToken`
- `NormalizeEmail`
- `ValidateUUID`
- existing audit log helpers
- existing collection/project helpers
- `github.com/golang-jwt/jwt/v5`
- `golang.org/x/crypto/bcrypt`

Avoid:

- a separate identity service
- Redis/session cache
- plaintext token storage
- accepting JWTs without checking user state

## Database

Create migration `core/migrations/0005_app_auth.sql`.

Control-plane tables:

```sql
create table if not exists _dbo.sessions (
    id           uuid primary key default gen_random_uuid(),
    project_id   uuid not null references _dbo.projects(id) on delete cascade,
    collection   text not null default 'users',
    user_id      uuid not null,
    token_hash   text unique not null,
    family_id    uuid not null,
    user_agent   text not null default '',
    ip           text not null default '',
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null,
    rotated_at   timestamptz null,
    revoked_at   timestamptz null,
    replaced_by  uuid null
);

create index if not exists sessions_user_idx
    on _dbo.sessions(project_id, collection, user_id);

create index if not exists sessions_active_token_idx
    on _dbo.sessions(token_hash)
    where revoked_at is null;

create table if not exists _dbo.auth_tokens (
    id          uuid primary key default gen_random_uuid(),
    project_id  uuid not null references _dbo.projects(id) on delete cascade,
    collection  text not null default 'users',
    user_id     uuid not null,
    type        text not null check (type in ('verify_email', 'password_reset')),
    token_hash  text unique not null,
    created_at  timestamptz not null default now(),
    expires_at  timestamptz not null,
    used_at     timestamptz null
);
```

Project `users` table:

- M4 creates a system collection row in `_dbo.collections`:
  - `name = 'users'`
  - `type = 'auth'`
  - `system = true`
  - default `view_rule = 'id = @request.auth.id'`
  - default `update_rule = 'id = @request.auth.id'`
  - `list_rule`, `create_rule`, `delete_rule` are `null`
- M4 creates the physical project table if missing.
- M4 adds hidden columns to auth collection tables:
  - `email text not null`
  - `email_normalized text not null`
  - `verified boolean not null default false`
  - `password_hash text not null`
  - `token_key text not null`
  - `disabled_at timestamptz null`
  - `last_login_at timestamptz null`
- Unique index:
  - `(email_normalized)` per project users table.

Hidden columns must never be writable through the generic records API:

- `email_normalized`
- `password_hash`
- `token_key`
- `disabled_at`
- `last_login_at`

The public record response for `users` must not expose `password_hash` or
`token_key`.

## APIs

Public app auth endpoints:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/projects/{slug}/auth/signup` | Create user and session |
| `POST` | `/api/projects/{slug}/auth/login` | Create session |
| `POST` | `/api/projects/{slug}/auth/refresh` | Rotate refresh token |
| `POST` | `/api/projects/{slug}/auth/logout` | Revoke current refresh token |
| `POST` | `/api/projects/{slug}/auth/logout-all` | Rotate token key and revoke all sessions |
| `GET` | `/api/projects/{slug}/auth/me` | Current authenticated user |
| `POST` | `/api/projects/{slug}/auth/request-verification` | Create verification token |
| `POST` | `/api/projects/{slug}/auth/confirm-verification` | Mark email verified |
| `POST` | `/api/projects/{slug}/auth/request-password-reset` | Create reset token |
| `POST` | `/api/projects/{slug}/auth/confirm-password-reset` | Set new password and rotate token key |

Signup request:

```json
{
  "email": "user@example.com",
  "password": "correct horse battery staple"
}
```

Auth response:

```json
{
  "token": "jwt",
  "expiresAt": "2026-07-03T17:00:00Z",
  "refreshToken": "dbo_refresh_...",
  "refreshExpiresAt": "2026-07-10T16:00:00Z",
  "user": {
    "id": "...",
    "email": "user@example.com",
    "verified": false,
    "created": "...",
    "updated": "..."
  }
}
```

Refresh request:

```json
{
  "refreshToken": "dbo_refresh_..."
}
```

Logout request:

```json
{
  "refreshToken": "dbo_refresh_..."
}
```

Reset/verification:

- Request endpoints always return `202 accepted` when the email shape is valid.
- Confirmation endpoints require token + email/new password as needed.
- If `AUTH_DEV_TOKENS=true`, request endpoints may include `devToken` in the JSON
  response for local tests only.
- In production with no SMTP, request endpoints still create tokens but do not
  expose plaintext tokens.

## UI/UX

No full admin UI in M4.

For API behavior:

- Responses should be predictable and PocketBase-like.
- Do not reveal whether a reset/verification email exists.
- Use stable error codes:
  - `user_exists`
  - `invalid_credentials`
  - `invalid_refresh_token`
  - `invalid_auth_token`
  - `user_disabled`

## Edge Cases

- Duplicate signup for same normalized email.
- Login with bad password.
- Refresh with expired token.
- Refresh with token already rotated.
- Refresh token replay after rotation.
- Logout with already-revoked refresh token.
- Logout-all followed by record API request using old access token.
- Password reset for disabled user.
- Verification token used twice.
- Password reset token used twice.
- Project exists from M3 with no `users` table yet.
- Existing project has a user-created `users` base collection; M4 must fail
  loudly with `409 provisioning_conflict` instead of taking it over silently.
- App-auth user is deleted or disabled between JWT issue and record request.

## Security

- Store only token hashes.
- Return refresh plaintext once.
- Keep password hashes out of record responses.
- Keep `token_key` out of record responses.
- Use constant-ish error shape for credential failures.
- Rate-limit signup, login, refresh, reset, and verification requests.
- Rotate refresh token on every refresh.
- Reuse of rotated refresh token revokes the family.
- Rotate `token_key` on logout-all and password reset.
- Do not accept authenticated JWTs unless:
  - signature is valid;
  - `exp` is valid;
  - `project` matches route;
  - `role` is `authenticated`;
  - `sub` is a UUID;
  - user exists in project `users`;
  - user is not disabled;
  - JWT `token_key` matches current DB `token_key`.
- Keep RLS as the record boundary; app auth only establishes request claims.

## Testing

Unit tests:

- email normalization and password validation.
- refresh token generation/hash prefix.
- access JWT claims and expiration.
- dev-token gate for reset/verification.

Integration tests with real Postgres:

- signup creates system users collection/table if missing.
- duplicate email conflicts.
- login returns token and refresh token.
- refresh rotates and invalidates old refresh token.
- replay of rotated refresh token revokes token family.
- logout revokes current refresh token.
- logout-all invalidates old access token in `ResolveRecordAuth`.
- password reset rotates `token_key`.
- authenticated token can access a record protected by
  `owner = @request.auth.id`.
- non-owner authenticated token cannot access that record.
- hidden auth columns are not writable/readable through records API.
- existing project with conflicting `users` base collection fails safely.

Release validation:

- `go test ./...`
- `go vet ./...`
- `git diff --check`
- disposable PostgreSQL 16/17/18 matrix.
- Portainer stack smoke after image publish.

## Implementation Steps

1. Add M4 migration for `_dbo.sessions` and `_dbo.auth_tokens`.
2. Add app-auth config:
   - `BCRYPT_COST`
   - `AUTH_DEV_TOKENS`
3. Add core token helpers for access, refresh, reset, and verification tokens.
4. Add users auth collection provisioning.
5. Update collection/table creation for hidden auth columns.
6. Block hidden auth columns in generic records reads/writes.
7. Implement signup/login core flows.
8. Implement refresh rotation and replay handling.
9. Implement logout and logout-all.
10. Implement verification and reset token request/confirm flows.
11. Update `ResolveRecordAuth` to verify `token_key`.
12. Add HTTP handlers and routes.
13. Add permanent tests for auth lifecycle and RLS integration.
14. Update README, docs, and deploy template env docs.
15. Run local and PostgreSQL 16/17/18 validation.
16. Commit, push, tag `v0.5.0`, wait for CI/GHCR, then deploy/test through
    Portainer.
