import type { Collection, CollectionIconOption, CollectionOptions, DiscoveredTable, Field, RecordItem } from "../../src/lib/types";
import { collectionIconMap } from "./constants";
import { fieldIsMultiple, isRecordFormField, optionValues } from "./fields";

// Collection metadata and schema-import helpers, split out of page.tsx.

export function sampleRecordPayload(collection: Collection): RecordItem {
  const out: RecordItem = {};
  for (const field of collection.fields.filter(isRecordFormField)) {
    if (field.hidden || field.type === "password") continue;
    out[field.name] = sampleValueForField(field);
  }
  return out;
}

export function sampleUpdatePayload(collection: Collection): RecordItem {
  const first = collection.fields.find((field) => isRecordFormField(field) && !field.hidden && field.type !== "password");
  return first ? { [first.name]: sampleValueForField(first) } : {};
}

export function sampleValueForField(field: Field): unknown {
  if (field.type === "number") return field.options?.onlyInt ? 10 : 10.5;
  if (field.type === "decimal") return "1234.56";
  if (field.type === "bool") return true;
  if (field.type === "date") return "2026-07-04T00:00:00Z";
  if (field.type === "email") return "user@example.com";
  if (field.type === "url") return "https://example.com";
  if (field.type === "select") {
    const values = optionValues(field.options);
    return fieldIsMultiple(field) ? values.slice(0, 2) : values[0] || "draft";
  }
  if (field.type === "json") return { source: "api" };
  if (field.type === "relation") return fieldIsMultiple(field) ? ["record-id-1", "record-id-2"] : "record-id";
  if (field.type === "editor") return "<p>Hello world</p>";
  return "Hello world";
}

