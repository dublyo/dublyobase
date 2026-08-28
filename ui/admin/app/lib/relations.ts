import type { Collection, Field, RecordItem } from "../../src/lib/types";
import type { RelationAnchor, RelationCardinality, RelationConstraint, RelationEdge } from "./view-types";
import { canSearchField, defaultOptionsForType, fieldIsMultiple, optionValues } from "./fields";

// Relation modelling helpers, split out of page.tsx.

export function expandableRelationFields(collection?: Collection | null): string[] {
  if (!collection) return [];
  return collection.fields
    .filter((field) => field.type === "relation" && !field.hidden && typeof field.options?.collection === "string")
    .map((field) => field.name);
}

export function relationExpandParam(collection?: Collection | null): string {
  return expandableRelationFields(collection).join(",");
}

// The label a related record should show. Falls back through the configured
// display field, then any presentable field, then common name-ish columns, and
// finally a shortened id — so a relation is never rendered as a raw UUID unless
// there is genuinely nothing else to show.

export function relationLabelFor(record: RecordItem | undefined, target?: Collection, displayField?: string): string {
  if (!record) return "";
  const read = (name?: string) => {
    if (!name) return "";
    const v = record[name];
    return typeof v === "string" || typeof v === "number" ? String(v) : "";
  };
  const named = (re: RegExp) =>
    target?.fields.find((f) => !f.hidden && (f.type === "text" || f.type === "email") && re.test(f.name))?.name;
  // A human name beats an identifier: someone who searched "Fahad" and is shown
  // "MRN-2026003" cannot confirm they picked the right person, which is the
  // whole reason for having a picker. Identifiers are still used when the
  // collection has no name-like column (invoice_no, sku, code).
  const label =
    read(displayField) ||
    read(target?.fields.find((f) => f.presentable && !f.hidden)?.name) ||
    read(named(/^(full_?name|name|display_?name|title|label|subject)$/)) ||
    read(named(/name|title|label|subject/)) ||
    read(named(/(^|_)(no|number|code|ref|reference|sku|mrn)$/)) ||
    read(named(/mrn|code|reference|sku|email/));
  if (label) return label;
  const id = typeof record.id === "string" ? record.id : "";
  return id ? id.slice(0, 8) + "…" : "";
}

// The muted text on the right of a picker row. An identifier the operator would
// recognise is far more use for disambiguation than a slice of a uuid.

export function relationSecondaryFor(record: RecordItem | undefined, target?: Collection, primary?: string): string {
  if (!record) return "";
  const idish = target?.fields.find(
    (f) => !f.hidden && f.type === "text" && /(^|_)(no|number|code|ref|reference|sku|mrn)$/.test(f.name),
  )?.name;
  const value = idish ? record[idish] : undefined;
  const text = typeof value === "string" || typeof value === "number" ? String(value) : "";
  if (text && text !== primary) return text;
  const id = typeof record.id === "string" ? record.id : "";
  return id ? id.slice(0, 8) + "…" : "";
}

export function singleRelationFields(collection?: Collection): Field[] {
  if (!collection) return [];
  return collection.fields.filter(
    (f) => f.type === "relation" && !f.hidden && typeof f.options?.collection === "string" && !fieldIsMultiple(f),
  );
}

// Links from `collection` to `targetName`. Used to decide both reachability and
// ambiguity: more than one link means the schema does not say which is meant,
// and guessing would be worse than not scoping.

export function linksTo(collection: Collection | undefined, targetName: string): Field[] {
  return singleRelationFields(collection).filter((f) => f.options?.collection === targetName);
}

