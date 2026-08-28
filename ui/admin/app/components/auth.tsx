"use client";

import { login } from "../../src/lib/api";
import type { Admin, Health } from "../../src/lib/types";
import type { Notice } from "../lib/view-types";
import { Info } from "./ui";
import { Eye, EyeOff, LogOut, RefreshCw, Save, ShieldCheck, X } from "lucide-react";
import { useState } from "react";

export function AuthScreen({
  busy,
  healthState,
  notice,
  onLogin,
}: {
  busy: boolean;
  healthState: Health | null;
  notice: Notice;
  onLogin: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  const [showPassword, setShowPassword] = useState(false);
  return (
    <main className="pb-login-screen">
      <section className="pb-login-card" aria-labelledby="login-title">
        <div className="pb-login-logo">
          <img className="pb-login-mark" src="/dublyobase-logo.png" alt="" aria-hidden="true" />
        </div>
        <h1 id="login-title">Superuser login</h1>
        {notice ? (
          <div className={`pb-inline-alert ${notice.type === "error" ? "danger" : "success"}`}>
            {notice.message}
          </div>
        ) : null}
        <form onSubmit={onLogin} className="pb-form-stack">
          <label className="pb-field">
            <span>Email</span>
            <input id="login_identity" name="email" type="email" autoComplete="username" required />
          </label>
          <label className="pb-field password-field">
            <span>Password</span>
            <input id="login_pass" name="password" type={showPassword ? "text" : "password"} autoComplete="current-password" required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? "Hide password" : "Show password"}>
              {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <details className="pb-link-hint">
            <summary>Forgotten password</summary>
            <p>
              Admin passwords are reset from the server, not by email — recovery requires access to
              the deployment rather than to an inbox. Run:
            </p>
            <code>dublyobase admin reset-password --email you@example.com</code>
            <p>
              It prints a one-time password, revokes that admin&rsquo;s sessions, and records the
              reset in the audit log.
            </p>
          </details>
          <button type="submit" disabled={busy} className="pb-btn primary lg block">
            {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
            Login
          </button>
        </form>
        <div className="pb-login-status">
          <span>DB {healthState?.db ?? "checking"}</span>
          <span>Storage {healthState?.storage ?? "checking"}</span>
          <span>{healthState?.version ?? "unknown"}</span>
        </div>
      </section>
    </main>
  );
}

export function PasswordChangeScreen({
  admin,
  busy,
  healthState,
  notice,
  onSubmit,
  onLogout,
}: {
  admin: Admin;
  busy: boolean;
  healthState: Health | null;
  notice: Notice;
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onLogout: () => void;
}) {
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  return (
    <main className="pb-login-screen">
      <section className="pb-login-card" aria-labelledby="password-change-title">
        <div className="pb-login-logo">
          <img className="pb-login-mark" src="/dublyobase-logo.png" alt="" aria-hidden="true" />
        </div>
        <h1 id="password-change-title">Change admin password</h1>
        <p className="pb-muted-copy">Signed in as {admin.email}. Set a new password before opening the control panel.</p>
        {notice ? (
          <div className={`pb-inline-alert ${notice.type === "error" ? "danger" : "success"}`}>
            {notice.message}
          </div>
        ) : null}
        <form onSubmit={onSubmit} className="pb-form-stack">
          <label className="pb-field password-field">
            <span>Current password</span>
            <input name="currentPassword" type={showCurrent ? "text" : "password"} autoComplete="current-password" required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowCurrent((value) => !value)} aria-label={showCurrent ? "Hide current password" : "Show current password"}>
              {showCurrent ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field password-field">
            <span>New password</span>
            <input name="newPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowNew((value) => !value)} aria-label={showNew ? "Hide new password" : "Show new password"}>
              {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field">
            <span>Confirm new password</span>
            <input name="confirmPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
          </label>
          <button type="submit" disabled={busy} className="pb-btn primary lg block">
            {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            Save password
          </button>
          <button type="button" onClick={onLogout} disabled={busy} className="pb-btn secondary block">
            <LogOut className="h-4 w-4" />
            Log out
          </button>
        </form>
        <div className="pb-login-status">
          <span>DB {healthState?.db ?? "checking"}</span>
          <span>Storage {healthState?.storage ?? "checking"}</span>
          <span>{healthState?.version ?? "unknown"}</span>
        </div>
      </section>
    </main>
  );
}

export function AccountModal({
  admin,
  busy,
  onEmailSubmit,
  onPasswordSubmit,
  onClose,
  onLogout,
}: {
  admin: Admin;
  busy: boolean;
  onEmailSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onPasswordSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onClose: () => void;
  onLogout: () => void;
}) {
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  const [showEmailPassword, setShowEmailPassword] = useState(false);
  return (
    <div className="pb-modal-layer" role="presentation">
      <section className="pb-modal account-modal" role="dialog" aria-modal="true" aria-labelledby="account-title">
        <header className="pb-modal-header">
          <h2 id="account-title">Account</h2>
          <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close account settings">
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="pb-modal-content account-form">
          <div className="pb-info-grid compact">
            <Info label="Signed in as" value={admin.email} />
            <Info label="Role" value={admin.role === "owner" ? "Owner" : "Super admin"} />
          </div>
          <form onSubmit={onEmailSubmit} className="pb-form-stack">
            <h3>Change email</h3>
            <label className="pb-field">
              <span>Email</span>
              <input name="email" type="email" defaultValue={admin.email} autoComplete="username" required />
            </label>
            <label className="pb-field password-field">
              <span>Current password</span>
              <input name="emailCurrentPassword" type={showEmailPassword ? "text" : "password"} autoComplete="current-password" required />
              <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowEmailPassword((value) => !value)} aria-label={showEmailPassword ? "Hide current password" : "Show current password"}>
                {showEmailPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </label>
            <button type="submit" disabled={busy} className="pb-btn secondary expanded-lg">
              <Save className="h-4 w-4" />
              Save email
            </button>
          </form>
          <form onSubmit={onPasswordSubmit} className="pb-form-stack">
            <h3>Change password</h3>
            <label className="pb-field password-field">
              <span>Current password</span>
              <input name="currentPassword" type={showCurrent ? "text" : "password"} autoComplete="current-password" required />
              <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowCurrent((value) => !value)} aria-label={showCurrent ? "Hide current password" : "Show current password"}>
                {showCurrent ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </label>
            <label className="pb-field password-field">
              <span>New password</span>
              <input name="newPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
              <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowNew((value) => !value)} aria-label={showNew ? "Hide new password" : "Show new password"}>
                {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </label>
            <label className="pb-field">
              <span>Confirm new password</span>
              <input name="confirmPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
            </label>
            <button type="submit" disabled={busy} className="pb-btn primary expanded-lg">
              {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              Save password
            </button>
          </form>
          <div className="account-actions">
            <button type="button" onClick={onLogout} disabled={busy} className="pb-btn secondary">
              <LogOut className="h-4 w-4" />
              Log out
            </button>
          </div>
        </div>
      </section>
    </div>
  );
}
