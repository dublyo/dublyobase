# M5 Spec: File Storage

Status: implemented in v0.6.0
Depends on: v0.5.0

M5 adds local file storage for project records without introducing another
service. Files live on the existing `/data/storage` volume; Postgres stores only
file metadata in collection rows.

## Goal

Let apps attach protected files to records through streamed multipart uploads.

Success means:

- Collections can define `file` fields.
- Single-file fields are the default; `options.multiple=true` stores an array.
- Uploads stream to local disk and are capped by `MAX_UPLOAD_MB`.
- File metadata is stored in the record's JSONB file field.
- Downloads require a short-lived file token minted after record `view` rules pass.
- `thumb=WxH` returns a cached JPEG thumbnail for image files.
- Replacing a file and deleting a record remove stale local files.

## Scope

In scope:

- Local storage only.
- `file` field type with JSONB column storage.
- Multipart upload route using form field `file`.
- Replace and append upload modes.
- Protected download tokens.
- Cached image thumbnails.
- Cleanup for replace, record delete, and collection delete.
- Tests for protected access, thumbnails, cleanup, and upload limits.

Out of scope:

- S3 uploads.
- Resumable/chunked uploads.
- Virus scanning.
- Public files by default.
- Admin UI file picker.
- Per-field MIME allow lists.

## User Flow

1. Admin creates a collection with a `file` field.
2. Client creates a record through the records API.
3. Client uploads one or more multipart `file` parts to the record field.
4. Dublyobase streams files into `/data/storage/<project>/<collection>/<record>/<field>/`.
5. Dublyobase updates the record's JSONB file field with stable metadata.
6. Client requests a file token using an app/admin/service bearer token.
7. Dublyobase verifies record `view` access and returns a short-lived token.
8. Client downloads the file, or requests `thumb=WxH` for a cached thumbnail.
9. Replacement and record/collection deletion remove stale files from the volume.

## Requirements

- `MAX_UPLOAD_MB` defaults to `64` and validates between `1` and `1024`.
- `file` columns are `jsonb`.
- Direct JSON writes to `file` fields are rejected; upload routes own file metadata.
- Single-file fields reject append mode and multiple multipart file parts.
- Multiple-file fields accept replace and append.
- File IDs are generated UUIDs.
- Original filenames are stored only as display metadata.
- On-disk paths use generated IDs, never trusted filenames.
- Stored metadata includes `id`, `name`, `size`, `mime`, `created`, and `path`.
- Direct downloads without a valid token return `401`.
- Token minting must respect the collection `view` rule.
- Uploading must respect the collection `update` rule.
- Oversize uploads return `413 file_too_large`.

## Frontend

M5 remains API-first. The embedded root page can remain the placeholder admin
shell until the admin-panel milestone.

## Backend

Main files:

- `core/files.go`: local storage, metadata, token, thumbnail, cleanup helpers.
- `apis/files.go`: multipart upload, token minting, protected download handlers.
- `core/fields.go`: `file` field validation and JSONB DDL.
- `core/records.go`: block direct JSON file writes and return deleted rows for cleanup.
- `core/config.go`: `MAX_UPLOAD_MB`.

## Database

No new migration is required. File fields are normal collection fields:

```sql
avatar jsonb
gallery jsonb
```

Single-file fields store one JSON object or `null`. Multiple-file fields store a
JSON array.

## APIs

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/projects/{slug}/files/{collection}/{recordId}/{field}` | Multipart upload; form field `file`; `?mode=replace\|append` |
| `POST` | `/api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/token` | Mint protected file token |
| `GET` | `/api/projects/{slug}/files/{collection}/{recordId}/{field}/{fileId}/{filename}?token=...` | Download original |
| `GET` | same as above with `&thumb=WxH` | Download cached JPEG thumbnail |

## Edge Cases

- Unknown collection/record/field.
- Non-file field upload.
- Single-file field receives multiple files.
- Append mode on a single-file field.
- Oversize request body.
- Expired, missing, mismatched, or forged token.
- Thumbnail request for non-image content.
- Deleted record with stale files on disk.
- Replacement upload where DB update fails after bytes were written.

## Security

- Storage paths are joined from validated/safe path segments.
- Path traversal and hidden dotfile fallback are not allowed.
- File tokens are HS256 JWTs scoped to one project, collection, record, field, and file ID.
- File tokens expire after 5 minutes.
- File metadata cannot be forged through generic JSON record writes.
- MIME type is detected server-side from file bytes.

## Testing

- Unit coverage for field/config changes.
- Integration coverage for:
  - protected `401` download without token;
  - owner-only token minting;
  - original download with token;
  - thumbnail dimensions;
  - replacement cleanup;
  - multiple-file append;
  - direct JSON write rejection;
  - oversize upload rejection;
  - record delete cleanup.
- Release validation also runs the full suite against PostgreSQL 16, 17, and 18,
  then deploys to Portainer and smokes upload/download behavior.
