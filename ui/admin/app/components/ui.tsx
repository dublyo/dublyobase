"use client";

import type { BackupRun, Collection, CollectionIconOption, CronRun, DiscoveredTable, FieldType, SQLResult } from "../../src/lib/types";
import { collectionIconFromOptions, defaultCollectionIcon } from "../lib/collections";
import { collectionIconMap } from "../lib/constants";
import { fieldTypeIcon } from "../lib/fields";
import { formatBytes, formatDate } from "../lib/format";
import { formatSQLValue } from "../lib/sql";
import { AlertCircle, Table2 } from "lucide-react";

export function CollectionIcon({ collection, icon, type }: { collection?: Collection; icon?: CollectionIconOption; type?: Collection["type"] }) {
  const resolvedType = type ?? collection?.type ?? "base";
  const resolved = icon ?? (collection ? collectionIconFromOptions(collection) : defaultCollectionIcon(resolvedType));
  const fallback = defaultCollectionIcon(resolvedType);
  const iconName = resolved.type === "lucide" ? resolved.name : fallback.type === "lucide" ? fallback.name : "table";
  const Icon = collectionIconMap[iconName] ?? Table2;
  return <Icon className="h-4 w-4" aria-hidden="true" />;
}

export function RunHistory({ runs }: { runs: Array<CronRun | BackupRun> }) {
  const recent = runs.slice(0, 8);
  if (recent.length === 0) return null;
  return (
    <div className="pb-run-history">
      <h3>Recent runs</h3>
      <div className="pb-table-wrap">
        <table className="pb-records-table compact">
          <thead>
            <tr>
              <th>Status</th>
              <th>Started</th>
              <th>Result</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {recent.map((run) => {
              const result = "statusCode" in run ? String(run.statusCode ?? "-") : "storageKey" in run ? `${formatBytes(run.sizeBytes)} ${run.storageKey}` : "-";
              return (
                <tr key={run.id}>
                  <td>{run.status}</td>
                  <td>{formatDate(run.startedAt)}</td>
                  <td>{result}</td>
                  <td>{run.error || "-"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export function SchemaStatusChip({ table }: { table: DiscoveredTable }) {
  if (table.existingCollection) return <span className="pb-chip">configured as {table.existingCollection}</span>;
  if (table.canImport) return <span className="pb-chip success">CRUD-ready</span>;
  return <span className="pb-chip warning">read-only</span>;
}

export function SQLResultTable({ result }: { result: SQLResult }) {
  return (
    <div className="pb-table-wrap sql-result-table">
      <table className="pb-records-table compact">
        <thead>
          <tr>
            {result.columns.map((column, index) => (
              <th key={`${column.name}-${index}`}>{column.name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {result.rows.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {result.columns.map((column, columnIndex) => (
                <td key={`${column.name}-${columnIndex}`} className="truncate-cell">
                  {formatSQLValue(row[columnIndex])}
                </td>
              ))}
            </tr>
          ))}
          {result.rows.length === 0 ? (
            <tr>
              <td colSpan={Math.max(result.columns.length, 1)} className="pb-empty-cell">
                <EmptyState label="No rows returned." />
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

export function LabeledInput({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="pb-field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} />
    </label>
  );
}

export function CompactTable({ headers, rows, empty }: { headers: string[]; rows: string[][]; empty: string }) {
  return (
    <div className="pb-table-wrap">
      <table className="pb-records-table compact">
        <thead>
          <tr>
            {headers.map((header) => (
              <th key={header}>{header}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index}>
              {row.map((cell, cellIndex) => (
                <td key={cellIndex} className="truncate-cell">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
          {rows.length === 0 ? (
            <tr>
              <td colSpan={headers.length} className="pb-empty-cell">
                <EmptyState label={empty} />
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

export function EmptyState({ label, title, detail, compact = false, action, onAction }: { label?: string; title?: string; detail?: string; compact?: boolean; action?: string; onAction?: () => void }) {
  const message = title ?? label ?? "No data";
  return (
    <div className={`pb-empty-state ${compact ? "compact" : ""}`}>
      <AlertCircle className="h-4 w-4" />
      <span>
        {detail ? (
          <>
            <strong>{message}</strong>
            <em>{detail}</em>
          </>
        ) : (
          message
        )}
      </span>
      {action && onAction ? (
        <button type="button" className="pb-btn secondary expanded-lg" onClick={onAction}>
          {action}
        </button>
      ) : null}
    </div>
  );
}

export function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="pb-info-row">
      <dt>{label}</dt>
      <dd>{value || "-"}</dd>
    </div>
  );
}

export function StatusPill({ label, value, ok }: { label: string; value: string; ok: boolean }) {
  return (
    <span className="pb-status-pill">
      <span className={`status-dot ${ok ? "success" : "warning"}`} />
      {label} {value}
    </span>
  );
}

export function PageFooter({ left, version }: { left: string; version: string }) {
  return (
    <footer className="pb-page-footer">
      <span>{left}</span>
      <span className="credits">
        <a href="https://github.com/dublyo/dublyobase" target="_blank" rel="noreferrer">
          Docs
        </a>
        <a href="https://github.com/dublyo/dublyobase/releases" target="_blank" rel="noreferrer">
          Dublyobase {version}
        </a>
      </span>
    </footer>
  );
}

// Relation columns used to render as bare UUIDs. Expanding them costs one
// extra query per relation field per page (the backend batches by id), so it is
// worth it for every relation that has a display field configured.

export function FieldTypeGlyph({ type }: { type: FieldType }) {
  const Icon = fieldTypeIcon(type);
  return <Icon className="h-4 w-4" aria-hidden="true" />;
}
