"use client";

import { login, me } from "../../src/lib/api";
import type { Admin, Health, InstanceSettings, OpsAlert, Project, ProjectAuthSettings, ProjectMetrics, ProjectQuotas } from "../../src/lib/types";
import { emptyAdminDraft, emptyCORSDraft, emptyQuotaDraft, emptySMTPDraft, emptyStorageDraft } from "../lib/constants";
import { formatBytes, formatCount, formatDate } from "../lib/format";
import { defaultAuthSettingsForProject } from "../lib/settings-drafts";
import { CompactTable, EmptyState, Info, LabeledInput } from "./ui";
import { Activity, ChevronDown, Database, Globe, HardDrive, KeyRound, List, Mail, Plus, RefreshCw, Save, ShieldCheck, UploadCloud, User, Users } from "lucide-react";

export function ApplicationSettings({
  project,
  projects,
  projectDraft,
  setProjectDraft,
  onSubmitProject,
  healthState,
  appUrl,
  settings,
  adminUsers,
  projectQuotas,
  onOpenAuth,
  onOpenMail,
  onOpenFiles,
  onOpenMCP,
}: {
  project: Project | null;
  projects: Project[];
  projectDraft: { slug: string; name: string };
  setProjectDraft: (draft: { slug: string; name: string }) => void;
  onSubmitProject: (event: React.FormEvent<HTMLFormElement>) => void;
  healthState: Health | null;
  appUrl: string;
  settings?: InstanceSettings | null;
  adminUsers?: Admin[];
  projectQuotas?: ProjectQuotas | null;
  onOpenAuth: () => void;
  onOpenMail: () => void;
  onOpenFiles: () => void;
  onOpenMCP: () => void;
}) {
  const appName = project?.name || "Dublyobase";
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Application</h2>
        <div className="pb-application-form">
          <label className="pb-field">
            <span>Application name</span>
            <input value={appName} readOnly />
          </label>
          <label className="pb-field">
            <span>Application URL</span>
            <input value={appUrl} readOnly />
          </label>
          <label className="pb-field accent-field">
            <span>Accent</span>
            <input value="#1055c9" readOnly />
          </label>
        </div>
        <div className="pb-settings-toggle-list">
          <div>
            <Database className="h-4 w-4" />
            <strong>Postgres connection</strong>
            <span className={`pb-status-badge ${healthState?.db === "ok" ? "success" : "warning"}`}>{healthState?.db ?? "checking"}</span>
          </div>
          <div>
            <HardDrive className="h-4 w-4" />
            <strong>File storage</strong>
            <span className={`pb-status-badge ${healthState?.storage === "ok" ? "success" : "warning"}`}>{settings?.storage.type ?? healthState?.storage ?? "checking"}</span>
          </div>
          <div>
            <Globe className="h-4 w-4" />
            <strong>CORS origins</strong>
            <span className="pb-status-badge">{settings?.cors.wildcard ? "wildcard" : `${settings?.cors.adminOrigins.length ?? 0} admin`}</span>
          </div>
          <div>
            <Activity className="h-4 w-4" />
            <strong>Rate limiting and quotas</strong>
            <span className={`pb-status-badge ${projectQuotas?.enabled ? "success" : ""}`}>{projectQuotas?.enabled ? "enabled" : "disabled"}</span>
          </div>
          <div>
            <Users className="h-4 w-4" />
            <strong>Super admins</strong>
            <span className="pb-status-badge">{formatCount(adminUsers?.length ?? 0)}</span>
          </div>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Core services</h2>
        <div className="pb-service-grid">
          <button type="button" className="pb-service-tile" onClick={onOpenAuth}>
            <ShieldCheck className="h-5 w-5" />
            <span>
              <strong>Auth</strong>
              <em>Email/password users, tokens, verification, reset</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenMail}>
            <Mail className="h-5 w-5" />
            <span>
              <strong>Email</strong>
              <em>SMTP delivery for auth and test messages</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenFiles}>
            <UploadCloud className="h-5 w-5" />
            <span>
              <strong>Files</strong>
              <em>Upload, protected tokens, thumbnails</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenMCP}>
            <KeyRound className="h-5 w-5" />
            <span>
              <strong>MCP</strong>
              <em>Scoped AI tool access to the live backend</em>
            </span>
          </button>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Project</h2>
        {project ? (
          <div className="pb-info-grid">
            <Info label="Slug" value={project.slug} />
            <Info label="Name" value={project.name} />
            <Info label="Schema" value={project.schemaName} />
            <Info label="Anon role" value={project.roles?.anon ?? ""} />
            <Info label="Authenticated role" value={project.roles?.authenticated ?? ""} />
            <Info label="Service role" value={project.roles?.service ?? ""} />
          </div>
        ) : (
          <EmptyState label="No project selected." />
        )}
      </section>
      <section className="pb-settings-block">
        <h2>Projects</h2>
        <form onSubmit={onSubmitProject} className="pb-grid-form">
          <LabeledInput label="Slug" value={projectDraft.slug} onChange={(value) => setProjectDraft({ ...projectDraft, slug: value })} placeholder="myapp" />
          <LabeledInput label="Name" value={projectDraft.name} onChange={(value) => setProjectDraft({ ...projectDraft, name: value })} placeholder="My app" />
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create project
          </button>
        </form>
        <CompactTable headers={["Name", "Slug", "Schema"]} rows={projects.map((item) => [item.name, item.slug, item.schemaName])} empty="No projects yet." />
      </section>
    </div>
  );
}

