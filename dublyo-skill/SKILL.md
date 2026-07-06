---
name: dublyobase-backend-builder
description: Build complete Dublyobase backends through remote HTTP MCP, admin REST APIs, and public project APIs. Use when an AI agent needs to connect to a live Dublyobase instance, create projects, design Postgres collections and fields, configure rules, seed records, create app users, upload files, configure SMTP/storage/CORS/auth settings, cron jobs, backups/restores, webhooks, quotas, orgs, invitations, SaaS or ecommerce schemas, schema import, and verify a PocketBase/Supabase-style backend.
---

# Dublyobase Backend Builder

Use this skill to turn an app brief into a working Dublyobase backend. Dublyobase is a Postgres-backed PocketBase/Supabase-style backend with projects, collections, records CRUD, app auth, sessions, files, SMTP, S3-compatible storage, cron jobs, backups, schema import, realtime SSE, webhooks, request logs, quotas, app organizations, invitations, and remote HTTP MCP.

This skill is written for AI agents. It favors direct, verified backend creation over long planning. Keep changes conservative, verify every critical workflow, and never expose secrets.

## Inputs

Require these before changing a live instance:

- `DUBLYOBASE_URL`: base URL, for example `https://example.com`.
- Either `DUBLYOBASE_MCP_TOKEN` for `POST /mcp`, or an admin bearer token for `/admin/api/*`.
- Target project slug, or permission to create one.
- App brief: users, workflows, tenants, roles, data, permissions, files, automations, email needs, frontend origins, and sample records.

If URL or credentials are missing, ask for them. If the app brief is enough to infer a conservative schema, do not ask extra questions.

Never print full tokens, API keys, SMTP passwords, S3 secrets, database URLs, restore files, or private webhook secrets in the final answer. Mask them.

## Operating Rules

- Inspect capabilities first: `GET /health`, `GET /ready`, MCP `initialize`, and MCP `tools/list`.
- Treat `tools/list` as the source of truth for MCP. Some product features may only be available through admin REST.
- Prefer project-scoped MCP tokens for app-specific schema/data work.
- Use admin MCP or admin REST only for instance-level settings: SMTP, storage, admin CORS, full backups/restores, MCP tokens, and super admins.
- Make destructive operations opt-in: collection deletion, field drops, destructive schema import, restore mode, global CORS wildcard, and full-instance backup changes.
- Use dry runs where available before schema import or restore.
- Preserve existing user data. Prefer additive schema changes unless the user asked for migration or cleanup.
- Verify with real REST requests after MCP writes. A schema that exists but cannot be used from the public API is not complete.
- Final reports must include project slug, created resources, API examples, verification evidence, and remaining boundaries.

## Dublyobase Surfaces

Admin UI:

```http
GET {DUBLYOBASE_URL}/_/
```

Admin REST:

```http
Authorization: Bearer {admin_token}
Content-Type: application/json
```

Remote HTTP MCP:

```http
POST {DUBLYOBASE_URL}/mcp
Authorization: Bearer {DUBLYOBASE_MCP_TOKEN}
Content-Type: application/json
```

Public project API:

```http
GET /api/projects/{slug}/collections/{collection}/records
Authorization: Bearer {app_access_token_or_api_key}
```

Health:

```http
GET /health
GET /ready
```

## MCP Connection

Initialize:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"ai-agent","version":"1"}}}
```

List tools:

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

Call a tool:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"collections.list","arguments":{"projectSlug":"app"}}}
```

MCP success results return text content containing JSON. Parse `result.content[0].text` as JSON when possible. Tool errors can appear as JSON-RPC errors or as tool results with `isError=true`; inspect both.

## MCP Tools

Default admin MCP tools:

- `projects.list`, `projects.create`
- `collections.list`, `collections.create`, `collections.update`
- `schema.discover`, `schema.import`
- `records.list`, `records.create`, `records.update`, `records.delete`
- `files.upload_base64`
- `users.create`
- `settings.smtp.update`, `settings.storage.update`, `settings.storage.test`
- `cron.list`, `cron.create`, `cron.run`
- `backups.list`, `backups.create`, `backups.run`

