# dublyobase — Development Plan

> An open-source, **Postgres-backed BaaS** in the spirit of PocketBase, modernized
> for Postgres (RLS, pgvector, LISTEN/NOTIFY realtime). Ships as a **single Docker
> container** to `ghcr.io/dublyo/dublyobase`, MIT-licensed, one-click deployable on
> **Dublyo** (PaaS on cloudflared + Traefik behind Portainer).

**Status:** v0.4.0 — M3 records API & rules complete
(self-closing setup, opaque hashed admin sessions, protected admin/project APIs,
project schema/role provisioning, collection metadata, transactional schema sync,
records CRUD, API keys, RLS-backed rules, audit log, and real Postgres 16/17/18
integration tests).
Next: **M4 (app auth)**.
**Repo:** `github.com/dublyo/dublyobase` · **Image:** `ghcr.io/dublyo/dublyobase`
**Local dev:** `/Users/dribrahimm/0-PostgresProject/dublyobase`

---

## 0. Deployment reality = hard constraints

dublyobase is deployed on **Dublyo**, behind **cloudflared → Traefik → Portainer**.
The 13 rules below come from real bugs deploying PocketBase, Dify, AppFlowy and
Plausible. They are **constraints, not preferences**.

### The 13 rules (binding)

1. **Single container, one process, port 8080.** Admin UI + REST + WS/SSE + uploads
   from one Go binary. Bind `0.0.0.0` (`HOST` overridable). Port `8080` (`PORT`).
   Static assets embedded via `embed.FS`. No nginx, no service split.
2. **Env-var contract** (§2) — exact names; the Dublyo template sets them. Additive
   changes only: new vars may be added (documented, with safe defaults); existing
   names are never renamed or repurposed.
3. **Migrations run automatically & idempotently on boot** (`MIGRATE_ON_START`);
   never require `docker exec`. No `CREATE DATABASE`; avoid `CREATE EXTENSION` unless
   superuser. Assume the DB exists. Migration runs are serialized with
   `pg_advisory_lock` so replicas can boot concurrently.
4. **Cloudflare-tunnel-safe HTTP**: trust `X-Forwarded-Proto/-For` (`TRUST_PROXY_HEADERS`);
   standard WS upgrade (the logging middleware passes `http.Hijacker` /
   `ResponseController` through — keep it that way); WS idle-timeout ~90s + 30s
   heartbeats (tunnels die past ~100s); SSE flush per event + `Cache-Control:
   no-cache, no-transform` + `X-Accel-Buffering: no`; **no hardcoded `localhost:PORT`**.
5. **Image**: run as **UID 1001**; `chown 1001 /data` at build; healthcheck via
   **`127.0.0.1:${PORT:-8080}`** (never `localhost`); multi-stage `golang:1.25-alpine`
   → `alpine`, final **<30 MB**; version injected via `-ldflags -X core.Version`;
   JSON logs to **stdout**, never files; no `curl`/`npm` at runtime.
6. **`/health` contract**: `200 {status:ok, db, storage, version}` / `503 degraded`,
   answer <3 s, **generic error strings only** (detail goes to logs — this route is
   public); **`/ready`** = `503 {status:migrating}` until boot completes. The HTTP
   listener starts **before** migrations so probes never see connection-refused.
7. **Realtime = Postgres `LISTEN/NOTIFY`**, no Redis. One LISTEN conn per process,
   fan out to WS/SSE subscribers by topic; rate-limit per subscriber.
8. **RLS is the killer feature.** Auto-generated per-collection policies, overridable
   in the admin UI. Deny → `403 {error:rls_denied, policy}`, never a mystery 500.
9. **Extensibility via outbound webhooks**, not embedded JS. No scripting runtime in v1.
10. **The Dublyo template** must work with zero customization: two services
    (`db` + `app`), `restart: unless-stopped`, db healthcheck + `depends_on:
    service_healthy`, **`PGDATA=/var/lib/postgresql/data` pinned** (postgres:18 moved
    its volume layout — without the pin, data lands in an anonymous volume and is
    lost on recreate), Traefik label to 8080, `ADMIN_EMAIL`/`ADMIN_PASSWORD` seeded.
