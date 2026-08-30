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
- Field types: text, rich editor, password, number, decimal, vector, bool, date,
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
| `ENABLE_PGVECTOR` | No | `false` | Reserved. Vector fields detect pgvector themselves; see below. |
| `DATABASE_MAX_CONNS` | No | `25` | Connection pool ceiling. Every request holds a connection for its transaction, so this is the practical limit on concurrent queries. pgx's own default is `max(4, numCPU)`, which caps an instance at roughly six requests a second. Raise it for a busy instance, keeping the total across all instances under the server's `max_connections`. A `pool_max_conns` in `DATABASE_URL` overrides this. |
| `DATABASE_MIN_CONNS` | No | `2` | Connections kept open when idle. |
| `CRON_ALLOW_PRIVATE_TARGETS` | No | `false` | Lets cron jobs call private, loopback, and link-local addresses. Off by default so a job URL cannot be pointed at cloud metadata or a service bound to localhost. Turn it on only if you deliberately cron an internal service. |

### Recovering a locked-out admin

There is no password reset by email — admin recovery requires access to the
deployment rather than to an inbox. From the server (or `docker exec` into the
container):

```bash
dublyobase admin reset-password --email you@example.com
```

It prints a one-time password, forces a change at next login, revokes that
admin's existing sessions, and records the reset in the audit log. Pass
`--password` to choose the password yourself.

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
| `POST /api/projects/{slug}/collections/{name}/records/search` | Nearest-neighbour search over a vector field. |
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

### Record history and optimistic locking

Every managed collection records its writes to `dbo_record_history` in the
project's own schema — actor, transaction id, which fields changed, and the full
before/after. Capture is a PostgreSQL trigger, not application code, so a write
from the admin SQL console or any other client is recorded exactly like an API
call. The project roles are granted `select` and nothing else: an audit trail a
caller can edit is not an audit trail.

```text
GET /api/projects/{slug}/collections/{name}/records/{id}/history
```

Reading a record's history requires being able to read the record, so the
endpoint cannot be used to see rows row-level security hides. Set
`"options": {"history": false}` on a high-churn collection to opt out.

Every record also carries `_version`, taken from PostgreSQL's own `xmin`, so it
needs no extra column and cannot drift from the row. Send it back as `If-Match`
(or `?version=`) on an update or delete and the write applies only if nobody
else got there first, returning `409` if they did. Omitting it keeps the
previous last-write-wins behaviour.

### Event delivery

Realtime events and webhook deliveries were published after the record
transaction committed, so a crash in that window lost the event with nothing
left to say it was owed. The event row is now written by the same trigger that
records history, inside the same transaction as the write: if the row exists,
the event exists.

Delivery is unchanged in the normal case — the request publishes immediately
after commit and marks its own row done. A sweep on the ops worker picks up
rows nobody marked, which in practice means the process died between COMMIT and
publish. The sweep ignores events younger than a minute so it never races a
live request, retries a failed delivery on the next pass, records the last
error, and gives up after ten attempts rather than looping forever. Delivered
rows are pruned after seven days; undelivered rows are never pruned, because an
event nobody could deliver is a fault to look at rather than litter to sweep
away.

### Computed columns, checks, and unique indexes

Three collection options push single-row invariants into PostgreSQL itself, so a
direct SQL session or a batch cannot get around them either.

A `computed` field is a stored generated column: the database owns the value, so
it cannot be forged by a client and cannot drift from its inputs. It is
supported on `decimal`, `number`, `bool`, and `text`, may not reference itself,
and may not also be `required` — the database supplies the value.

`checks` are named row-level `CHECK` constraints. `indexes` create lookup or
unique indexes over one or more fields.