Default project MCP tools:

- `collections.list`, `collections.create`, `collections.update`
- `schema.discover`, `schema.import`
- `records.list`, `records.create`, `records.update`, `records.delete`
- `files.upload_base64`
- `users.create`
- `backups.list`, `backups.create`, `backups.run`

Admin REST is still needed for:

- API keys
- Webhooks and webhook deliveries
- CORS settings
- Auth settings and email templates
- Project quotas and metrics
- Backup downloads and restore upload
- Request logs and audit logs
- Owner/super-admin management

## Build Workflow

1. Check health:
   - `GET {DUBLYOBASE_URL}/health`
   - `GET {DUBLYOBASE_URL}/ready`
   - Expect database and storage readiness before writes.
2. Initialize MCP and call `tools/list`.
3. Find or create the project:
   - Admin MCP: `projects.list`, then `projects.create` if needed.
   - Project MCP: use the scoped project.
   - Admin REST: `GET /admin/api/projects`, `POST /admin/api/projects`.
4. Model the backend:
   - Identify app users, organizations, roles, ownership fields, public content, private tenant data, files, statuses, automation, webhooks, and frontend origins.
   - Create referenced collections before relation fields.
   - Prefer normalized tables for relations. Use `json` only for flexible metadata or provider payloads.
   - Mark display fields as `presentable:true`.
   - Mark only useful text/email/url/select/number/date/relation fields as `searchable:true`.
   - Choose collection icons with `options.icon`.
5. Create collections and rules.
6. Seed minimal records and at least one app user when the app uses auth.
7. Configure auth settings, SMTP, storage, CORS, quotas, API keys, webhooks, cron, and backups only when requested or needed by the brief.
8. Verify public REST flows, auth flows, file upload/download, webhooks, cron, backups, and realtime where relevant.
9. Return a concise build summary with examples and proof.

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
    {"name": "author", "type": "relation", "required": true, "options": {"collection": "users", "displayField": "email", "reverseName": "posts"}},
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
- `view`: metadata for database views; avoid writes and realtime assumptions.

Collection icon options:

```json
{"icon":{"type":"lucide","name":"table"}}
{"icon":{"type":"emoji","value":"<real emoji character>"}}
```

Prefer Lucide names for portable agent output. Use the emoji fallback only when the caller can provide a real emoji character.

Useful Lucide names:

- SaaS: `building-2`, `users`, `user-round`, `shield`, `key-round`, `credit-card`, `chart-no-axes-column`, `bell`, `briefcase`, `settings`
- Ecommerce: `shopping-cart`, `package`, `boxes`, `tag`, `receipt`, `truck`, `store`, `badge-percent`, `star`, `warehouse`
- Content: `file-text`, `book-open`, `message-square`, `image`, `folder`, `globe`, `calendar`, `mail`

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
- `bool`: boolean.
- `date`: timestamp.
- `autodate`: server-managed timestamp. Options: `onCreate`, `onUpdate`.
- `email`: email string. Options: `onlyDomains`, `exceptDomains`.
- `url`: URL string. Options: `min`, `max`, `pattern`.
- `select`: text or text array. Required option: `values`. Options: `minSelect`, `maxSelect`; `maxSelect > 1`, `multi`, or `multiple` makes it multiple.
- `json`: JSONB object. Options: `maxSize`.
- `relation`: UUID or UUID array to another collection. Required option: `collection`. Options: `minSelect`, `maxSelect`, `displayField`, `reverseName`, `unique`, `onDelete`.
- `file`: JSONB metadata. Options: `multiple`, `minSelect`, `maxSelect`, `maxSize`, `mimeTypes`.

Naming rules:

- Use lower snake_case names.
- Do not create fields named `id`, `created`, or `updated` for managed Dublyobase tables.
- Create relation targets before relation fields.
- Use explicit names for tenant fields: `organization`, `workspace`, `owner`, `customer`, `created_by`.
- Use status select values instead of free text for workflows.

## Relations

Many-to-one:

```json
{"name":"customer","type":"relation","required":true,"options":{"collection":"users","displayField":"email","reverseName":"orders"}}
```

