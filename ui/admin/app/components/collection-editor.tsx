"use client";

import type { LucideIcon } from "lucide-react";

import type { Collection, CollectionIconOption, Field, FieldType } from "../../src/lib/types";
import { collectionImportedFromOptions, collectionPrimaryKeyFieldName, collectionSourceTable, collectionStandardSystemColumns } from "../lib/collections";
import { collectionIconChoices, fieldTypeChoices, fieldTypes } from "../lib/constants";
import { canSearchField, fieldMetaSummary, fieldWithType, numberOptionValue, optionValuesText, relationDisplayFieldOptions, setFieldOption, setNumberFieldOption, setRelationTargetOption, setStringFieldOption, splitOptionValues, stringOptionValue } from "../lib/fields";
import { collectionIndexHints, collectionReverseRelations, relationCardinalityLabel, relationCardinalityType, relationConstraintLabel, relationJunctionName, relationOnDeleteValue, relationReversePlaceholder, relationStorageLabel, relationTargetName, setRelationCardinality } from "../lib/relations";
import type { CollectionDraft, RelationCardinality, RuleDraft, View } from "../lib/view-types";
import { CollectionIcon, EmptyState, FieldTypeGlyph, Info } from "./ui";
import { Layers3, Link2, List, Plus, Save, Settings, Share2, Table2, Trash2, Type, X } from "lucide-react";
import { useEffect, useState } from "react";

export function CollectionIconPicker({ icon, onChange }: { icon: CollectionIconOption; onChange: (icon: CollectionIconOption) => void }) {
  const currentName = icon.type === "lucide" ? icon.name : "table";
  return (
    <fieldset className="pb-icon-picker">
      <legend>Icon</legend>
      <div className="pb-icon-picker-head">
        <div className="pb-icon-preview" aria-hidden="true">
          <CollectionIcon icon={{ type: "lucide", name: currentName }} />
        </div>
        <span>{collectionIconChoices.find((choice) => choice.name === currentName)?.label ?? "Table"}</span>
      </div>
      <div className="pb-icon-grid" role="list" aria-label="Collection icons">
        {collectionIconChoices.map((choice) => {
          const Icon = choice.icon;
          const selected = currentName === choice.name;
          return (
            <button key={choice.name} type="button" className={selected ? "active" : ""} aria-pressed={selected} aria-label={`Use ${choice.label} icon`} title={choice.label} onClick={() => onChange({ type: "lucide", name: choice.name })}>
              <Icon className="h-4 w-4" />
            </button>
          );
        })}
      </div>
    </fieldset>
  );
}