```json
{
  "name": "quote_items",
  "fields": [
    { "name": "doc_no", "type": "text" },
    { "name": "qty", "type": "number", "options": { "onlyInt": true } },
    { "name": "unit_price", "type": "decimal", "options": { "precision": 18, "scale": 3 } },
    { "name": "line_total", "type": "decimal",
      "options": { "precision": 18, "scale": 3, "computed": "qty * unit_price" } }
  ],
  "options": {
    "checks": [
      { "name": "qty_positive", "expression": "qty > 0" },
      { "name": "price_non_negative", "expression": "unit_price >= 0" }
    ],
    "indexes": [
      { "name": "doc_no_unique", "fields": ["doc_no"], "unique": true }
    ]
  }
}
```

A comparison against a null column is null, not false, and a `CHECK` passes
unless it evaluates to false — so `usage_count <= usage_limit` enforces nothing
on rows where `usage_limit` is null. That is SQL's rule rather than
Dublyobase's, and the fix is to say what should happen:
`usage_limit = null || usage_count <= usage_limit`. Both `&&`/`||` and SQL's
`and`/`or` are accepted.

Expressions here are compiled more strictly than API rules: they live inside
DDL, so a `@request` reference, a subquery, or a call to `now()` is refused
rather than silently evaluated. Arithmetic over a `decimal` stays exact — it is
not widened to floating point. A violated constraint returns `422` (or `409` for
a uniqueness collision) naming the rule, not a `500`.

Adding `computed` to a field that already exists rebuilds the column, so it is
only applied when the request opts into dropping and recreating fields.

### Filtering through relations

A filter key may walk relations with a dotted path, so "every ticket whose
organization is on the free plan" is one request rather than fetching the ids
first:

```text
GET .../collections/tickets/records?filter={"team.org.plan":{"_eq":"free"}}
```

Each hop compiles to a nested `EXISTS`, and every segment before the last must
be a declared relation field — a filter can only reach tables the schema already
links to, never an arbitrary one. Paths are capped at four hops, since each is
another subquery. Any leaf operator works, and relation filters combine with
ordinary ones.

The subqueries run as the same database role as the outer query, so row-level
security applies to the related tables too. Filtering on a table the caller
cannot read matches nothing rather than revealing it, and negating the filter
does not invert that into a full listing.

Sorting does not yet follow relations; `sort` still takes fields on the
collection itself.

### Vector fields and similarity search

A `vector` field stores an embedding and is searched by distance, which covers
the retrieval half of a RAG pipeline. Dublyobase does not generate embeddings —
you write them, from whichever model you use.

```json
{ "name": "embedding", "type": "vector",
  "options": { "dimensions": 1536, "metric": "cosine" } }
```

`metric` is `cosine` (default), `l2`, or `inner_product`. Each vector field gets
an HNSW index built for its own metric, so ordering by a different distance
would silently fall back to scanning the table. Dimensions above pgvector's
indexing ceiling of 2000 still store and search, just without the index.

Write the value as a JSON array and read it back as one. The dimension count is
checked before the write reaches the database, so a wrong-length embedding names
the field and both counts instead of surfacing a driver error.

Search is a POST, because a 1536-number embedding does not belong in a URL:

```text
POST /api/projects/{slug}/collections/{name}/records/search
{ "field": "embedding", "vector": [0.01, -0.2, ...], "limit": 10,
  "filter": {"published": {"_eq": true}} }
```

It runs through the ordinary list path, so the same row-level security, filters,
projection and paging apply as on any other read — a similarity search cannot
return rows the caller could not otherwise see.

pgvector has to be available to the database. The bundled `docker-compose.yml`
and the Dublyo deploy template both use `pgvector/pgvector`, which is the
official Postgres image plus the extension, so vector fields work on a fresh
install. Against a database that does not have it — including stock
`postgres:*` images — creating a vector field is refused with that reason rather
than failing obscurely, and every other field type is unaffected.

### Unenforced relations

A relation creates a real foreign key, which is almost always what you want. An
application being migrated from elsewhere may already hold references its old
database never checked — a Laravel `integer('customer_id')` with no
`->foreign()` is the common case — and importing those rows against a real
constraint fails on the ones that are already dangling.

