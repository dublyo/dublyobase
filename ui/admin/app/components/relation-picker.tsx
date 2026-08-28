"use client";

import { ApiError, getRecord, listRecords } from "../../src/lib/api";
import type { Collection, Field, RecordItem } from "../../src/lib/types";
import { canSearchField, renderValue } from "../lib/fields";
import { buildRelationConstraint, relationLabelFor, relationSecondaryFor, singleRelationFields } from "../lib/relations";
import type { RelationAnchor } from "../lib/view-types";
import { Search, X } from "lucide-react";
import { useEffect, useMemo, useState } from "react";

export function renderRelationCell(record: RecordItem, field: Field, collections: Collection[]) {
  const targetName = typeof field.options?.collection === "string" ? field.options.collection : "";
  const target = collections.find((c) => c.name === targetName);
  const displayField = typeof field.options?.displayField === "string" ? field.options.displayField : undefined;
  const expand = (record.expand as Record<string, unknown> | undefined)?.[field.name];
  if (Array.isArray(expand)) {
    if (expand.length === 0) return "-";
    return expand.map((item) => relationLabelFor(item as RecordItem, target, displayField)).join(", ");
  }
  if (expand && typeof expand === "object") {
    return relationLabelFor(expand as RecordItem, target, displayField) || "-";
  }
  return renderValue(record[field.name]);
}


// ── Relational context ──────────────────────────────────────────────────
// A form is a set of relation fields, and the schema is a directed graph, so
// the values already chosen constrain the ones still to be chosen. Rather than
// special-casing directions, everything reduces to ANCHORS: a collection plus a
// record id that this form is known to be about.
//
//   explicit  a relation field on this form that already has a value
//   derived   a relation value read FROM an explicit anchor's own record
//
// Choosing an invoice on a payment yields an explicit `invoices` anchor, and
// reading that invoice's own `patient` yields a derived `patients` anchor — so
// the patient field can then be narrowed even though `patients` has no relation
// back to `invoices`. The same machinery scopes an order's addresses in an
// ecommerce schema, or a project's tasks in a tracker; nothing here knows what
// a patient or an invoice is.