export function CollectionModal({
  mode,
  collection,
  collections,
  draft,
  setDraft,
  icon,
  setIcon,
  managed,
  setManaged,
  fields,
  setFields,
  rules,
  setRules,
  onAddField,
  onClose,
  onSubmit,
  onSave,
}: {
  mode: "create" | "settings";
  collection?: Collection;
  collections: Collection[];
  draft?: CollectionDraft;
  setDraft?: React.Dispatch<React.SetStateAction<CollectionDraft>>;
  icon: CollectionIconOption;
  setIcon: (icon: CollectionIconOption) => void;
  managed?: boolean;
  setManaged?: (managed: boolean) => void;
  fields: Field[];
  setFields: (fields: Field[]) => void;
  rules: RuleDraft;
  setRules: (rules: RuleDraft) => void;
  onAddField: () => void;
  onClose: () => void;
  onSubmit?: (event: React.FormEvent<HTMLFormElement>) => void;
  onSave?: () => void;
}) {
  const [tab, setTab] = useState<"fields" | "rules" | "options">("fields");
  const title = mode === "create" ? "Create collection" : "Collection settings";
  const imported = collection ? collectionImportedFromOptions(collection) : false;
  const manageReady = collection ? collectionStandardSystemColumns(collection) : false;
  const schemaLocked = imported && !managed;
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);
  return (
    <div className="pb-modal-layer" role="presentation">
      <form
        className="pb-modal collection-upsert-modal"
        onSubmit={(event) => {
          if (mode === "create" && onSubmit) {
            onSubmit(event);
            return;
          }
          event.preventDefault();
          onSave?.();
        }}
      >
        <header className="pb-modal-header">
          <h2>{title}</h2>
          <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </header>

        <div className="pb-modal-content">
          {mode === "create" && draft && setDraft ? (
            <div className="pb-collection-topline">
              <label className="pb-field">
                <span>Name</span>
                <input value={draft.name} onChange={(event) => setDraft((next) => ({ ...next, name: event.target.value }))} placeholder="e.g. posts" required />
              </label>
              <label className="pb-field compact-select">
                <span>Type</span>
                <select value={draft.type} onChange={(event) => setDraft((next) => ({ ...next, type: event.target.value as Collection["type"] }))}>
                  <option value="base">Base</option>
                  <option value="auth">Auth</option>
                </select>
              </label>
            </div>
          ) : null}

          {mode === "settings" && collection ? (
            <div className="pb-collection-title">
              <CollectionIcon collection={collection} icon={icon} />
              <div>
                <p>{collection.name}</p>
                <span>{collection.type} collection{imported ? ` · ${collectionSourceTable(collection)}` : ""}</span>
              </div>
            </div>
          ) : null}

          <CollectionIconPicker icon={icon} onChange={setIcon} />

          {mode === "settings" && imported ? (
            <div className="pb-managed-toggle">
              <div>
                <strong>Imported Postgres table</strong>
                <span>{manageReady ? "Field edits can be enabled because standard system columns exist." : "Field edits require id uuid, created, and updated columns first."}</span>
              </div>
              <label className="pb-checkline switchline">
                <input type="checkbox" checked={Boolean(managed)} disabled={!manageReady} onChange={(event) => setManaged?.(event.target.checked)} />
                Managed by Dublyobase
              </label>
            </div>
          ) : null}

          <div className="pb-tabs" role="tablist" aria-label="Collection editor">
            <button type="button" role="tab" aria-selected={tab === "fields"} className={`pb-tab-item ${tab === "fields" ? "active" : ""}`} onClick={() => setTab("fields")}>
              Fields
            </button>
            {imported ? null : (
              <button type="button" role="tab" aria-selected={tab === "rules"} className={`pb-tab-item ${tab === "rules" ? "active" : ""}`} onClick={() => setTab("rules")}>
                API rules
              </button>
            )}
            <button type="button" role="tab" aria-selected={tab === "options"} className={`pb-tab-item ${tab === "options" ? "active" : ""}`} onClick={() => setTab("options")}>
              Options
            </button>
          </div>

          {tab === "fields" ? (
            <>
              {schemaLocked ? <div className="pb-inline-alert warning">This imported table is staged for CRUD. Enable managed takeover before editing columns or field definitions.</div> : null}
              <SystemFieldPreview mode={mode} collection={collection} />
              <FieldRows fields={fields} collections={collections} onChange={setFields} onAdd={onAddField} readOnly={schemaLocked} />
            </>
          ) : null}
          {tab === "rules" && !imported ? <RuleInputs rules={rules} onChange={setRules} /> : null}
          {imported ? (
            <div className="pb-inline-alert warning">
              Access to this table is governed by its existing PostgreSQL grants and row-level security, not by Dublyobase API rules. Define policies on the source table from the SQL console.
            </div>
          ) : null}
          {tab === "options" ? <CollectionOptionsPanel collection={collection} fields={fields} collections={collections} icon={icon} imported={imported} managed={Boolean(managed)} /> : null}
        </div>

        <footer className="pb-modal-footer">
          <button type="button" className="pb-btn transparent" onClick={onClose}>
            Close
          </button>
          <button type="submit" className="pb-btn primary expanded-lg">
            {mode === "create" ? "Create" : "Save changes"}
          </button>
        </footer>
      </form>
    </div>
  );
}

export function SystemFieldPreview({ mode, collection }: { mode: "create" | "settings"; collection?: Collection }) {
  const primaryKey = collection ? collectionPrimaryKeyFieldName(collection) : "id";
  const standard = mode === "create" || !collection || collectionStandardSystemColumns(collection);
  const rows = [
    { name: primaryKey, type: "text", note: "Primary key", required: true },
    { name: "created", type: "autodate", note: "Create", required: standard },
    { name: "updated", type: "autodate", note: "Create/Update", required: standard },
  ];
  return (
    <div className="pb-system-field-preview" aria-label="System fields">
      {rows.map((row) => (
        <div key={row.name}>
          <FieldTypeGlyph type={row.type as FieldType} />
          <strong>{row.name}</strong>
          <span>{row.note}</span>
          {row.required ? <em>Required</em> : null}
        </div>
      ))}
    </div>
  );
}