One-to-many:

- Store the relation on the many side.
- Example: `order_items.order -> orders`, with `reverseName:"items"`.

Strict one-to-one:

```json
{"name":"customer","type":"relation","required":true,"options":{"collection":"users","displayField":"email","unique":true,"reverseName":"profile"}}
```

Many-to-many:

- Use a multiple relation when it is simple tagging or categorization:
  ```json
  {"name":"tags","type":"relation","options":{"collection":"tags","maxSelect":20,"displayField":"name","reverseName":"products"}}
  ```
- Use a join collection when the relationship has fields:
  - `product_categories`: `product`, `category`, `position`
  - `organization_memberships`: `organization`, `user`, `role`, `status`
  - `subscription_items`: `subscription`, `price`, `quantity`

For billing, permissions, invitations, product variants, inventory, and order items, prefer join collections. They scale better and are easier to audit.

## Rules

Rule values:

- Omitted rule or JSON `null`: admin/service only.
- Empty string `""`: public.
- Expression: compiled to Postgres RLS.

Supported request references:

- `@request.auth.id`
- `@request.auth.role`
- `@request.auth.collection`

Typical roles:

- `anon`
- `authenticated`
- `service`

Common rule patterns:

```text
published = true
owner = @request.auth.id
published = true || owner = @request.auth.id
created_by = @request.auth.id
```

Only use org-scoped rules if the target auth context actually exposes the needed org claim or your frontend/API layer enforces organization selection. Otherwise model org membership in collections and use service-side endpoints for sensitive multi-tenant actions.

Security defaults:

- Private user data: list/view/update/delete should require ownership or organization membership.
- Public catalog data: list/view can be public; create/update/delete should be service/admin only.
- Orders, payments, invoices, licenses, API keys, audit trails: service/admin only unless there is a clear user-owned read rule.
- Webhook logs, cron logs, backups: admin only.

## Existing Postgres Tables

Use schema discovery when the user wants Dublyobase to manage or expose existing Postgres tables:

```json
{"name":"schema.discover","arguments":{"projectSlug":"app","schema":"public"}}
```

Import safely:

1. Run `schema.discover`.
2. Import only tables with one usable primary key for CRUD.
3. First use dry run:
   ```json
   {"projectSlug":"app","dryRun":true,"items":[{"schema":"public","table":"customers","name":"customers"}]}
   ```
4. Apply with `dryRun:false` only after reviewing the preview.

Compatibility guidance:

- Read-only discovery can include broad non-system schemas.
- CRUD import requires a usable primary key.
- Field/schema management should stay conservative unless the table is managed by Dublyobase or the user approves a migration.
- For full Dublyobase-managed editing, prefer standard `id uuid`, `created`, and `updated` columns or run an explicit migration.

## Records

Create:

```json
{"name":"records.create","arguments":{"projectSlug":"app","collection":"posts","data":{"title":"Hello","published":true}}}
```

List:

```json
{"name":"records.list","arguments":{"projectSlug":"app","collection":"posts","page":1,"perPage":25,"sort":"-created","search":"hello","fields":"id,title,created"}}
```

Update:

```json
{"name":"records.update","arguments":{"projectSlug":"app","collection":"posts","id":"record-id","data":{"published":true}}}
```

Delete:

```json
{"name":"records.delete","arguments":{"projectSlug":"app","collection":"posts","id":"record-id"}}
```

Pagination:

- Use `page` and `perPage`.
- UI page sizes commonly include `10`, `25`, `100`, `250`, and `500`.
- For big datasets, prefer narrow `fields`, indexed filters, and stable sort keys.

Filters:

- PocketBase-style:
  ```text
  title = "Hello"
  status = "paid" && total > 100
  ```
- Directus-style JSON:
  ```json
  {"title":{"_icontains":"hello"}}
  ```
- Combined:
  ```json
  {"_or":[{"title":{"_icontains":"hello"}},{"status":{"_eq":"live"}}]}
  ```

Use searchable fields deliberately. Do not mark every text field searchable in large schemas.

## Files

