---
name: dublyobase-backend-builder
description: Build complete Dublyobase backends through its remote HTTP MCP and REST APIs. Use when an AI agent needs to connect to a live Dublyobase instance, create projects, design Postgres collections and fields, configure rules, seed records, create users, upload files, configure SMTP/storage, cron jobs, backups, schema import, and verify a PocketBase/Supabase-style backend.
---

# Dublyobase Backend Builder

Use this skill to turn an app brief into a working Dublyobase backend. Dublyobase is a Postgres-backed PocketBase/Supabase-style backend with projects, collections, records CRUD, app auth, files, SMTP, cron, backups, schema import, realtime SSE, and remote HTTP MCP.

## Inputs

Require these before changing a live instance:

- `DUBLYOBASE_URL`: base URL, for example `https://example.com`.
- `DUBLYOBASE_MCP_TOKEN`: bearer token for `POST /mcp`.
- Target project slug, or permission to create one.
- App brief: users, workflows, data, permissions, files, automation, email, and sample records.

If the URL or token is missing, ask for it. Do not ask extra questions when the app brief is enough to make a conservative schema.

Never print full tokens, API keys, SMTP passwords, S3 secrets, or database URLs in the final answer. Mask them.

## Connection

Dublyobase MCP is JSON-RPC over HTTP:

```http
POST {DUBLYOBASE_URL}/mcp
Authorization: Bearer {DUBLYOBASE_MCP_TOKEN}
Content-Type: application/json
```

Initialize and inspect available tools before acting:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ai-agent","version":"1"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

