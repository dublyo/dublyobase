import { Braces, Sparkles, Calendar, CalendarCheck2, Hash, Image, KeyRound, Link2, List, Mail, PencilLine, Share2, ToggleLeft, Type } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { Collection, Field, FieldType, RecordItem } from "../../src/lib/types";
import type { RelationCardinality } from "./view-types";
import { reservedDataFieldNames } from "./constants";

// Field definition and record value helpers, split out of page.tsx.

export function renderValue(value: unknown) {
  if (value === null || value === undefined || value === "") return "-";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

export function parseRecordDraft(raw: string): { ok: true; value: RecordItem } | { ok: false; error: string } {
  try {
    const parsed = raw.trim() ? JSON.parse(raw) : {};
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return { ok: false, error: "Payload must be a JSON object." };
    }
    return { ok: true, value: parsed as RecordItem };
  } catch (error) {
    return { ok: false, error: error instanceof Error ? error.message : "Invalid JSON." };
  }
}

export function isRecordFormField(field: Field): boolean {
  if (field.type === "password") return true;
  return !field.hidden && field.type !== "file" && field.type !== "autodate";
}

export function fieldIsMultiple(field: Field): boolean {
  if (Boolean(field.options?.multiple) || Boolean(field.options?.multi)) return true;
  const maxSelect = field.options?.maxSelect;
  return typeof maxSelect === "number" && maxSelect > 1;
}