MCP upload:

```json
{"name":"files.upload_base64","arguments":{"projectSlug":"app","collection":"products","recordId":"record-id","field":"image","filename":"image.png","dataBase64":"...","mode":"replace"}}
```

Use `mode:"append"` only for multi-file fields.

Public multipart upload:

```http
POST /api/projects/{slug}/files/{collection}/{recordId}/{field}?mode=replace
Authorization: Bearer {token}
Content-Type: multipart/form-data
```

Resumable upload:

```http
POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/uploads
PUT /api/projects/{slug}/files/uploads/{uploadId}/chunks/{index}
POST /api/projects/{slug}/files/uploads/{uploadId}/complete
DELETE /api/projects/{slug}/files/uploads/{uploadId}
```

Protected downloads:

```http
POST /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/token
GET /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}?token={file_token}
```

File field guidance:

- Product image: single file, images only, max 5-10 MB.
- Product gallery: multiple files, maxSelect 10-20.
- User avatar: single file, images only, low max size.
- Documents: restrict mime types, keep private rules.
- Large uploads: use resumable upload, not base64 MCP.

## App Auth

Create an app auth user with MCP:

```json
{"name":"users.create","arguments":{"projectSlug":"app","email":"user@example.com","password":"change-me-strong"}}
```

Public app auth endpoints:

```http
POST /api/projects/{slug}/auth/signup
POST /api/projects/{slug}/auth/login
POST /api/projects/{slug}/auth/request-otp
POST /api/projects/{slug}/auth/login-otp
POST /api/projects/{slug}/auth/refresh
POST /api/projects/{slug}/auth/logout
POST /api/projects/{slug}/auth/logout-all
GET  /api/projects/{slug}/auth/sessions
DELETE /api/projects/{slug}/auth/sessions/{sessionId}
GET  /api/projects/{slug}/auth/me
POST /api/projects/{slug}/auth/request-email-change
POST /api/projects/{slug}/auth/confirm-email-change
POST /api/projects/{slug}/auth/request-verification
POST /api/projects/{slug}/auth/confirm-verification
POST /api/projects/{slug}/auth/request-password-reset
POST /api/projects/{slug}/auth/confirm-password-reset
```

Hosted action pages:

```http
GET  /auth/verify
POST /auth/verify
GET  /auth/reset-password
POST /auth/reset-password
GET  /auth/email-change
POST /auth/email-change
```

Auth settings via admin REST:

```http
GET /admin/api/projects/{slug}/auth-settings
PUT /admin/api/projects/{slug}/auth-settings
```

Use auth settings for:

- Access token minutes.
- Refresh token days.
- Verify token hours.
- Reset token hours.
- OTP enabled and OTP token minutes.
- Email change enabled and password requirement.
- Email templates for verification, reset, email change, OTP, and invitations.
- OAuth provider skeletons if present in current instance.

Recommended auth UX:

- Signup creates user and sends verification email when SMTP is configured.
- Login returns access and refresh tokens.
- Refresh rotates sessions.
- Logout revokes one session.
- Logout-all revokes all user sessions.
- Sessions UI lets users see and revoke devices.
- Email change requires current password, then email confirmation.
- Password reset uses one-time email token.
- OTP is optional passwordless login, not full MFA unless the app adds a second factor requirement.

## App Organizations And Invitations

Public app organization endpoints:

```http
GET  /api/projects/{slug}/orgs
POST /api/projects/{slug}/orgs
GET  /api/projects/{slug}/orgs/{orgId}/members
POST /api/projects/{slug}/orgs/{orgId}/invitations
POST /api/projects/{slug}/org-invitations/accept
```

Create org:

```json
{"name":"Acme Inc","slug":"acme","metadata":{"plan":"pro"}}
```

Create invitation:

```json
{"email":"member@example.com","role":"admin"}
```

Accept invitation:

```json
{"token":"invitation-token"}
```

Recommended SaaS primitives:

- Built-in app orgs: create through `/api/projects/{slug}/orgs` for login-time organization membership and invitations.
- Built-in app org members: query through `/api/projects/{slug}/orgs/{orgId}/members`.
- Built-in app org invitations: create and accept through the org invitation endpoints.
- Normal collection relations need normal collection metadata. If the app needs collection-level relations to tenants, create an `organizations` collection or import the built-in org table as a collection after discovery.
- Billing collections should relate to organization/account, not only to a user.
- App API keys should relate to organization/account and creator.
- Use roles like `owner`, `admin`, `member`, `viewer`, `billing`.

## API Keys

Admin REST:

```http
GET    /admin/api/projects/{slug}/api-keys
POST   /admin/api/projects/{slug}/api-keys
DELETE /admin/api/projects/{slug}/api-keys/{id}
```

Use API keys for frontend server-side code, trusted workers, webhooks, migrations, and seed scripts. Do not expose service keys in browsers.

Recommended key types:

- Anon key: public browser reads only when rules allow it.
- Service key: server-only automation and privileged writes.
- Project-scoped MCP token: AI/backend builder operations.

## SMTP And Email

MCP update:

```json
{"name":"settings.smtp.update","arguments":{"enabled":true,"host":"smtp.example.com","port":"587","username":"user","password":"secret","from":"support@example.com"}}
```

Admin REST:

```http
PUT  /admin/api/settings/smtp
POST /admin/api/settings/smtp/test
```

Safety:

- Only update SMTP with explicit credentials.
- Send a test email after changing SMTP.
- Do not print SMTP passwords.
- Use the project/app name in auth templates.
- For production, make sure SPF/DKIM/DMARC are configured outside Dublyobase.

## Storage

MCP update:

```json
{"name":"settings.storage.update","arguments":{"type":"s3","s3":{"endpoint":"https://account.r2.cloudflarestorage.com","bucket":"app-files","region":"auto","accessKey":"...","secretKey":"...","prefix":"uploads","useSSL":true,"forcePathStyle":true}}}
```

Admin REST:

```http
PUT  /admin/api/settings/storage
POST /admin/api/settings/storage/test
```

Storage types:

- `local`: simple single-server storage.
- `s3`: S3-compatible storage such as Cloudflare R2, Backblaze B2, MinIO, or AWS S3.

Always run `settings.storage.test` or `POST /admin/api/settings/storage/test` after changing storage.

## CORS

Admin CORS:

```http
PUT /admin/api/settings/cors
```

Project public API CORS:

```http
PUT /admin/api/projects/{slug}/cors
```

Recommended defaults:

- Admin CORS: lock to `APP_URL` or the trusted admin domain.
- Public project CORS: lock to frontend domains for that project.
- Avoid `*` for authenticated browser apps.
- Allow wildcard only for intentionally public APIs with no credentials and no sensitive data.

Example:

```json
{"publicOrigins":["https://app.example.com","https://www.example.com"],"allowWildcard":false}
```

## Cron Jobs

MCP:

```json
{"name":"cron.create","arguments":{"projectSlug":"app","name":"daily sync","type":"http","schedule":"0 2 * * *","timezone":"UTC","enabled":true,"method":"POST","url":"https://example.com/api/sync","headers":{"Authorization":"Bearer ..."},"body":"{}","timeoutSeconds":30,"retryCount":2}}
```

Admin REST:

```http
GET  /admin/api/cron-jobs
POST /admin/api/cron-jobs
GET  /admin/api/cron-jobs/{id}/runs
POST /admin/api/cron-jobs/{id}/run
```

Guidance:

- Use `http` cron jobs for external automation.
- Include auth headers when calling private endpoints.
- Keep timeouts short and retries bounded.
- Run a job once manually after creation.
- Review run logs for status code, response, and error.

## Backups And Restore

MCP:

```json
{"name":"backups.create","arguments":{"name":"daily project backup","scope":"project","projectSlug":"app","schedule":"0 3 * * *","timezone":"UTC","enabled":true,"retentionDays":14,"retentionCount":14}}
```

Admin REST:

```http
GET  /admin/api/backups
POST /admin/api/backups
GET  /admin/api/backups/{id}/runs
POST /admin/api/backups/{id}/run
GET  /admin/api/backups/{id}/runs/{runId}/download
POST /admin/api/restores
```