Call tools like this:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"collections.list","arguments":{"projectSlug":"app"}}}
```

MCP success results return text content containing JSON. Parse `result.content[0].text` as JSON when possible. Tool errors may also be returned as `result.content[0].isError=true`; inspect both JSON-RPC errors and tool result errors.

## Token Scopes

Admin MCP tokens can use all default tools:

- `projects.list`, `projects.create`
- `collections.list`, `collections.create`, `collections.update`
- `schema.discover`, `schema.import`
- `records.list`, `records.create`, `records.update`, `records.delete`
- `files.upload_base64`
- `users.create`
- `settings.smtp.update`, `settings.storage.update`, `settings.storage.test`
- `cron.list`, `cron.create`, `cron.run`
- `backups.list`, `backups.create`, `backups.run`

Project MCP tokens are scoped to one project and do not manage projects or instance SMTP/storage settings. For project tokens, omit `projectSlug` when allowed or pass the token project slug.

## Build Workflow

1. Check health:
   - `GET {DUBLYOBASE_URL}/health`
   - expect `status=ok`, `db=ok`, `storage=ok`.
2. Initialize MCP and call `tools/list`.
3. Find or create the project:
   - Admin token: `projects.list`, then `projects.create` if needed.
   - Project token: use its scoped project.
4. Model the backend before writing:
   - Identify auth users, tenant ownership, public vs private data, files, statuses, lookups, and automations.
   - Create referenced collections before relation fields that point to them.
   - Prefer normalized tables for relations; use `json` only for flexible payloads.
   - Mark display fields as `presentable:true`; mark selected text/email/url/select/number/date/relation fields as `searchable:true`.
   - Choose collection icons with `options.icon`.
5. Create or update collections.
6. Seed minimal records and at least one app user.
7. Configure storage/SMTP only when the user provided credentials and allowed instance-level changes.
8. Add cron jobs and backups if requested.
9. Verify all workflows through MCP and public REST APIs.
10. Return a concise backend summary: project slug, collections, key fields, auth model, file fields, rules, API examples, and test evidence.

## Collection Design

Create a collection:

```json
{
  "projectSlug": "app",
  "name": "posts",
  "type": "base",
  "options": {"icon": {"type": "lucide", "name": "file-text"}},
  "fields": [
    {"name": "title", "type": "text", "required": true, "presentable": true, "searchable": true},
    {"name": "body", "type": "editor", "options": {"maxSize": 200000}},
    {"name": "published", "type": "bool"},
    {"name": "author", "type": "relation", "required": true, "options": {"collection": "users"}},
    {"name": "cover", "type": "file", "options": {"multiple": false, "maxSize": 5242880, "mimeTypes": ["image/png", "image/jpeg"]}},
    {"name": "created_auto", "type": "autodate", "options": {"onCreate": true}},
    {"name": "updated_auto", "type": "autodate", "options": {"onCreate": true, "onUpdate": true}}
  ],
  "listRule": "published = true",
  "viewRule": "published = true || author = @request.auth.id",
  "createRule": "@request.auth.role = \"authenticated\"",
  "updateRule": "author = @request.auth.id",
  "deleteRule": "author = @request.auth.id"
}
```

Collection types:

- `base`: normal table-backed collection.
- `auth`: app-user collection.
- `view`: listed as a collection type, but realtime and some table operations are not for view collections.

Collection icon options:

- Lucide: `{"icon":{"type":"lucide","name":"table"}}`
- Emoji: `{"icon":{"type":"emoji","value":"📦"}}`
- Useful Lucide names: `table`, `shield`, `eye`, `book-open`, `boxes`, `users`, `user`, `package`, `shopping-cart`, `file-text`, `folder`, `message-square`, `globe`, `tag`, `star`, `bell`, `credit-card`, `briefcase`, `database`.

Rules:

- Omitted rule or JSON `null`: admin/service only.
- Empty string `""`: public.
- Expression: rule compiled to Postgres RLS.
- Supported request references: `@request.auth.id`, `@request.auth.role`, `@request.auth.collection`.
- Typical roles: `anon`, `authenticated`, `service`.
- Use owner fields for per-user data: `owner = @request.auth.id`.
- Use public read: `published = true`.
- Use private owner read: `owner = @request.auth.id`.
- Use mixed read: `published = true || owner = @request.auth.id`.

## Field Reference

Field shape:

```json
{"name":"field_name","type":"text","required":false,"hidden":false,"presentable":false,"searchable":false,"help":"optional","options":{}}
```

Supported types and common options:

- `text`: string. Options: `min`, `max`, `pattern`.
- `editor`: rich text/HTML string. Options: `maxSize`.
- `password`: bcrypt-hashed string, never returned. Options: `min`, `max`, `cost`.
- `number`: double precision. Options: `onlyInt`, `min`, `max`.
- `bool`: boolean, defaults to false unless required.
- `date`: timestamp.
- `autodate`: server-managed timestamp. Options: `onCreate`, `onUpdate`.
- `email`: email string. Options: `onlyDomains`, `exceptDomains`.
- `url`: URL string. Options: `min`, `max`, `pattern`.
- `select`: text or text array. Required option: `values`. Options: `minSelect`, `maxSelect`; `maxSelect > 1`, `multi`, or `multiple` makes it multiple.
- `json`: JSONB object. Options: `maxSize`.
- `relation`: UUID or UUID array to another collection. Required option: `collection`. Options: `minSelect`, `maxSelect`, `displayField`, `reverseName`, `unique`.
  - Many-to-one: single relation, for example `order.customer -> users`.
  - One-to-many: model the many side with a single relation and set `reverseName`, for example `order_items.order -> orders`.
  - Many-to-many: use a multiple relation with `maxSelect > 1` when a simple UUID array is enough, or create a join collection when you need extra fields on the relationship.
  - Strict one-to-one: single relation with `unique:true`, for example `customer_profiles.customer -> users`.
- `file`: JSONB metadata. Options: `multiple`, `minSelect`, `maxSelect`, `maxSize`, `mimeTypes`.

Naming rules:

- Use lower snake_case names.
- Do not create fields named `id`, `created`, or `updated`; Dublyobase manages standard IDs/timestamps for managed tables.
- Create relation target collections first.

## Existing Postgres Tables

Use schema discovery when the user wants Dublyobase to manage or expose existing Postgres tables:

```json
{"name":"schema.discover","arguments":{"projectSlug":"app","schema":"public"}}
```

Import safely:

1. Run `schema.discover`.
2. Import only tables with one usable primary key.
3. First use dry run:
   ```json
   {"projectSlug":"app","dryRun":true,"items":[{"schema":"public","table":"customers","name":"customers"}]}
   ```
4. Apply with `dryRun:false`.

Imported collections can be used for admin CRUD when a usable primary key exists. Field/schema management should stay conservative unless the collection is managed by Dublyobase or the user explicitly asks for a migration.

## Records, Files, and Users

Create a record:

```json
{"name":"records.create","arguments":{"projectSlug":"app","collection":"posts","data":{"title":"Hello","published":true}}}
```

List records:

```json
{"name":"records.list","arguments":{"projectSlug":"app","collection":"posts","page":1,"perPage":25,"sort":"-created","search":"hello","fields":"id,title,created"}}
```

Filter examples:

- PocketBase-style: `title = "Hello"`
- Directus-style JSON: `{"title":{"_icontains":"hello"}}`
- Combined: `{"_or":[{"title":{"_icontains":"hello"}},{"status":{"_eq":"live"}}]}`

Create an app auth user:

```json
{"name":"users.create","arguments":{"projectSlug":"app","email":"user@example.com","password":"change-me-strong"}}
```

Upload a file to a file field:

```json
{"name":"files.upload_base64","arguments":{"projectSlug":"app","collection":"posts","recordId":"record-id","field":"cover","filename":"cover.png","dataBase64":"...","mode":"replace"}}
```

Use `mode:"append"` only for multi-file fields.

## Settings, Cron, and Backups

Update SMTP only with admin scope and explicit credentials:

```json
{"name":"settings.smtp.update","arguments":{"enabled":true,"host":"smtp.example.com","port":"587","username":"user","password":"secret","from":"support@example.com"}}
```

Update storage:

```json
{"name":"settings.storage.update","arguments":{"type":"s3","s3":{"endpoint":"https://account.r2.cloudflarestorage.com","bucket":"app-files","region":"auto","accessKey":"...","secretKey":"...","useSSL":true,"forcePathStyle":true}}}
```

Always run `settings.storage.test` after changing storage.

Create cron jobs for app automation:

```json
{"name":"cron.create","arguments":{"projectSlug":"app","name":"daily sync","type":"http","schedule":"0 2 * * *","timezone":"UTC","enabled":true,"method":"POST","url":"https://example.com/api/sync","headers":{"Authorization":"Bearer ..."},"timeoutSeconds":30,"retryCount":2}}
```

Create project backups:

```json
{"name":"backups.create","arguments":{"name":"daily project backup","scope":"project","projectSlug":"app","schedule":"0 3 * * *","timezone":"UTC","enabled":true,"retentionDays":14,"retentionCount":14}}
```

Run `backups.run` once after creating an important backup job.

## REST and Realtime Verification

After MCP writes, verify the public backend API:

```http
GET /api/projects/{slug}/collections/{collection}/records?page=1&perPage=25
POST /api/projects/{slug}/collections/{collection}/records
PATCH /api/projects/{slug}/collections/{collection}/records/{id}
DELETE /api/projects/{slug}/collections/{collection}/records/{id}
POST /api/projects/{slug}/auth/signup
POST /api/projects/{slug}/auth/login
```

Realtime SSE:

```http
GET /api/projects/{slug}/realtime?collection=posts&events=create,update
Authorization: Bearer {api-key-or-app-token}
```

Browser `EventSource` cannot set headers; use `token` or `access_token` query only when the URL will not be logged. Realtime fanout is currently in-process; use one Dublyobase app replica for realtime-sensitive projects until database-backed fanout exists. Delete events include payloads only for service subscribers.

## Safety Rules

- Do not use admin-scope MCP tokens when a project-scoped token is enough.
- Do not change SMTP/storage unless explicitly requested.
- Do not delete collections, delete records, drop fields, or run destructive imports without explicit approval.
- Use `dropMissingFields:false` unless the user explicitly wants removed fields dropped.
- Dry-run schema imports first.
- Seed only harmless sample data unless the user supplied real data.
- Keep `AUTH_DEV_TOKENS=false` for production verification.
- Redact secrets in summaries and logs.

## Completion Checklist

Before reporting done:

- Health is OK.
- MCP `tools/list` worked and the needed tools were allowed.
- Project exists.
- Collections exist with expected field types, relations, icons, rules, searchable and presentable fields.
- At least one record create/list/update path was verified for critical collections.
- Auth user creation and login were tested if the app uses auth.
- File upload/download path was tested if file fields exist.
- Storage test passed if storage settings changed.
- SMTP test passed if SMTP settings changed.
- Cron run or backup run was tested if jobs were created.
- Realtime create/update was tested if realtime is part of the app.
- Final answer includes project slug, API examples, created resources, tests performed, and remaining boundaries.
