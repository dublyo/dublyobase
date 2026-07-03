# M6 Spec: Email SMTP

Status: implemented in v0.7.0
Depends on: v0.6.1

M6 adds email delivery for the app-auth verification and password reset tokens
created in M4. It keeps the one-binary deployment model: SMTP is optional and
configured only through environment variables.

## Goal

Send verification and password reset emails for project app users.

Success means:

- Signup sends a verification email when SMTP is configured.
- Manual verification requests send a verification email.
- Password reset requests send a reset email.
- Existing confirm endpoints keep working.
- No SMTP configured means flows still return generic accepted responses.
- API responses do not leak token values unless `AUTH_DEV_TOKENS=true`.

## Scope

In scope:

- Global SMTP configuration from environment variables.
- No-op mailer when `SMTP_HOST` is unset.
- Plain-text email templates using `APP_URL` links.
- Verification and password reset delivery.
- Tests with an injected fake mailer.

Out of scope:

- Per-project SMTP settings.
- Encrypted SMTP credentials in `_dbo`.
- Admin UI email settings.
- OAuth email-change flows.
- Queue/retry worker.

## User Flow

1. User signs up or requests verification/reset.
2. Dublyobase creates the existing one-use hashed token.
3. Dublyobase sends an email if SMTP is configured.
4. The user follows the app link or copies the token into the app.
5. The app calls the existing confirm endpoint.

## Requirements

- `SMTP_HOST` enables SMTP.
- `SMTP_PORT` defaults to `587` and must be `1..65535`.
- `SMTP_FROM` is required when `SMTP_HOST` is set.
- `SMTP_USER` and `SMTP_PASSWORD` must be set together.
- Request endpoints keep generic `202 accepted` behavior.
- Mail delivery failures are logged without token/email values and do not reveal
  whether a user exists.
- Signup email failures do not fail signup.
- Links are built from `APP_URL`, never the request host.

## Frontend

No real UI change in M6. Links point to future frontend routes:

- `/auth/verify?project=...&email=...&token=...`
- `/auth/reset-password?project=...&email=...&token=...`

M7 can make those routes interactive.

## Backend

Main files:

- `core/mail.go`: mailer interface, no-op mailer, SMTP mailer, templates.
- `core/app.go`: app-level mailer dependency.
- `core/app_auth.go`: internal token result for delivery.
- `apis/app_auth.go`: email delivery wiring.
- `core/config.go`: SMTP validation.

## Database

No migration. M6 reuses `_dbo.auth_tokens`.

## APIs

No new public API is required. Existing endpoints:

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/projects/{slug}/auth/signup` | Creates user and sends verification email if configured |
| `POST` | `/api/projects/{slug}/auth/request-verification` | Creates and emails verification token |
| `POST` | `/api/projects/{slug}/auth/request-password-reset` | Creates and emails reset token |
| `POST` | `/api/projects/{slug}/auth/confirm-verification` | Confirms token |
| `POST` | `/api/projects/{slug}/auth/confirm-password-reset` | Confirms reset token and password |

## UI/UX

The email text is short and direct:

- Verification: project name, verification link, copyable token fallback.
- Reset: project name, reset link, copyable token fallback.

## Edge Cases

- No SMTP configured.
- SMTP configured but remote server unavailable.
- User not found or disabled.
- Invalid email request.
- Token generated but email send fails.
- `APP_URL` has a sub-path.
- `SMTP_FROM` contains a display name.

## Security

- Tokens are hashed at rest.
- Tokens are not logged.
- Email delivery logs only project/type/error.
- Request endpoints do not reveal whether the email exists.
- SMTP credentials remain env-only in M6.
- SMTP transport uses STARTTLS when the server advertises it.

## Testing

- Config validation for SMTP env.
- Unit coverage for link/template generation.
- Integration coverage for:
  - signup sends verification email through injected mailer;
  - verification request does not expose dev token when disabled;
  - reset email includes token and confirm flow succeeds;
  - no-op/failing mailer keeps request responses generic.

## Implementation Steps

1. Add mailer abstraction and SMTP implementation.
2. Add SMTP config validation.
3. Add internal auth-token result fields.
4. Wire signup, verification, and reset emails.
5. Add tests.
6. Update docs/template to v0.7.0.
7. Run validation, commit, tag, push, deploy.