```json
{ "name": "customer", "type": "relation",
  "options": { "collection": "customers", "enforced": false } }
```

The column, and everything built on it, is unchanged: `expand`, relation
filters and relation sorting all work, because they read the column rather than
the constraint. What is gone is the guarantee. Deleting the target leaves the
reference pointing at nothing, and nothing will tell you.

The target collection is still resolved when the field is saved, so a typo is
caught rather than becoming a column that expands to nothing. `onDelete` is
refused, since there is no constraint for it to act on.

Prefer a real relation. Reach for this to get a legacy dataset in, then clean
the data and turn enforcement on.

### Rollups

A rollup field is the running aggregate of a related collection: an order's line
subtotal, a product's review count, a customer's lifetime spend.

```json
{ "name": "line_subtotal", "type": "decimal",
  "options": { "precision": 12, "scale": 4,
    "rollup": { "collection": "order_line_items", "field": "order",
                "aggregate": "sum", "source": "total" } } }
```

`aggregate` is `sum`, `count`, `avg`, `min` or `max`; `source` is the child field
being aggregated and is not needed for `count`. An optional `where` restricts
which children count, using the same expression language as a check.

It is maintained rather than checked. A constraint that merely rejected a
disagreeing total would leave every caller to compute the total correctly first,
which is the work they wanted the database to do. A trigger on the child table
writes the value, so inserting, updating, deleting or re-parenting a child all
correct it — moving a line from one order to another fixes both orders. Adding a
rollup to a collection that already has rows backfills it. The API rejects any
attempt to write the field, exactly as it does for a computed column.

`sum` and `count` of nothing are zero, so those columns default to zero and a
parent with no children reads `0` rather than null. `avg`, `min` and `max` of
nothing are genuinely unknown and stay null.

### Sorting through relations

`sort` accepts the same dotted paths as filters, so products can be ordered by
their category's name, or by their category's brand:

```text
GET .../collections/products/records?sort=cat.brand.name
```

Each hop compiles to a correlated subquery rather than a join, so the row count
is unchanged even when the path walks a multi-value relation. Ordering follows
PostgreSQL's own null placement — ascending puts rows with no related record
last, descending puts them first — which is what a plain column sort already
does.

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
| `PATCH /admin/api/cron-jobs/{id}` | Replace every field of a cron job and reschedule it. |
| `DELETE /admin/api/cron-jobs/{id}` | Delete a cron job and its run history. |
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
- **A project-scoped backup is not a recovery artifact.** It contains the
  project's schema, rows and policies, but none of the `_dbo` metadata that
  registers the project and defines its collections, so restoring one into a
  fresh instance leaves tables Dublyobase does not know exist. Use a full backup
  for disaster recovery; use project backups to move or inspect a tenant's data.
- **A restore does not carry roles or grants.** Roles live in the cluster rather
  than the database, so `pg_dump` never writes them, and the restore runs with
  `--no-privileges`. Every RLS policy names a role and calls a function in
  `_dbo`, so a fresh restore creates no policies at all — `pg_restore` reports
  those as errors it ignored and still exits 0. Row-level security stays enabled
  on the tables, so this fails closed rather than open. On the next boot the
  server detects the missing roles and rebuilds the roles, grants and policies
  from `_dbo` before serving; the log records this per project. Restoring into a
  cluster where the roles already exist is unaffected.
- Cron and webhook targets may not reach private, loopback, or link-local
  addresses. The check runs twice on purpose: once when the job is saved, which
  resolves the hostname so a name pointing inward is caught immediately, and
  again in the dialer at connect time, which is the authoritative one — it
  catches a public name whose DNS answer changes later. Save-time resolution is
  best effort, so a name that will not resolve right now is still allowed
  through and the dialer decides. Set `CRON_ALLOW_PRIVATE_TARGETS=true` only if
  you deliberately cron an internal service; it is env-only, so an admin API or
  MCP token cannot turn it on.
