import type { AuditEntry, Collection, Field, RecordItem, RequestLogEntry } from "../../src/lib/types";

// Formatting and small pure data helpers, split out of page.tsx.

export function formatDate(value: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

export function formatTime(value: Date) {
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(value);
}

export function buildLogBuckets(items: Array<AuditEntry | RequestLogEntry>, mode: "audit" | "requests") {
  const bucketCount = 18;
  const now = Date.now();
  const timestamps = items
    .map((entry) => Date.parse(entry.createdAt))
    .filter((value) => Number.isFinite(value));
  const latest = Math.max(now, ...timestamps);
  const earliest = timestamps.length > 0 ? Math.min(...timestamps) : latest - 60 * 60 * 1000;
  const span = Math.max(60 * 1000, latest - earliest);
  const step = Math.max(60 * 1000, Math.ceil(span / bucketCount));
  const start = latest - step * (bucketCount - 1);
  const buckets = Array.from({ length: bucketCount }, (_, index) => ({
    label: formatTime(new Date(start + index * step)),
    count: 0,
    errors: 0,
  }));
  for (const entry of items) {
    const time = Date.parse(entry.createdAt);
    if (!Number.isFinite(time)) continue;
    const index = Math.min(bucketCount - 1, Math.max(0, Math.floor((time - start) / step)));
    buckets[index].count += 1;
    if (mode === "requests" && "status" in entry && entry.status >= 400) buckets[index].errors += 1;
    if (mode === "audit" && "action" in entry && /fail|error/i.test(entry.action)) buckets[index].errors += 1;
  }
  return buckets;
}

export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "-";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = value;
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function formatCount(value: number) {
  if (!Number.isFinite(value)) return "-";
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value);
}

export function formatCompactNumber(value: number) {
  if (!Number.isFinite(value)) return "-";
  return new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: Math.abs(value) < 10 ? 1 : 0 }).format(value);
}

export function formatPercent(value: number) {
  if (!Number.isFinite(value)) return "-";
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: value < 10 ? 1 : 0 }).format(value)}%`;
}

export function formatDurationMs(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 ms";
  if (value < 1000) return `${formatCount(Math.round(value))} ms`;
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value / 1000)} s`;
}

export function formatInsightTick(value: string, hours: number) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  if (hours <= 24) {
    return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(date);
  }
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(date);
}

export function defaultInsightCollection(collections: Collection[]) {
  return collections.find((collection) => collection.type !== "view" && !collection.system)?.name ?? collections.find((collection) => collection.type !== "view")?.name ?? "";
}

export function parseHeadersJSON(value: string): Record<string, string> {
  const raw = value.trim() ? JSON.parse(value) : {};
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("Headers must be a JSON object");
  }
  const headers: Record<string, string> = {};
  for (const [key, headerValue] of Object.entries(raw)) {
    if (typeof headerValue !== "string") {
      throw new Error("Header values must be strings");
    }
    headers[key] = headerValue;
  }
  return headers;
}

export type FileMeta = { recordId: string; field: string; id: string; name: string; size: string };

export function findFiles(records: RecordItem[], fields: Field[]): FileMeta[] {
  const fieldNames = new Set(fields.map((field) => field.name));
  const out: FileMeta[] = [];
  for (const record of records) {
    const recordId = String(record.id ?? "");
    for (const [field, value] of Object.entries(record)) {
      if (!fieldNames.has(field)) continue;
      const values = Array.isArray(value) ? value : value ? [value] : [];
      for (const item of values) {
        if (!item || typeof item !== "object") continue;
        const raw = item as Record<string, unknown>;
        const id = String(raw.id ?? "");
        const name = String(raw.name ?? "");
        if (!id || !name) continue;
        out.push({
          recordId,
          field,
          id,
          name,
          size: typeof raw.size === "number" ? `${Math.round(raw.size / 1024)} KB` : String(raw.size ?? "-"),
        });
      }
    }
  }
  return out;
}

// The server's error code and its message often say the same thing — a failed
// validation arrived as "validation_failed: validation failed: outbound host
// must not target private or local addresses", and a bad route as "not found:
// route not found". The code is only worth prepending when the sentence does
// not already carry it.
export function formatApiError(code: string, message: string): string {
  const text = (message || "").trim();
  const label = (code || "").replace(/_/g, " ").trim();
  if (!text) return label || "Request failed";
  if (!label) return text;
  if (text.toLowerCase().includes(label.toLowerCase())) return text;
  return `${label}: ${text}`;
}
