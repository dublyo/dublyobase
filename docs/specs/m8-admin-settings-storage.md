# M8 Spec: PocketBase-style Admin Controls and Provider Settings

Status: implemented in v0.9.0
Depends on: v0.8.0

M8 improves the admin panel where users spend most of their time: collection
creation, email setup, and file-storage setup. It keeps the single Go process
and embedded static admin UI while adding runtime-editable settings stored in
Postgres.

## Goal

Make Dublyobase feel closer to PocketBase for day-one setup:

- create and edit collections through structured controls instead of JSON;
- configure SMTP from the admin panel;
- configure S3-compatible storage for providers such as Cloudflare R2,
  Backblaze B2, MinIO, AWS S3, and other S3-compatible endpoints;
- keep local storage as the safe default.

## Scope

In scope:

- PocketBase-style collection builder for fields and rules.
- Field option controls for supported types:
  - `select`: values and multi-select.
  - `relation`: target collection and multi-relation.
  - `file`: single/multiple files.
  - all field types: name, type, required.
- Admin settings API backed by `_dbo.instance_settings`.
- Runtime SMTP settings with masked secrets.
- Runtime storage settings with local and S3-compatible providers.
- Go storage provider interface with local and S3-compatible drivers.
- S3-compatible upload/download/delete for normal file uploads.
- Resumable uploads keep local temp chunks, then store the assembled object in
  the active provider.
- Test actions for SMTP and storage settings.

Out of scope:

- Direct rclone binary execution.
- Non-S3 providers such as SFTP, Google Drive, Dropbox, WebDAV, etc.
- Browser direct-to-S3 presigned uploads.
- Per-project SMTP/storage settings.
- Full relation-picker UI for record editing.
- Raw SQL migrations for destructive schema changes.

## User Flow

1. Admin logs in.
2. Admin opens Collections and creates a collection with form controls:
   collection name, type, field rows, per-field options, and rules.
3. Admin edits fields in the selected collection and saves schema changes.
4. Admin opens Settings.
5. Admin enters SMTP host/port/from/user/password and sends a test email.
6. Admin selects storage provider:
   - Local: keep current local volume.
   - S3-compatible: endpoint, bucket, region, access key, secret key, path-style,
     HTTPS, and optional prefix.
7. Admin saves and runs a storage test.
8. File uploads/downloads use the active provider immediately.

## Requirements

- Settings must take effect without rebuilding the container.
- Secret values must never be returned by GET settings APIs.
- Empty secret fields in update requests preserve the existing secret.
- Explicit clear flags can remove stored secrets.
- Settings validation must fail before saving broken required fields.
- SMTP disabled means auth-token flows still work and silently skip delivery.
- S3 provider must support R2/B2-style custom endpoints and path-style buckets.
- Local storage must remain the default and must continue to work with existing
  files.
- File metadata `path` remains a provider object key, not a public URL.
- Admin UI must not ask users to write raw JSON for common field setup.

## Frontend

The admin UI remains Next static export + Tailwind CSS embedded in `ui/dist`.

Collection UI:

- Left pane: collection table.
- Right pane: create/edit collection form.
- Field rows with stable dimensions, compact controls, and icon buttons.
- Option panel changes by selected field type.
- Rules remain textareas, one per operation.
- JSON payload stays an internal escape hatch only if needed later.

Settings UI:

- SMTP panel:
  - enabled toggle;
  - host, port, from, username, password;
  - password state indicator;
  - test recipient and send-test button.
- Storage panel:
  - provider select: local or S3-compatible;
  - local status/path readout;
  - S3 endpoint, bucket, region, access key, secret key, prefix;
  - HTTPS and path-style toggles;
  - secret state indicator;
  - storage test button.

## Backend

New admin endpoints:

- `GET /admin/api/settings`
- `PUT /admin/api/settings/smtp`
- `POST /admin/api/settings/smtp/test`
- `PUT /admin/api/settings/storage`
- `POST /admin/api/settings/storage/test`

All endpoints require admin auth and audit mutations.

Runtime resolution:

- Settings are loaded from `_dbo.instance_settings`.
- Stored settings override env config.
- If no stored settings exist, env config remains effective.
- SMTP and storage code resolve effective settings at request time.

## Database

Reuse existing `_dbo.instance_settings`:

```sql
create table if not exists _dbo.instance_settings (
    id         boolean primary key default true,
    data       jsonb   not null default '{}'::jsonb,
    updated_at timestamptz not null default now(),
    constraint instance_settings_single_row check (id)
);
```

Stored secret fields are encrypted in the app layer using `JWT_SECRET` as the
instance master material. API responses only include `passwordSet` or
`secretKeySet`.

## APIs

Settings response:

```json
{
  "smtp": {
    "enabled": true,
    "host": "smtp.example.com",
    "port": "587",
    "from": "Dublyobase <no-reply@example.com>",
    "username": "mailer",
    "passwordSet": true
  },
  "storage": {
    "type": "s3",
    "localPath": "/data/storage",
    "s3": {
      "endpoint": "s3.us-west-004.backblazeb2.com",
      "bucket": "dublyobase",
      "region": "us-west-004",
      "accessKey": "key-id",
      "secretKeySet": true,
      "prefix": "prod",
      "useSSL": true,
      "forcePathStyle": true
    }
  }
}
```

## UI/UX

- Keep the existing quiet admin dashboard style.
- No marketing sections.
- Dense controls, clear labels, explicit disabled/empty states.
- Use native labels, fieldsets, buttons, toggles, and selects.
- Avoid nested cards; panels are the outer frame, repeated rows are rows.
- Secrets show state, not value.
- Test buttons report success/failure without exposing credentials.

## Edge Cases

- Secret omitted during save should preserve current secret.
- Secret intentionally cleared should disable auth for that service.
- Invalid SMTP from address.
- SMTP server unavailable.
- S3 endpoint missing scheme or malformed.
- Bucket exists but credentials cannot write/delete.
- S3 object uploaded but DB update fails.
- DB update succeeds but stale object delete fails.
- Existing local files after switching to S3.
- Existing S3 objects after switching back to local.
- Resumable upload expires while using S3 backend.

## Security

- Admin middleware remains the security boundary.
- Secrets are encrypted at rest in `_dbo.instance_settings`.
- Settings GET never returns passwords or secret keys.
- Audit data records provider type and non-secret identifiers only.
- Storage object keys are generated from validated path segments.
- S3 downloads still require Dublyobase file tokens and record view access.
- SMTP test emails never include credentials or auth tokens.

## Testing

- Unit tests for settings validation and secret redaction.
- Unit tests for local provider behavior.
- Integration tests for admin settings endpoints.
- Existing file upload/download tests must still pass.
- S3 driver should be exercised by interface-level tests where a live S3
  endpoint is not available.
- UI typecheck/build must pass.
- Browser smoke: collection builder, settings panels, health/admin shell.

## Implementation Steps

1. Add settings core types, validation, encryption, and API endpoints.
2. Add storage provider interface and local/S3-compatible drivers.
3. Refactor file upload/download/cleanup paths to use active provider.
4. Refactor auth email delivery to resolve runtime SMTP settings.
5. Replace JSON-first collection create UI with structured builder controls.
6. Add SMTP and storage settings panels.
7. Add tests and docs.
8. Build UI, run Go vet/build/tests, run browser smoke.