export function AuthSettingsPanel({
  project,
  appUrl,
  settings,
  authSettings,
  setAuthSettings,
  onSaveAuthSettings,
  onOpenMail,
}: {
  project: Project | null;
  appUrl: string;
  settings: InstanceSettings | null;
  authSettings: ProjectAuthSettings | null;
  setAuthSettings: React.Dispatch<React.SetStateAction<ProjectAuthSettings | null>>;
  onSaveAuthSettings: (settings: ProjectAuthSettings) => void;
  onOpenMail: () => void;
}) {
  const projectSlug = project?.slug || "{project}";
  const base = `${appUrl}/api/projects/${projectSlug}`;
  const draft = authSettings ?? defaultAuthSettingsForProject(project);
  function updateDraft(patch: Partial<ProjectAuthSettings>) {
    setAuthSettings({ ...draft, ...patch });
  }
  function updateTemplate(key: keyof ProjectAuthSettings["templates"], value: string) {
    setAuthSettings({ ...draft, templates: { ...draft.templates, [key]: value } });
  }
  const oauthProviders = [
    { id: "google", label: "Google", authURL: "https://accounts.google.com/o/oauth2/v2/auth", tokenURL: "https://oauth2.googleapis.com/token", userInfoURL: "https://openidconnect.googleapis.com/v1/userinfo", scopes: "openid email profile" },
    { id: "github", label: "GitHub", authURL: "https://github.com/login/oauth/authorize", tokenURL: "https://github.com/login/oauth/access_token", userInfoURL: "https://api.github.com/user", scopes: "read:user user:email" },
    { id: "facebook", label: "Facebook", authURL: "https://www.facebook.com/v25.0/dialog/oauth", tokenURL: "https://graph.facebook.com/v25.0/oauth/access_token", userInfoURL: "https://graph.facebook.com/v25.0/me?fields=id,email,name,picture", scopes: "email public_profile" },
    { id: "oidc", label: "OIDC", authURL: "", tokenURL: "", userInfoURL: "", scopes: "openid email profile" },
  ];
  function providerConfig(id: string): Record<string, unknown> {
    const value = draft.providers?.[id];
    return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
  }
  function providerString(id: string, key: string, fallback = "") {
    const value = providerConfig(id)[key];
    return typeof value === "string" ? value : fallback;
  }
  function providerBool(id: string, key: string) {
    return providerConfig(id)[key] === true;
  }
  function updateProvider(id: string, patch: Record<string, unknown>) {
    setAuthSettings({
      ...draft,
      providers: {
        ...draft.providers,
        [id]: {
          ...providerConfig(id),
          ...patch,
        },
      },
    });
  }
  const routes = [
    ["POST", `${base}/auth/signup`, "Create an app user"],
    ["POST", `${base}/auth/login`, "Email/password login"],
    ["POST", `${base}/auth/request-otp`, "Send one-time login code"],
    ["POST", `${base}/auth/login-otp`, "Login with one-time code"],
    ["GET", `${base}/auth/oauth/{provider}/start`, "Start OAuth login"],
    ["GET", `${base}/auth/oauth/{provider}/callback`, "OAuth provider callback"],
    ["POST", `${base}/auth/mfa/enroll`, "Start TOTP MFA enrollment"],
    ["POST", `${base}/auth/mfa/confirm`, "Confirm TOTP and receive recovery codes"],
    ["POST", `${base}/auth/mfa/verify`, "Finish login with MFA code"],
    ["POST", `${base}/auth/mfa/recovery`, "Finish login with recovery code"],
    ["POST", `${base}/auth/mfa/disable`, "Disable MFA for current app user"],
    ["POST", `${base}/auth/refresh`, "Rotate refresh token"],
    ["GET", `${base}/auth/sessions`, "List app user sessions"],
    ["GET", `${base}/auth/me`, "Current app user"],
    ["POST", `${base}/auth/request-verification`, "Send verification email"],
    ["POST", `${base}/auth/confirm-verification`, "Confirm verification token"],
    ["POST", `${base}/auth/request-password-reset`, "Send password reset email"],
    ["POST", `${base}/auth/confirm-password-reset`, "Set a new password"],
    ["POST", `${base}/auth/request-email-change`, "Send email change confirmation"],
    ["POST", `${base}/auth/confirm-email-change`, "Confirm email change"],
    ["GET", `${base}/orgs`, "List current user's organizations"],
    ["POST", `${base}/orgs`, "Create organization"],
    ["POST", `${base}/orgs/{orgId}/invitations`, "Invite organization member"],
    ["POST", `${base}/org-invitations/accept`, "Accept organization invitation"],
  ];
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Auth settings</h2>
        <div className="pb-inline-alert info">
          Email/password auth is available per project through the system `users` collection. Verification and reset emails use the SMTP settings.
        </div>
        <div className="pb-info-grid compact">
          <Info label="Project" value={project?.slug ?? "No project selected"} />
          <Info label="Users collection" value={project ? "users" : ""} />
          <Info label="SMTP" value={settings?.smtp.enabled ? "enabled" : "not enabled"} />
        </div>
        {!settings?.smtp.enabled ? (
          <div className="pb-row-actions">
            <button type="button" className="pb-btn secondary" onClick={onOpenMail}>
              <Mail className="h-4 w-4" />
              Configure SMTP
            </button>
          </div>
        ) : null}
        <div className="pb-auth-feature-grid">
          <div>
            <KeyRound className="h-4 w-4" />
            <strong>Email/password</strong>
            <span>Signup, login, refresh, sessions</span>
          </div>
          <div>
            <Mail className="h-4 w-4" />
            <strong>Verification and reset</strong>
            <span>Template-controlled auth emails</span>
          </div>
          <div>
            <ShieldCheck className="h-4 w-4" />
            <strong>OTP and MFA</strong>
            <span>Email codes, TOTP, recovery codes</span>
          </div>
          <div>
            <Globe className="h-4 w-4" />
            <strong>OAuth</strong>
            <span>Google, GitHub, Facebook, OIDC</span>
          </div>
        </div>
      </section>
      <section className="pb-settings-block">
        <details className="pb-settings-disclosure">
          <summary>
            <span>
              <strong>Auth API</strong>
              <em>Signup, login, OTP, MFA, OAuth, sessions, orgs</em>
            </span>
            <ChevronDown className="h-4 w-4" />
          </summary>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Method</th>
                <th>Endpoint</th>
                <th>Use</th>
              </tr>
            </thead>
            <tbody>
              {routes.map(([method, route, use]) => (
                <tr key={`${method}-${route}`}>
                  <td>{method}</td>
                  <td className="truncate-cell">
                    <code>{route}</code>
                  </td>
                  <td>{use}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        </details>
      </section>
      <section className="pb-settings-block">
        <h2>Client payloads</h2>
        <div className="pb-sync-grid">
          <pre className="pb-code-box">{`POST ${base}/auth/signup
{
  "email": "user@example.com",
  "password": "password-123"
}`}</pre>
          <pre className="pb-code-box">{`POST ${base}/auth/login
{
	  "email": "user@example.com",
	  "password": "password-123"
	}`}</pre>
          <pre className="pb-code-box">{`POST ${base}/auth/request-otp
{
  "email": "user@example.com"
}

POST ${base}/auth/login-otp
{
  "email": "user@example.com",
  "token": "dbo_otp_..."
}`}</pre>
          <pre className="pb-code-box">{`GET ${base}/auth/oauth/google/start?format=json

POST ${base}/auth/mfa/enroll
Authorization: Bearer $ACCESS_TOKEN
{"name":"Authenticator app"}

POST ${base}/auth/mfa/confirm
Authorization: Bearer $ACCESS_TOKEN
{"factorId":"...","code":"123456"}

POST ${base}/auth/mfa/verify
{"mfaToken":"dbo_mfa_...","code":"123456"}`}</pre>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Token durations</h2>
        <div className="pb-grid-form four">
          <LabeledInput label="Access minutes" value={String(draft.accessTokenMinutes)} onChange={(value) => updateDraft({ accessTokenMinutes: Number.parseInt(value, 10) || 60 })} />
          <LabeledInput label="Refresh days" value={String(draft.refreshTokenDays)} onChange={(value) => updateDraft({ refreshTokenDays: Number.parseInt(value, 10) || 7 })} />
          <LabeledInput label="Verify hours" value={String(draft.verifyTokenHours)} onChange={(value) => updateDraft({ verifyTokenHours: Number.parseInt(value, 10) || 24 })} />
          <LabeledInput label="Reset hours" value={String(draft.resetTokenHours)} onChange={(value) => updateDraft({ resetTokenHours: Number.parseInt(value, 10) || 1 })} />
          <LabeledInput label="OTP minutes" value={String(draft.otpTokenMinutes)} onChange={(value) => updateDraft({ otpTokenMinutes: Number.parseInt(value, 10) || 10 })} />
        </div>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={draft.otpEnabled} onChange={(event) => updateDraft({ otpEnabled: event.target.checked })} />
          Enable email one-time password login
        </label>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={draft.mfaEnabled} onChange={(event) => updateDraft({ mfaEnabled: event.target.checked })} />
          Enable TOTP multi-factor auth enrollment
        </label>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={draft.mfaRequired} onChange={(event) => updateDraft({ mfaRequired: event.target.checked })} />
          Mark MFA as required for projects that enforce enrollment in the app UI
        </label>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={draft.emailChangeEnabled} onChange={(event) => updateDraft({ emailChangeEnabled: event.target.checked })} />
          Allow app users to change email after confirmation
        </label>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={draft.emailChangeRequiresPassword} onChange={(event) => updateDraft({ emailChangeRequiresPassword: event.target.checked })} />
          Require current password before requesting email change
        </label>
      </section>
      <section className="pb-settings-block">
        <h2>Email templates</h2>
        <div className="pb-template-grid">
          <details open>
            <summary>Verification email</summary>
            <label className="pb-field">
              <span>Subject</span>
              <input value={draft.templates.verifySubject ?? ""} onChange={(event) => updateTemplate("verifySubject", event.target.value)} />
            </label>
            <label className="pb-field">
              <span>Body</span>
              <textarea value={draft.templates.verifyBody ?? ""} onChange={(event) => updateTemplate("verifyBody", event.target.value)} rows={6} />
            </label>
          </details>
          <details>
            <summary>Password reset email</summary>
            <label className="pb-field">
              <span>Subject</span>
              <input value={draft.templates.resetSubject ?? ""} onChange={(event) => updateTemplate("resetSubject", event.target.value)} />
            </label>
            <label className="pb-field">
              <span>Body</span>
              <textarea value={draft.templates.resetBody ?? ""} onChange={(event) => updateTemplate("resetBody", event.target.value)} rows={6} />
            </label>
          </details>
          <details>
            <summary>One-time password</summary>
            <label className="pb-field">
              <span>Subject</span>
              <input value={draft.templates.otpSubject ?? ""} onChange={(event) => updateTemplate("otpSubject", event.target.value)} />
            </label>
            <label className="pb-field">
              <span>Body</span>
              <textarea value={draft.templates.otpBody ?? ""} onChange={(event) => updateTemplate("otpBody", event.target.value)} rows={6} />
            </label>
          </details>
          <details>
            <summary>Email change</summary>
            <label className="pb-field">
              <span>Subject</span>
              <input value={draft.templates.emailChangeSubject ?? ""} onChange={(event) => updateTemplate("emailChangeSubject", event.target.value)} />
            </label>
            <label className="pb-field">
              <span>Body</span>
              <textarea value={draft.templates.emailChangeBody ?? ""} onChange={(event) => updateTemplate("emailChangeBody", event.target.value)} rows={6} />
            </label>
          </details>
          <details>
            <summary>Organization invitation</summary>
            <label className="pb-field">
              <span>Subject</span>
              <input value={draft.templates.invitationSubject ?? ""} onChange={(event) => updateTemplate("invitationSubject", event.target.value)} />
            </label>
            <label className="pb-field">
              <span>Body</span>
              <textarea value={draft.templates.invitationBody ?? ""} onChange={(event) => updateTemplate("invitationBody", event.target.value)} rows={6} />
            </label>
          </details>
        </div>
        <div className="pb-inline-alert info">Template variables: {"{APP_NAME}"}, {"{PROJECT}"}, {"{EMAIL}"}, {"{NEW_EMAIL}"}, {"{TOKEN}"}, {"{LINK}"}.</div>
        <div className="pb-row-actions">
          <button type="button" className="pb-btn primary" disabled={!project} onClick={() => onSaveAuthSettings(draft)}>
            <Save className="h-4 w-4" />
            Save auth settings
          </button>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Providers and factors</h2>
        <div className="pb-inline-alert info">OAuth providers run through Dublyobase callbacks. Leave the secret field blank to keep a saved secret.</div>
        <div className="pb-provider-grid">
          {oauthProviders.map((provider) => {
            const clientSecret = providerString(provider.id, "clientSecret");
            const secretSaved = providerBool(provider.id, "clientSecretSet");
            return (
              <div key={provider.id} className="pb-provider-tile oauth">
                <div className="pb-provider-tile-head">
                  <ShieldCheck className="h-4 w-4" />
                  <span>
                    <strong>{provider.label}</strong>
                    <em>{`${appUrl}/api/projects/${projectSlug}/auth/oauth/${provider.id}/callback`}</em>
                  </span>
                  <label className="pb-checkline">
                    <input type="checkbox" checked={providerBool(provider.id, "enabled")} onChange={(event) => updateProvider(provider.id, { enabled: event.target.checked })} />
                    Enabled
                  </label>
                </div>
                <div className="pb-grid-form two compact">
                  <LabeledInput label="Client ID" value={providerString(provider.id, "clientId")} onChange={(value) => updateProvider(provider.id, { clientId: value })} />
                  <LabeledInput label={secretSaved ? "Client secret (saved)" : "Client secret"} value={clientSecret} onChange={(value) => updateProvider(provider.id, { clientSecret: value })} />
                  <LabeledInput label="Auth URL" value={providerString(provider.id, "authURL", provider.authURL)} onChange={(value) => updateProvider(provider.id, { authURL: value })} />
                  <LabeledInput label="Token URL" value={providerString(provider.id, "tokenURL", provider.tokenURL)} onChange={(value) => updateProvider(provider.id, { tokenURL: value })} />
                  <LabeledInput label="User info URL" value={providerString(provider.id, "userInfoURL", provider.userInfoURL)} onChange={(value) => updateProvider(provider.id, { userInfoURL: value })} />
                  <LabeledInput label="Scopes" value={providerString(provider.id, "scopes", provider.scopes)} onChange={(value) => updateProvider(provider.id, { scopes: value })} />
                </div>
              </div>
            );
          })}
          {[
            ["One-time password", draft.otpEnabled ? "Enabled for email-code login." : "Disabled for this project."],
            ["Multi-factor auth", draft.mfaEnabled ? "Enabled with TOTP challenges and recovery codes." : "Disabled for this project."],
            ["Email change", draft.emailChangeEnabled ? "Enabled for app users." : "Disabled for app users."],
          ].map(([label, note]) => (
            <div key={label} className="pb-provider-tile disabled">
              <ShieldCheck className="h-4 w-4" />
              <span>
                <strong>{label}</strong>
                <em>{note}</em>
              </span>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

export function QuotasSettingsPanel({
  project,
  projectQuotas,
  quotaDraft,
  setQuotaDraft,
  projectMetrics,
  opsAlerts,
  onSaveQuotas,
  onRefreshMetrics,
  onResolveOpsAlert,
}: {
  project: Project | null;
  projectQuotas: ProjectQuotas | null;
  quotaDraft: typeof emptyQuotaDraft;
  setQuotaDraft: React.Dispatch<React.SetStateAction<typeof emptyQuotaDraft>>;
  projectMetrics: ProjectMetrics | null;
  opsAlerts: OpsAlert[];
  onSaveQuotas: (event: React.FormEvent<HTMLFormElement>) => void;
  onRefreshMetrics: () => void;
  onResolveOpsAlert: (id: string) => void;
}) {
  const quotaEnabled = projectQuotas?.enabled ?? quotaDraft.enabled;
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Project quotas</h2>
        <div className="pb-inline-alert info">Quotas apply to public project APIs. Set any limit to 0 to leave it unlimited.</div>
        <form className="pb-settings-stack" onSubmit={onSaveQuotas}>
          <label className="pb-checkline switchline">
            <input type="checkbox" checked={quotaDraft.enabled} onChange={(event) => setQuotaDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enforce quotas for {project?.slug ?? "selected project"}
          </label>
          <div className="pb-grid-form four">
            <LabeledInput label="API requests / minute" value={quotaDraft.requestsPerMinute} onChange={(value) => setQuotaDraft((draft) => ({ ...draft, requestsPerMinute: value }))} />
            <LabeledInput label="Auth requests / minute" value={quotaDraft.authRequestsPerMinute} onChange={(value) => setQuotaDraft((draft) => ({ ...draft, authRequestsPerMinute: value }))} />
            <LabeledInput label="Max app users" value={quotaDraft.maxAppUsers} onChange={(value) => setQuotaDraft((draft) => ({ ...draft, maxAppUsers: value }))} />
            <LabeledInput label="Max storage MB" value={quotaDraft.maxStorageMb} onChange={(value) => setQuotaDraft((draft) => ({ ...draft, maxStorageMb: value }))} />
          </div>
          <div className="pb-row-actions">
            <button type="submit" className="pb-btn primary" disabled={!project}>
              <Save className="h-4 w-4" />
              Save quotas
            </button>
          </div>
        </form>
      </section>
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <div>
            <h2>Metrics</h2>
            <p className="pb-muted-copy">Last {projectMetrics?.windowHours ?? 24} hours for the selected project.</p>
          </div>
          <button type="button" className="pb-btn secondary" onClick={onRefreshMetrics} disabled={!project}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </button>
        </div>
        <div className="pb-info-grid compact">
          <Info label="Project" value={project?.slug ?? ""} />
          <Info label="Quotas" value={quotaEnabled ? "enabled" : "disabled"} />
          <Info label="App users" value={formatCount(projectMetrics?.appUsers ?? 0)} />
          <Info label="Active sessions" value={formatCount(projectMetrics?.activeSessions ?? 0)} />
          <Info label="Organizations" value={formatCount(projectMetrics?.organizations ?? 0)} />
          <Info label="Storage" value={formatBytes(projectMetrics?.storageBytes ?? 0)} />
          <Info label="Requests" value={formatCount(projectMetrics?.requests.total ?? 0)} />
          <Info label="Errors" value={formatCount(projectMetrics?.requests.errors ?? 0)} />
          <Info label="Avg duration" value={`${Math.round(projectMetrics?.requests.avgDurationMs ?? 0)} ms`} />
          <Info label="P95 duration" value={`${Math.round(projectMetrics?.requests.p95DurationMs ?? 0)} ms`} />
        </div>
        <div className="pb-settings-subsection">
          <h3>Ops alerts</h3>
          {opsAlerts.length ? (
            <div className="pb-list-stack">
              {opsAlerts.map((alert) => (
                <div key={alert.id} className={`pb-inline-alert ${alert.severity === "critical" ? "danger" : "info"}`}>
                  <div>
                    <strong>{alert.code}</strong>
                    <p>{alert.message}</p>
                    <small>{formatDate(alert.createdAt)}</small>
                  </div>
                  <button type="button" className="pb-btn sm secondary" onClick={() => onResolveOpsAlert(alert.id)}>
                    Resolve
                  </button>
                </div>
              ))}
            </div>
          ) : (
            <p className="pb-muted-copy">No active alerts for the selected project.</p>
          )}
        </div>
      </section>
    </div>
  );
}

export function MailSettings({
  settings,
  smtpDraft,
  setSMTPDraft,
  onSaveSMTP,
  onTestSMTP,
}: {
  settings: InstanceSettings | null;
  smtpDraft: typeof emptySMTPDraft;
  setSMTPDraft: React.Dispatch<React.SetStateAction<typeof emptySMTPDraft>>;
  onSaveSMTP: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestSMTP: () => void;
}) {
  return (
    <form onSubmit={onSaveSMTP} className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Mail settings</h2>
        <p className="pb-muted-copy">Configure common settings for sending emails.</p>
        <div className="pb-info-grid compact">
          <Info label="Source" value={settings?.smtp.source ?? ""} />
          <Info label="SMTP" value={smtpDraft.enabled ? "enabled" : "disabled"} />
          <Info label="Saved password" value={settings?.smtp.passwordSet ? "yes" : "no"} />
        </div>
        <div className="pb-grid-form two">
          <LabeledInput label="Sender address" value={smtpDraft.from} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, from: value }))} placeholder="Support <support@example.com>" />
          <label className="pb-checkline switchline">
            <input type="checkbox" checked={smtpDraft.enabled} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Use SMTP mail server <strong>(recommended)</strong>
          </label>
        </div>
        {smtpDraft.enabled ? (
          <div className="pb-smtp-card">
            <div className="pb-grid-form smtp-grid">
              <LabeledInput label="SMTP server host" value={smtpDraft.host} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, host: value }))} placeholder="smtp.example.com" />
              <LabeledInput label="Port" value={smtpDraft.port} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, port: value }))} placeholder="587" />
              <LabeledInput label="Username" value={smtpDraft.username} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, username: value }))} />
              <label className="pb-field">
                <span>Password {settings?.smtp.passwordSet ? <em>(saved)</em> : null}</span>
                <input type="password" value={smtpDraft.password} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, password: event.target.value, clearPassword: false }))} placeholder={settings?.smtp.passwordSet ? "* * * * * *" : ""} autoComplete="new-password" />
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={smtpDraft.clearPassword} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, clearPassword: event.target.checked, password: "" }))} />
                Clear password
              </label>
            </div>
          </div>
        ) : null}
      </section>
      <section className="pb-settings-actions">
        <label className="pb-field test-recipient">
          <span>Test recipient</span>
          <input value={smtpDraft.testTo} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, testTo: event.target.value }))} placeholder="you@example.com" />
        </label>
        <button type="button" onClick={onTestSMTP} className="pb-btn outline expanded-lg">
          <Mail className="h-4 w-4" />
          Send test email
        </button>
        <button type="submit" className="pb-btn primary expanded-lg">
          Save changes
        </button>
      </section>
    </form>
  );
}