export function toDateTimeLocal(value: unknown) {
  if (typeof value !== "string" || !value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

export function formatJSONInput(value: unknown) {
  if (value === undefined || value === null || value === "") return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return "";
  }
}

export function sanitizeEditorHTML(value: string) {
  if (typeof window === "undefined" || !value) return value || "";
  const doc = new DOMParser().parseFromString(value, "text/html");
  doc.querySelectorAll("script,style,iframe,object,embed,link,meta").forEach((node) => node.remove());
  doc.body.querySelectorAll("*").forEach((node) => {
    for (const attr of Array.from(node.attributes)) {
      const name = attr.name.toLowerCase();
      const attrValue = attr.value.trim().toLowerCase();
      if (name.startsWith("on") || ((name === "href" || name === "src") && attrValue.startsWith("javascript:"))) {
        node.removeAttribute(attr.name);
      }
    }
  });
  return doc.body.innerHTML;
}

export function newDefaultField(type: FieldType = "text"): Field {
  return fieldWithType({ name: "", type, required: false, options: {} }, type);
}

export function isVisibleRecordField(field: Field): boolean {
  return !field.hidden && field.type !== "password";
}

export function canSearchField(field: Field): boolean {
  if (field.hidden || field.type === "password") return false;
  return ["text", "editor", "email", "url", "select", "number", "bool", "date", "autodate", "relation"].includes(field.type);
}

export function isReservedDataFieldName(name: string): boolean {
  const normalized = name.trim().toLowerCase();
  return reservedDataFieldNames.has(normalized) || normalized.startsWith("_dbo") || normalized.startsWith("pg_");
}

export function relationDisplayFieldOptions(collection?: Collection, currentDisplayField?: unknown): Field[] {
  const fields = (collection?.fields ?? []).filter((field) => field.type !== "password" && !isReservedDataFieldName(field.name));
  const seen = new Set(fields.map((field) => field.name));
  const out = [...fields];
  const addSynthetic = (name: unknown) => {
    if (typeof name !== "string") return;
    const normalized = name.trim();
    if (!normalized || seen.has(normalized) || isReservedDataFieldName(normalized)) return;
    seen.add(normalized);
    out.push({ name: normalized, type: "text" });
  };
  if (collection?.system && collection.name === "users") {
    addSynthetic("email");
  }
  addSynthetic(currentDisplayField);
  return out;
}

export function fieldTypeIcon(type: FieldType): LucideIcon {
  switch (type) {
    case "editor":
      return PencilLine;
    case "password":
      return KeyRound;
    case "number":
      return Hash;
    case "vector":
      return Sparkles;
    case "bool":
      return ToggleLeft;
    case "date":
      return Calendar;
    case "autodate":
      return CalendarCheck2;
    case "email":
      return Mail;
    case "url":
      return Link2;
    case "select":
      return List;
    case "json":
      return Braces;
    case "relation":
      return Share2;
    case "file":
      return Image;
    case "text":
    default:
      return Type;
  }
}

export function fieldMetaSummary(field: Field) {
  const parts = [];
  if (field.hidden) parts.push("hidden");
  if (field.presentable) parts.push("presentable");
  if (field.searchable) parts.push("searchable");
  if (typeof field.options?.sourceColumn === "string") parts.push(`source: ${field.options.sourceColumn}`);
  if (field.type === "relation" && field.options?.collection) parts.push(`to ${field.options.collection}`);
  if (field.type === "file" && field.options?.multiple) parts.push("multiple");
  return parts.length ? parts.join(" · ") : "default";
}

export function fieldWithType(field: Field, type: FieldType): Field {
  const hidden = type === "password" ? true : field.hidden;
  const nextField = {
    ...field,
    type,
    hidden,
    presentable: type === "password" || hidden ? false : field.presentable,
    options: defaultOptionsForType(type, field.options),
  };
  return {
    ...nextField,
    searchable: Boolean(field.searchable) && canSearchField(nextField),
  };
}

export function cleanField(field: Field): Field {
  const name = field.name.trim();
  const type = field.type;
  const hidden = Boolean(field.hidden) || type === "password";
  const normalizedField = { ...field, type, hidden };
  return {
    name,
    type,
    required: Boolean(field.required),
    hidden,
    presentable: Boolean(field.presentable) && !hidden,
    searchable: Boolean(field.searchable) && canSearchField(normalizedField),
    help: field.help?.trim() || undefined,
    options: defaultOptionsForType(type, field.options),
  };
}

export function defaultOptionsForType(type: FieldType, options: Record<string, unknown> = {}): Record<string, unknown> {
  const withNumber = (key: string) => {
    const value = options[key];
    return typeof value === "number" && Number.isFinite(value) ? { [key]: value } : {};
  };
  const withBool = (key: string) => (Boolean(options[key]) ? { [key]: true } : {});
  const withString = (key: string) => {
    const value = typeof options[key] === "string" ? options[key].trim() : "";
    return value ? { [key]: value } : {};
  };
  const withStringList = (key: string) => {
    const values = optionValues(options, key);
    return values.length > 0 ? { [key]: values } : {};
  };
  const sourceOptions = {
    ...withString("sourceColumn"),
    ...withString("sourceType"),
  };
  if (type === "text" || type === "url" || type === "password") {
    return {
      ...sourceOptions,
      ...withNumber("min"),
      ...withNumber("max"),
      ...withString("pattern"),
      ...(type === "password" ? withNumber("cost") : {}),
    };
  }
  if (type === "vector") {
    // Same trap as decimal: without this branch the editor would collect
    // dimensions and metric and then drop them on save.
    return {
      ...sourceOptions,
      ...withNumber("dimensions"),
      ...withString("metric"),
    };
  }
  if (type === "decimal") {
    // Without this branch the editor showed precision/scale/min/max and then
    // dropped them, so every decimal field silently became numeric(18,2).
    return {
      ...sourceOptions,
      ...withNumber("precision"),
      ...withNumber("scale"),
      ...withString("min"),
      ...withString("max"),
      ...withString("computed"),
    };
  }
  if (type === "number") {
    return {
      ...sourceOptions,
      ...withString("computed"),
      ...withNumber("min"),
      ...withNumber("max"),
      ...withBool("onlyInt"),
    };
  }
  if (type === "email") {
    return {
      ...sourceOptions,
      ...withStringList("onlyDomains"),
      ...withStringList("exceptDomains"),
    };
  }
  if (type === "editor") {
    return {
      ...sourceOptions,
      ...withNumber("maxSize"),
      ...withBool("convertURLs"),
    };
  }
  if (type === "json") {
    return {
      ...sourceOptions,
      ...withNumber("maxSize"),
    };
  }
  if (type === "autodate") {
    const onCreate = options.onCreate !== false;
    const onUpdate = Boolean(options.onUpdate);
    return {
      ...sourceOptions,
      ...(onCreate ? { onCreate: true } : {}),
      ...(onUpdate ? { onUpdate: true } : {}),
    };
  }
  if (type === "select") {
    return {
      ...sourceOptions,
      values: splitOptionValues(optionValuesText(options)),
      ...withNumber("minSelect"),
      ...withNumber("maxSelect"),
    };
  }
  if (type === "relation") {
    const rawOnDelete = typeof options.onDelete === "string" ? options.onDelete.trim() : "";
    const onDelete = rawOnDelete || (Boolean(options.cascadeDelete) ? "cascade" : "");
    const relationType = relationOptionType(options);
    const multipleRelation = relationType === "one_to_many" || relationType === "many_to_many" || Boolean(options.multiple) || Boolean(options.multi);
    const normalized: Record<string, unknown> = {
      ...sourceOptions,
      collection: typeof options.collection === "string" ? options.collection : "",
      relationType,
      storage: relationType === "many_to_one" || relationType === "one_to_one" ? "foreign_key" : "array_ids",
      ...withString("displayField"),
      ...withString("reverseName"),
      ...withString("targetSchema"),
      ...withString("targetTable"),
      ...withString("targetColumn"),
      ...(onDelete ? { onDelete } : {}),
      ...withNumber("minSelect"),
      ...(relationType === "many_to_one" || relationType === "one_to_one" ? {} : withNumber("maxSelect")),
      ...(multipleRelation ? { multiple: true } : {}),
      ...(relationType === "one_to_one" ? { unique: true } : withBool("unique")),
    };
    if (relationType === "one_to_many" || relationType === "many_to_many") {
      Object.assign(normalized, withNumber("perPage"));
    }
    if (relationType === "many_to_many") {
      Object.assign(
        normalized,
        withString("junctionCollection"),
        withString("junctionSourceField"),
        withString("junctionTargetField"),
        withString("junctionFieldLocation"),
        withBool("allowDuplicates"),
      );
    }
    return normalized;
  }
  if (type === "file") {
    return {
      ...sourceOptions,
      ...withBool("multiple"),
      ...withBool("protected"),
      ...withNumber("maxSelect"),
      ...withNumber("maxSize"),
      ...withStringList("mimeTypes"),
      ...withStringList("thumbs"),
    };
  }
  return sourceOptions;
}

export function setFieldOption(field: Field, key: string, value: unknown): Field {
  const options = { ...(field.options ?? {}), [key]: value };
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

export function setRelationTargetOption(field: Field, target: string, collections: Collection[]): Field {
  const next = setFieldOption(field, "collection", target);
  const displayField = typeof next.options?.displayField === "string" ? next.options.displayField : "";
  if (!displayField) return next;
  const targetCollection = collections.find((collection) => collection.name === target);
  const allowed = new Set(relationDisplayFieldOptions(targetCollection).map((item) => item.name));
  return allowed.has(displayField) ? next : setFieldOption(next, "displayField", "");
}

export function setNumberFieldOption(field: Field, key: string, value: string): Field {
  const trimmed = value.trim();
  const options = { ...(field.options ?? {}) };
  if (trimmed === "") {
    delete options[key];
  } else {
    const parsed = Number(trimmed);
    options[key] = Number.isFinite(parsed) ? parsed : options[key];
  }
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

export function numberOptionValue(options: Record<string, unknown> = {}, key: string) {
  const value = options[key];
  return typeof value === "number" && Number.isFinite(value) ? String(value) : "";
}

// Decimal bounds stay strings end to end — parsing them as JS numbers would
// reintroduce the float rounding the decimal field type exists to avoid.

export function setStringFieldOption(field: Field, key: string, value: string): Field {
  const trimmed = value.trim();
  const options = { ...(field.options ?? {}) };
  if (trimmed === "") {
    delete options[key];
  } else {
    options[key] = trimmed;
  }
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

export function stringOptionValue(options: Record<string, unknown> = {}, key: string) {
  const value = options[key];
  return typeof value === "string" ? value : "";
}

export function optionValuesText(options: Record<string, unknown> = {}, key = "values") {
  return optionValues(options, key).join("\n");
}

export function optionValues(options: Record<string, unknown> = {}, key = "values") {
  const values = options[key];
  if (Array.isArray(values)) {
    return values.map((value) => String(value).trim()).filter(Boolean);
  }
  return [];
}

export function splitOptionValues(raw: string) {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const line of raw.split(/\r?\n|,/)) {
    const value = line.trim();
    if (!value || seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out;
}

export function relationOptionType(options: Record<string, unknown>): RelationCardinality {
  const raw = typeof options.relationType === "string" ? options.relationType : "";
  const normalized = raw.trim().toLowerCase().replaceAll("-", "_").replaceAll(" ", "_");
  if (normalized === "one_to_one" || normalized === "one_to_many" || normalized === "many_to_many" || normalized === "many_to_one") {
    return normalized;
  }
  const maxSelect = typeof options.maxSelect === "number" ? options.maxSelect : 0;
  if (Boolean(options.unique) && !Boolean(options.multiple) && !Boolean(options.multi)) return "one_to_one";
  return Boolean(options.multiple) || Boolean(options.multi) || maxSelect > 1 ? "many_to_many" : "many_to_one";
}