Backup scope:

- `project`: preferred for app-specific backups.
- `full`: admin-only full instance backup.

Restore flow:

1. Download or choose a backup file.
2. Upload with `mode=dry_run`.
3. Inspect the restore job result and warnings.
4. Upload again with `mode=restore` only with explicit approval and required confirmation.

Safety:

- Do not run full restore automatically.
- Do not restore into a production database without explicit approval.
- Prefer project-scoped backup jobs for SaaS/ecommerce apps.
- Storage must be configured and tested before scheduled backups.

## Webhooks

Admin REST:

```http
GET    /admin/api/projects/{slug}/webhooks
POST   /admin/api/projects/{slug}/webhooks
DELETE /admin/api/projects/{slug}/webhooks/{id}
GET    /admin/api/projects/{slug}/webhooks/{id}/deliveries
```

Create:

```json
{
  "name": "Order events",
  "url": "https://example.com/webhooks/dublyobase",
  "events": ["orders.create", "orders.update"],
  "enabled": true,
  "secret": "generated-secret"
}
```

Event examples:

- `records.create`, `records.update`, `records.delete`
- `{collection}.create`, `{collection}.update`, `{collection}.delete`
- `orders.create`
- `users.create`

Delivery headers:

- `X-Dublyobase-Event`
- `X-Dublyobase-Delivery`
- `X-Dublyobase-Signature`

Verify:

- Create a test record that matches the event.
- Check deliveries endpoint for `success`, attempts, status code, response, and error.
- If possible, verify the receiver saw the request.

Security:

- Require HTTPS for production webhook targets.
- Generate strong secrets and verify signatures on the receiver.
- Avoid sending sensitive fields to untrusted targets.
- Treat webhook.site and similar tools as temporary smoke-test receivers.

## Realtime

Public SSE:

```http
GET /api/projects/{slug}/realtime?collection=posts&events=create,update
Authorization: Bearer {api-key-or-app-token}
```

Browser `EventSource` cannot set headers. Use `token` or `access_token` query only when the URL will not be logged.

Current boundary:

- Realtime fanout is in-process.
- Use one Dublyobase app replica for realtime-sensitive projects until database-backed or external fanout is configured.
- Delete events include payloads only for service subscribers.

## Logs, Audit, Metrics, And Quotas

Admin REST:

```http
GET /admin/api/audit-log?project={slug}&page=1&perPage=25
GET /admin/api/request-logs?project={slug}&page=1&perPage=25
PUT /admin/api/settings/logs
GET /admin/api/projects/{slug}/quotas
PUT /admin/api/projects/{slug}/quotas
GET /admin/api/projects/{slug}/metrics?hours=24
```

Quota settings:

```json
{
  "enabled": true,
  "requestsPerMinute": 600,
  "authRequestsPerMinute": 60,
  "maxAppUsers": 100000,
  "maxStorageMB": 102400
}
```

Use quotas for:

- Auth abuse protection.
- Public API abuse limits.
- Per-project user caps.
- Storage caps.

Use metrics for:

- App users.
- Active sessions.
- Organizations.
- Storage usage.
- Request totals, errors, average duration, and p95 duration.

## Admins

Admin REST:

```http
GET  /admin/api/admins
POST /admin/api/admins
PATCH /admin/api/auth/email
POST /admin/api/auth/change-password
```

Rules:

- First admin is owner.
- Owner can create super admins with temporary passwords.
- New super admins must change password before normal admin access.
- Super admins can manage the app and change their own email/password.
- Super admins must not remove or demote the owner.

## SaaS Schema Pattern

Use this pattern for B2B SaaS, team apps, dashboards, and developer platforms.

Collections:

