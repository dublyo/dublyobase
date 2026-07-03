# M2 Spec: Collections and Schema Sync

Status: draft for v0.3.0
Depends on: v0.2.0

M2 turns the M1 control plane into a real PocketBase-style schema builder for Postgres. It adds project-scoped collection metadata, safe physical table creation, field validation, and default-deny RLS. It does not implement the records CRUD API yet; that is M3.

## Goal

Let an authenticated admin define project collections through the API and have dublyobase materialize those definitions as real Postgres tables inside the project schema.

Success means:

- Collections are persisted in `_dbo.collections`.
- Base/auth collection creation creates a real table in `proj_<slug>`.
- Table/field/index identifiers are validated and quoted.
- Every collection table has `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY`.
- Default-deny policies exist immediately, before any public records API exists.
- Update operations can safely add, rename, and remove fields within the M2-supported subset.

## Scope

In scope:

- Migration `0003_collections.sql`.
- Field registry and validation for the first M2 field set.
- Collection create/list/get/update/delete API under `/api/projects/{slug}/collections`.
- Admin auth for all collection writes.
- Schema sync for base/auth collection tables.
- Default-deny RLS on every materialized table.
- Tests proving identifier safety, physical DDL, RLS presence, and safe rename behavior.

Out of scope:

- Records CRUD.
- Public anon/authenticated/service key access.
- App-user auth behavior.
- Full rule DSL parser/compiler.
- File upload handling.
- Realtime events.
- Admin UI screens.
- Large-table online migration background workers.
- View collections backed by arbitrary SQL.

## User Flow

1. Admin logs in through M1.
2. Admin creates a project.
3. Admin calls `POST /api/projects/{slug}/collections` with collection name, type, and fields.
4. dublyobase validates identifiers and field definitions.
5. dublyobase stores the collection definition in `_dbo.collections`.
6. dublyobase creates `proj_<slug>."<collection>"` with system columns and requested fields.
7. dublyobase enables and forces RLS and creates default-deny policies.
8. Admin lists collections and fetches one collection.
9. Admin updates fields; schema sync applies safe DDL.
10. Admin deletes an empty/non-system collection; table and metadata are removed.

## Requirements

- All collection APIs return the common JSON error envelope.
- Admin auth is required for every M2 collection route.
- Project slug must already exist.
- Collection names match `^[a-z][a-z0-9_]{0,58}$`.
- Field names match `^[a-z][a-z0-9_]{0,58}$`.
- Reserved names are rejected:
  - prefixes: `_dbo`, `pg_`
  - exact: `id`, `created`, `updated`, `tableoid`, `xmin`, `cmin`, `xmax`, `cmax`, `ctid`, `oid`, `information_schema`, `public`
- Duplicate field names are rejected.
- Collection types in M2: `base`, `auth`.
- `view` collection type is rejected as `not_implemented` until a later milestone.
- System columns always exist:
  - `id uuid primary key default gen_random_uuid()`
  - `created timestamptz not null default now()`
  - `updated timestamptz not null default now()`
- Every materialized table has RLS enabled and forced before commit.
- Default-deny policies exist for select/insert/update/delete.
- Schema sync must be transactional except operations Postgres forbids inside transactions. M2 avoids `CREATE INDEX CONCURRENTLY`; concurrent index builds are postponed.
- Delete/rename operations must not silently drop data unless explicitly requested.

## Frontend

M2 remains API-first. The embedded UI should not claim a visual collection editor exists yet.

Allowed UI change:

- Update placeholder copy to say the collection API exists if useful.

No Svelte/Vite app work is required in M2.

## Backend

Suggested files:

- `core/identifier.go`: shared identifier validation and quoting helpers.
- `core/fields.go`: field definitions, registry, JSON parsing/validation, column DDL.
- `core/collections.go`: collection model, CRUD, schema sync orchestration.
- `core/rls.go`: default-deny RLS and policy helpers.
- `apis/collections.go`: collection API handlers.

Keep using stdlib `net/http`, `pgx/v5`, and existing middleware/auth patterns.

## Database

Create migration `core/migrations/0003_collections.sql`.

Tables:

### `_dbo.collections`