export function buildRelationConstraint(
  field: Field,
  targetName: string,
  anchors: RelationAnchor[],
  collections: Collection[],
  ownerCollection: Collection,
  selfId: string,
): RelationConstraint | null {
  const target = collections.find((c) => c.name === targetName);
  const clauses: Record<string, unknown>[] = [];
  const reasons: string[] = [];

  const identity = anchors.find((a) => a.collection === targetName && a.formField !== field.name);
  if (identity) {
    clauses.push({ id: { _eq: identity.value } });
    reasons.push(
      identity.viaCollection
        ? `determined by the selected ${identity.viaCollection}`
        : `must match the selected ${identity.sourceField}`,
    );
  } else {
    for (const anchor of anchors) {
      if (anchor.formField === field.name) continue;
      const links = linksTo(target, anchor.collection);
      if (links.length !== 1) continue;
      clauses.push({ [links[0].name]: { _eq: anchor.value } });
      reasons.push(
        anchor.viaCollection
          ? `for the ${anchor.collection} of the selected ${anchor.viaCollection}`
          : `for the selected ${anchor.sourceField}`,
      );
    }
  }

  if (targetName === ownerCollection.name && selfId) {
    clauses.push({ id: { _neq: selfId } });
  }
  if (clauses.length === 0) return null;
  const filter = clauses.length === 1 ? clauses[0] : { _and: clauses };
  return { filter: JSON.stringify(filter), reasons };
}

// ── Relation picker ──────────────────────────────────────────────────────
// Relations used to be a text box asking for a UUID, and a textarea asking for
// one UUID per line. Both invited exactly the mistake they look like: pasting a
// plausible-but-wrong id, with nothing to confirm what it points at. This
// browses the related collection instead — searching it through the same record
// list API, so RLS decides what is offered — and stores the id underneath.

export function relationTargetName(field: Field): string {
  return typeof field.options?.collection === "string" ? field.options.collection : "";
}

export function collectionRelationEdges(collections: Collection[]): RelationEdge[] {
  return collections.flatMap((collection) =>
    collection.fields
      .filter((field) => field.type === "relation")
      .map((field) => ({
        sourceCollection: collection.name,
        sourceField: field.name,
        targetCollection: relationTargetName(field),
        displayField: typeof field.options?.displayField === "string" ? field.options.displayField : "",
        cardinality: relationCardinalityType(field),
        required: Boolean(field.required),
        multiple: fieldIsMultiple(field),
      })),
  );
}

export function collectionReverseRelations(collectionName: string, collections: Collection[]) {
  return collections.flatMap((collection) =>
    collection.fields
      .filter((field) => field.type === "relation" && relationTargetName(field) === collectionName)
      .map((field) => ({
        collection: collection.name,
        field: field.name,
        cardinality: relationCardinalityType(field),
        multiple: fieldIsMultiple(field),
      })),
  );
}

export function relationCardinalityType(field: Field): RelationCardinality {
  const raw = typeof field.options?.relationType === "string" ? field.options.relationType : "";
  const normalized = raw.trim().toLowerCase().replaceAll("-", "_").replaceAll(" ", "_");
  if (normalized === "one_to_one" || normalized === "one_to_many" || normalized === "many_to_many" || normalized === "many_to_one") {
    return normalized;
  }
  if (Boolean(field.options?.unique) && !fieldIsMultiple(field)) return "one_to_one";
  return fieldIsMultiple(field) ? "many_to_many" : "many_to_one";
}

export function relationCardinalityLabel(type: RelationCardinality) {
  switch (type) {
    case "one_to_one":
      return "one-to-one";
    case "one_to_many":
      return "one-to-many";
    case "many_to_many":
      return "many-to-many";
    case "many_to_one":
    default:
      return "many-to-one";
  }
}

export function relationStorageLabel(field: Field) {
  const type = relationCardinalityType(field);
  if (type === "many_to_many") return "array ids + junction metadata";
  if (type === "one_to_many") return "array ids";
  if (type === "one_to_one") return "uuid foreign key + unique index";
  return "uuid foreign key";
}