- `users` auth collection: built-in app users.
- Built-in app orgs: create through `/api/projects/{slug}/orgs`.
- `organizations`: optional normal collection mirror for relation fields, billing records, and admin CRUD.
- `projects`: tenant-owned workspaces/resources.
- `roles` or `permission_sets`: optional role catalog.
- `invitations`: if app needs custom invite metadata beyond built-in invitations.
- `api_keys`: app-facing keys, scoped to organization/project.
- `subscriptions`: billing plan, status, current period, provider IDs.
- `invoices`: billing documents and payment state.
- `usage_events`: metered usage records.
- `audit_events`: tenant-visible app audit.
- `notifications`: user/org notifications.
- `webhook_endpoints`: customer-defined outbound webhooks if the SaaS exposes them.

Example collection fields:

```json
{
  "name": "projects",
  "type": "base",
  "options": {"icon": {"type": "lucide", "name": "briefcase"}},
  "fields": [
    {"name":"organization","type":"relation","required":true,"options":{"collection":"organizations","displayField":"name","reverseName":"projects"}},
    {"name":"name","type":"text","required":true,"presentable":true,"searchable":true},
    {"name":"slug","type":"text","required":true,"searchable":true},
    {"name":"status","type":"select","required":true,"options":{"values":["active","archived","suspended"]}},
    {"name":"settings","type":"json"},
    {"name":"created_by","type":"relation","options":{"collection":"users","displayField":"email"}}
  ],
  "listRule": "@request.auth.role = \"authenticated\"",
  "viewRule": "@request.auth.role = \"authenticated\"",
  "createRule": "@request.auth.role = \"authenticated\"",
  "updateRule": "@request.auth.role = \"authenticated\"",
  "deleteRule": null
}
```

For high-security SaaS, do not rely only on broad authenticated rules. Add service-side endpoints or stricter org-aware rules once org claims are available in the auth context.

## Ecommerce Schema Pattern

Use this pattern for storefronts, marketplaces, and WooCommerce-style apps.

Collections:

- `users` auth collection.
- `customers`: profile data related one-to-one to users.
- `addresses`: customer shipping/billing addresses.
- `products`: catalog.
- `product_variants`: SKU/price/inventory variant rows.
- `categories`: product hierarchy.
- `product_categories`: join collection with position.
- `brands`: optional.
- `product_images`: optional image records, or use file fields on products/variants.
- `carts`: current cart per user/session.
- `cart_items`: cart lines.
- `orders`: order header.
- `order_items`: order lines.
- `payments`: payment attempts and provider state.
- `shipments`: shipment tracking.
- `coupons`: discount definitions.
- `coupon_redemptions`: usage tracking.
- `reviews`: customer reviews.
- `inventory_movements`: stock ledger.
- `webhook_events`: payment/provider webhook audit if needed.

Recommended public rules:

- Products/categories: public list/view for active published data.
- Reviews: public list/view after approval; authenticated create.
- Carts/orders/payments/addresses: owner only or service/admin only.
- Inventory and payment provider payloads: service/admin only.

Example `products`:

```json
{
  "projectSlug": "shop",
  "name": "products",
  "type": "base",
  "options": {"icon": {"type": "lucide", "name": "package"}},
  "fields": [
    {"name":"name","type":"text","required":true,"presentable":true,"searchable":true},
    {"name":"slug","type":"text","required":true,"searchable":true},
    {"name":"description","type":"editor","options":{"maxSize":200000}},
    {"name":"status","type":"select","required":true,"options":{"values":["draft","active","archived"]}},
    {"name":"price","type":"number","required":true,"options":{"min":0}},
    {"name":"currency","type":"select","required":true,"options":{"values":["USD","EUR","GBP","AED","KWD"]}},
    {"name":"image","type":"file","options":{"multiple":false,"maxSize":5242880,"mimeTypes":["image/png","image/jpeg","image/webp"]}},
    {"name":"gallery","type":"file","options":{"multiple":true,"maxSelect":12,"maxSize":10485760,"mimeTypes":["image/png","image/jpeg","image/webp"]}},
    {"name":"metadata","type":"json"}
  ],
  "listRule": "status = \"active\"",
  "viewRule": "status = \"active\"",
  "createRule": null,
  "updateRule": null,
  "deleteRule": null
}
```

Example `orders`:

```json
{
  "name": "orders",
  "type": "base",
  "options": {"icon": {"type": "lucide", "name": "receipt"}},
  "fields": [
    {"name":"customer","type":"relation","required":true,"options":{"collection":"users","displayField":"email","reverseName":"orders"}},
    {"name":"status","type":"select","required":true,"options":{"values":["pending","paid","processing","shipped","completed","cancelled","refunded"]}},
    {"name":"subtotal","type":"number","required":true,"options":{"min":0}},
    {"name":"tax","type":"number","options":{"min":0}},
    {"name":"shipping","type":"number","options":{"min":0}},
    {"name":"total","type":"number","required":true,"options":{"min":0}},
    {"name":"currency","type":"select","required":true,"options":{"values":["USD","EUR","GBP","AED","KWD"]}},
    {"name":"billing_address","type":"json"},
    {"name":"shipping_address","type":"json"},
    {"name":"provider_payload","type":"json","hidden":true}
  ],
  "listRule": "customer = @request.auth.id",
  "viewRule": "customer = @request.auth.id",
  "createRule": null,
  "updateRule": null,
  "deleteRule": null
}
```

## Public REST Verification

Records:

```http
GET    /api/projects/{slug}/collections/{collection}/records?page=1&perPage=25
POST   /api/projects/{slug}/collections/{collection}/records
GET    /api/projects/{slug}/collections/{collection}/records/{id}
PATCH  /api/projects/{slug}/collections/{collection}/records/{id}
DELETE /api/projects/{slug}/collections/{collection}/records/{id}
```

Batch:

```http
POST /api/projects/{slug}/batch
```

Auth:

```http
POST /api/projects/{slug}/auth/signup
POST /api/projects/{slug}/auth/login
GET  /api/projects/{slug}/auth/me
```

Files:

```http
POST /api/projects/{slug}/files/{collection}/{recordId}/{field}
GET  /api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}
```

Always verify:

- Create/list/update critical records.
- Filter and search on configured searchable fields.
- Auth signup/login/me if auth is used.
- Owner/private rules reject another user where relevant.
- File upload and download if file fields exist.
- Webhook delivery after record create/update/delete if webhooks exist.
- Realtime event if realtime is part of the app.
- Cron run if cron jobs exist.
- Backup run if backup jobs exist.

## Security Checklist

Before reporting done:

- Health and readiness are OK.
- MCP token scope is the least privilege available.
- Admin REST tokens and MCP tokens are not printed.
- SMTP, S3, webhook, database, and API secrets are redacted.
- Admin CORS is not wildcard for production.
- Project public CORS is locked to known frontend origins unless explicitly public.
- Public write rules are intentional.
- Private data has owner, organization, or service-only rules.
- Service API keys are never placed in browser examples.
- Webhook secrets are generated and signature verification is documented.
- Quotas are enabled or a reason is given.
- Auth token durations and email templates are reviewed when auth is used.
- Backup jobs are project-scoped unless full-instance backups were requested.
- Restore was only dry-run unless explicit restore approval was given.
- Realtime single-replica boundary is mentioned if realtime is used.

## Completion Checklist

Before reporting done:

- `GET /health` and `GET /ready` passed.
- MCP `initialize` and `tools/list` worked when MCP is used.
- Project exists.
- Collections exist with expected field types, relations, icons, rules, searchable fields, and presentable fields.
- Existing tables were discovered/imported with dry-run first when schema import is used.
- At least one create/list/update path was verified for critical collections.
- Pagination, filters, search, sort, and field selection were tested for primary lists.
- App signup/login/me was tested if the app uses auth.
- Verification, reset, email change, OTP, sessions, or org invitations were tested if configured.
- File upload/download was tested if file fields exist.
- Storage test passed if storage settings changed.
- SMTP test passed if SMTP settings changed.
- CORS settings match frontend origins.
- API keys were created only when needed and masked in output.
- Webhook delivery success was verified if webhooks exist.
- Cron run was tested if cron jobs exist.
- Backup run was tested if backup jobs exist.
- Restore stayed in dry-run unless explicitly approved.
- Metrics/quotas were checked for SaaS or production-like apps.
- Final answer includes project slug, created resources, API examples, tests performed, and remaining boundaries.