export function useRelationAnchors(
  draft: RecordItem,
  collection: Collection,
  collections: Collection[],
  token: string | null,
  project: string,
): RelationAnchor[] {
  const explicit = useMemo<RelationAnchor[]>(() => {
    const out: RelationAnchor[] = [];
    for (const field of singleRelationFields(collection)) {
      const value = draft[field.name];
      const target = typeof field.options?.collection === "string" ? field.options.collection : "";
      if (typeof value === "string" && value && target) {
        out.push({ collection: target, value, sourceField: field.name, formField: field.name });
      }
    }
    return out;
  }, [draft, collection]);

  const [derived, setDerived] = useState<RelationAnchor[]>([]);
  const key = explicit.map((a) => `${a.collection}:${a.value}`).sort().join("|");

  useEffect(() => {
    let cancelled = false;
    if (!token || explicit.length === 0) {
      setDerived([]);
      return;
    }
    (async () => {
      const out: RelationAnchor[] = [];
      for (const anchor of explicit) {
        const anchorCollection = collections.find((c) => c.name === anchor.collection);
        if (!anchorCollection) continue;
        try {
          const record = await getRecord(token, project, anchor.collection, anchor.value);
          for (const field of singleRelationFields(anchorCollection)) {
            const value = record[field.name];
            const target = typeof field.options?.collection === "string" ? field.options.collection : "";
            if (typeof value === "string" && value && target) {
              out.push({ collection: target, value, sourceField: field.name, viaCollection: anchor.collection });
            }
          }
        } catch {
          // an anchor we cannot read simply contributes nothing
        }
      }
      if (!cancelled) setDerived(out);
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, token, project, collections]);

  return useMemo(() => {
    // Explicit anchors win over derived ones for the same collection: what the
    // operator picked outranks what was inferred on their behalf.
    const seen = new Set(explicit.map((a) => a.collection));
    return [...explicit, ...derived.filter((a) => !seen.has(a.collection))];
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [explicit, derived]);
}

export function RelationPicker({
  field,
  value,
  collections,
  collection,
  anchors,
  selfId,
  token,
  project,
  onChange,
  disabled,
}: {
  field: Field;
  value: unknown;
  collections: Collection[];
  collection: Collection;
  anchors: RelationAnchor[];
  selfId: string;
  token: string | null;
  project: string;
  onChange: (value: unknown) => void;
  disabled?: boolean;
}) {
  const targetName = typeof field.options?.collection === "string" ? field.options.collection : "";
  const target = collections.find((c) => c.name === targetName);
  const displayField = typeof field.options?.displayField === "string" ? field.options.displayField : undefined;
  const multiple = Boolean(field.options?.multiple) || Number(field.options?.maxSelect ?? 1) > 1;

  const selectedIds = useMemo<string[]>(() => {
    if (multiple) return Array.isArray(value) ? value.map(String).filter(Boolean) : [];
    return typeof value === "string" && value ? [value] : [];
  }, [value, multiple]);

  const scope = useMemo(
    () => buildRelationConstraint(field, targetName, anchors, collections, collection, selfId),
    [field, targetName, anchors, collections, collection, selfId],
  );
  const [scopeOff, setScopeOff] = useState(false);
  const activeScope = scopeOff ? null : scope;

  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [options, setOptions] = useState<RecordItem[]>([]);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  const searchable = useMemo(
    () => Boolean(target?.fields.some((f) => f.searchable && canSearchField(f))),
    [target],
  );

  // Resolve labels for ids already stored on the record, so an existing value
  // never renders as a bare uuid while the user is looking at it.
  useEffect(() => {
    let cancelled = false;
    const missing = selectedIds.filter((id) => !labels[id]);
    if (!token || !targetName || missing.length === 0) return;
    (async () => {
      const found: Record<string, string> = {};
      for (const id of missing.slice(0, 25)) {
        try {
          const record = await getRecord(token, project, targetName, id);
          found[id] = relationLabelFor(record, target, displayField) || id.slice(0, 8) + "…";
        } catch {
          found[id] = "(not found)";
        }
      }
      if (!cancelled && Object.keys(found).length) setLabels((prev) => ({ ...prev, ...found }));
    })();
    return () => {
      cancelled = true;
    };
  }, [selectedIds, labels, token, project, targetName, target, displayField]);

  // Debounced browse of the target collection.
  useEffect(() => {
    if (!open || !token || !targetName) return;
    let cancelled = false;
    setLoading(true);
    setError("");
    const handle = window.setTimeout(async () => {
      try {
        const response = await listRecords(token, project, targetName, {
          page: 1,
          perPage: 25,
          search: searchable ? query : "",
          skipTotal: true,
          filter: activeScope ? activeScope.filter : "",
        });
        if (cancelled) return;
        setOptions(response.items);
        setLabels((prev) => {
          const next = { ...prev };
          for (const item of response.items) {
            const id = typeof item.id === "string" ? item.id : "";
            if (id) next[id] = relationLabelFor(item, target, displayField) || id.slice(0, 8) + "…";
          }
          return next;
        });
      } catch (err) {
        if (!cancelled) setError(err instanceof ApiError ? err.message : "Could not load records");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, 220);
    return () => {
      cancelled = true;
      window.clearTimeout(handle);
    };
  }, [open, query, token, project, targetName, target, displayField, searchable, activeScope]);

  if (!targetName) {
    return <div className="pb-inline-alert danger">This relation has no target collection configured.</div>;
  }

  const pick = (id: string) => {
    if (multiple) {
      if (selectedIds.includes(id)) return;
      onChange([...selectedIds, id]);
    } else {
      onChange(id);
      setOpen(false);
    }
    setQuery("");
  };
  const remove = (id: string) => {
    if (multiple) onChange(selectedIds.filter((x) => x !== id).length ? selectedIds.filter((x) => x !== id) : undefined);
    else onChange(undefined);
  };
  const labelOf = (id: string) => labels[id] || id.slice(0, 8) + "…";
  const filtered = options.filter((o) => {
    const id = typeof o.id === "string" ? o.id : "";
    if (!id) return false;
    if (multiple && selectedIds.includes(id)) return false;
    // when the target has no searchable field the API cannot filter, so filter here
    if (!searchable && query) return labelOf(id).toLowerCase().includes(query.toLowerCase());
    return true;
  });

  return (
    <div className="pb-relation">
      {selectedIds.length > 0 ? (
        <div className="pb-chips">
          {selectedIds.map((id) => (
            <span key={id} className="pb-chip" title={id}>
              {labelOf(id)}
              {disabled ? null : (
                <button type="button" aria-label={`Remove ${labelOf(id)}`} onClick={() => remove(id)}>
                  <X className="h-3 w-3" />
                </button>
              )}
            </span>
          ))}
        </div>
      ) : null}
      {disabled ? null : (
        <div className="pb-relation-search">
          <input
            value={query}
            onFocus={() => setOpen(true)}
            onChange={(event) => {
              setQuery(event.target.value);
              setOpen(true);
            }}
            placeholder={multiple || selectedIds.length === 0 ? `Search ${targetName}…` : `Replace selection…`}
            aria-label={`Search ${targetName}`}
          />
          <button type="button" className="pb-btn sm transparent secondary" onClick={() => setOpen((v) => !v)}>
            {open ? "Close" : "Browse"}
          </button>
        </div>
      )}
      {open && !disabled ? (
        <div className="pb-relation-list" role="listbox">
          {scope && scope.reasons.length > 0 ? (
            <div className="pb-relation-scope">
              {activeScope ? (
                <>
                  Showing only {targetName} <b>{scope.reasons.join(" and ")}</b>
                  <button type="button" onClick={() => setScopeOff(true)}>Show all</button>
                </>
              ) : (
                <>
                  Showing all {targetName}, ignoring the rest of this form
                  <button type="button" onClick={() => setScopeOff(false)}>Scope again</button>
                </>
              )}
            </div>
          ) : null}
          {loading ? <div className="pb-relation-empty">Loading…</div> : null}
          {error ? <div className="pb-relation-empty danger">{error}</div> : null}
          {!loading && !error && filtered.length === 0 ? (
            <div className="pb-relation-empty">
              No matching records in {targetName}
              {activeScope && activeScope.reasons.length ? " " + activeScope.reasons.join(" and ") : ""}.
            </div>
          ) : null}
          {filtered.map((item) => {
            const id = String(item.id);
            return (
              <button type="button" role="option" aria-selected={false} key={id} className="pb-relation-option" onClick={() => pick(id)}>
                <span className="pb-relation-label">{labelOf(id)}</span>
                <span className="pb-relation-id">{relationSecondaryFor(item, target, labelOf(id))}</span>
              </button>
            );
          })}
          {!searchable && target ? (
            <div className="pb-relation-hint">
              No field in <b>{targetName}</b> is marked searchable, so this list shows the most recent 25. Mark a field
              searchable in the collection editor to search it.
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