export function StorageSettingsPanel({
  settings,
  storageDraft,
  setStorageDraft,
  onSaveStorage,
  onTestStorage,
}: {
  settings: InstanceSettings | null;
  storageDraft: typeof emptyStorageDraft;
  setStorageDraft: React.Dispatch<React.SetStateAction<typeof emptyStorageDraft>>;
  onSaveStorage: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestStorage: () => void;
}) {
  const s3Enabled = storageDraft.type === "s3";
  return (
    <form onSubmit={onSaveStorage} className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>File storage</h2>
        <p className="pb-muted-copy">By default Dublyobase uses and recommends the local file system to store uploaded files because it is faster to manage and backup.</p>
        <p className="pb-muted-copy">Alternatively, if you have limited disk space available, you could opt to an S3 compatible external storage.</p>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={s3Enabled} onChange={(event) => setStorageDraft((draft) => ({ ...draft, type: event.target.checked ? "s3" : "local" }))} />
          Use S3 storage
        </label>
        <div className="pb-info-grid compact">
          <Info label="Source" value={settings?.storage.source ?? ""} />
          <Info label="Local path" value={settings?.storage.localPath ?? ""} />
        </div>
        {s3Enabled ? (
          <>
            <div className="pb-inline-alert info">
              If you have existing uploaded files, migrate them manually from local storage to S3 storage. Useful tools include{" "}
              <a href="https://github.com/rclone/rclone" target="_blank" rel="noreferrer">
                rclone
              </a>{" "}
              and{" "}
              <a href="https://github.com/peak/s5cmd" target="_blank" rel="noreferrer">
                s5cmd
              </a>
              .
            </div>
            <div className="pb-provider-presets" aria-label="S3-compatible provider presets">
              {([
                ["Cloudflare R2", { region: "auto", forcePathStyle: true }],
                ["Backblaze B2", { region: storageDraft.region || "us-east-005", forcePathStyle: true }],
                ["AWS S3", { region: storageDraft.region || "us-east-1", forcePathStyle: false }],
                ["MinIO", { region: storageDraft.region || "us-east-1", forcePathStyle: true }],
              ] as const).map(([label, preset]) => (
                <button key={label} type="button" onClick={() => setStorageDraft((draft) => ({ ...draft, ...preset, useSSL: true }))}>
                  {label}
                </button>
              ))}
            </div>
            <div className="pb-grid-form s3-grid">
              <LabeledInput label="Endpoint" value={storageDraft.endpoint} onChange={(value) => setStorageDraft((draft) => ({ ...draft, endpoint: value }))} placeholder="https://s3.example.com" />
              <LabeledInput label="Bucket" value={storageDraft.bucket} onChange={(value) => setStorageDraft((draft) => ({ ...draft, bucket: value }))} placeholder="dublyobase" />
              <LabeledInput label="Region" value={storageDraft.region} onChange={(value) => setStorageDraft((draft) => ({ ...draft, region: value }))} placeholder="auto" />
              <LabeledInput label="Prefix" value={storageDraft.prefix} onChange={(value) => setStorageDraft((draft) => ({ ...draft, prefix: value }))} placeholder="prod" />
              <LabeledInput label="Access key" value={storageDraft.accessKey} onChange={(value) => setStorageDraft((draft) => ({ ...draft, accessKey: value }))} />
              <label className="pb-field">
                <span>Secret {settings?.storage.s3.secretKeySet ? <em>(saved)</em> : null}</span>
                <input type="password" value={storageDraft.secretKey} onChange={(event) => setStorageDraft((draft) => ({ ...draft, secretKey: event.target.value, clearSecretKey: false }))} placeholder={settings?.storage.s3.secretKeySet ? "* * * * * *" : ""} />
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.forcePathStyle} onChange={(event) => setStorageDraft((draft) => ({ ...draft, forcePathStyle: event.target.checked }))} />
                Force path-style addressing
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.useSSL} onChange={(event) => setStorageDraft((draft) => ({ ...draft, useSSL: event.target.checked }))} />
                HTTPS
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.clearSecretKey} onChange={(event) => setStorageDraft((draft) => ({ ...draft, clearSecretKey: event.target.checked, secretKey: "" }))} />
                Clear secret
              </label>
            </div>
          </>
        ) : null}
      </section>
      <section className="pb-settings-actions">
        <button type="button" onClick={onTestStorage} className="pb-btn outline expanded-lg">
          Test storage
        </button>
        <button type="submit" className="pb-btn primary expanded-lg">
          Save changes
        </button>
      </section>
    </form>
  );
}

