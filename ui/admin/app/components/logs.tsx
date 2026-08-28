"use client";

import type { ApiEnvelope, AuditEntry, InstanceSettings, RequestLogEntry } from "../../src/lib/types";
import { emptyAuditFilters, emptyLogDraft, emptyRequestFilters, recordPageSizes } from "../lib/constants";
import { formatCount, formatDate } from "../lib/format";
import { LogActivityPanel } from "./insights";
import { EmptyState, PageFooter } from "./ui";
import { ChevronRight, Copy, Database, Download, Hash, ListFilter, RefreshCw, Save, Search, Trash2, X } from "lucide-react";
import { useState } from "react";

export function LogsView({
  mode,
  setMode,
  audit,
  auditPerPage,
  filters,
  requestLogs,
  requestPerPage,
  requestFilters,
  settings,
  logDraft,
  setLogDraft,
  onFilterChange,
  onRequestFilterChange,
  onSaveLogSettings,
  onRefresh,
  onClearLogs,
  onPageChange,
  onRequestPageChange,
  onPageSizeChange,
  onRequestPageSizeChange,
  onCopy,
  version,
}: {
  mode: "audit" | "requests";
  setMode: (mode: "audit" | "requests") => void;
  audit: ApiEnvelope<AuditEntry>;
  auditPerPage: (typeof recordPageSizes)[number];
  filters: typeof emptyAuditFilters;
  requestLogs: ApiEnvelope<RequestLogEntry>;
  requestPerPage: (typeof recordPageSizes)[number];
  requestFilters: typeof emptyRequestFilters;
  settings: InstanceSettings | null;
  logDraft: typeof emptyLogDraft;
  setLogDraft: React.Dispatch<React.SetStateAction<typeof emptyLogDraft>>;
  onFilterChange: React.Dispatch<React.SetStateAction<typeof emptyAuditFilters>>;
  onRequestFilterChange: React.Dispatch<React.SetStateAction<typeof emptyRequestFilters>>;
  onSaveLogSettings: (event: React.FormEvent<HTMLFormElement>) => void;
  onRefresh: () => void;
  onClearLogs: () => void;
  onPageChange: (page: number) => void;
  onRequestPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: (typeof recordPageSizes)[number]) => void;
  onRequestPageSizeChange: (pageSize: (typeof recordPageSizes)[number]) => void;
  onCopy: (text: string) => void;
  version: string;
}) {
  const [selectedEntry, setSelectedEntry] = useState<AuditEntry | null>(null);
  const [selectedRequest, setSelectedRequest] = useState<RequestLogEntry | null>(null);
  const page = audit.page ?? 1;
  const perPage = audit.perPage ?? auditPerPage;
  const totalItems = audit.totalItems ?? audit.items.length;
  const totalPages = Math.max(1, Math.ceil(totalItems / Math.max(1, perPage)));
  const requestPage = requestLogs.page ?? 1;
  const requestItems = requestLogs.totalItems ?? requestLogs.items.length;
  const requestPages = Math.max(1, Math.ceil(requestItems / Math.max(1, requestLogs.perPage ?? requestPerPage)));
  const actionOptions = Array.from(new Set(audit.items.map((entry) => entry.action))).sort();
  const activeTotal = mode === "audit" ? totalItems : requestItems;
  const visibleTotal = mode === "audit" ? audit.items.length : requestLogs.items.length;
  const errorTotal = mode === "audit" ? audit.items.filter((entry) => entry.action.includes("fail") || entry.action.includes("error")).length : requestLogs.items.filter((entry) => entry.status >= 400).length;
  return (
    <section className="pb-page single">
      <div className="pb-page-content full-height">
        <header className="pb-page-header">
          <nav className="pb-breadcrumbs" aria-label="Breadcrumb">
            <span>Logs</span>
          </nav>
          <div className="pb-header-primary-btns">
            <div className="pb-segmented">
              <button type="button" className={mode === "audit" ? "active" : ""} onClick={() => setMode("audit")}>
                Audit
              </button>
              <button type="button" className={mode === "requests" ? "active" : ""} onClick={() => setMode("requests")}>
                Requests
              </button>
            </div>
            <button type="button" onClick={onRefresh} className="pb-btn outline">
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
            <button type="button" onClick={onClearLogs} className="pb-btn danger">
              <Trash2 className="h-4 w-4" />
              Clear log table
            </button>
          </div>
        </header>
        <LogActivityPanel mode={mode} audit={audit.items} requests={requestLogs.items} total={activeTotal} visible={visibleTotal} errors={errorTotal} />
        {mode === "audit" ? (
          <form
            className="pb-record-toolbar logs-toolbar"
            onSubmit={(event) => {
              event.preventDefault();
              onPageChange(1);
            }}
          >
            <label className="pb-record-control search">
              <Search className="h-4 w-4" />
              <input value={filters.search} onChange={(event) => onFilterChange((current) => ({ ...current, search: event.target.value }))} placeholder="Search action, target, IP, user agent, data" />
            </label>
            <label className="pb-record-control action-filter">
              <ListFilter className="h-4 w-4" />
              <input list="audit-actions" value={filters.action} onChange={(event) => onFilterChange((current) => ({ ...current, action: event.target.value }))} placeholder="Action" />
              <datalist id="audit-actions">
                {actionOptions.map((action) => (
                  <option key={action} value={action} />
                ))}
              </datalist>
            </label>
            <label className="pb-record-control target-filter">
              <Database className="h-4 w-4" />
              <input value={filters.target} onChange={(event) => onFilterChange((current) => ({ ...current, target: event.target.value }))} placeholder="Target type or id" />
            </label>
            <button type="submit" className="pb-btn sm secondary">
              Apply
            </button>
            <button
              type="button"
              className="pb-btn sm transparent"
              onClick={() => {
                onFilterChange(emptyAuditFilters);
                window.setTimeout(() => onPageChange(1), 0);
              }}
            >
              Clear
            </button>
          </form>
        ) : (
          <form
            className="pb-record-toolbar logs-toolbar"
            onSubmit={(event) => {
              event.preventDefault();
              onRequestPageChange(1);
            }}
          >
            <label className="pb-record-control search">
              <Search className="h-4 w-4" />
              <input value={requestFilters.search} onChange={(event) => onRequestFilterChange((current) => ({ ...current, search: event.target.value }))} placeholder="Search path, IP, user agent, metadata" />
            </label>
            <label className="pb-record-control action-filter">
              <ListFilter className="h-4 w-4" />
              <select value={requestFilters.method} onChange={(event) => onRequestFilterChange((current) => ({ ...current, method: event.target.value }))}>
                <option value="">Any method</option>
                {["GET", "POST", "PUT", "PATCH", "DELETE"].map((method) => (
                  <option key={method} value={method}>
                    {method}
                  </option>
                ))}
              </select>
            </label>
            <label className="pb-record-control target-filter">
              <Hash className="h-4 w-4" />
              <input value={requestFilters.status} onChange={(event) => onRequestFilterChange((current) => ({ ...current, status: event.target.value }))} placeholder="Status" />
            </label>
            <button type="submit" className="pb-btn sm secondary">
              Apply
            </button>
            <button
              type="button"
              className="pb-btn sm transparent"
              onClick={() => {
                onRequestFilterChange(emptyRequestFilters);
                window.setTimeout(() => onRequestPageChange(1), 0);
              }}
            >
              Clear
            </button>
          </form>
        )}
        <form className="pb-log-retention-panel" onSubmit={onSaveLogSettings}>
          <div>
            <strong>Retention</strong>
            <span>
              Keep audit logs for {settings?.logs.retentionDays ?? 30} days, capped at {formatCount(settings?.logs.retentionCount ?? 100000)} rows.
              Source: {settings?.logs.source ?? "default"}.
            </span>
          </div>
          <label className="pb-field compact">
            <span>Days</span>
            <input
              inputMode="numeric"
              value={logDraft.retentionDays}
              onChange={(event) => setLogDraft((draft) => ({ ...draft, retentionDays: event.target.value }))}
            />
          </label>
          <label className="pb-field compact">
            <span>Rows</span>
            <input
              inputMode="numeric"
              value={logDraft.retentionCount}
              onChange={(event) => setLogDraft((draft) => ({ ...draft, retentionCount: event.target.value }))}
            />
          </label>
          <button type="submit" className="pb-btn sm secondary">
            Save
          </button>
        </form>
        {mode === "audit" ? <div className="pb-table-wrap logs-table-wrap">
          <table className="pb-records-table logs-table">
            <thead>
              <tr>
                <th className="col-bulk" />
                <th>Action</th>
                <th>Target</th>
                <th>IP</th>
                <th>Created</th>
                <th>Data</th>
                <th className="col-meta" />
              </tr>
            </thead>
            <tbody>
              {audit.items.map((entry) => (
                <tr key={entry.id} className="clickable-row" onClick={() => setSelectedEntry(entry)}>
                  <td className="col-bulk">
                    <span className="pb-log-level-dot" aria-hidden="true" />
                  </td>
                  <td>
                    <div className="pb-log-message">
                      <strong>{entry.action}</strong>
                      <span>
                        <span className="pb-log-chip">audit</span>
                        {entry.adminId ? <span className="pb-log-chip">admin</span> : null}
                      </span>
                    </div>
                  </td>
                  <td>
                    {entry.targetType} {entry.targetId}
                  </td>
                  <td>{entry.ip || "-"}</td>
                  <td>{formatDate(entry.createdAt)}</td>
                  <td className="truncate-cell">{JSON.stringify(entry.data)}</td>
                  <td className="col-meta">
                    <ChevronRight className="h-4 w-4" />
                  </td>
                </tr>
              ))}
              {audit.items.length === 0 ? (
                <tr>
                  <td colSpan={7} className="pb-empty-cell">
                    <EmptyState label="No audit entries yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
          <div className="pb-log-cards" aria-label="Audit logs">
            {audit.items.map((entry) => (
              <button key={entry.id} type="button" onClick={() => setSelectedEntry(entry)}>
                <strong>{entry.action}</strong>
                <span>{entry.targetType} {entry.targetId}</span>
                <em>{entry.ip || "-"} · {formatDate(entry.createdAt)}</em>
              </button>
            ))}
          </div>
        </div> : <div className="pb-table-wrap logs-table-wrap">
          <table className="pb-records-table logs-table">
            <thead>
              <tr>
                <th className="col-bulk" />
                <th>Method</th>
                <th>Path</th>
                <th>Status</th>
                <th>Duration</th>
                <th>IP</th>
                <th>Created</th>
                <th className="col-meta" />
              </tr>
            </thead>
            <tbody>
              {requestLogs.items.map((entry) => (
                <tr key={entry.id} className="clickable-row" onClick={() => setSelectedRequest(entry)}>
                  <td className="col-bulk">
                    <span className={`pb-log-level-dot ${entry.status >= 400 ? "danger" : ""}`} aria-hidden="true" />
                  </td>
                  <td>
                    <span className="pb-log-chip method">{entry.method}</span>
                  </td>
                  <td className="truncate-cell">{entry.path}</td>
                  <td>
                    <span className={`pb-log-chip ${entry.status >= 400 ? "danger" : "success"}`}>status: {entry.status}</span>
                  </td>
                  <td>
                    <span className="pb-log-chip">{entry.durationMs}ms</span>
                  </td>
                  <td>{entry.ip || "-"}</td>
                  <td>{formatDate(entry.createdAt)}</td>
                  <td className="col-meta">
                    <ChevronRight className="h-4 w-4" />
                  </td>
                </tr>
              ))}
              {requestLogs.items.length === 0 ? (
                <tr>
                  <td colSpan={8} className="pb-empty-cell">
                    <EmptyState label="No request logs yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
          <div className="pb-log-cards" aria-label="Request logs">
            {requestLogs.items.map((entry) => (
              <button key={entry.id} type="button" onClick={() => setSelectedRequest(entry)}>
                <strong>{entry.method} {entry.path}</strong>
                <span>
                  <span className={`pb-log-chip ${entry.status >= 400 ? "danger" : "success"}`}>status: {entry.status}</span>
                  <span className="pb-log-chip">{entry.durationMs}ms</span>
                </span>
                <em>{entry.ip || "-"} · {formatDate(entry.createdAt)}</em>
              </button>
            ))}
          </div>
        </div>}
        {mode === "audit" ? <div className="pb-record-pagination" aria-label="Audit log pagination">
          <label className="pb-page-size-control">
            <span>Per page</span>
            <select
              value={auditPerPage}
              onChange={(event) => {
                const next = Number(event.target.value) as (typeof recordPageSizes)[number];
                onPageSizeChange(next);
              }}
            >
              {recordPageSizes.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
          <div className="pb-pagination-actions">
            <button type="button" className="pb-btn outline" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
              Previous
            </button>
            <span>
              Page {page} of {totalPages}
            </span>
            <button type="button" className="pb-btn outline" disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>
              Next
            </button>
          </div>
        </div> : <div className="pb-record-pagination" aria-label="Request log pagination">
          <label className="pb-page-size-control">
            <span>Per page</span>
            <select
              value={requestPerPage}
              onChange={(event) => {
                const next = Number(event.target.value) as (typeof recordPageSizes)[number];
                onRequestPageSizeChange(next);
              }}
            >
              {recordPageSizes.map((size) => (
                <option key={size} value={size}>
                  {size}
                </option>
              ))}
            </select>
          </label>
          <div className="pb-pagination-actions">
            <button type="button" className="pb-btn outline" disabled={requestPage <= 1} onClick={() => onRequestPageChange(requestPage - 1)}>
              Previous
            </button>
            <span>
              Page {requestPage} of {requestPages}
            </span>
            <button type="button" className="pb-btn outline" disabled={requestPage >= requestPages} onClick={() => onRequestPageChange(requestPage + 1)}>
              Next
            </button>
          </div>
        </div>}
        <PageFooter left={mode === "audit" ? `Showing ${audit.items.length} of ${totalItems}` : `Showing ${requestLogs.items.length} of ${requestItems}`} version={version} />
      </div>
      {selectedEntry ? <AuditDetailDrawer entry={selectedEntry} onClose={() => setSelectedEntry(null)} onCopy={onCopy} /> : null}
      {selectedRequest ? <RequestLogDetailDrawer entry={selectedRequest} onClose={() => setSelectedRequest(null)} onCopy={onCopy} /> : null}
    </section>
  );
}

export function AuditDetailDrawer({ entry, onClose, onCopy }: { entry: AuditEntry; onClose: () => void; onCopy: (text: string) => void }) {
  const json = JSON.stringify(entry, null, 2);
  const rows = [
    ["id", entry.id],
    ["adminId", entry.adminId ?? ""],
    ["action", entry.action],
    ["targetType", entry.targetType],
    ["targetId", entry.targetId],
    ["ip", entry.ip],
    ["userAgent", entry.userAgent],
    ["createdAt", entry.createdAt],
    ["data", JSON.stringify(entry.data, null, 2)],
  ];
  function downloadJSON() {
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `dublyobase-log-${entry.id}.json`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }
  return (
    <aside className="pb-detail-drawer" aria-label="Log details">
      <header>
        <div>
          <h2>{entry.action}</h2>
          <span>{formatDate(entry.createdAt)}</span>
        </div>
        <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close log details">
          <X className="h-4 w-4" />
        </button>
      </header>
      <div className="pb-detail-actions">
        <button type="button" className="pb-btn sm secondary" onClick={() => onCopy(json)}>
          <Copy className="h-4 w-4" />
          Copy JSON
        </button>
        <button type="button" className="pb-btn sm outline" onClick={downloadJSON}>
          <Download className="h-4 w-4" />
          Download JSON
        </button>
      </div>
      <dl className="pb-detail-list">
        {rows.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>
              <code>{value || "-"}</code>
              <button type="button" className="pb-icon-btn" onClick={() => onCopy(value)} aria-label={`Copy ${label}`}>
                <Copy className="h-3.5 w-3.5" />
              </button>
            </dd>
          </div>
        ))}
      </dl>
    </aside>
  );
}

export function RequestLogDetailDrawer({ entry, onClose, onCopy }: { entry: RequestLogEntry; onClose: () => void; onCopy: (text: string) => void }) {
  const json = JSON.stringify(entry, null, 2);
  const rows = [
    ["id", entry.id],
    ["project", entry.projectSlug],
    ["method", entry.method],
    ["path", entry.path],
    ["status", String(entry.status)],
    ["durationMs", String(entry.durationMs)],
    ["ip", entry.ip],
    ["userAgent", entry.userAgent],
    ["requestId", entry.requestId],
    ["createdAt", entry.createdAt],
    ["metadata", JSON.stringify(entry.metadata, null, 2)],
  ];
  function downloadJSON() {
    const blob = new Blob([json], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `request-log-${entry.id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }
  return (
    <aside className="pb-detail-drawer" aria-label="Request log detail">
      <div className="pb-detail-header">
        <div>
          <h2>{entry.method} {entry.status}</h2>
          <span>{formatDate(entry.createdAt)}</span>
        </div>
        <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close">
          <X className="h-4 w-4" />
        </button>
      </div>
      <div className="pb-detail-actions">
        <button type="button" className="pb-btn sm secondary" onClick={() => onCopy(json)}>
          <Copy className="h-4 w-4" />
          Copy JSON
        </button>
        <button type="button" className="pb-btn sm outline" onClick={downloadJSON}>
          <Download className="h-4 w-4" />
          Download
        </button>
      </div>
      <dl className="pb-detail-list">
        {rows.map(([key, value]) => (
          <div key={key}>
            <dt>{key}</dt>
            <dd>{value || "-"}</dd>
          </div>
        ))}
      </dl>
    </aside>
  );
}
