import type { SQLResult } from "../../src/lib/types";
import { SQL_HISTORY_KEY } from "./constants";

// SQL console helpers, split out of page.tsx.

export function isDangerousSQL(query: string) {
  const first = query.trim().split(/\s+/, 1)[0]?.toLowerCase().replace(/;$/, "") ?? "";
  return ["alter", "replace", "insert", "create", "update", "delete", "drop", "truncate", "grant", "revoke"].includes(first);
}

export function loadSQLHistory() {
  try {
    const raw = window.localStorage.getItem(SQL_HISTORY_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string").slice(0, 20) : [];
  } catch {
    return [];
  }
}

export function saveSQLHistory(query: string, current: string[]) {
  const next = [query, ...current.filter((item) => item !== query)].slice(0, 20);
  try {
    window.localStorage.setItem(SQL_HISTORY_KEY, JSON.stringify(next));
  } catch {
    // Query history is a convenience; failing to persist it must not block SQL execution.
  }
  return next;
}

export function formatSQLValue(value: unknown) {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

export function sqlResultToCSV(result: SQLResult) {
  const rows = [result.columns.map((column) => column.name), ...result.rows.map((row) => result.columns.map((_, index) => formatSQLValue(row[index])))];
  return rows.map((row) => row.map(csvCell).join(",")).join("\n");
}

export function csvCell(value: string) {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replaceAll('"', '""')}"`;
  }
  return value;
}