11. **CI/release**: every published image is **test-gated** (`image` job `needs: test`
    with a real Postgres service). Tags: `:vX.Y.Z` + `:latest` on version tags,
    `:main` + `:main-<sha>` on main, nightly rebuild of `:main`. Templates pin exact
    semver, never `:latest`. `concurrency` group prevents out-of-order `:main`.
12. **Anti-patterns** — never derive the public URL from `Host` (use `APP_URL`);
    never serve on 80/443; no interactive setup wizard; no `os.Exit` on *transient*
    DB errors (retry 60s — but **unparsable** `DATABASE_URL` fails immediately);
    never write outside `/data`; never log secrets (`RedactURL` covers userinfo,
    query-param and key/value-DSN passwords); config typos fail loud (strict bools);
    no runtime dependency on any external service.
13. **v1.0 checklist** (§10) — must pass on a real Dublyo `cx22`.

---

## 1. Architecture

**One stateless Go process** serving everything on `:8080`, talking to an **external
Postgres** given by `DATABASE_URL`. State lives in Postgres and the `/data` volume
(local file storage). Multiple replicas are safe: migrations take an advisory lock,
seeds are conflict-tolerant, realtime is coordinated through `LISTEN/NOTIFY`.

```
   Internet ── Cloudflare Tunnel ── Traefik (TLS) ──► dublyobase :8080  (one Go binary)
                                                        │  admin SPA (embed.FS, Svelte)
                                                        │  REST + WS/SSE + uploads
                                                        │  /health  /ready
                                                        ▼
                                          Postgres (separate service / managed)
                                            _dbo schema  (control plane)
                                            proj_<slug> schemas (per project)
                                            roles: <slug>_anon / _authenticated / _service
                                            FORCE ROW LEVEL SECURITY
                                                        ▲
                                          /data volume (local file storage) or S3
```

**Tenancy = schema-per-project** inside the one database (no `CREATE DATABASE`).
**Repo layout:** `cmd/` (cobra), `core/` (config, connect, migrate+migrations/,
seed, logger, app, version, collection, field), `apis/` (serve, middleware, later:
records/auth/storage/realtime), `ui/` (Svelte SPA → `embed.FS`), `deploy/`,
workflows. **Stack:** Go 1.25 · cobra · stdlib mux · pgx/v5 · bcrypt ·
golang-jwt/v5 · fexpr (rules) · mailyak (SMTP) · **Svelte + Vite** (admin UI —
decided: PocketBase lineage, smallest embedded bundles).

---

## 2. Environment variable contract (pinned)

```
DATABASE_URL   required   postgres://user:pass@host:5432/db?sslmode=disable
APP_URL        required   https://app.dublyo.xyz — every link/callback/webhook uses this
JWT_SECRET     required   >=32 chars (trimmed); refuse to start if missing/short
ADMIN_EMAIL    optional   seed first admin on empty DB (with ADMIN_PASSWORD)
ADMIN_PASSWORD optional
STORAGE_TYPE   local|s3   default local
STORAGE_LOCAL_PATH        default /data/storage
S3_ENDPOINT S3_BUCKET S3_ACCESS_KEY S3_SECRET_KEY S3_REGION
MIGRATE_ON_START    default true    (strict bool; typos exit 1)
TRUST_PROXY_HEADERS default true
CORS_ORIGINS        default *       (comma-separated exact origins)
LOG_LEVEL  debug|info|warn|error    default info
LOG_FORMAT json|text                default json
ENABLE_PGVECTOR default false       (true only if the extension is installed)
SMTP_HOST SMTP_PORT SMTP_USER SMTP_PASSWORD SMTP_FROM   (email skipped if host unset)
HOST default 0.0.0.0    PORT default 8080
```

Planned additions (additive only): `MAX_UPLOAD_MB` (default 64, M5),
`WEBHOOK_TIMEOUT_MS` (M8). Seeding runs **independently of** `MIGRATE_ON_START`
(warns instead of exiting when migrations are off and the schema is absent).

---

## 3. API surface (spec — implement exactly, no drift)