export function pascalCase(value: string) {
  return value
    .split(/[^a-zA-Z0-9]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join("");
}

export function collectionIconFromOptions(collection: Collection): CollectionIconOption {
  return normalizeCollectionIcon(normalizeCollectionOptions(collection.options).icon, collection.type);
}

export function collectionOptionsWithIcon(options: CollectionOptions | undefined, icon: CollectionIconOption): CollectionOptions {
  return {
    ...normalizeCollectionOptions(options),
    icon: normalizeCollectionIcon(icon, "base"),
  };
}

export function collectionImportedFromOptions(collection: Collection): boolean {
  return Boolean(normalizeCollectionOptions(collection.options).imported);
}

export function collectionManagedByDublyobase(collection: Collection): boolean {
  const options = normalizeCollectionOptions(collection.options);
  return !options.imported || Boolean(options.managed);
}

export function collectionStandardSystemColumns(collection: Collection): boolean {
  const options = normalizeCollectionOptions(collection.options);
  return !options.imported || Boolean(options.standardSystemColumns);
}

export function collectionSourceTable(collection: Collection): string {
  const options = normalizeCollectionOptions(collection.options);
  const schema = typeof options.sourceSchema === "string" ? options.sourceSchema : "";
  const table = typeof options.sourceTable === "string" ? options.sourceTable : "";
  return [schema, table].filter(Boolean).join(".");
}

export function collectionPrimaryKeyFieldName(collection: Collection): string {
  const options = normalizeCollectionOptions(collection.options);
  return typeof options.primaryKeyField === "string" && options.primaryKeyField ? options.primaryKeyField : "id";
}

export function recordPrimaryKeyValue(collection: Collection, record: RecordItem): string {
  const value = record[collectionPrimaryKeyFieldName(collection)];
  return value === null || value === undefined ? "" : String(value);
}

export function normalizeCollectionOptions(options: unknown): CollectionOptions {
  if (!options || typeof options !== "object" || Array.isArray(options)) return {};
  return { ...(options as CollectionOptions) };
}

export function normalizeCollectionIcon(raw: unknown, collectionType: Collection["type"]): CollectionIconOption {
  const fallback = defaultCollectionIcon(collectionType);
  if (raw && typeof raw === "object" && !Array.isArray(raw)) {
    const body = raw as { type?: unknown; name?: unknown };
    if (body.type === "lucide" && typeof body.name === "string") {
      const name = normalizeLucideIconName(body.name);
      return collectionIconMap[name] ? { type: "lucide", name } : fallback;
    }
  }
  if (typeof raw === "string") {
    const value = raw.trim();
    if (value.startsWith("lucide:")) {
      const name = normalizeLucideIconName(value.slice("lucide:".length));
      return collectionIconMap[name] ? { type: "lucide", name } : fallback;
    }
    const name = normalizeLucideIconName(value);
    if (collectionIconMap[name]) return { type: "lucide", name };
  }
  return fallback;
}

export function defaultCollectionIcon(type: Collection["type"]): CollectionIconOption {
  if (type === "auth") return { type: "lucide", name: "shield" };
  if (type === "view") return { type: "lucide", name: "eye" };
  return { type: "lucide", name: "table" };
}

export function normalizeLucideIconName(value: string) {
  return value.trim().replaceAll("_", "-").replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

export function extractImportItems(raw: string): unknown[] {
  const parsed = JSON.parse(raw);
  if (Array.isArray(parsed)) return parsed;
  if (parsed && typeof parsed === "object") {
    const body = parsed as { items?: unknown; collections?: unknown };
    if (Array.isArray(body.items)) return body.items;
    if (Array.isArray(body.collections)) return body.collections;
  }
  throw new Error("Import JSON must be an array or include an items array");
}

export function discoveredTableKey(table: Pick<DiscoveredTable, "schema" | "table">) {
  return `${table.schema}.${table.table}`;
}

export function schemaImportNameIssues(tables: DiscoveredTable[], names: Record<string, string>) {
  const issues: string[] = [];
  const seen = new Map<string, string>();
  for (const table of tables) {
    const key = discoveredTableKey(table);
    const name = (names[key] || table.suggestedName || "").trim();
    if (!isValidCollectionName(name)) {
      issues.push(`${table.schema}.${table.table} needs a valid collection name: lowercase letters, numbers, and underscores, starting with a letter.`);
      continue;
    }
    const previous = seen.get(name);
    if (previous) {
      issues.push(`${previous} and ${table.schema}.${table.table} both import as "${name}".`);
      continue;
    }
    seen.set(name, `${table.schema}.${table.table}`);
  }
  return issues;
}

export function isValidCollectionName(name: string) {
  return /^[a-z][a-z0-9_]{0,58}$/.test(name) && !name.startsWith("pg_") && name !== "id" && name !== "created" && name !== "updated";
}

export function tableRelationSummary(table: DiscoveredTable, tables: DiscoveredTable[], names: Record<string, string>) {
  const bySource = new Map(tables.map((item) => [discoveredTableKey(item), item]));
  return table.foreignKeys.map((foreignKey) => {
    const targetKey = `${foreignKey.targetSchema}.${foreignKey.targetTable}`;
    const targetTable = bySource.get(targetKey);
    const targetName = targetTable ? targetTable.existingCollection || names[targetKey] || targetTable.suggestedName : foreignKey.targetTable;
    return {
      column: foreignKey.column,
      label: `${foreignKey.column} -> ${targetName}.${foreignKey.targetColumn}`,
    };
  });
}

export function stripSystemFields(record: RecordItem, collection?: Collection | null): RecordItem {
  const writable = collection ? new Set(collection.fields.filter(isRecordFormField).map((field) => field.name)) : null;
  const primaryKeyField = collection ? collectionPrimaryKeyFieldName(collection) : "id";
  const next: RecordItem = {};
  for (const [key, value] of Object.entries(record)) {
    if (key !== primaryKeyField && !["id", "created", "updated"].includes(key) && (!writable || writable.has(key))) {
      next[key] = value;
    }
  }
  return next;
}