export function relationConstraintLabel(field: Field) {
  const type = relationCardinalityType(field);
  if (type === "one_to_one") return "unique";
  if (type === "many_to_one") return relationOnDeleteValue(field);
  if (type === "many_to_many" && field.options?.allowDuplicates) return "duplicates allowed";
  return "multiple";
}

export function setRelationCardinality(field: Field, type: RelationCardinality): Field {
  const options: Record<string, unknown> = { ...(field.options ?? {}), relationType: type };
  const clearJunctionOptions = () => {
    delete options.junctionCollection;
    delete options.junctionSourceField;
    delete options.junctionTargetField;
    delete options.junctionFieldLocation;
    delete options.allowDuplicates;
  };
  if (type === "many_to_one") {
    delete options.multiple;
    delete options.multi;
    delete options.maxSelect;
    delete options.unique;
    delete options.perPage;
    clearJunctionOptions();
    options.storage = "foreign_key";
  } else if (type === "one_to_one") {
    delete options.multiple;
    delete options.multi;
    delete options.maxSelect;
    delete options.perPage;
    clearJunctionOptions();
    options.unique = true;
    options.storage = "foreign_key";
  } else if (type === "one_to_many") {
    options.multiple = true;
    delete options.unique;
    clearJunctionOptions();
    options.storage = "array_ids";
    if (!options.reverseName) options.reverseName = relationReversePlaceholder(field, type);
  } else {
    options.multiple = true;
    delete options.unique;
    options.storage = "array_ids";
    if (!options.reverseName) options.reverseName = relationReversePlaceholder(field, type);
    if (!options.junctionCollection) options.junctionCollection = relationJunctionName(field);
    if (!options.junctionSourceField) options.junctionSourceField = `${safeIdentifier(field.name || "source")}_id`;
    if (!options.junctionTargetField) options.junctionTargetField = `${safeIdentifier(String(options.collection || "target"))}_id`;
    if (!options.perPage) options.perPage = 25;
  }
  if (!options.onDelete) options.onDelete = "restrict";
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

export function relationReversePlaceholder(field: Field, type: RelationCardinality) {
  const base = safeIdentifier(field.name || "related");
  if (type === "one_to_one") return `${base}_detail`;
  if (type === "many_to_one") return `${base}_records`;
  if (type === "one_to_many") return `${base}_parent`;
  return `${base}_items`;
}

export function relationJunctionName(field: Field) {
  const source = safeIdentifier(field.name || "source");
  const target = safeIdentifier(relationTargetName(field) || "target");
  return `${source}_${target}_junction`;
}

export function safeIdentifier(value: string) {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9_]+/g, "_").replace(/^_+|_+$/g, "").replace(/_+/g, "_");
  if (!normalized) return "item";
  return /^[a-z_]/.test(normalized) ? normalized : `_${normalized}`;
}

export function relationOnDeleteValue(field: Field): string {
  const raw = typeof field.options?.onDelete === "string" ? field.options.onDelete.trim() : "";
  return raw || (field.options?.cascadeDelete ? "cascade" : "restrict");
}

export function collectionIndexHints(fields: Field[]) {
  const hints: Array<{ kind: string; field: string; detail: string }> = [];
  for (const field of fields) {
    if (field.searchable && canSearchField(field)) {
      hints.push({ kind: "search", field: field.name, detail: "Included in record search" });
    }
    if (field.type === "relation") {
      hints.push({ kind: field.options?.unique ? "unique relation" : "relation", field: field.name, detail: `Targets ${relationTargetName(field) || "unconfigured collection"}` });
    }
    if (field.type === "email" && optionValues(field.options, "onlyDomains").length > 0) {
      hints.push({ kind: "email constraint", field: field.name, detail: `Only ${optionValues(field.options, "onlyDomains").join(", ")}` });
    }
    if (field.type === "select") {
      hints.push({ kind: "select constraint", field: field.name, detail: `${optionValues(field.options).length} allowed values` });
    }
  }
  return hints;
}