export function FieldRows({
  fields,
  collections,
  onChange,
  onAdd,
  readOnly,
}: {
  fields: Field[];
  collections: Collection[];
  onChange: (fields: Field[]) => void;
  onAdd: (type?: FieldType) => void;
  readOnly?: boolean;
}) {
  const [pickerOpen, setPickerOpen] = useState(false);
  const [openRows, setOpenRows] = useState<Record<string, boolean>>({});
  const update = (index: number, field: Field) => onChange(fields.map((item, i) => (i === index ? field : item)));
  const addField = (type: FieldType) => {
    onAdd(type);
    setPickerOpen(false);
  };
  const toggleRow = (key: string) => setOpenRows((current) => ({ ...current, [key]: !(current[key] ?? false) }));
  return (
    <div className="pb-fields-editor">
      {fields.length === 0 ? <EmptyState label="No fields yet" /> : null}
      {fields.map((field, index) => {
        const rowKey = `${index}-${field.name || "field"}-${field.type}`;
        const open = openRows[rowKey] ?? (field.name === "" || field.type === "relation");
        return (
          <div key={`${field.name}-${index}`} className={`pb-field-row ${open ? "open" : ""}`}>
            <div className="pb-field-row-main">
              <label className="pb-field-type">
                <span className="sr-only">Type</span>
                <select value={field.type} disabled={readOnly} onChange={(event) => update(index, fieldWithType(field, event.target.value as FieldType))}>
                  {fieldTypes.map((type) => (
                    <option key={type} value={type}>
                      {type}
                    </option>
                  ))}
                </select>
              </label>
              <input value={field.name} disabled={readOnly} onChange={(event) => update(index, { ...field, name: event.target.value })} placeholder="Field name*" />
              <span className="pb-field-row-meta">{fieldMetaSummary(field)}</span>
              <label className="pb-checkline sm">
                <input type="checkbox" checked={Boolean(field.required)} disabled={readOnly} onChange={(event) => update(index, { ...field, required: event.target.checked })} />
                Required
              </label>
              <button type="button" aria-label={`${open ? "Collapse" : "Expand"} field ${field.name || index + 1}`} aria-expanded={open} className="pb-btn sm circle transparent" onClick={() => toggleRow(rowKey)}>
                <Settings className="h-4 w-4" />
              </button>
              <button type="button" aria-label={`Remove field ${field.name || index + 1}`} className="pb-btn sm circle transparent danger" disabled={readOnly} onClick={() => onChange(fields.filter((_, i) => i !== index))}>
                <Trash2 className="h-4 w-4" />
              </button>
            </div>
            {open ? <FieldOptionsEditor field={field} collections={collections} onChange={(next) => update(index, next)} readOnly={readOnly} /> : null}
          </div>
        );
      })}
      {!readOnly ? (
        <button type="button" className={`pb-new-field-toggle ${pickerOpen ? "open" : ""}`} onClick={() => setPickerOpen((value) => !value)} aria-expanded={pickerOpen}>
          <Plus className="h-4 w-4" />
          New field
        </button>
      ) : null}
      {pickerOpen && !readOnly ? (
        <div className="pb-field-type-picker" role="list" aria-label="Choose field type">
          {fieldTypeChoices.map((choice) => {
            const Icon = choice.icon;
            return (
              <button
                key={choice.label}
                type="button"
                className={`pb-field-type-choice ${choice.disabled ? "disabled" : ""}`}
                disabled={choice.disabled}
                title={choice.disabled ? `${choice.label} is not implemented yet` : `Add ${choice.label} field`}
                onClick={() => choice.type && addField(choice.type)}
              >
                <Icon className="h-5 w-5" />
                <span>{choice.label}</span>
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

export function CollectionOptionsPanel({
  collection,
  fields,
  collections,
  icon,
  imported,
  managed,
}: {
  collection?: Collection;
  fields: Field[];
  collections: Collection[];
  icon: CollectionIconOption;
  imported: boolean;
  managed: boolean;
}) {
  const relationFields = fields.filter((field) => field.type === "relation");
  const reverseRelations = collection ? collectionReverseRelations(collection.name, collections) : [];
  const indexHints = collectionIndexHints(fields);
  return (
    <div className="pb-collection-options-panel">
      <section>
        <h3>Collection options</h3>
        <div className="pb-info-grid compact">
          <Info label="Type" value={collection?.type ?? "base"} />
          <Info label="Icon" value={icon.type === "lucide" ? `lucide:${icon.name}` : "lucide:table"} />
          <Info label="Imported table" value={imported ? "yes" : "no"} />
          <Info label="Managed schema" value={imported ? (managed ? "enabled" : "staged") : "native"} />
          <Info label="Primary key" value={collection ? collectionPrimaryKeyFieldName(collection) : "id"} />
          <Info label="Source table" value={collection ? collectionSourceTable(collection) : ""} />
        </div>
      </section>
      <section>
        <h3>Indexes and constraints</h3>
        {indexHints.length === 0 ? (
          <div className="pb-inline-alert info">No searchable, unique, or relation constraints are configured yet.</div>
        ) : (
          <div className="pb-index-list">
            {indexHints.map((hint) => (
              <div key={`${hint.kind}-${hint.field}`}>
                <strong>{hint.kind}</strong>
                <span>{hint.field}</span>
                <em>{hint.detail}</em>
              </div>
            ))}
          </div>
        )}
      </section>
      <section>
        <h3>Relations</h3>
        {relationFields.length === 0 && reverseRelations.length === 0 ? (
          <div className="pb-inline-alert info">No relation fields point to or from this collection.</div>
        ) : (
          <div className="pb-relation-map">
            {relationFields.map((field) => (
              <div key={`out-${field.name}`}>
                <Share2 className="h-4 w-4" />
                <span>
                  <strong>{field.name}</strong>
                  <em>
                    {relationCardinalityLabel(relationCardinalityType(field))} to {relationTargetName(field) || "unconfigured"}
                    {typeof field.options?.reverseName === "string" && field.options.reverseName ? ` · reverse ${field.options.reverseName}` : ""}
                  </em>
                </span>
              </div>
            ))}
            {reverseRelations.map((relation) => (
              <div key={`in-${relation.collection}-${relation.field}`}>
                <Share2 className="h-4 w-4" />
                <span>
                  <strong>{relation.collection}.{relation.field}</strong>
                  <em>{relationCardinalityLabel(relation.cardinality)} points here</em>
                </span>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

export function FieldOptionsEditor({ field, collections, onChange, readOnly }: { field: Field; collections: Collection[]; onChange: (field: Field) => void; readOnly?: boolean }) {
  const searchSupported = canSearchField(field);
  const relationTarget = field.type === "relation" ? collections.find((collection) => collection.name === field.options?.collection) : undefined;
  const relationDisplayFields = relationDisplayFieldOptions(relationTarget, field.options?.displayField);
  const relationType = field.type === "relation" ? relationCardinalityType(field) : "many_to_one";
  const relationTypeChoices: Array<{ type: RelationCardinality; title: string; note: string; icon: LucideIcon }> = [
    { type: "many_to_one", title: "Many to one", note: "Many records here point to one target record.", icon: Link2 },
    { type: "one_to_one", title: "One to one", note: "One record here points to one unique target record.", icon: Share2 },
    { type: "one_to_many", title: "One to many", note: "One record here links to many target records.", icon: Layers3 },
    { type: "many_to_many", title: "Many to many", note: "Many records on both sides share related records.", icon: Table2 },
  ];
  const commonOptions = (
    <div className="pb-field-options common">
      <label className="pb-field">
        <span>Help text</span>
        <input value={field.help ?? ""} onChange={(event) => onChange({ ...field, help: event.target.value })} placeholder="Shown below the generated input" />
      </label>
      <label className="pb-checkline">
        <input
          type="checkbox"
          checked={Boolean(field.presentable)}
          disabled={Boolean(field.hidden) || field.type === "password"}
          onChange={(event) => onChange({ ...field, presentable: event.target.checked })}
        />
        Presentable
      </label>
      <label className="pb-checkline">
        <input
          type="checkbox"
          checked={Boolean(field.hidden)}
          onChange={(event) =>
            onChange({
              ...field,
              hidden: event.target.checked,
              presentable: event.target.checked ? false : field.presentable,
              searchable: event.target.checked ? false : field.searchable,
            })
          }
        />
        Hidden
      </label>
      <label className="pb-checkline" title={searchSupported ? "Include this field in the records search input" : "This field type cannot be searched"}>
        <input
          type="checkbox"
          checked={Boolean(field.searchable) && searchSupported}
          disabled={!searchSupported}
          onChange={(event) => onChange({ ...field, searchable: event.target.checked })}
        />
        Searchable
      </label>
    </div>
  );
  let typeOptions: React.ReactNode = null;
  if (field.type === "select") {
    typeOptions = (
      <div className="pb-field-options two">
        <label className="pb-field">
          <span>Values</span>
          <textarea value={optionValuesText(field.options)} onChange={(event) => onChange(setFieldOption(field, "values", splitOptionValues(event.target.value)))} rows={3} placeholder={"draft\npublished"} />
        </label>
        <div className="pb-option-grid">
          <label className="pb-field">
            <span>Min select</span>
            <input type="number" min={0} value={numberOptionValue(field.options, "minSelect")} onChange={(event) => onChange(setNumberFieldOption(field, "minSelect", event.target.value))} placeholder="0" />
          </label>
          <label className="pb-field">
            <span>Max select</span>
            <input type="number" min={1} value={numberOptionValue(field.options, "maxSelect")} onChange={(event) => onChange(setNumberFieldOption(field, "maxSelect", event.target.value))} placeholder="1" />
          </label>
        </div>
      </div>
    );
  } else if (field.type === "relation") {
    typeOptions = (
      <div className="pb-field-options relation-builder">
        <div className="pb-relation-type-grid" role="radiogroup" aria-label="Relation cardinality">
          {relationTypeChoices.map((choice) => {
            const Icon = choice.icon;
            const selected = relationType === choice.type;
            return (
              <button key={choice.type} type="button" role="radio" aria-checked={selected} className={selected ? "active" : ""} onClick={() => onChange(setRelationCardinality(field, choice.type))}>
                <Icon className="h-4 w-4" />
                <span>
                  <strong>{choice.title}</strong>
                  <em>{choice.note}</em>
                </span>
              </button>
            );
          })}
        </div>
        <div className="pb-info-grid compact">
          <Info label="Cardinality" value={relationCardinalityLabel(relationType)} />
          <Info label="Storage" value={relationStorageLabel(field)} />
          <Info label="Reverse label" value={String(field.options?.reverseName ?? "")} />
          <Info label="Constraint" value={relationConstraintLabel(field)} />
        </div>
        {relationType === "one_to_many" || relationType === "many_to_many" ? (
          <div className="pb-inline-alert info pb-relation-hint">
            Multi-record relations are saved as ordered record-id arrays in the current release. Junction metadata is stored now so the schema can be migrated to physical junction tables when that backend path is enabled.
          </div>
        ) : null}
        <div className="pb-relation-config-grid">
          <label className="pb-field">
            <span>Related collection</span>
            <select value={String(field.options?.collection ?? "")} onChange={(event) => onChange(setRelationTargetOption(field, event.target.value, collections))}>
              <option value="">Choose collection</option>
              {collections.map((collection) => (
                <option key={collection.id} value={collection.name}>
                  {collection.name}
                </option>
              ))}
            </select>
          </label>
          <label className="pb-field">
            <span>Display field</span>
            <select value={String(field.options?.displayField ?? "")} onChange={(event) => onChange(setFieldOption(field, "displayField", event.target.value))} disabled={!relationTarget}>
              <option value="">Auto</option>
              {relationDisplayFields.map((item) => (
                <option key={item.name} value={item.name}>
                  {item.name}
                </option>
              ))}
            </select>
            {relationTarget && relationDisplayFields.length === 0 ? <small>Reserved and password fields cannot be used as relation labels.</small> : null}
          </label>
          <label className="pb-field">
            <span>Reverse field name</span>
            <input value={String(field.options?.reverseName ?? "")} onChange={(event) => onChange(setFieldOption(field, "reverseName", event.target.value))} placeholder={relationReversePlaceholder(field, relationType)} />
          </label>
          <label className="pb-field">
            <span>On target delete</span>
            <select value={String(field.options?.onDelete ?? "")} onChange={(event) => onChange(setFieldOption(field, "onDelete", event.target.value))}>
              <option value="">Restrict</option>
              <option value="set_null">Set null</option>
              <option value="cascade">Cascade</option>
            </select>
          </label>
        </div>
        <div className="pb-relation-behavior-grid">
          <label className="pb-field">
            <span>Min records</span>
            <input type="number" min={0} value={numberOptionValue(field.options, "minSelect")} onChange={(event) => onChange(setNumberFieldOption(field, "minSelect", event.target.value))} placeholder={field.required ? "1" : "0"} />
          </label>
          <label className="pb-field">
            <span>Max records</span>
            <input type="number" min={1} value={numberOptionValue(field.options, "maxSelect")} onChange={(event) => onChange(setNumberFieldOption(field, "maxSelect", event.target.value))} placeholder={relationType === "many_to_one" || relationType === "one_to_one" ? "1" : "unlimited"} disabled={relationType === "many_to_one" || relationType === "one_to_one"} />
          </label>
          <label className="pb-checkline">
            <input type="checkbox" checked={relationOnDeleteValue(field) === "cascade"} onChange={(event) => onChange(setFieldOption(field, "onDelete", event.target.checked ? "cascade" : "restrict"))} />
            Cascade on target delete
          </label>
          <label className="pb-checkline">
            <input type="checkbox" checked={Boolean(field.options?.unique)} disabled={relationType === "one_to_one" || relationType === "one_to_many" || relationType === "many_to_many"} onChange={(event) => onChange(setFieldOption(field, "unique", event.target.checked))} />
            Unique relation
          </label>
        </div>
        {relationType === "many_to_many" ? (
          <details className="pb-relation-advanced" open>
            <summary>Junction metadata</summary>
            <div className="pb-relation-config-grid">
              <label className="pb-field">
                <span>Junction collection</span>
                <input value={String(field.options?.junctionCollection ?? "")} onChange={(event) => onChange(setFieldOption(field, "junctionCollection", event.target.value))} placeholder={relationJunctionName(field)} />
              </label>
              <label className="pb-field">
                <span>This side field</span>
                <input value={String(field.options?.junctionSourceField ?? "")} onChange={(event) => onChange(setFieldOption(field, "junctionSourceField", event.target.value))} placeholder={`${field.name || "source"}_id`} />
              </label>
              <label className="pb-field">
                <span>Related side field</span>
                <input value={String(field.options?.junctionTargetField ?? "")} onChange={(event) => onChange(setFieldOption(field, "junctionTargetField", event.target.value))} placeholder={`${relationTargetName(field) || "target"}_id`} />
              </label>
              <label className="pb-field">
                <span>Per page</span>
                <input type="number" min={1} value={numberOptionValue(field.options, "perPage")} onChange={(event) => onChange(setNumberFieldOption(field, "perPage", event.target.value))} placeholder="25" />
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={Boolean(field.options?.allowDuplicates)} onChange={(event) => onChange(setFieldOption(field, "allowDuplicates", event.target.checked))} />
                Allow duplicate pairs
              </label>
            </div>
          </details>
        ) : null}
        {field.options?.targetTable || field.options?.sourceColumn ? (
          <div className="pb-relation-source">
            <Info label="Source column" value={String(field.options?.sourceColumn ?? field.name)} />
            <Info label="Target table" value={[field.options?.targetSchema, field.options?.targetTable].filter(Boolean).join(".")} />
            <Info label="Target column" value={String(field.options?.targetColumn ?? "id")} />
          </div>
        ) : null}
      </div>
    );
  } else if (field.type === "file") {
    typeOptions = (
      <div className="pb-field-options two">
        <div className="pb-option-grid">
          <label className="pb-checkline">
            <input type="checkbox" checked={Boolean(field.options?.multiple)} onChange={(event) => onChange(setFieldOption(field, "multiple", event.target.checked))} />
            Allow multiple files
          </label>
          <label className="pb-checkline">
            <input type="checkbox" checked={Boolean(field.options?.protected)} onChange={(event) => onChange(setFieldOption(field, "protected", event.target.checked))} />
            Protected
          </label>
          <label className="pb-field">
            <span>Max select</span>
            <input type="number" min={1} value={numberOptionValue(field.options, "maxSelect")} onChange={(event) => onChange(setNumberFieldOption(field, "maxSelect", event.target.value))} placeholder="1" />
          </label>
          <label className="pb-field">
            <span>Max size</span>
            <input type="number" min={0} value={numberOptionValue(field.options, "maxSize")} onChange={(event) => onChange(setNumberFieldOption(field, "maxSize", event.target.value))} placeholder="global upload limit" />
          </label>
        </div>
        <label className="pb-field">
          <span>MIME types</span>
          <textarea value={optionValuesText(field.options, "mimeTypes")} onChange={(event) => onChange(setFieldOption(field, "mimeTypes", splitOptionValues(event.target.value)))} rows={3} placeholder={"image/png\ntext/*"} />
        </label>
      </div>
    );
  } else if (field.type === "decimal") {
    return (
      <div className="pb-field-options">
        <p className="pb-field-hint">
          Stored as Postgres <code>numeric</code> and sent as a JSON string, so totals stay exact. Use this for money, quantities and rates — not <code>number</code>, which is floating point.
        </p>
        <div className="pb-field-grid">
          <label>
            <span>Precision</span>
            <input type="number" min={1} max={38} value={numberOptionValue(field.options, "precision")} onChange={(event) => onChange(setNumberFieldOption(field, "precision", event.target.value))} placeholder="18" disabled={readOnly} />
          </label>
          <label>
            <span>Scale</span>
            <input type="number" min={0} max={38} value={numberOptionValue(field.options, "scale")} onChange={(event) => onChange(setNumberFieldOption(field, "scale", event.target.value))} placeholder="2" disabled={readOnly} />
          </label>
          <label>
            <span>Min</span>
            <input value={stringOptionValue(field.options, "min")} onChange={(event) => onChange(setStringFieldOption(field, "min", event.target.value))} placeholder="No min" disabled={readOnly} />
          </label>
          <label>
            <span>Max</span>
            <input value={stringOptionValue(field.options, "max")} onChange={(event) => onChange(setStringFieldOption(field, "max", event.target.value))} placeholder="No max" disabled={readOnly} />
          </label>
        </div>
        <label className="pb-field">
          <span>Computed from</span>
          <input
            value={stringOptionValue(field.options, "computed")}
            onChange={(event) => onChange(setStringFieldOption(field, "computed", event.target.value))}
            placeholder="qty * unit_price"
            disabled={readOnly}
          />
        </label>
        <p className="pb-field-hint">
          Leave empty for a normal field. With an expression, PostgreSQL computes and stores the
          value on every write and the API rejects any attempt to set it, so the number cannot
          disagree with the columns it is derived from. Arithmetic over this row&rsquo;s own
          columns only.
        </p>
      </div>
    );
  } else if (field.type === "number") {
    typeOptions = (
      <div className="pb-field-options three">
        <label className="pb-field">
          <span>Min</span>
          <input type="number" value={numberOptionValue(field.options, "min")} onChange={(event) => onChange(setNumberFieldOption(field, "min", event.target.value))} placeholder="No min" />
        </label>
        <label className="pb-field">
          <span>Max</span>
          <input type="number" value={numberOptionValue(field.options, "max")} onChange={(event) => onChange(setNumberFieldOption(field, "max", event.target.value))} placeholder="No max" />
        </label>
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.onlyInt)} onChange={(event) => onChange(setFieldOption(field, "onlyInt", event.target.checked))} />
          Only integer
        </label>
      </div>
    );
  } else if (field.type === "text" || field.type === "url" || field.type === "password") {
    typeOptions = (
      <div className="pb-field-options three">
        <label className="pb-field">
          <span>Min length</span>
          <input type="number" min={0} max={field.type === "password" ? 71 : undefined} value={numberOptionValue(field.options, "min")} onChange={(event) => onChange(setNumberFieldOption(field, "min", event.target.value))} placeholder="No min" />
        </label>
        <label className="pb-field">
          <span>Max length</span>
          <input type="number" min={0} max={field.type === "password" ? 71 : undefined} value={numberOptionValue(field.options, "max")} onChange={(event) => onChange(setNumberFieldOption(field, "max", event.target.value))} placeholder={field.type === "password" ? "71" : "No max"} />
        </label>
        <label className="pb-field">
          <span>Pattern</span>
          <input value={String(field.options?.pattern ?? "")} onChange={(event) => onChange(setFieldOption(field, "pattern", event.target.value))} placeholder="^[a-z0-9]+$" />
        </label>
        {field.type === "password" ? (
          <label className="pb-field">
            <span>Bcrypt cost</span>
            <input type="number" min={4} max={31} value={numberOptionValue(field.options, "cost")} onChange={(event) => onChange(setNumberFieldOption(field, "cost", event.target.value))} placeholder="10" />
          </label>
        ) : null}
      </div>
    );
  } else if (field.type === "email") {
    typeOptions = (
      <div className="pb-field-options two">
        <label className="pb-field">
          <span>Only domains</span>
          <textarea value={optionValuesText(field.options, "onlyDomains")} onChange={(event) => onChange(setFieldOption(field, "onlyDomains", splitOptionValues(event.target.value)))} rows={3} placeholder={"example.com\nexample.org"} />
        </label>
        <label className="pb-field">
          <span>Except domains</span>
          <textarea value={optionValuesText(field.options, "exceptDomains")} onChange={(event) => onChange(setFieldOption(field, "exceptDomains", splitOptionValues(event.target.value)))} rows={3} placeholder={"blocked.com"} />
        </label>
      </div>
    );
  } else if (field.type === "editor" || field.type === "json") {
    typeOptions = (
      <div className="pb-field-options two">
        <label className="pb-field">
          <span>Max size</span>
          <input type="number" min={0} value={numberOptionValue(field.options, "maxSize")} onChange={(event) => onChange(setNumberFieldOption(field, "maxSize", event.target.value))} placeholder={field.type === "editor" ? "5MB default" : "1MB default"} />
        </label>
        {field.type === "editor" ? (
          <label className="pb-checkline">
            <input type="checkbox" checked={Boolean(field.options?.convertURLs)} onChange={(event) => onChange(setFieldOption(field, "convertURLs", event.target.checked))} />
            Strip URLs domain
          </label>
        ) : null}
      </div>
    );
  } else if (field.type === "autodate") {
    typeOptions = (
      <div className="pb-field-options two">
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.onCreate)} onChange={(event) => onChange(setFieldOption(field, "onCreate", event.target.checked))} />
          On create
        </label>
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.onUpdate)} onChange={(event) => onChange(setFieldOption(field, "onUpdate", event.target.checked))} />
          On update
        </label>
      </div>
    );
  }
  return (
    <fieldset className="pb-field-options-stack" disabled={readOnly}>
      {commonOptions}
      {typeOptions}
    </fieldset>
  );
}

export function RuleInputs({ rules, onChange }: { rules: RuleDraft; onChange: (rules: RuleDraft) => void }) {
  const update = (key: keyof RuleDraft, value: string) => onChange({ ...rules, [key]: value });
  return (
    <div className="pb-rules-grid">
      <RuleTextarea label="List rule" value={rules.listRule} onChange={(value) => update("listRule", value)} />
      <RuleTextarea label="View rule" value={rules.viewRule} onChange={(value) => update("viewRule", value)} />
      <RuleTextarea label="Create rule" value={rules.createRule} onChange={(value) => update("createRule", value)} />
      <RuleTextarea label="Update rule" value={rules.updateRule} onChange={(value) => update("updateRule", value)} />
      <RuleTextarea label="Delete rule" value={rules.deleteRule} onChange={(value) => update("deleteRule", value)} />
    </div>
  );
}

export function RuleTextarea({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="pb-field">
      <span>{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.target.value)} rows={4} placeholder='@request.auth.id != ""' className="mono" />
    </label>
  );
}
