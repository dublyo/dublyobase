"use client";

import { backupDownloadURL } from "../../src/lib/api";
import type { APIKey, Admin, BackupJob, BackupRun, Collection, CollectionExport, CollectionImportResult, CronJob, CronRun, DiscoveredTable, Field, MCPToken, Project, RecordItem, RecordList, RestoreJob, SQLResult, Webhook, WebhookDelivery } from "../../src/lib/types";
import { discoveredTableKey, schemaImportNameIssues, tableRelationSummary } from "../lib/collections";
import { emptyBackupDraft, emptyCronDraft, emptyMCPDraft, emptyWebhookDraft } from "../lib/constants";
import { findFiles, formatBytes, formatDate } from "../lib/format";
import { CompactTable, EmptyState, Info, LabeledInput, RunHistory, SQLResultTable, SchemaStatusChip } from "./ui";
import { Code2, Copy, Download, FileUp, KeyRound, Plus, RefreshCw, Trash2, Type, UploadCloud, X } from "lucide-react";
import { useRef, useState } from "react";

export function BackupsView(props: {
  project: Project | null;
  projects: Project[];
  backupJobs: BackupJob[];
  backupRuns: Record<string, BackupRun[]>;
  backupDraft: typeof emptyBackupDraft;
  setBackupDraft: React.Dispatch<React.SetStateAction<typeof emptyBackupDraft>>;
  onCreateBackup: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunBackup: (job: BackupJob) => void;
  onLoadBackupRuns: (job: BackupJob) => void;
  restoreFile: File | null;
  setRestoreFile: React.Dispatch<React.SetStateAction<File | null>>;
  restoreMode: "dry_run" | "restore";
  setRestoreMode: React.Dispatch<React.SetStateAction<"dry_run" | "restore">>;
  restoreConfirm: string;
  setRestoreConfirm: React.Dispatch<React.SetStateAction<string>>;
  restoreResult: RestoreJob | null;
  onSubmitRestore: (event: React.FormEvent<HTMLFormElement>) => void;
  token: string;
  onLoadCollectionExport: () => void;
  onOpenExport: () => void;
}) {
  const schema = props.project?.schemaName ?? "proj_app";
  async function downloadBackupRun(job: BackupJob, run: BackupRun) {
    const response = await fetch(backupDownloadURL(job.id, run.id), {
      headers: { Authorization: `Bearer ${props.token}` },
    });
    if (!response.ok) return;
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = run.storageKey.split("/").pop() || `${run.id}.dump`;
    a.click();
    URL.revokeObjectURL(url);
  }
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Backups</h2>
        <div className="pb-inline-alert info">Backups run with pg_dump and are stored in the configured file storage, including S3-compatible targets.</div>
        <div className="pb-info-grid compact">
          <Info label="Project schema" value={schema} />
          <Info label="Backup engine" value="pg_dump custom format" />
        </div>
        <div className="pb-row-actions">
          <button type="button" className="pb-btn secondary" onClick={props.onLoadCollectionExport}>
            <FileUp className="h-4 w-4" />
            Export schema JSON
          </button>
          <button type="button" className="pb-btn outline" onClick={props.onOpenExport}>
            Open export
          </button>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create backup job</h2>
        <form onSubmit={props.onCreateBackup} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.backupDraft.name} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, name: value }))} placeholder="nightly project backup" />
          <label className="pb-field">
            <span>Scope</span>
            <select value={props.backupDraft.scope} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, scope: event.target.value as "full" | "project" }))}>
              <option value="project">Project</option>
              <option value="full">Full database</option>
            </select>
          </label>
          {props.backupDraft.scope === "project" ? (
            <label className="pb-field">
              <span>Project</span>
              <select value={props.backupDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
                <option value="">Select project</option>
                {props.projects.map((project) => (
                  <option key={project.id} value={project.slug}>
                    {project.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <LabeledInput label="Schedule" value={props.backupDraft.schedule} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, schedule: value }))} placeholder="0 2 * * *" />
          <LabeledInput label="Timezone" value={props.backupDraft.timezone} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, timezone: value }))} placeholder="UTC" />
          <LabeledInput label="Retention days" value={props.backupDraft.retentionDays} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, retentionDays: value }))} />
          <LabeledInput label="Retention count" value={props.backupDraft.retentionCount} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, retentionCount: value }))} />
          <label className="pb-checkline">
            <input type="checkbox" checked={props.backupDraft.enabled} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enabled
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create backup
          </button>
        </form>
      </section>
      <section className="pb-settings-block">
        <h2>Backup jobs</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Scope</th>
                <th>Schedule</th>
                <th>Next run</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.backupJobs.map((job) => {
                const latest = props.backupRuns[job.id]?.[0];
                return (
                  <tr key={job.id}>
                    <td>{job.name}</td>
                    <td>{job.scope === "project" ? job.projectSlug || "project" : "full"}</td>
                    <td>{job.schedule}</td>
                    <td>{formatDate(job.nextRunAt ?? "")}</td>
                    <td>{latest ? `${latest.status}${latest.storageKey ? ` · ${formatBytes(latest.sizeBytes)}` : ""}` : job.enabled ? "enabled" : "paused"}</td>
                    <td>
                      <div className="pb-row-actions tight">
                        <button type="button" className="pb-btn sm secondary" onClick={() => props.onLoadBackupRuns(job)}>
                          Runs
                        </button>
                        <button type="button" className="pb-btn sm primary" onClick={() => props.onRunBackup(job)}>
                          Run
                        </button>
                        {latest?.status === "success" ? (
                          <button type="button" className="pb-btn sm outline" onClick={() => void downloadBackupRun(job, latest)}>
                            <Download className="h-3.5 w-3.5" />
                          </button>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                );
              })}
              {props.backupJobs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No backup jobs yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <RunHistory runs={Object.values(props.backupRuns).flat()} />
      </section>
      <section className="pb-settings-block">
        <h2>Restore / dry-run</h2>
        <div className="pb-inline-alert warning">Dry-run lists a custom-format pg_dump archive. Restore is destructive and requires confirmation.</div>
        <form className="pb-grid-form two" onSubmit={props.onSubmitRestore}>
          <label className="pb-field">
            <span>Backup file</span>
            <input type="file" accept=".dump,.backup,.sql" onChange={(event) => props.setRestoreFile(event.target.files?.[0] ?? null)} />
          </label>
          <label className="pb-field">
            <span>Mode</span>
            <select value={props.restoreMode} onChange={(event) => props.setRestoreMode(event.target.value as "dry_run" | "restore")}>
              <option value="dry_run">Dry run</option>
              <option value="restore">Restore database</option>
            </select>
          </label>
          {props.restoreMode === "restore" ? (
            <LabeledInput label="Confirmation" value={props.restoreConfirm} onChange={props.setRestoreConfirm} placeholder="RESTORE_DATABASE" />
          ) : null}
          <button type="submit" className="pb-btn primary" disabled={!props.restoreFile}>
            <UploadCloud className="h-4 w-4" />
            {props.restoreMode === "restore" ? "Restore" : "Dry run"}
          </button>
        </form>
        {props.restoreResult ? (
          <pre className="pb-code-box">{props.restoreResult.output || props.restoreResult.error || props.restoreResult.status}</pre>
        ) : null}
      </section>
    </div>
  );
}

