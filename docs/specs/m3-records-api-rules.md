# M3 Spec: Records API and Rules

Status: implemented in v0.4.0
Depends on: v0.3.0

M3 turns M2 collection tables into usable data APIs. It adds records CRUD,
pagination/sort/filter, request role selection with `SET LOCAL ROLE`, and
collection rules compiled into native Postgres RLS policies. M3 does not add
signup/login for app users; M4 will issue the authenticated JWTs that M3 already
knows how to validate.

## Goal

Let clients read and mutate records through the public project API while Postgres
itself enforces collection rules.

Success means:

- Records CRUD works for base/auth collection tables created in M2.
- Every record request runs inside a transaction with `SET LOCAL ROLE`,
  request claims, request operation, and project search path set locally.
- Collection rules compile into Postgres RLS policies, not only app-side WHERE
  clauses.
- Service/admin access can manage records.
- Anonymous and authenticated requests are constrained by RLS.
- Filter/sort/field selection accepts only validated collection fields and
  parameterized values.

## Scope

In scope:

- Records API:
  - `GET/POST /api/projects/{slug}/collections/{name}/records`
  - `GET/PATCH/DELETE /api/projects/{slug}/collections/{name}/records/{id}`
- Minimal project access-key lifecycle using existing `_dbo.api_keys`:
  - `GET/POST /admin/api/projects/{slug}/api-keys`
  - `DELETE /admin/api/projects/{slug}/api-keys/{id}`
- Request auth resolution for records:
  - no token or anon key -> `<slug>_anon`
  - valid app-user JWT -> `<slug>_authenticated`
  - valid service key or admin session -> `<slug>_service`
- Rule compiler for the M3-safe subset.
- RLS policy generation from `list_rule`, `view_rule`, `create_rule`,
  `update_rule`, and `delete_rule`.
- Record input validation from collection field definitions.
- Real Postgres integration tests proving RLS, not only app code, blocks data.

Out of scope:

- App-user signup/login/refresh/logout. That is M4.
- Password fields, token revocation, email verification, reset flows. Those are M4.
- File uploads. That is M5.
- Realtime events and webhooks. Those are M8.
- Admin UI screens.
- Arbitrary SQL view collections.
- Full PocketBase/fexpr compatibility.
- Online backfills and long-running background migrations.

## User Flow

1. Admin logs in.
2. Admin creates a project and a collection.
3. Admin creates a service key for server-side access.
4. Admin sets collection rules with `PATCH /api/projects/{slug}/collections/{name}`.
5. A service client inserts records through the records API.
6. Anonymous clients list or view only records allowed by collection rules.
7. Authenticated clients pass a signed app-user JWT and can access records allowed
   by `@request.auth.id` rules.
8. Admin or service clients update/delete records.
9. Tests directly query Postgres under project roles to prove RLS is the boundary.

## Requirements

- All records APIs return the common JSON error envelope.
- Public records routes are not behind admin middleware.
- Admin sessions are accepted on records routes and map to service role.
- Service keys are hashed at rest and shown once on create.
- Invalid or revoked keys return `401 unauthorized`.
- Project slug and collection name are validated before any SQL is built.
- Record IDs are UUIDs.
- Collection must exist and be `base` or `auth`.
- `view` collections remain `not_implemented`.
- Every record operation uses one transaction.
- Every transaction sets:
  - `SET LOCAL ROLE <project role>`
  - `set_config('request.jwt.claims', <json>, true)`
  - `set_config('request.operation', <operation>, true)`
  - `set_config('search_path', 'proj_<slug>, pg_catalog', true)`
- No record SQL concatenates unvalidated identifiers.
- Values are always parameters.
- Unknown JSON fields are rejected.
- Client writes to system fields `id`, `created`, and `updated` are rejected in M3.
- `created` and `updated` are returned in every record response.
- `updated` is set to `now()` on every successful update.
- Pagination defaults to `page=1&perPage=30`; max `perPage=500`.
- Sort accepts comma-separated fields with optional `-` prefix.
- `fields` projection can include only system fields or collection fields.
- Filters and rules use the same safe expression compiler.

## Frontend

M3 remains API-first.

Allowed UI change:

- Update placeholder copy/API reference links if useful.

No Svelte/Vite records table is required in M3. The records table belongs to the
admin panel milestone.

## Backend

Suggested files:

- `core/api_keys.go`: generate, hash, list, revoke, and resolve project API keys.
- `core/records.go`: record CRUD, validation, query planning, result decoding.
- `core/record_auth.go`: request role resolution and transaction role setup.
- `core/rules.go`: rule parser/compiler and RLS policy generator.
- `apis/api_keys.go`: admin API-key handlers.
- `apis/records.go`: public records handlers.

Keep using stdlib `net/http`, `pgx/v5`, and existing middleware patterns.

New dependency allowed:

- `github.com/golang-jwt/jwt/v5` for validating app-user JWTs that M4 will issue.

Avoid adding a general SQL builder or ORM in M3.

## Database

Create migration `core/migrations/0004_records_rules.sql`.

M3 can use existing `_dbo.api_keys`; no schema change is required for key storage.

Add helper functions so policies are readable and testable:

```sql
create or replace function _dbo.request_claim(name text)
returns text
language sql
stable
as $$
  select nullif(current_setting('request.jwt.claims', true)::jsonb ->> name, '')
$$;

create or replace function _dbo.request_role()
returns text
language sql
stable
as $$
  select coalesce(_dbo.request_claim('role'), 'anon')
$$;

create or replace function _dbo.request_auth_id()
returns uuid
language sql
stable
as $$
  select nullif(_dbo.request_claim('sub'), '')::uuid
$$;

create or replace function _dbo.request_operation()
returns text
language sql
stable
as $$
  select nullif(current_setting('request.operation', true), '')
$$;
```

Grant execute on these functions to project roles. If function-level grants are
awkward for per-project roles, grant execute to `public`; the functions only expose
request-local settings already controlled by the app transaction.

For every collection table, M3 policy sync must also grant table privileges:

```sql
grant select, insert, update, delete on table proj_slug.posts
  to slug_anon, slug_authenticated, slug_service;
```

RLS remains enabled and forced.

## Rules

Rules live in `_dbo.collections`:

- `list_rule`
- `view_rule`
- `create_rule`
- `update_rule`
- `delete_rule`

Rule meanings:

- `null`: service/admin only.
- `""`: public for anon/authenticated plus service/admin.
- expression: anon/authenticated allowed only when expression is true; service/admin
  always allowed.

M3 rule grammar:

```text
expr       = or_expr
or_expr    = and_expr { "||" and_expr }
and_expr   = cmp_expr { "&&" cmp_expr }
cmp_expr   = primary [ cmp_op primary ]
primary    = field | literal | request_ref | "(" expr ")"
cmp_op     = "=" | "!=" | ">" | ">=" | "<" | "<="
literal    = string | number | boolean | null
request_ref = "@request.auth.id" | "@request.auth.role" | "@request.auth.collection"
```

Allowed identifiers:

- Collection fields.
- System fields: `id`, `created`, `updated`.

Allowed request refs:

- `@request.auth.id` -> `_dbo.request_auth_id()`
- `@request.auth.role` -> `_dbo.request_role()`
- `@request.auth.collection` -> `_dbo.request_claim('collection')`

Examples:

```text
published = true
owner = @request.auth.id
status = "published" || owner = @request.auth.id
@request.auth.role = "authenticated" && owner = @request.auth.id
```

Unsupported in M3:

- function calls
- regex
- array contains
- joins and relation traversal
- arithmetic
- raw SQL snippets
- unquoted date literals

Any unsupported expression returns `422 invalid_rule`.

## RLS Policies

M2 default-deny policies are replaced by operation-aware policies.

For each collection table:

- Service policy:
  - select/insert/update/delete: always true for `<slug>_service`.
- Anonymous/authenticated policies:
  - select policy checks `_dbo.request_operation()`:
    - `list` uses `list_rule`
    - `view` uses `view_rule`
  - insert policy uses `create_rule`.
  - update policy uses `update_rule` for both `USING` and `WITH CHECK`.
  - delete policy uses `delete_rule`.

Policy names should be stable and short enough for Postgres identifier limits:

```text
dbo_svc_select
dbo_svc_insert
dbo_svc_update
dbo_svc_delete
dbo_client_select
dbo_client_insert
dbo_client_update
dbo_client_delete
```

The policy generator must drop/recreate only dublyobase-owned policies.

## APIs

### GET `/api/projects/{slug}/collections/{name}/records`

Query params:

- `page`: default `1`, min `1`.
- `perPage`: default `30`, max `500`.
- `sort`: comma list, e.g. `-created,title`.
- `filter`: M3 expression subset.
- `fields`: comma list of system/collection fields.

Response:

```json
{
  "items": [
    {
      "id": "9c10d5b9-3a23-4f25-91c3-09a40d7e9f7e",
      "created": "2026-07-03T15:00:00Z",
      "updated": "2026-07-03T15:00:00Z",
      "title": "Hello"
    }
  ],
  "page": 1,
  "perPage": 30,
  "totalItems": 1
}
```

### POST `/api/projects/{slug}/collections/{name}/records`

Body:

```json
{
  "title": "Hello",
  "published": true
}
```

Response:

- `201` record object

### GET `/api/projects/{slug}/collections/{name}/records/{id}`

Response:

- `200` record object
- `404 record_not_found` when missing or hidden by RLS

### PATCH `/api/projects/{slug}/collections/{name}/records/{id}`

Body:

```json
{
  "title": "Updated"
}
```

Response:

- `200` record object
- `404 record_not_found` when missing or hidden by RLS

### DELETE `/api/projects/{slug}/collections/{name}/records/{id}`

Response:

- `204` on success
- `404 record_not_found` when missing or hidden by RLS

### GET `/admin/api/projects/{slug}/api-keys`

Admin only.

Response:

```json
{
  "items": [
    {
      "id": "...",
      "name": "server",
      "type": "service",
      "prefix": "dbo_service_abcd",
      "createdAt": "2026-07-03T15:00:00Z",
      "revokedAt": null
    }
  ]
}
```

### POST `/admin/api/projects/{slug}/api-keys`

Admin only.

Body:

```json
{"name":"server","type":"service"}
```

Response:

```json
{
  "id": "...",
  "name": "server",
  "type": "service",
  "prefix": "dbo_service_abcd",
  "key": "dbo_service_abcd..."
}
```

The plaintext `key` is returned once.

### DELETE `/admin/api/projects/{slug}/api-keys/{id}`

Admin only. Marks `revoked_at = now()`.

Response:

- `204`

## Record Validation

Create:

- Required collection fields must be present unless their column has a default.
- Unknown fields are rejected.
- System fields are rejected.
- `null` is rejected for required fields.

Patch:

- Unknown fields are rejected.
- System fields are rejected.
- Empty patch body is rejected.
- `null` is allowed only for non-required fields whose SQL column accepts null.

Type rules:

- `text`, `email`, `url`: JSON string.
- `number`: JSON number.
- `bool`: JSON boolean.
- `date`: RFC3339 string stored as `timestamptz`.
- `json`: any JSON object/array/value accepted and stored as `jsonb`.
- `select`: string for single, string array for multi; values must be in
  `options.values`.
- `relation`: UUID string for single, UUID string array for multi.

Email/url format checks can be app-level validation in M3; database CHECK
constraints can land later if needed.

## SQL Shape

Every records call follows this pattern:

```text
begin
set local role <project_role>
select set_config('request.jwt.claims', $1, true)
select set_config('request.operation', $2, true)
select set_config('search_path', $3, true)
run parameterized SQL against quoted table/columns
commit
```

Use `pgx.Identifier` for identifiers and `$n` parameters for values.

List:

- Query visible rows under RLS.
- Run total count under the same role, operation, filter, and RLS.
- Apply sort, limit, and offset after validation.

Create:

- Insert only validated collection field columns.
- Return inserted row.

Patch:

- Update only provided fields plus `updated = now()`.
- Return updated row.

Delete:

- `delete ... returning id` so RLS-hidden rows become `404`.

## Edge Cases