All JSON. Every error uses one envelope:
`{"error": "<machine_slug>", "message": "<human text>", "details": {...}?}` with
correct HTTP status (400/401/403/404/409/422/429/500). RLS denials are
`403 {"error":"rls_denied","policy":"<name>"}`. Never leak SQL or stack traces.

| Area | Routes |
|---|---|
| Meta | `GET /health` · `GET /ready` (implemented) |
| Setup | `POST /setup` — creates first admin **only while `_dbo.admins` is empty**, then 410 forever; rate-limited |
| Admin auth | `POST /admin/api/auth/login` (email+password → opaque session token) · `POST .../logout` · `GET .../me` |
| Control plane | `GET/POST /admin/api/projects` · `GET/PATCH/DELETE /admin/api/projects/{slug}` — **every `/admin/api/*` route behind auth middleware** (postbase's fatal bug: UI-only gating) |
| Collections | `GET/POST /api/projects/{slug}/collections` · `GET/PATCH/DELETE .../collections/{name}` (admin-auth for writes) |
| Records | `GET/POST /api/projects/{slug}/collections/{name}/records` · `GET/PATCH/DELETE .../records/{id}` |
| App auth | `POST /api/projects/{slug}/auth/signup` · `/auth/login` · `/auth/refresh` · `/auth/logout` · `/auth/reset-request` · `/auth/reset-confirm` · `/auth/verify` (M4) · OAuth: `GET /api/projects/{slug}/auth/oauth/{provider}` + `/callback` (M7) |
| Storage | `POST /api/projects/{slug}/files/{collection}/{recordId}/{field}` (multipart, streamed) · `GET /api/files/{...path}` (+ `?thumb=WxH`, `?token=` for protected) (M5) |
| Realtime | `GET /api/projects/{slug}/realtime` (SSE) · `GET .../realtime/ws` (WebSocket); subscribe topics `collection` or `collection/recordId` (M6) |
| Webhooks | `GET/POST/DELETE /admin/api/projects/{slug}/hooks` (M8) |

**Records list params:** `?page=1&perPage=30` (max 500), `sort=-created,title`,
`filter=<fexpr>` (safe subset: comparison + and/or + parentheses; compiled to
parameterized SQL — identifiers validated against the collection's fields),
`fields=a,b,c`. List response: `{"items":[...],"page":1,"perPage":30,"totalItems":N}`.

**Rule context:** rules reference `@request.auth.id`, `@request.auth.collection`,
record columns, and constants. `null` rule = admin-only; `""` = public.

---

## 4. Security model (the differentiator)

Enforce **in Postgres**, not app-layer string checks (postbase's fatal mistake).

- Per-project roles `<slug>_anon` / `<slug>_authenticated` / `<slug>_service`
  (NOLOGIN). The app's login role is granted them; per request:
  **one transaction wrapping `SET LOCAL ROLE <role>` + `set_config('request.jwt.claims',
  <json>, true)` + the query** — `SET LOCAL` guarantees the pooled connection resets
  on commit/rollback; never `SET ROLE` outside a transaction; `search_path` is set
  per-tx to `proj_<slug>, pg_catalog` (never trust client identifiers).
- Every collection table: `ENABLE` + **`FORCE ROW LEVEL SECURITY`** (owners obey).
- Collection rules compile to **`CREATE POLICY`** (DB-enforced) — the app's WHERE
  fragment is an optimization, not the boundary.
- Raw SQL endpoint (if ever exposed) is service-role only. API keys **hashed**
  (SHA-256) at rest, shown once. SMTP/S3/OAuth secrets **encrypted** (AES-GCM, key
  derived from `JWT_SECRET` via HKDF). Rate limits on auth + setup routes.
- Identifier rules: project slugs `^[a-z][a-z0-9_]{2,30}$`; collection/field names
  `^[a-z][a-z0-9_]{0,58}$`, reserved prefixes `_dbo`, `pg_`, `information_schema`
  rejected; all DDL identifiers pass this validation **and** are double-quoted.

---

## 5. Data model — collections on Postgres

`_dbo.collections`: `id uuid, project_id uuid fk, name text, type text
(base|auth|view), fields jsonb, indexes jsonb, list_rule/view_rule/create_rule/
update_rule/delete_rule text null, options jsonb, created/updated timestamptz`.
Each base/auth collection → real table `proj_<slug>."<name>"` with system columns
`id uuid pk default gen_random_uuid(), created timestamptz default now(), updated
timestamptz default now()`.

Field types → DDL via `Field.ColumnType()`: text→`text`, number→`double precision`,
bool→`boolean`, date→`timestamptz`, email/url→`text` + CHECK, select→`text` +
CHECK / `text[]`, json→`jsonb`, relation→`uuid` + FK (multi: `uuid[]`),
file→`text` / `jsonb`, geo→`point`. Schema-sync diffs old/new fields →
`ALTER TABLE ADD/DROP/RENAME COLUMN`, recreates indexes + RLS policies in the same
tx. Large-table safety: `ADD COLUMN` nullable-first; `CREATE INDEX CONCURRENTLY`
outside the tx.

---

## 6. Multi-replica rules (now binding — the code enforces them)

- **Migrations**: whole run under `pg_advisory_lock(326326001)` on a dedicated
  connection; latecomers block, then see everything recorded. (Tested:
  `TestMigrateAndSeedConcurrent`.)
- **Seeds / one-time writes**: always `ON CONFLICT DO NOTHING` + count-guard.
- **Cron-like work** (log retention, webhook retries): `pg_try_advisory_lock` per
  task — winner runs, losers skip; never assume a single instance.
- **Realtime**: events flow through `LISTEN/NOTIFY`, so every replica sees writes
  regardless of which one handled the mutation.
- **Graceful shutdown**: `srv.Shutdown` is awaited (drain in-flight, incl. SSE/WS)
  before the pool closes.

---

## 7. Testing & release (as practiced from v0.1.1)

**Tests** live beside code; integration tests use `TEST_DATABASE_URL` and skip when
unset; CI + release provide `postgres:16-alpine` as a service. Current suite: config
contract, DSN redaction, middleware (Hijack/Flush passthrough, CORS incl. Vary and
credentials rules, client IP), health/ready shape + <3s + no-internals, SPA fallback,
migrate idempotency, **concurrent 2-replica boot**, seed never-overwrites,
collection create/update/delete lifecycle, identifier/field validation, default-deny
RLS policy creation, concurrent collection create conflicts, API-key generation,
record payload validation, rule/filter compiler checks, record CRUD, and direct-role
RLS integration tests proving app filters are not the data boundary. M3 was verified
against temporary PostgreSQL 16, 17, and 18 clusters. Every milestone adds: happy
path + auth-boundary + concurrency test for its feature. Security regressions
(RLS bypass, unauth control plane) get permanent tests.

**Release process** (per milestone): bump nothing in code (version is injected);
update `deploy/dublyo.template.yml` pin → commit → `git tag vX.Y.Z` → push tag →
release workflow test-gates → multi-arch image `:vX.Y.Z` + `:latest`. `/health`
reports the tag (ldflags). Nightly rebuilds `:main` only.

---

## 8. Roadmap (each milestone = deployable release + updated checklist)

### M0 — Foundation & deploy contract — DONE (v0.1.0, 2026-07-03)
Config contract · retrying connect · idempotent embedded migrations · admin seed ·
`/health` `/ready` · single process :8080 · <30MB image · compose + Dublyo template ·
CI + GHCR release.

### M0.5 — Audit remediation + tests — DONE (v0.1.1, 2026-07-03)
All 19 review findings fixed (advisory-lock migrations, redaction, awaited shutdown,
Hijacker passthrough, listen-before-migrate, test-gated releases, PGDATA pin,
version ldflags, health hardening, CORS/XFF, strict bools, SPA fallback, restart
policies, template admin seed) · first unit + integration test suite.

### M0.6 — Follow-up hardening checkpoint — DONE (v0.1.2, 2026-07-03)
Reserved API prefixes return JSON 404 instead of SPA HTML; config validates log
settings and paired admin seed credentials; quoted key/value DSN passwords are
redacted; the public README links to this repo-local roadmap; full suite passed
against a disposable PostgreSQL 16 cluster.

### M1 — Control plane & admin auth — DONE (v0.2.0, 2026-07-03)
- [x] Follow `docs/specs/m1-control-plane-admin-auth.md`
- [x] `_dbo` migrations: `projects`, `admin_sessions(hash)`, `api_keys(hash)`, `audit_log`
- [x] Admin login: bcrypt verify → opaque hashed session token (24h); `GET /admin/api/me`
- [x] **Auth middleware on every `/admin/api/*`** (401 without valid token) + audit log
- [x] `POST /setup` (self-closing, rate-limited) when no ENV admin
- [x] Projects create/list/get → `CREATE SCHEMA proj_<slug>` + 3 NOLOGIN roles + grants + revoke-public
- [x] Rate limiting (token bucket, in-process) on `/setup` + `/admin/api/auth/*`
- **Accept:** unauth `/admin/api/projects` → 401; login → create project → schema+roles
  exist in pg_catalog; second `/setup` → 410; audit rows written; all under tests.

### M2 — Collections & schema sync — DONE (v0.3.0, 2026-07-03)
- [x] Follow `docs/specs/m2-collections-schema-sync.md`
- [x] M2 field registry + validation; collections CRUD persisting to `_dbo.collections`
- [x] Schema-sync engine (add/rename/drop field DDL in tx; identifier validation + quoting everywhere)
- [x] RLS: enable+force on every materialized table; default-deny select/insert/update/delete policies
- **Accept:** create `posts` via API → table + policies visible in `pg_policies`;
  rename field → column renamed; forbidden identifiers rejected; unsafe DDL changes
  return `409 destructive_change`; tests cover PostgreSQL 16, 17, and 18.

### M3 — Records API + rules — DONE (v0.4.0, 2026-07-03)
- [x] Follow `docs/specs/m3-records-api-rules.md`
- [x] Records CRUD with pagination/sort/filter (M3-safe expression subset → parameterized SQL)
- [x] Per-request tx: `SET LOCAL ROLE` + `request.jwt.claims` + `search_path`
- [x] `403 rls_denied` mapping; list/view/create/update/delete rule enforcement
- **Accept:** anon vs authenticated vs service behave per rules **and** per RLS (test
  hits Postgres directly to prove policies fire without the app's WHERE); filter
  injection attempts (quoted idents, stacked queries) rejected by tests.

### M4 — App auth (email/password)  →  v0.5.0
- [ ] `users` auth collection per project (email unique, bcrypt, verified flag)
- [ ] Tokens: access JWT (1h, per-record `token_key` + `JWT_SECRET`) + refresh (7d,
      rotating, stored hashed in `_dbo.sessions`); logout-everywhere = rotate `token_key`
- [ ] signup/login/refresh/logout + reset/verify flows (email via M6 mailer; until
      then tokens logged at debug in dev)
- **Accept:** full lifecycle green; refresh rotation invalidates the old token;
  `token_key` rotation kills all sessions; bcrypt cost configurable.

### M5 — File storage  →  v0.6.0
- [ ] Streamed multipart upload → local FS layout `/data/storage/<project>/<collection>/<record>/<field>/`
      (S3 backend behind same interface); `MAX_UPLOAD_MB` (default 64)
- [ ] File field type wiring; delete cascade; protected files via short-lived file
      token; thumbnails (`?thumb=WxH`, cached)
- **Accept:** 50 MB upload+download streams (constant memory, verified); ownership
  UID 1001 on volume; protected file 401s without token; thumb correct.

### M6 — Email (SMTP)  →  v0.7.0
- [ ] Mailer interface: SMTP (mailyak) when `SMTP_HOST` set, else dev console logger
- [ ] Templates (verify, reset, email-change) using `APP_URL` links; per-project
      overrides in `_dbo`; SMTP creds encrypted at rest
- **Accept:** with MailHog in compose: signup → verification email → link verifies;
  reset flow end-to-end; no SMTP configured → flows degrade gracefully.

### M7 — OAuth2 + admin panel v1 (Svelte)  →  v0.8.0
- [ ] OAuth2 code flow (google, github, discord): `authorize` redirect built from
      `APP_URL`, callback exchanges + links to user (create-or-link by email);
      provider configs per project (encrypted secrets)
- [ ] Svelte SPA embedded: login, projects, collections editor, records table
      (CRUD + filters), users, settings, API keys — talking to the real APIs
- **Accept:** OAuth login round-trips through `APP_URL=https://…` (tip-13 checklist
  item); panel can do everything the API can for M1–M4 scope; bundle stays embedded
  (no runtime npm).

### M8 — Realtime + webhooks  →  v0.9.0
- [ ] `NOTIFY dbo_events, <json>` triggers on collection tables; one LISTEN conn
      per process; WS (+SSE fallback) endpoint with 30s heartbeat, 90s idle timeout,
      per-subscriber buffer with slow-client drop; subscribe respects list rules
- [ ] Outbound webhooks: `_dbo.hooks` (url, events, secret); fire on record events
      with HMAC signature; retries with backoff via `pg_try_advisory_lock` worker
- **Accept:** two replicas + one writer → both replicas' subscribers receive events;
  WS survives 5 min through the tunnel (heartbeats); webhook receives signed payload,
  retry fires on 500.

### M9 — Ops hardening → v1.0.0
- [ ] Backups: `pg_dump` trigger from panel + storage archive download; restore doc
- [ ] Log retention cron (advisory-lock guarded); request/audit log viewer in panel
- [ ] pgvector opt-in (`ENABLE_PGVECTOR` gates a `vector` field type; docs: DBA runs
      `CREATE EXTENSION vector` first)
- [ ] Docs site content (README quickstart, self-host guide, API reference from spec)
- [ ] Full tip-13 checklist run on a real Dublyo `cx22`
- **Accept:** §10 checklist 100% green on Dublyo; `v1.0.0` tagged; template pinned.

---

## 9. Working agreements for whoever continues this (human or AI)

1. **Never violate the 13 rules** (§0). When a feature conflicts, the rule wins.
2. **Spec drift is a bug**: implement §3 routes/params/envelopes exactly; change the
   spec first (in this file) if reality demands it, in the same commit.
3. **Every milestone**: code + tests + this file's checkboxes updated + template pin
   bumped + tag pushed + release green + `PROGRESS.md` entry (see prompt file).
4. **Tests before tag**: `go test ./...` green with `TEST_DATABASE_URL` set locally;
   CI must also be green before the tag is pushed.
5. **Security tests are permanent**: anything that ever guarded auth/RLS keeps its
   regression test forever.
6. **Additive env contract** only; document every new var in §2 + README + template.
7. **No new services** (Redis, queues, nginx) — Postgres and the binary do everything.
8. **Commit style**: imperative subject, body lists user-visible changes; one
   milestone may span several commits but lands as one tagged release.

---

## 10. v1.0 test checklist (tip 13)

- [x] Cold start → `/health` 200 in <30 s (verified vs postgres:16)
- [x] Fresh Postgres → migrations run → admin seeded from ENV
- [x] Missing/typo'd required ENV → exit 1 with clear message
- [x] Restart/second replica → no data loss, no re-init, no race (advisory-lock test)
- [x] `/ready` reports `migrating` during boot (listener starts pre-migration)
- [ ] Realtime WS open 5 min with heartbeat through the tunnel (M8)
- [ ] 50 MB upload without proxy timeout, constant memory (M5)
- [ ] OAuth callback with `APP_URL=https://foo.dublyo.xyz` redirects correctly (M7)
- [ ] `docker exec app ls -ln /data/storage` shows UID 1001 ownership (verify on deploy)
- [ ] Deploy the built image to Dublyo/Portainer stack and re-run this list (in progress)

---

## 11. Decisions log

- **2026-07-03** Module/repo `github.com/dublyo/dublyobase`; image `ghcr.io/dublyo/dublyobase`.
- **2026-07-03** External-Postgres single-container model (13 rules) supersedes the
  bundled-supervisor design; `pgsuper` parked at commit `98a18de`, not planned for v1.
- **2026-07-03** Admin UI framework: **Svelte** (PocketBase lineage, smallest bundles).
- **2026-07-03** PG version choice = Dublyo template option (db image tag), with
  `PGDATA` pinned for postgres:18 volume-layout compatibility.
- **2026-07-03** Webhooks over embedded JS for v1 extensibility.
