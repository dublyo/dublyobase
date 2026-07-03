# M7 Spec: Admin Panel v1

Status: implemented in v0.8.0
Depends on: v0.7.0

M7 replaces the placeholder home page with a real embedded admin panel for the
backend already shipped through M6. The panel is a same-origin static frontend
compiled into `ui/dist` and served by the Go binary; there is no Node runtime in
production.

## Goal

Make Dublyobase usable from the browser for the core backend workflows:

- log in as an admin;
- create and inspect projects;
- manage collections and fields;
- browse, create, edit, delete records;
- manage app users through the `users` auth collection;
- create and revoke project API keys;
- upload and download file fields;
- inspect health and recent audit activity.

Success means a user can open `/`, log in, and operate the existing M1-M6 APIs
without using curl.

## Scope

In scope:

- Static admin SPA embedded in the Go binary.
- Tailwind CSS utility styling with a dense, work-focused dashboard layout.
- Same-origin API client for `/admin/api/*` and `/api/projects/*`.
- Admin login/logout/session restore.
- Setup screen for first-admin creation when setup is still open.
- Project list, create, and detail shell.
- Collection list/create/edit/delete, including field editor and rules editor.
- Records table with pagination, sort, filter, JSON editor, create/edit/delete.
- Auth users view backed by the system `users` collection.
- API key list/create/revoke with one-time secret display.
- File-field upload controls using the existing multipart endpoint; download via
  short-lived file token.
- Health/ready status chip and recent audit log viewer.
- New backend endpoint for audit-log listing if the panel needs it.

Out of scope:

- OAuth provider setup and OAuth login.
- Realtime subscriptions.
- Webhooks.
- S3 storage settings.
- Per-project SMTP settings.
- Raw SQL console.
- Full visual database ERD designer.

## User Flow

1. User opens `/`.
2. App checks `GET /admin/api/me`.
3. If unauthenticated, show login. If setup is still open, show setup option.
4. After login, show a left sidebar with Projects, Health, and account controls.
5. User creates or opens a project.
6. Project workspace shows tabs: Overview, Collections, Records, Users, API Keys,
   Files, Logs, Settings.
7. User edits schema in Collections, then manages data in Records.
8. User logs out; token is removed locally and server session is revoked.

## Requirements

1. The first screen must be the actual admin app, not a marketing landing page.
2. All routes must work through the current SPA fallback and deep-link reloads.
3. The production container must still run one Go process on port `8080`.
4. No runtime dependency on Node, CDN JS, external fonts, or external UI services.
5. The panel must call the real APIs; no mock data outside tests/story fixtures.
6. API errors must show the server `message` and preserve the machine `error`.
7. Admin token must never be placed in URLs or logs.
8. Destructive actions require confirmation and show the exact resource name.
9. Long lists use pagination; the UI must not fetch unbounded tables.
10. Large upload controls must default to tunnel-safe behavior and clear progress.

## Frontend

Recommended implementation:

- Next.js static export + Tailwind CSS, embedded into `ui/dist`.
- Client-side routes only; no Next server, server actions, or SSR at runtime.
- Build output copied into `ui/dist` before Go build.
- TypeScript for API shapes and form state.
- Native controls and semantic HTML following MDN guidance.
- Icons from `lucide-react`.

Layout:

- Header: project switcher, health status, user menu.
- Sidebar: Projects, Collections, Records, Users, API Keys, Logs, Settings.
- Main area: dense tables/forms with fixed toolbar heights and responsive grids.
- Mobile: collapsible sidebar, single-column detail panes, no overlapping text.

State:

- Admin token in `sessionStorage`.
- Optional "remember this browser" can use `localStorage` later, but not in M7.
- Fetch wrapper injects bearer token, handles `401` by clearing session, and
  normalizes error envelopes.

## Backend

Use existing APIs first:

- `POST /setup`
- `POST /admin/api/auth/login`
- `POST /admin/api/auth/logout`
- `GET /admin/api/me`
- `GET/POST /admin/api/projects`
- `GET /admin/api/projects/{slug}`
- `GET/POST /admin/api/projects/{slug}/api-keys`
- `DELETE /admin/api/projects/{slug}/api-keys/{id}`
- `GET/POST /api/projects/{slug}/collections`
- `GET/PATCH/DELETE /api/projects/{slug}/collections/{name}`
- record CRUD routes
- file upload/token/download routes