- Unknown project slug returns `404 project_not_found`.
- Unknown collection returns `404 collection_not_found`.
- Invalid record UUID returns `422 validation_failed`.
- Invalid filter returns `422 invalid_filter`.
- Invalid rule returns `422 invalid_rule`.
- Invalid sort or projection field returns `422 validation_failed`.
- Rule-hidden record returns `404 record_not_found` for get/update/delete.
- RLS insert/update rejection returns `403 rls_denied`.
- Service key can access all rows in its project.
- Service key for project A cannot access project B.
- Revoked key returns `401 unauthorized`.
- App-user JWT with wrong project claim returns `401 unauthorized`.
- App-user JWT with bad signature or expired `exp` returns `401 unauthorized`.
- Client cannot write unknown fields or system fields.
- Duplicate/invalid relation UUIDs return `422 validation_failed`.
- Filter injection attempts are rejected before SQL execution.
- Empty collection fields still allow records with only system columns.
- `totalItems` counts only rows visible to the current role and rules.

## Security

- RLS is the data boundary; app-side filters are never the only protection.
- Record routes use `SET LOCAL ROLE`, never session-level `SET ROLE`.
- Every transaction commits or rolls back before the pooled connection is reused.
- `search_path` is local to the transaction and includes only the project schema
  and `pg_catalog`.
- Service keys are random, prefixed, hashed at rest, and shown once.
- API key hashes use the existing token-hash helper.
- JWTs require `JWT_SECRET`, `exp`, `sub`, `role`, and `project` claims.
- SQL errors are logged with detail but returned through safe error envelopes.
- Do not leak whether a hidden record exists.
- Do not expose raw Postgres permission messages to clients.
- RLS policies are generated from a parser AST, never from string concatenation.

## Testing

Unit tests:

- API key generation, prefixing, hashing, and revocation lookup.
- Record payload validation for each M2 field type.
- Rule parser accepted expressions.
- Rule parser rejected expressions.
- Filter compiler parameterizes values.
- Sort/projection validation.
- SQL identifier quoting helpers.

Integration tests with real Postgres:

- Admin creates project, collection, service key.
- Service key creates/list/gets/patches/deletes records.
- Anonymous public list/view rules expose only allowed records.
- `null` rules block anonymous access.
- Authenticated JWT with `owner = @request.auth.id` sees only owned rows.
- Direct SQL under `<slug>_anon` proves RLS blocks private rows.
- Direct SQL under `<slug>_authenticated` with claims proves RLS allows owned rows.
- Service role policy can access all rows.
- Insert/update/delete rules produce `403 rls_denied`.
- Missing/hidden record returns `404`.
- Filter/sort injection attempts fail.
- Concurrent creates produce unique records and no transaction leakage.

Validation commands:

```bash
go test ./...
TEST_DATABASE_URL="postgres://..." go test -v ./...
go vet ./...
git diff --check
```

Before tagging `v0.4.0`, run the full suite against PostgreSQL 16, 17, and 18.

## Implementation Steps

1. Add `0004_records_rules.sql` with request helper functions.
2. Add API key core helpers and admin handlers.
3. Add record auth resolution for anon, service key, admin session, and app-user JWT.
4. Add transaction helper that sets local role, claims, operation, and search path.
5. Add rule parser AST.
6. Add rule/filter SQL compiler.
7. Add RLS policy sync from collection rules.
8. Update collection create/update paths to regenerate grants and policies.
9. Add record payload validation and SQL row scanning.
10. Add records list/create/get/patch/delete core functions.
11. Add public records HTTP handlers and route registration.
12. Map `record_not_found`, `invalid_filter`, `invalid_rule`, and `rls_denied`.
13. Add unit tests.
14. Add real Postgres integration tests.
15. Update README and docs only after implementation is complete.
16. Run real PostgreSQL 16/17/18 validation before tagging `v0.4.0`.

## Acceptance Criteria

- `v0.3.0` collection tables can be used immediately by the records API after
  M3 policy sync.
- Service key can perform full CRUD.
- Anonymous and authenticated clients are constrained by RLS.
- App-side query filters cannot bypass RLS.
- Direct Postgres tests prove policies enforce the same access boundary without
  application WHERE clauses.
- Invalid identifiers, filters, sorts, and payloads fail before unsafe SQL.
- All checks pass locally and against real PostgreSQL.