export function CronsView(props: {
  projects: Project[];
  project: Project | null;
  cronJobs: CronJob[];
  cronRuns: Record<string, CronRun[]>;
  cronDraft: typeof emptyCronDraft;
  setCronDraft: React.Dispatch<React.SetStateAction<typeof emptyCronDraft>>;
  onCreateCron: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunCron: (job: CronJob) => void;
  onLoadCronRuns: (job: CronJob) => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Crons</h2>
        <div className="pb-inline-alert info">HTTP jobs support cron expressions and @every durations. Retries and response snippets are stored in the run log.</div>
        <form onSubmit={props.onCreateCron} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.cronDraft.name} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, name: value }))} placeholder="refresh cache" />
          <label className="pb-field">
            <span>Project</span>
            <select value={props.cronDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
              <option value="">Global</option>
              {props.projects.map((project) => (
                <option key={project.id} value={project.slug}>
                  {project.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Schedule" value={props.cronDraft.schedule} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, schedule: value }))} placeholder="@every 5m" />
          <LabeledInput label="Timezone" value={props.cronDraft.timezone} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, timezone: value }))} placeholder="UTC" />
          <label className="pb-field">
            <span>Method</span>
            <select value={props.cronDraft.method} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, method: event.target.value }))}>
              {["GET", "POST", "PUT", "PATCH", "DELETE"].map((method) => (
                <option key={method} value={method}>
                  {method}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="URL" value={props.cronDraft.url} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, url: value }))} placeholder="https://example.com/api/job" />
          <LabeledInput label="Timeout seconds" value={props.cronDraft.timeoutSeconds} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, timeoutSeconds: value }))} />
          <LabeledInput label="Retries" value={props.cronDraft.retryCount} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, retryCount: value }))} />
          <label className="pb-field wide-field">
            <span>Headers JSON</span>
            <textarea value={props.cronDraft.headersJSON} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, headersJSON: event.target.value }))} rows={4} />
          </label>
          <label className="pb-field wide-field">
            <span>Body</span>
            <textarea value={props.cronDraft.body} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, body: event.target.value }))} rows={4} />
          </label>
          <label className="pb-checkline">
            <input type="checkbox" checked={props.cronDraft.enabled} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enabled
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create cron
          </button>
        </form>
      </section>
      <section className="pb-settings-block">
        <h2>Cron jobs</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Method</th>
                <th>Schedule</th>
                <th>Next run</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.cronJobs.map((job) => {
                const latest = props.cronRuns[job.id]?.[0];
                return (
                  <tr key={job.id}>
                    <td>{job.name}</td>
                    <td>{job.method}</td>
                    <td>{job.schedule}</td>
                    <td>{formatDate(job.nextRunAt ?? "")}</td>
                    <td>{latest ? `${latest.status}${latest.statusCode ? ` · ${latest.statusCode}` : ""}` : job.enabled ? "enabled" : "paused"}</td>
                    <td>
                      <div className="pb-row-actions tight">
                        <button type="button" className="pb-btn sm secondary" onClick={() => props.onLoadCronRuns(job)}>
                          Runs
                        </button>
                        <button type="button" className="pb-btn sm primary" onClick={() => props.onRunCron(job)}>
                          Run
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {props.cronJobs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No cron jobs registered." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <RunHistory runs={Object.values(props.cronRuns).flat()} />
      </section>
    </div>
  );
}

export function MCPAccessView(props: {
  project: Project | null;
  projects: Project[];
  mcpTokens: MCPToken[];
  oneTimeMCPToken: MCPToken | null;
  mcpDraft: typeof emptyMCPDraft;
  setMCPDraft: React.Dispatch<React.SetStateAction<typeof emptyMCPDraft>>;
  onCreateMCPToken: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeMCPToken: (token: MCPToken) => void;
  onDismissMCPSecret: () => void;
  onCopy: (text: string) => void;
}) {
  const endpoint = typeof window !== "undefined" ? `${window.location.origin}/mcp` : "/mcp";
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>MCP access</h2>
        <div className="pb-inline-alert info">Create scoped tokens for AI tools. Admin tokens can manage the instance; project tokens are restricted to one project and their allowlist.</div>
        <div className="pb-info-grid compact">
          <Info label="Remote endpoint" value={endpoint} />
          <Info label="Protocol" value="HTTP JSON-RPC MCP" />
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create MCP token</h2>
        <form onSubmit={props.onCreateMCPToken} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.mcpDraft.name} onChange={(value) => props.setMCPDraft((draft) => ({ ...draft, name: value }))} placeholder="codex project agent" />
          <label className="pb-field">
            <span>Scope</span>
            <select value={props.mcpDraft.scope} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, scope: event.target.value as "admin" | "project" }))}>
              <option value="project">Project</option>
              <option value="admin">Admin</option>
            </select>
          </label>
          {props.mcpDraft.scope === "project" ? (
            <label className="pb-field">
              <span>Project</span>
              <select value={props.mcpDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
                <option value="">Select project</option>
                {props.projects.map((project) => (
                  <option key={project.id} value={project.slug}>
                    {project.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label className="pb-field">
            <span>Expires at</span>
            <input type="datetime-local" value={props.mcpDraft.expiresAt} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, expiresAt: event.target.value }))} />
          </label>
          <label className="pb-field wide-field">
            <span>Allowed tools</span>
            <textarea value={props.mcpDraft.allowedTools} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, allowedTools: event.target.value }))} rows={5} placeholder="Leave empty for safe defaults, or enter comma/newline separated tool names." />
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create token
          </button>
        </form>
        {props.oneTimeMCPToken?.token ? (
          <div className="pb-secret-box">
            <strong>Copy this token now. It will not be shown again.</strong>
            <code>{props.oneTimeMCPToken.token}</code>
            <div className="pb-row-actions">
              <button type="button" className="pb-btn secondary" onClick={() => props.onCopy(props.oneTimeMCPToken?.token ?? "")}>
                <Copy className="h-4 w-4" />
                Copy token
              </button>
              <button type="button" className="pb-btn transparent" onClick={props.onDismissMCPSecret}>
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </section>
      <section className="pb-settings-block">
        <h2>Tokens</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Scope</th>
                <th>Prefix</th>
                <th>Tools</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.mcpTokens.map((token) => (
                <tr key={token.id}>
                  <td>{token.name}</td>
                  <td>{token.scope === "project" ? token.projectSlug || "project" : "admin"}</td>
                  <td>{token.prefix}</td>
                  <td>{token.allowedTools.length ? token.allowedTools.join(", ") : "default"}</td>
                  <td>{token.revokedAt ? "revoked" : token.expiresAt ? `expires ${formatDate(token.expiresAt)}` : "active"}</td>
                  <td>
                    <button type="button" className="pb-btn sm transparent danger" onClick={() => props.onRevokeMCPToken(token)} disabled={Boolean(token.revokedAt)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
              {props.mcpTokens.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No MCP tokens yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

export function WebhooksView(props: {
  webhooks: Webhook[];
  webhookDraft: typeof emptyWebhookDraft;
  setWebhookDraft: React.Dispatch<React.SetStateAction<typeof emptyWebhookDraft>>;
  webhookDeliveries: Record<string, WebhookDelivery[]>;
  onCreateWebhook: (event: React.FormEvent<HTMLFormElement>) => void;
  onDeleteWebhook: (hook: Webhook) => void;
  onLoadWebhookDeliveries: (hook: Webhook) => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Webhooks</h2>
        <div className="pb-inline-alert info">Send signed HTTP POST deliveries for record create, update and delete events. Events can be records.*, records.create, or collection.create.</div>
        <form onSubmit={props.onCreateWebhook} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.webhookDraft.name} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, name: value }))} placeholder="frontend cache purge" />
          <LabeledInput label="URL" value={props.webhookDraft.url} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, url: value }))} placeholder="https://example.com/webhooks/dublyobase" />
          <LabeledInput label="Events" value={props.webhookDraft.events} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, events: value }))} placeholder="records.*" />
          <LabeledInput label="Timeout seconds" value={props.webhookDraft.timeoutSeconds} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, timeoutSeconds: value }))} />
          <LabeledInput label="Max attempts" value={props.webhookDraft.maxAttempts} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, maxAttempts: value }))} />
          <LabeledInput label="Secret" value={props.webhookDraft.secret} onChange={(value) => props.setWebhookDraft((draft) => ({ ...draft, secret: value }))} placeholder="auto-generated if blank" />
          <label className="pb-checkline">
            <input type="checkbox" checked={props.webhookDraft.enabled} onChange={(event) => props.setWebhookDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enabled
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create webhook
          </button>
        </form>
      </section>
      <section className="pb-settings-block">
        <h2>Configured webhooks</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>URL</th>
                <th>Events</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.webhooks.map((hook) => (
                <tr key={hook.id}>
                  <td>{hook.name}</td>
                  <td className="truncate-cell">{hook.url}</td>
                  <td>{hook.events.join(", ")}</td>
                  <td>{hook.enabled ? "enabled" : "paused"}</td>
                  <td>
                    <div className="pb-row-actions tight">
                      <button type="button" className="pb-btn sm secondary" onClick={() => props.onLoadWebhookDeliveries(hook)}>
                        Deliveries
                      </button>
                      <button type="button" className="pb-btn sm danger" onClick={() => props.onDeleteWebhook(hook)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {props.webhooks.length === 0 ? (
                <tr>
                  <td colSpan={5} className="pb-empty-cell">
                    <EmptyState label="No webhooks configured." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        {props.webhooks.map((hook) => {
          const deliveries = props.webhookDeliveries[hook.id] ?? [];
          return deliveries.length ? (
            <div key={hook.id} className="pb-run-history">
              <h3>{hook.name} deliveries</h3>
              <CompactTable
                headers={["Event", "Status", "Attempts", "HTTP", "Created"]}
                rows={deliveries.map((item) => [item.event, item.status, String(item.attempts), item.lastStatusCode ? String(item.lastStatusCode) : "-", formatDate(item.createdAt)])}
                empty="No deliveries."
              />
            </div>
          ) : null;
        })}
      </section>
    </div>
  );
}

export function ExportCollectionsView(props: {
  collectionExport: CollectionExport | null;
  exportSelection: string[];
  selectedExportItems: CollectionExport["items"];
  exportPreview: string;
  onLoadCollectionExport: () => void;
  onToggleExportCollection: (name: string) => void;
  onCopyExport: () => void;
  onDownloadExport: () => void;
  onUseExportForImport: () => void;
}) {
  const items = props.collectionExport?.items ?? [];
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Export collections</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn outline" onClick={props.onLoadCollectionExport}>
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onCopyExport} disabled={items.length === 0}>
              <Copy className="h-4 w-4" />
              Copy
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onDownloadExport} disabled={items.length === 0}>
              <Download className="h-4 w-4" />
              Download JSON
            </button>
          </div>
        </div>
        <div className="pb-sync-grid">
          <div className="pb-sync-list" aria-label="Collections to export">
            {items.map((item) => (
              <label key={item.name} className="pb-sync-item">
                <input type="checkbox" checked={props.exportSelection.includes(item.name)} onChange={() => props.onToggleExportCollection(item.name)} />
                <span>
                  <strong>{item.name}</strong>
                  <em>{item.system ? "system" : item.type}</em>
                </span>
              </label>
            ))}
            {items.length === 0 ? <EmptyState label="Load collection schema from the selected project." action="Load schema" onAction={props.onLoadCollectionExport} /> : null}
          </div>
          <div className="pb-sync-preview">
            <div className="pb-sync-preview-bar">
              <span>{props.selectedExportItems.length} selected</span>
              <button type="button" className="pb-btn sm primary" onClick={props.onUseExportForImport} disabled={props.selectedExportItems.length === 0}>
                Use for import
              </button>
            </div>
            <pre className="pb-code-box schema-preview">{props.exportPreview}</pre>
          </div>
        </div>
      </section>
    </div>
  );
}

export function ImportCollectionsView(props: {
  importJSON: string;
  setImportJSON: React.Dispatch<React.SetStateAction<string>>;
  importMode: "create_missing" | "upsert";
  setImportMode: React.Dispatch<React.SetStateAction<"create_missing" | "upsert">>;
  importDropMissingFields: boolean;
  setImportDropMissingFields: React.Dispatch<React.SetStateAction<boolean>>;
  importResult: CollectionImportResult | null;
  onPreviewImport: () => void;
  onApplyImport: () => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Import collections</h2>
        <label className="pb-field">
          <span>Collections JSON</span>
          <textarea className="mono import-json-editor" value={props.importJSON} onChange={(event) => props.setImportJSON(event.target.value)} placeholder='{"items":[{"name":"posts","type":"base","fields":[]}]}' />
        </label>
        <div className="pb-grid-form import-options">
          <label className="pb-field">
            <span>Mode</span>
            <select value={props.importMode} onChange={(event) => props.setImportMode(event.target.value as "create_missing" | "upsert")}>
              <option value="create_missing">Create missing</option>
              <option value="upsert">Create and update</option>
            </select>
          </label>
          <label className="pb-checkline switchline">
            <input type="checkbox" checked={props.importDropMissingFields} onChange={(event) => props.setImportDropMissingFields(event.target.checked)} />
            Drop fields missing from JSON
          </label>
          <div className="pb-row-actions import-actions">
            <button type="button" className="pb-btn outline expanded-lg" onClick={props.onPreviewImport}>
              Preview import
            </button>
            <button
              type="button"
              className="pb-btn primary expanded-lg"
              onClick={() => {
                if (props.importDropMissingFields && !window.confirm("Drop fields that are missing from the import JSON?")) return;
                props.onApplyImport();
              }}
            >
              Apply import
            </button>
          </div>
        </div>
      </section>
      {props.importResult ? (
        <section className="pb-settings-block">
          <h2>{props.importResult.dryRun ? "Import preview" : "Import result"}</h2>
          <div className="pb-info-grid compact">
            <Info label="Create" value={String(props.importResult.created)} />
            <Info label="Update" value={String(props.importResult.updated)} />
            <Info label="Skip" value={String(props.importResult.skipped)} />
          </div>
          <CompactTable headers={["Collection", "Action", "Status", "Message"]} rows={props.importResult.items.map((item) => [item.name, item.action, item.status, item.message ?? ""])} empty="No import changes." />
        </section>
      ) : null}
    </div>
  );
}

export function DiscoverTablesView(props: {
  discoveredTables: DiscoveredTable[];
  schemaSelection: string[];
  schemaFilters: { schema: string; table: string };
  setSchemaFilters: React.Dispatch<React.SetStateAction<{ schema: string; table: string }>>;
  setSchemaSelection: React.Dispatch<React.SetStateAction<string[]>>;
  schemaImportNames: Record<string, string>;
  schemaImportResult: CollectionImportResult | null;
  onScanSchema: () => void;
  onToggleSchemaTable: (table: DiscoveredTable) => void;
  onSetSchemaImportName: (table: DiscoveredTable, name: string) => void;
  onPreviewSchemaImport: () => void;
  onApplySchemaImport: () => void;
}) {
  const selectedKeys = new Set(props.schemaSelection);
  const eligibleTables = props.discoveredTables.filter((table) => table.canImport && !table.existingCollection);
  const selectedTables = props.discoveredTables.filter((table) => selectedKeys.has(discoveredTableKey(table)));
  const selectedCount = props.schemaSelection.length;
  const schemaCount = new Set(props.discoveredTables.map((table) => table.schema)).size;
  const importedCount = props.discoveredTables.filter((table) => table.existingCollection).length;
  const readOnlyCount = props.discoveredTables.filter((table) => !table.canImport && !table.existingCollection).length;
  const relationCount = props.discoveredTables.reduce((total, table) => total + table.foreignKeys.length, 0);
  const importNameIssues = schemaImportNameIssues(selectedTables, props.schemaImportNames);
  const canSubmit = selectedCount > 0 && importNameIssues.length === 0;

  function selectEligibleTables() {
    props.setSchemaSelection(eligibleTables.map(discoveredTableKey));
  }

  function clearSelection() {
    props.setSchemaSelection([]);
  }

  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Discover existing tables</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn secondary" onClick={selectEligibleTables} disabled={eligibleTables.length === 0}>
              Select CRUD-ready
            </button>
            <button type="button" className="pb-btn outline" onClick={props.onScanSchema}>
              <RefreshCw className="h-4 w-4" />
              Scan database
            </button>
          </div>
        </div>
        <div className="pb-inline-alert info">
          Discovery is read-only. Tables can be imported for admin CRUD only when they have a single usable primary key. Field edits stay locked until the table has `id uuid`, `created`, and `updated` and is marked managed.
        </div>
        <div className="pb-discovery-stats" aria-label="Schema discovery summary">
          <Info label="Schemas" value={String(schemaCount)} />
          <Info label="Tables" value={String(props.discoveredTables.length)} />
          <Info label="CRUD-ready" value={String(eligibleTables.length)} />
          <Info label="Relations" value={String(relationCount)} />
          <Info label="Read-only" value={String(readOnlyCount)} />
          <Info label="Configured" value={String(importedCount)} />
        </div>
        <div className="pb-grid-form import-options">
          <LabeledInput label="Schema filter" value={props.schemaFilters.schema} onChange={(value) => props.setSchemaFilters((current) => ({ ...current, schema: value }))} placeholder="public" />
          <LabeledInput label="Table search" value={props.schemaFilters.table} onChange={(value) => props.setSchemaFilters((current) => ({ ...current, table: value }))} placeholder="users" />
        </div>
      </section>

      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Tables</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn outline" onClick={clearSelection} disabled={selectedCount === 0}>
              Clear
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onPreviewSchemaImport} disabled={!canSubmit}>
              Preview import
            </button>
            <button type="button" className="pb-btn primary" onClick={props.onApplySchemaImport} disabled={!canSubmit}>
              Import selected
            </button>
          </div>
        </div>
        {importNameIssues.length > 0 ? (
          <div className="pb-inline-alert danger">
            {importNameIssues[0]}
          </div>
        ) : selectedCount > 0 ? (
          <div className="pb-inline-alert success">
            {selectedCount} table{selectedCount === 1 ? "" : "s"} selected for import.
          </div>
        ) : null}
        <div className="pb-table-wrap">
          <table className="pb-records-table compact schema-discovery-table">
            <thead>
              <tr>
                <th aria-label="Select" />
                <th>Source table</th>
                <th>Collection</th>
                <th>Primary key</th>
                <th>Fields</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {props.discoveredTables.map((table) => {
                const key = discoveredTableKey(table);
                const selected = props.schemaSelection.includes(key);
                const disabled = !table.canImport || Boolean(table.existingCollection);
                const relationSummary = tableRelationSummary(table, props.discoveredTables, props.schemaImportNames);
                return (
                  <tr key={key}>
                    <td>
                      <input type="checkbox" checked={selected} disabled={disabled} onChange={() => props.onToggleSchemaTable(table)} aria-label={`Import ${table.schema}.${table.table}`} />
                    </td>
                    <td>
                      <div className="pb-table-identity">
                        <strong>{table.table}</strong>
                        <span>{table.schema}</span>
                      </div>
                    </td>
                    <td>
                      <input
                        className="pb-inline-input"
                        value={props.schemaImportNames[key] ?? table.suggestedName}
                        disabled={disabled}
                        onChange={(event) => props.onSetSchemaImportName(table, event.target.value)}
                      />
                    </td>
                    <td>{table.primaryKey ? `${table.primaryKey.column} · ${table.primaryKey.type}` : "-"}</td>
                    <td>
                      {table.fields.length}/{table.columns.length} supported
                      {relationSummary.length > 0 ? (
                        <div className="pb-relation-chip-list">
                          {relationSummary.slice(0, 3).map((relation) => (
                            <span key={`${key}-${relation.column}`}>{relation.label}</span>
                          ))}
                          {relationSummary.length > 3 ? <span>+{relationSummary.length - 3}</span> : null}
                        </div>
                      ) : null}
                    </td>
                    <td>
                      <div className="pb-chip-row">
                        <SchemaStatusChip table={table} />
                        {table.standardSystemColumns ? <span className="pb-chip success">managed-ready</span> : <span className="pb-chip">staged</span>}
                      </div>
                      {table.reason ? <small className="pb-muted-inline">{table.reason}</small> : null}
                    </td>
                  </tr>
                );
              })}
              {props.discoveredTables.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="Scan the database to preview existing tables." action="Scan database" onAction={props.onScanSchema} />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>

      {props.discoveredTables.length > 0 ? (
        <section className="pb-settings-block">
          <h2>Preview fields and relations</h2>
          <div className="pb-discovery-preview-grid">
            {(selectedTables.length > 0 ? selectedTables : props.discoveredTables.slice(0, 12)).map((table) => (
              <details key={`preview-${discoveredTableKey(table)}`} className="pb-discovery-card">
                <summary>
                  <span>{table.schema}.{table.table}</span>
                  <em>{table.fields.length} fields · {table.foreignKeys.length} relations</em>
                </summary>
                <div className="pb-discovery-card-meta">
                  <Info label="Collection" value={props.schemaImportNames[discoveredTableKey(table)] ?? table.suggestedName} />
                  <Info label="Primary key" value={table.primaryKey ? `${table.primaryKey.field} (${table.primaryKey.type})` : "none"} />
                </div>
                {table.foreignKeys.length > 0 ? (
                  <div className="pb-discovery-relations">
                    {tableRelationSummary(table, props.discoveredTables, props.schemaImportNames).map((relation) => (
                      <span key={`${table.schema}.${table.table}.${relation.column}`}>{relation.label}</span>
                    ))}
                  </div>
                ) : null}
                <div className="pb-discovery-fields">
                  {table.columns.map((column) => (
                    <span key={column.name} className={`${column.supported ? "" : "muted"} ${column.primaryKey ? "primary-key" : ""}`} title={column.reason || column.udtName}>
                      {column.fieldName || column.name}
                      <em>{column.primaryKey ? "pk" : column.udtName}</em>
                    </span>
                  ))}
                </div>
              </details>
            ))}
          </div>
        </section>
      ) : null}

      {props.schemaImportResult ? (
        <section className="pb-settings-block">
          <h2>{props.schemaImportResult.dryRun ? "Import preview" : "Import result"}</h2>
          <div className="pb-info-grid compact">
            <Info label="Import" value={String(props.schemaImportResult.created)} />
            <Info label="Skip" value={String(props.schemaImportResult.skipped)} />
          </div>
          <CompactTable headers={["Collection", "Action", "Status", "Message"]} rows={props.schemaImportResult.items.map((item) => [item.name, item.action, item.status, item.message ?? ""])} empty="No table import changes." />
        </section>
      ) : null}
    </div>
  );
}