Add only if needed:

- `GET /admin/api/audit-log?project=&page=&perPage=`
  - admin-auth required;
  - returns newest-first audit entries;
  - redacts token-like values and secrets from `data`;
  - bounded `perPage`, max 100.

No backend route should weaken existing auth boundaries. Client-side route guards
are only UX; server-side middleware remains the boundary.

## Database

No required migration for the panel itself.

If audit listing needs indexing, add a small migration:

- `_dbo.audit_log(created desc)` index;
- optional `_dbo.audit_log(project)` expression/index only if current query shape
  needs it after testing.

No table should store admin UI preferences in M7.

## APIs

Panel API client behavior:

- Always send `Accept: application/json`.
- Send `Content-Type: application/json` for JSON bodies.
- Use `FormData` only for multipart file uploads.
- Treat non-2xx as structured errors.
- Preserve `rls_denied`, `unauthorized`, `validation_failed`, and
  `destructive_change` as first-class UI states.

Audit-log endpoint response, if added:

```json
{
  "items": [
    {
      "id": "uuid",
      "action": "project.create",
      "targetType": "project",
      "targetId": "uuid",
      "ip": "203.0.113.10",
      "userAgent": "browser",
      "data": {},
      "created": "2026-07-03T00:00:00Z"
    }
  ],
  "page": 1,
  "perPage": 30,
  "totalItems": 1
}
```

## UI/UX

Design direction:

- Operational SaaS dashboard, not a landing page.
- Neutral base with clear status colors; avoid a one-color theme.
- Tables are compact, scannable, and keyboard-friendly.
- Forms show inline validation near the field.
- Dangerous actions are visually distinct and require explicit confirmation.
- Loading states use skeleton rows or disabled controls, not layout shifts.
- Empty states provide a direct create action.

Primary screens:

- Login/setup.
- Project list.
- Project overview.
- Collection schema editor.
- Records table and record drawer.
- Users table.
- API keys.
- Logs.
- File upload/download controls inside record forms.

## Edge Cases

- First admin already exists when setup form is submitted.
- Session expires while editing a record.
- Admin opens a deep link directly after reload.
- Collection update returns `409 destructive_change`.
- Field type is not supported by the current UI.
- Record JSON is invalid.
- Rule/filter expression is invalid.
- File upload exceeds `MAX_UPLOAD_MB`.
- File token expires before download.
- API key secret is dismissed before copying.
- Health degraded while the panel is open.

## Security

- Server-side admin middleware remains the security boundary.
- Token stored in session storage only for M7.
- Never log bearer tokens, API key secrets, reset tokens, or file tokens.
- Do not render user-provided HTML with `dangerouslySetInnerHTML`.
- Escape/encode filenames and record values through framework defaults.
- API key secret is shown once and can be manually copied; it is never persisted
  in frontend state after dismiss.
- Setup screen must not reveal whether a specific email exists.
- Audit-log UI must not show secret-like fields.

## Testing

Local validation:

- `go test -count=1 ./...`
- `go vet ./...`
- frontend typecheck/build
- `git diff --check`

Frontend tests:

- login/logout flow;
- setup-open and setup-closed states;
- project create/list;
- collection create/edit/delete;
- record create/edit/delete;
- API key create/revoke with one-time display;
- file upload and token download path;
- expired session handling.

Browser verification:

- Run the app locally and inspect with Playwright.
- Desktop and mobile screenshots.
- Check no text overlaps, no blank page, no broken asset paths under SPA fallback.
- Live Portainer stack smoke after release.

## Implementation Steps

1. Finalize this spec.
2. Scaffold static admin UI under `ui/app` or `ui/admin` with Next.js + Tailwind.
3. Add build scripts that export into `ui/dist`.
4. Update Dockerfile/CI to build UI before Go embed.
5. Add API client and session store.
6. Implement login/setup shell.
7. Implement project list/create/detail shell.
8. Implement collection schema editor.
9. Implement records table and record drawer.
10. Implement users and API keys screens.
11. Add file upload/download controls.
12. Add audit-log endpoint and UI if required.
13. Add tests and browser verification.
14. Update README, roadmap, and Dublyo template pin to `v0.8.0`.
15. Commit, tag `v0.8.0`, push, wait for CI/GHCR, deploy, smoke live.