export function CORSSettingsPanel({
  project,
  settings,
  corsDraft,
  setCORSDraft,
  onSaveCORS,
}: {
  project: Project | null;
  settings: InstanceSettings | null;
  corsDraft: typeof emptyCORSDraft;
  setCORSDraft: React.Dispatch<React.SetStateAction<typeof emptyCORSDraft>>;
  onSaveCORS: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <form onSubmit={onSaveCORS} className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>CORS origins</h2>
        <div className="pb-inline-alert warning">
          `*` allows any website to call browser-exposed APIs. Use explicit origins unless this instance is intentionally public.
        </div>
        <div className="pb-info-grid compact">
          <Info label="Admin source" value={settings?.cors.source ?? ""} />
          <Info label="Admin wildcard" value={settings?.cors.wildcard ? "yes" : "no"} />
          <Info label="Project" value={project?.slug ?? "No project selected"} />
          <Info label="Public API source" value={project?.cors?.source ?? ""} />
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Admin panel</h2>
        <label className="pb-field">
          <span>Allowed admin origins</span>
          <textarea
            value={corsDraft.adminOrigins}
            onChange={(event) => setCORSDraft((draft) => ({ ...draft, adminOrigins: event.target.value }))}
            placeholder="https://app.example.com"
            rows={5}
          />
        </label>
        <label className="pb-checkline">
          <input type="checkbox" checked={corsDraft.allowAdminWildcard} onChange={(event) => setCORSDraft((draft) => ({ ...draft, allowAdminWildcard: event.target.checked }))} />
          Allow `*` for admin origins
        </label>
      </section>
      <section className="pb-settings-block">
        <h2>Public project API</h2>
        {project ? (
          <>
            <label className="pb-field">
              <span>Allowed public API origins</span>
              <textarea
                value={corsDraft.publicOrigins}
                onChange={(event) => setCORSDraft((draft) => ({ ...draft, publicOrigins: event.target.value }))}
                placeholder="https://www.example.com"
                rows={5}
              />
            </label>
            <label className="pb-checkline">
              <input type="checkbox" checked={corsDraft.allowPublicWildcard} onChange={(event) => setCORSDraft((draft) => ({ ...draft, allowPublicWildcard: event.target.checked }))} />
              Allow `*` for this project API
            </label>
          </>
        ) : (
          <EmptyState label="Select a project to edit public API CORS." />
        )}
      </section>
      <section className="pb-settings-actions">
        <button type="submit" className="pb-btn primary expanded-lg">
          Save CORS
        </button>
      </section>
    </form>
  );
}