- S3 and SMTP secrets are masked in API responses and audit logs.
- OAuth provider secrets are encrypted at rest and masked in API responses.
- For multi-replica realtime, use the same Postgres database so persisted events
  and `LISTEN/NOTIFY` fanout are shared by all replicas.

## Source Layout

| Path | Contents |
|---|---|
| `main.go`, `cmd/` | Entry point and CLI verbs (`serve`, `admin`). |
| `core/` | Everything that touches the database: collections, records, rules, auth, cron, backups, storage, migrations. No HTTP. |
| `apis/` | HTTP layer — routing, request decoding, error mapping. Handlers stay thin and call `core`. |
| `ui/admin/` | The embedded Next.js admin panel, compiled and served from `/_/`. |

Rules are compiled to PostgreSQL row-level security rather than checked in Go,
so a client reaching the database another way is bound by the same rules. When
adding an enforcement feature, prefer pushing it into the database over adding a
check in a handler.

### Admin panel layout

`app/page.tsx` was a single 9,000-line file; it is now the app shell and its
state, with everything it renders imported. Feature code lives beside it:

| Path | Contents |
|---|---|
| `app/lib/constants.ts` | Field/icon choices, nav items, and the empty form drafts. |
| `app/lib/view-types.ts` | View-layer types (`View`, `SettingsSection`, drafts, relation types). |
| `app/lib/format.ts` | Formatting and small pure data helpers. |
| `app/lib/fields.ts` | Field definitions, options, and record value handling. |
| `app/lib/relations.ts` | Relation modelling — cardinality, anchors, labels. |
| `app/lib/collections.ts` | Collection metadata and schema import. |
| `app/lib/sql.ts`, `app/lib/settings-drafts.ts` | SQL console helpers; server settings mapped onto form drafts. |
| `app/components/*.tsx` | One module per feature area: `ui` (shared primitives), `auth`, `collections`, `collection-editor`, `record-editor`, `relation-picker`, `insights`, `logs`, `settings-shell`, `settings-panels`, `ops-views`. |

The library modules form a one-way chain — `fields` → `relations` →
`collections` — so keep new helpers pointing the same direction rather than
importing backwards.

### Conventions worth knowing

- **Values crossing the API are rendered, never `fmt.Sprint`ed.** pgx hands back
  driver types, and printing them produces Go debug output — a uuid as a byte
  array, a numeric as `{2885000 -3 false finite true}`, jsonb as `map[a:1]`.
  `numeric` in particular must go through the exact decimal formatter rather
  than a float, or money loses precision on the way out.
- **A `decimal` is a string on the wire.** It is `numeric(p,s)` in Postgres and
  JSON has no exact decimal type, so it is transported as a string. Do not
  parse it into a float to "clean it up".
- **Errors carry a code and a message that should not repeat each other.** The
  panel prepends the code only when the message does not already contain it.
- **Panel error toasts persist until dismissed.** They used to auto-dismiss,
  which made failures easy to miss entirely.

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
createdb dublyobase_test
TEST_DATABASE_URL="postgres://postgres@localhost:5432/dublyobase_test?sslmode=disable" go test ./...
go build ./...
```

**Set `TEST_DATABASE_URL` or most of the suite silently does nothing.** Every
test that needs a database skips without it, and `go test ./...` still prints
`ok` — at the time of writing that is 63 of the 80 tests in `apis`. A green run
with no database configured says almost nothing. CI sets the variable, so this
only bites locally. Each integration test creates and drops its own database, so
the role in that URL needs `CREATEDB`.

The admin panel is compiled into the binary with `go:embed`, so a UI change is
not visible to `go run .` until `npm run --prefix ui/admin build` has run.

Run locally against your own database:

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/dublyobase?sslmode=disable"
export APP_URL="http://localhost:8080"
export JWT_SECRET="$(openssl rand -base64 32)"
go run . serve
```

## License

MIT