export function SQLConsoleView(props: {
  sqlQuery: string;
  setSQLQuery: React.Dispatch<React.SetStateAction<string>>;
  sqlMaxRows: string;
  setSQLMaxRows: React.Dispatch<React.SetStateAction<string>>;
  sqlResult: SQLResult | null;
  sqlHistory: string[];
  onExecuteSQL: (event: React.FormEvent<HTMLFormElement>) => void;
  onCopySQLCSV: () => void;
}) {
  const hasRows = Boolean(props.sqlResult?.columns.length);
  return (
    <form onSubmit={props.onExecuteSQL} className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>SQL console</h2>
          <div className="pb-row-actions tight">
            <label className="pb-field sql-history-field">
              <span>History</span>
              <select value="" onChange={(event) => event.target.value && props.setSQLQuery(event.target.value)}>
                <option value="">Recent queries</option>
                {props.sqlHistory.map((query) => (
                  <option key={query} value={query}>
                    {query}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
        <label className="pb-field">
          <span>Query</span>
          <textarea className="mono sql-editor" value={props.sqlQuery} onChange={(event) => props.setSQLQuery(event.target.value)} spellCheck={false} />
        </label>
        <div className="pb-settings-actions sql-actions">
          <label className="pb-field max-rows-field">
            <span>Max rows</span>
            <input value={props.sqlMaxRows} onChange={(event) => props.setSQLMaxRows(event.target.value)} inputMode="numeric" />
          </label>
          {hasRows ? (
            <button type="button" className="pb-btn outline expanded-lg" onClick={props.onCopySQLCSV}>
              <Copy className="h-4 w-4" />
              Copy CSV
            </button>
          ) : null}
          <button type="submit" className="pb-btn primary expanded-lg">
            <Code2 className="h-4 w-4" />
            Execute
          </button>
        </div>
      </section>
      {props.sqlResult ? (
        <section className="pb-settings-block">
          <div className={`pb-inline-alert ${props.sqlResult.readOnly ? "info" : "warning"}`}>
            {props.sqlResult.command || "SQL"} in {props.sqlResult.durationMs}ms. {props.sqlResult.columns.length > 0 ? `${props.sqlResult.rows.length} rows returned.` : `${props.sqlResult.affectedRows} rows affected.`}
            {props.sqlResult.truncated ? " Result truncated by max rows." : ""}
          </div>
          {hasRows ? <SQLResultTable result={props.sqlResult} /> : null}
        </section>
      ) : null}
    </form>
  );
}

export function APIKeysView(props: {
  apiKeys: APIKey[];
  oneTimeKey: APIKey | null;
  keyDraft: { name: string; type: "anon" | "service" };
  setKeyDraft: React.Dispatch<React.SetStateAction<{ name: string; type: "anon" | "service" }>>;
  onCreateKey: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeKey: (key: APIKey) => void;
  onCopy: (text: string) => void;
  onDismissSecret: () => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>API keys</h2>
        <CompactTable
          headers={["Name", "Type", "Prefix", "Created", "Status", ""]}
          rows={props.apiKeys.map((key) => [key.name, key.type, key.prefix, formatDate(key.createdAt), key.revokedAt ? "revoked" : "active", ""])}
          empty="No API keys yet."
        />
        <div className="pb-table-actions">
          {props.apiKeys.map((key) =>
            !key.revokedAt ? (
              <button key={key.id} type="button" onClick={() => props.onRevokeKey(key)} className="pb-btn sm transparent danger">
                Revoke {key.name}
              </button>
            ) : null,
          )}
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create key</h2>
        <form onSubmit={props.onCreateKey} className="pb-grid-form">
          <LabeledInput label="Name" value={props.keyDraft.name} onChange={(value) => props.setKeyDraft((draft) => ({ ...draft, name: value }))} placeholder="production service" />
          <label className="pb-field">
            <span>Type</span>
            <select value={props.keyDraft.type} onChange={(event) => props.setKeyDraft((draft) => ({ ...draft, type: event.target.value as "anon" | "service" }))}>
              <option value="service">service</option>
              <option value="anon">anon</option>
            </select>
          </label>
          <button type="submit" className="pb-btn primary">
            <KeyRound className="h-4 w-4" />
            Create key
          </button>
        </form>
        {props.oneTimeKey?.key ? (
          <div className="pb-inline-alert warning">
            <p>Copy this key now. It will not be shown again.</p>
            <pre className="pb-code-box">{props.oneTimeKey.key}</pre>
            <div className="pb-row-actions">
              <button type="button" onClick={() => props.onCopy(props.oneTimeKey?.key ?? "")} className="pb-btn secondary">
                <Copy className="h-4 w-4" />
                Copy
              </button>
              <button type="button" onClick={props.onDismissSecret} className="pb-btn transparent">
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </section>
    </div>
  );
}

export function FilesView(props: {
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  setSelectedCollection: (name: string) => void;
  fileFields: Field[];
  fileDraft: { recordId: string; field: string };
  setFileDraft: React.Dispatch<React.SetStateAction<{ recordId: string; field: string }>>;
  selectedUploadFile: File | null;
  setSelectedUploadFile: React.Dispatch<React.SetStateAction<File | null>>;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  projectSlug: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
}) {
  const availableFiles = findFiles(props.records.items, props.fileFields);
  const [dragActive, setDragActive] = useState(false);
  const dragDepthRef = useRef(0);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const pickFile = (files: FileList | null) => {
    const file = files?.[0] ?? null;
    props.setSelectedUploadFile(file);
  };
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Upload file</h2>
        <form onSubmit={props.onSubmitUpload} className="pb-grid-form file-upload-form">
          <label className="pb-field">
            <span>Collection</span>
            <select value={props.selectedCollectionName} onChange={(event) => props.setSelectedCollection(event.target.value)}>
              {props.collections.map((collection) => (
                <option key={collection.id} value={collection.name}>
                  {collection.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Record ID" value={props.fileDraft.recordId} onChange={(value) => props.setFileDraft((draft) => ({ ...draft, recordId: value }))} placeholder="uuid" />
          <label className="pb-field">
            <span>File field</span>
            <select value={props.fileDraft.field} onChange={(event) => props.setFileDraft((draft) => ({ ...draft, field: event.target.value }))}>
              {props.fileFields.map((field) => (
                <option key={field.name} value={field.name}>
                  {field.name}
                </option>
              ))}
            </select>
          </label>
          <div
            className={`pb-dropzone ${dragActive ? "active" : ""} ${props.selectedUploadFile ? "has-file" : ""}`}
            onDragEnter={(event) => {
              event.preventDefault();
              dragDepthRef.current += 1;
              setDragActive(true);
            }}
            onDragOver={(event) => {
              event.preventDefault();
              setDragActive(true);
            }}
            onDragLeave={(event) => {
              event.preventDefault();
              dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
              if (dragDepthRef.current === 0) setDragActive(false);
            }}
            onDrop={(event) => {
              event.preventDefault();
              dragDepthRef.current = 0;
              setDragActive(false);
              pickFile(event.dataTransfer.files);
            }}
          >
            <input ref={fileInputRef} name="file" type="file" className="sr-only" onChange={(event) => pickFile(event.currentTarget.files)} />
            <UploadCloud className="h-6 w-6" />
            <div>
              <strong>{props.selectedUploadFile ? props.selectedUploadFile.name : "Drop file here"}</strong>
              <span>{props.selectedUploadFile ? `${formatBytes(props.selectedUploadFile.size)} · use Browse to replace` : "or use Browse to select from your computer"}</span>
            </div>
            <button type="button" className="pb-btn secondary" onClick={() => fileInputRef.current?.click()}>
              Browse
            </button>
            {props.selectedUploadFile ? (
              <button
                type="button"
                className="pb-icon-btn"
                aria-label="Clear selected file"
                onClick={() => {
                  props.setSelectedUploadFile(null);
                  if (fileInputRef.current) fileInputRef.current.value = "";
                }}
              >
                <X className="h-4 w-4" />
              </button>
            ) : null}
          </div>
          <button type="submit" className="pb-btn primary">
            <UploadCloud className="h-4 w-4" />
            Upload
          </button>
        </form>
        {props.fileResult ? <pre className="pb-code-box">{JSON.stringify(props.fileResult, null, 2)}</pre> : null}
      </section>
      <section className="pb-settings-block">
        <h2>Protected files</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table">
            <thead>
              <tr>
                <th>Record</th>
                <th>Field</th>
                <th>File</th>
                <th>Size</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {availableFiles.map((file) => (
                <tr key={`${file.recordId}-${file.field}-${file.id}`}>
                  <td>{file.recordId}</td>
                  <td>{file.field}</td>
                  <td>{file.name}</td>
                  <td>{file.size}</td>
                  <td className="row-actions">
                    <button
                      type="button"
                      onClick={async () => {
                        const fileToken = await props.onCreateFileToken(file.recordId, file.field, file.id);
                        if (fileToken) {
                          window.open(`/api/projects/${encodeURIComponent(props.projectSlug)}/files/${encodeURIComponent(props.selectedCollection?.name ?? "")}/${encodeURIComponent(file.recordId)}/${encodeURIComponent(file.field)}/${encodeURIComponent(file.id)}/${encodeURIComponent(file.name)}?token=${encodeURIComponent(fileToken)}`, "_blank", "noopener,noreferrer");
                        }
                      }}
                      className="pb-btn sm secondary"
                    >
                      Download
                    </button>
                  </td>
                </tr>
              ))}
              {availableFiles.length === 0 ? (
                <tr>
                  <td colSpan={5} className="pb-empty-cell">
                    <EmptyState label="Load records from a collection with file fields." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