export function AdminUsersPanel({
  admin,
  adminUsers,
  adminDraft,
  setAdminDraft,
  oneTimeAdmin,
  onCreateAdmin,
  onDismissAdminSecret,
}: {
  admin: Admin | null;
  adminUsers: Admin[];
  adminDraft: typeof emptyAdminDraft;
  setAdminDraft: React.Dispatch<React.SetStateAction<typeof emptyAdminDraft>>;
  oneTimeAdmin: { email: string; password: string } | null;
  onCreateAdmin: (event: React.FormEvent<HTMLFormElement>) => void;
  onDismissAdminSecret: () => void;
}) {
  const isOwner = admin?.role === "owner";
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Admin users</h2>
        <div className="pb-inline-alert info">
          The first admin is the owner. Owner can create super admins with a temporary password; new super admins must change it on first login.
        </div>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Email</th>
                <th>Role</th>
                <th>Status</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {adminUsers.map((item) => (
                <tr key={item.id}>
                  <td>{item.email}</td>
                  <td>{item.role === "owner" ? "Owner" : "Super admin"}</td>
                  <td>{item.mustChangePassword ? "Must change password" : "Active"}</td>
                  <td>{formatDate(item.createdAt ?? "")}</td>
                </tr>
              ))}
              {adminUsers.length === 0 ? (
                <tr>
                  <td colSpan={4} className="pb-empty-cell">
                    <EmptyState label="No admin users found." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
      {oneTimeAdmin ? (
        <section className="pb-settings-block">
          <h2>Temporary password</h2>
          <div className="pb-inline-alert warning">This password is shown once. Share it with {oneTimeAdmin.email}, then they must change it after login.</div>
          <pre className="pb-code-box">{oneTimeAdmin.password}</pre>
          <button type="button" className="pb-btn secondary" onClick={onDismissAdminSecret}>
            Dismiss
          </button>
        </section>
      ) : null}
      <form onSubmit={onCreateAdmin} className="pb-settings-block">
        <h2>Create super admin</h2>
        {!isOwner ? <div className="pb-inline-alert warning">Only the owner can create super admins.</div> : null}
        <div className="pb-grid-form two">
          <LabeledInput label="Email" value={adminDraft.email} onChange={(value) => setAdminDraft((draft) => ({ ...draft, email: value }))} placeholder="admin@example.com" />
          <label className="pb-field">
            <span>Temporary password</span>
            <input
              type="password"
              value={adminDraft.temporaryPassword}
              onChange={(event) => setAdminDraft((draft) => ({ ...draft, temporaryPassword: event.target.value }))}
              minLength={12}
              autoComplete="new-password"
            />
          </label>
          <button type="submit" className="pb-btn primary" disabled={!isOwner}>
            <Plus className="h-4 w-4" />
            Create super admin
          </button>
        </div>
      </form>
    </div>
  );
}