- `id uuid primary key default gen_random_uuid()`
- `project_id uuid not null references _dbo.projects(id) on delete cascade`
- `name text not null`
- `type text not null check (type in ('base','auth','view'))`
- `system boolean not null default false`
- `fields jsonb not null default '[]'::jsonb`
- `indexes jsonb not null default '[]'::jsonb`
- `list_rule text null`
- `view_rule text null`
- `create_rule text null`
- `update_rule text null`
- `delete_rule text null`
- `options jsonb not null default '{}'::jsonb`
- `created_at timestamptz not null default now()`
- `updated_at timestamptz not null default now()`
- `unique(project_id, name)`

Indexes:

- `collections_project_id_idx on _dbo.collections(project_id)`

M2 does not add `_dbo.collection_migrations`; schema changes are represented by the current collection JSON and applied synchronously. A later milestone can add generated migration history if needed.

## Field Set

M2 supports this initial field set:

- `text`
- `number`
- `bool`
- `date`
- `json`
- `email`
- `url`
- `select`
- `relation`

Deferred fields:

- `file` moves to M5.
- `geo` moves to a later milestone.
- `vector` moves to M9 behind `ENABLE_PGVECTOR`.

### Field JSON Shape

Common:

```json
{
  "name": "title",
  "type": "text",
  "required": true,
  "options": {}
}
```

Types:

- `text`: `text`, optional `max`
- `number`: `double precision`, optional `min`, `max`
- `bool`: `boolean not null default false`
- `date`: `timestamptz`
- `json`: `jsonb not null default '{}'::jsonb`
- `email`: `text`, optional case-insensitive unique index later
- `url`: `text`
- `select`: `text` for single, `text[]` for multi, options `values`, `maxSelect`
- `relation`: `uuid` for single, `uuid[]` for multi, options `collection`, `cascadeDelete`

M2 relation support can validate the referenced collection exists, but foreign key creation for multi-relations can be deferred if the model would require join tables.

## APIs

All routes require admin auth.

### GET `/api/projects/{slug}/collections`

Response:

```json
{"items":[{"id":"...","name":"posts","type":"base","fields":[]}]}
```

### POST `/api/projects/{slug}/collections`

Body:

```json
{
  "name": "posts",
  "type": "base",
  "fields": [
    {"name":"title","type":"text","required":true},
    {"name":"published","type":"bool"}
  ],
  "listRule": null,
  "viewRule": null,
  "createRule": null,
  "updateRule": null,
  "deleteRule": null
}
```

Response:

- `201` collection object

Failures:

- `401 unauthorized`
- `404 project_not_found`
- `409 collection_exists`
- `422 validation_failed`

### GET `/api/projects/{slug}/collections/{name}`

Returns one collection or `404 collection_not_found`.

### PATCH `/api/projects/{slug}/collections/{name}`

Body:

```json
{
  "name": "articles",
  "fields": [
    {"name":"title","type":"text","required":true},
    {"name":"summary","type":"text"}
  ]
}
```

M2 supports:

- Rename collection.
- Add field.
- Drop field only when request includes `"dropMissingFields": true`.
- Rename field only with explicit field option `"oldName": "previous_name"`.
- Change `required` from false to true only if the column has no nulls or a default is provided.

### DELETE `/api/projects/{slug}/collections/{name}`

Deletes metadata and drops the physical table for non-system collections.

M2 may require `"confirm": "<name>"` in body/query to avoid accidental deletes.

## RLS

M2 creates only default-deny policies.

For each table:

```sql
alter table proj_slug.posts enable row level security;
alter table proj_slug.posts force row level security;

create policy posts_select_deny on proj_slug.posts for select using (false);
create policy posts_insert_deny on proj_slug.posts for insert with check (false);
create policy posts_update_deny on proj_slug.posts for update using (false) with check (false);
create policy posts_delete_deny on proj_slug.posts for delete using (false);
```

Why default-deny in M2:

- It prevents accidental public data exposure before M3 records/rules exist.
- It gives tests a concrete DB-level security boundary immediately.
- M3 can replace policies from collection rules.

Important test:

- Direct SQL as the table owner should still be blocked by `FORCE ROW LEVEL SECURITY` unless the caller has explicit bypass behavior. If Postgres owner behavior makes this hard with the current app role, the test must document the exact role setup and assert the strongest available boundary.

## Schema Sync

Creation transaction:

1. Lock project-level schema sync with `pg_advisory_xact_lock`.
2. Insert `_dbo.collections`.
3. Create physical table with system columns.
4. Add field columns.
5. Add basic constraints/checks.
6. Enable and force RLS.
7. Create default-deny policies.
8. Insert audit row.
9. Commit.

Update transaction:

1. Lock project-level schema sync.
2. Load existing collection.
3. Validate requested collection name/fields.
4. Compute diff.
5. Apply safe DDL.
6. Update `_dbo.collections`.
7. Recreate RLS default policies if needed.
8. Insert audit row.
9. Commit.

Delete transaction:

1. Lock project-level schema sync.
2. Verify collection exists and is not system.
3. Drop physical table.
4. Delete `_dbo.collections`.
5. Insert audit row.
6. Commit.

## Edge Cases

- Unknown project slug returns `404 project_not_found`.
- Invalid collection or field identifier returns `422 validation_failed`.
- Duplicate collection name returns `409 collection_exists`.
- Duplicate field name returns `422 validation_failed`.
- Reserved system field name returns `422 validation_failed`.
- Unsupported field type returns `422 validation_failed`.
- Dropping a field without explicit confirmation returns `409 destructive_change`.
- Rename collision returns `409 collection_exists` or `422 validation_failed`.
- Physical table exists but metadata does not: return `409 provisioning_conflict`.
- Metadata exists but table does not: return `500 schema_drift` and log details.
- Two admins create same collection concurrently: one succeeds, one returns `409`.
- Two admins update same collection concurrently: project schema lock serializes changes.

## Security

- Admin auth required on all routes.
- Never concatenate unvalidated identifiers.
- All DDL identifiers pass validation and `pgx.Identifier.Sanitize()`.
- Do not expose raw SQL/DDL errors to clients.
- Audit collection create/update/delete.
- RLS default-deny is created in the same transaction as the table.
- Public record APIs remain unimplemented in M2; no anon/service API key access yet.

## Testing

Unit tests:

- Identifier validation.
- Field JSON parsing.
- Field type validation.
- Column DDL generation.
- Schema diff computation.
- Reserved name rejection.

Integration tests with real Postgres:

- Create project, then create `posts` collection.
- Verify `_dbo.collections` row.
- Verify physical table columns.
- Verify duplicate collection returns `409`.
- Verify invalid identifiers are rejected and create no table.
- Verify RLS enabled and forced.
- Verify default-deny policies exist.
- Verify direct unauthorized read is blocked where role setup allows.
- Verify add-field update preserves existing table.
- Verify explicit field rename preserves data.
- Verify delete drops table and metadata.
- Verify concurrent same-name collection creation returns one success and one conflict.

Validation commands:

```bash
go test ./...
TEST_DATABASE_URL="postgres://..." go test -v ./...
go vet ./...
git diff --check
```

## Implementation Steps

1. Add `0003_collections.sql`.
2. Add identifier validation helpers.
3. Replace the current placeholder field structs with a JSON-driven field registry.
4. Add collection model and persistence helpers.
5. Add table creation DDL builder.
6. Add default-deny RLS helper.
7. Add collection create/list/get handlers.
8. Add update schema-diff path for add/rename/drop fields.
9. Add delete handler with explicit confirmation.
10. Add audit rows for collection actions.
11. Add unit tests for fields, identifiers, and diffing.
12. Add real Postgres integration tests.
13. Update README and roadmap.
14. Run real Postgres validation before tagging `v0.3.0`.

## Acceptance Criteria

- Admin can create/list/get/update/delete collections through the API.
- Creating a collection creates a real table in the project schema.
- Default-deny RLS is enabled and forced on every materialized table.
- Invalid identifiers and unsupported fields cannot create partial DB state.
- Duplicate and concurrent creates are safe.
- Schema updates preserve data for add/rename operations.
- Tests pass locally and against real PostgreSQL.
