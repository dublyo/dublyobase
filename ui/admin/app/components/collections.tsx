"use client";

import type { Collection, RecordItem, RecordList } from "../../src/lib/types";
import { collectionPrimaryKeyFieldName, collectionStandardSystemColumns, recordPrimaryKeyValue } from "../lib/collections";
import { recordPageSizes } from "../lib/constants";
import { canSearchField, isVisibleRecordField, renderValue } from "../lib/fields";
import { collectionRelationEdges, relationCardinalityLabel, relationCardinalityType, relationTargetName } from "../lib/relations";
import type { CollectionsMode, OverviewTab, RelationEdge, View } from "../lib/view-types";
import { renderRelationCell } from "./relation-picker";
import { CollectionIcon, EmptyState, FieldTypeGlyph, PageFooter } from "./ui";
import { ChevronRight, Code2, Download, List, ListFilter, MoreHorizontal, Plus, RefreshCw, Search, Settings, Share2, ShieldCheck, Table2, Trash2, X } from "lucide-react";
import { useMemo, useState } from "react";

export function CollectionsWorkspace({
  project,
  collections,
  selectedCollection,
  selectedCollectionName,
  records,
  recordSearch,
  setRecordSearch,
  recordFilter,
  setRecordFilter,
  recordPerPage,
  onSelectCollection,
  onRefresh,
  onPageChange,
  onPageSizeChange,
  onOpenCreateCollection,
  onOpenCollectionSettings,
  onOpenAPIPreview,
  onExportCSV,
  onOpenNewRecord,
  onEditRecord,
  onDeleteRecord,
  onDeleteCollection,
  version,
}: {
  project: string;
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  records: RecordList;
  recordSearch: string;
  setRecordSearch: (value: string) => void;
  recordFilter: string;
  setRecordFilter: (value: string) => void;
  recordPerPage: (typeof recordPageSizes)[number];
  onSelectCollection: (name: string) => void;
  onRefresh: () => void;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: (typeof recordPageSizes)[number]) => void;
  onOpenCreateCollection: () => void;
  onOpenCollectionSettings: () => void;
  onOpenAPIPreview: () => void;
  onExportCSV: (format?: "csv" | "xlsx") => void;
  onOpenNewRecord: () => void;
  onEditRecord: (record: RecordItem) => void;
  onDeleteRecord: (record: RecordItem) => void;
  onDeleteCollection: (collection: Collection) => void;
  version: string;
}) {
  const [mode, setMode] = useState<CollectionsMode>("records");
  const primaryKeyField = selectedCollection ? collectionPrimaryKeyFieldName(selectedCollection) : "id";
  const systemColumns = selectedCollection && collectionStandardSystemColumns(selectedCollection) ? ["created", "updated"] : [];
  const columns = selectedCollection ? Array.from(new Set([primaryKeyField, ...selectedCollection.fields.filter(isVisibleRecordField).map((field) => field.name), ...systemColumns])) : ["id"];
  const totalPages = Math.max(1, Math.ceil(records.totalItems / Math.max(1, records.perPage || recordPerPage)));
  const currentPage = Math.max(1, records.page || 1);
  const searchableFields = selectedCollection?.fields.filter(canSearchField).filter((field) => field.searchable).map((field) => field.name) ?? [];
  return (
    <section className="pb-page">
      <CollectionSidebar collections={collections} selected={selectedCollectionName} onSelect={onSelectCollection} onNewCollection={onOpenCreateCollection} />
      <div className="pb-page-content full-height">
        <header className="pb-page-header flex-nowrap">
          <nav className="pb-breadcrumbs" aria-label="Breadcrumb">
            <span>Collections</span>
            {selectedCollection ? <span title={selectedCollection.name}>{selectedCollection.name}</span> : null}
          </nav>
          <div className="pb-header-secondary-btns">
            <div className="pb-segmented-control compact" role="tablist" aria-label="Collections view">
              <button type="button" role="tab" aria-selected={mode === "records"} className={mode === "records" ? "active" : ""} onClick={() => setMode("records")}>
                Records
              </button>
              <button type="button" role="tab" aria-selected={mode === "overview"} className={mode === "overview" ? "active" : ""} onClick={() => setMode("overview")}>
                Overview
              </button>
            </div>
            <button type="button" className="pb-btn circle transparent secondary" onClick={onOpenCollectionSettings} disabled={!selectedCollection} aria-label="Collection settings">
              <Settings className="h-4 w-4" />
            </button>
            <button type="button" className="pb-btn circle transparent secondary" onClick={onRefresh} disabled={!selectedCollection} aria-label="Refresh records">
              <RefreshCw className="h-4 w-4" />
            </button>
            {selectedCollection && !selectedCollection.system ? (
              <button type="button" className="pb-btn circle transparent danger" onClick={() => onDeleteCollection(selectedCollection)} aria-label="Delete collection">
                <Trash2 className="h-4 w-4" />
              </button>
            ) : null}
          </div>
          <div className="pb-header-primary-btns">
            <button type="button" className="pb-btn outline" onClick={() => onExportCSV("csv")} disabled={!selectedCollection} title="Download the rows currently shown, as CSV">
              <Download className="h-4 w-4" />
              <span>CSV</span>
            </button>
            <button type="button" className="pb-btn outline" onClick={() => onExportCSV("xlsx")} disabled={!selectedCollection} title="Download the rows currently shown, as an Excel workbook">
              <Download className="h-4 w-4" />
              <span>Excel</span>
            </button>
            <button type="button" className="pb-btn outline api-preview-btn" onClick={onOpenAPIPreview} disabled={!selectedCollection}>
              <Code2 className="h-4 w-4" />
              <span>API preview</span>
            </button>
            <button type="button" className="pb-btn primary new-record-btn" onClick={onOpenNewRecord} disabled={!selectedCollection}>
              <Plus className="h-4 w-4" />
              <span>New record</span>
            </button>
          </div>
        </header>

        {mode === "overview" ? (
          <CollectionOverview collections={collections} selected={selectedCollectionName} onSelect={onSelectCollection} onCreate={onOpenCreateCollection} />
        ) : (
          <>
            <form
              className="pb-record-toolbar"
              onSubmit={(event) => {
                event.preventDefault();
                onRefresh();
              }}
            >
              <label className="pb-record-control search">
                <Search className="h-4 w-4" />
                <input
                  value={recordSearch}
                  onChange={(event) => setRecordSearch(event.target.value)}
                  placeholder={searchableFields.length > 0 ? `Search ${searchableFields.slice(0, 3).join(", ")}` : "Search selected fields"}
                  disabled={!selectedCollection || searchableFields.length === 0}
                />
              </label>
              <label className="pb-record-control filter">
                <ListFilter className="h-4 w-4" />
                <input value={recordFilter} onChange={(event) => setRecordFilter(event.target.value)} placeholder='{"title":{"_icontains":"hello"}}' disabled={!selectedCollection} />
              </label>
              <label className="pb-page-size-control">
                <span>Rows</span>
                <select
                  value={recordPerPage}
                  disabled={!selectedCollection}
                  onChange={(event) => onPageSizeChange(Number(event.target.value) as (typeof recordPageSizes)[number])}
                >
                  {recordPageSizes.map((size) => (
                    <option key={size} value={size}>
                      {size}
                    </option>
                  ))}
                </select>
              </label>
              <button type="submit" className="pb-btn sm secondary">
                Apply
              </button>
            </form>

            <div className="pb-table-wrap">
              <table className="pb-records-table">
                <thead>
                  <tr>
                    <th className="col-bulk" />
                    {columns.map((column) => (
                      <th key={column}>{column}</th>
                    ))}
                    <th className="col-meta">
                      <MoreHorizontal className="h-4 w-4" />
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {records.items.map((record, index) => {
                    const recordKey = selectedCollection ? recordPrimaryKeyValue(selectedCollection, record) || String(index) : String(index);
                    return (
                      <tr key={recordKey} onDoubleClick={() => onEditRecord(record)}>
                        <td className="col-bulk">
                          <input type="checkbox" aria-label={`Select record ${recordKey}`} />
                        </td>
                        {columns.map((column) => {
                          const columnField = selectedCollection?.fields.find((f) => f.name === column);
                          return (
                            <td key={column} className="truncate-cell">
                              {columnField?.type === "relation"
                                ? renderRelationCell(record, columnField, collections)
                                : renderValue(record[column])}
                            </td>
                          );
                        })}
                        <td className="row-actions">
                          <button type="button" className="pb-btn sm transparent secondary" onClick={() => onEditRecord(record)}>
                            Edit
                          </button>
                          <button type="button" className="pb-btn sm transparent danger" onClick={() => onDeleteRecord(record)}>
                            Delete
                          </button>
                        </td>
                      </tr>
                    );
                  })}
                  {records.items.length === 0 ? (
                    <tr>
                      <td colSpan={columns.length + 2} className="pb-empty-cell">
                        <EmptyState label={selectedCollection ? "No records found." : project ? "Select a collection." : "Create or select a project."} action={selectedCollection ? "New record" : undefined} onAction={selectedCollection ? onOpenNewRecord : undefined} />
                      </td>
                    </tr>
                  ) : null}
                </tbody>
              </table>
            </div>

            <div className="pb-record-pagination" aria-label="Record pagination">
              <span>{selectedCollection ? `Total ${records.totalItems} · Page ${currentPage} of ${totalPages}` : "No collection selected"}</span>
              <div className="pb-pagination-actions">
                <button type="button" className="pb-btn sm secondary" disabled={!selectedCollection || currentPage <= 1} onClick={() => onPageChange(currentPage - 1)}>
                  Previous
                </button>
                <button type="button" className="pb-btn sm secondary" disabled={!selectedCollection || currentPage >= totalPages} onClick={() => onPageChange(currentPage + 1)}>
                  Next
                </button>
              </div>
            </div>
          </>
        )}

        <PageFooter left={mode === "overview" ? `${collections.length} collections` : selectedCollection ? `Showing ${records.items.length} of ${records.totalItems}` : "No collection selected"} version={version} />
      </div>
    </section>
  );
}

export function CollectionOverview({
  collections,
  selected,
  onSelect,
  onCreate,
}: {
  collections: Collection[];
  selected: string;
  onSelect: (name: string) => void;
  onCreate: () => void;
}) {
  const [tab, setTab] = useState<OverviewTab>("fields");
  const [showSystem, setShowSystem] = useState(false);
  const [zoom, setZoom] = useState<"compact" | "normal" | "wide">("normal");
  const visibleCollections = collections.filter((collection) => showSystem || !collection.system);
  const relationEdges = collectionRelationEdges(visibleCollections);
  const fieldCount = visibleCollections.reduce((total, collection) => total + collection.fields.length, 0);
  const minNodeWidth = zoom === "compact" ? 210 : zoom === "wide" ? 310 : 245;
  return (
    <div className="pb-overview">
      <div className="pb-overview-topbar">
        <div>
          <h2>Collections overview</h2>
          <span>
            {visibleCollections.length} collections · {fieldCount} fields · {relationEdges.length} relations
          </span>
        </div>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={showSystem} onChange={(event) => setShowSystem(event.target.checked)} />
          System collections
        </label>
        <div className="pb-overview-actions" aria-label="Overview density">
          <button type="button" className={zoom === "compact" ? "active" : ""} onClick={() => setZoom("compact")}>
            Fit
          </button>
          <button type="button" className={zoom === "normal" ? "active" : ""} onClick={() => setZoom("normal")}>
            100%
          </button>
          <button type="button" className={zoom === "wide" ? "active" : ""} onClick={() => setZoom("wide")}>
            Wide
          </button>
        </div>
      </div>
      <div className="pb-overview-tabs" role="tablist" aria-label="Collections overview">
        <button type="button" role="tab" aria-selected={tab === "fields"} className={tab === "fields" ? "active" : ""} onClick={() => setTab("fields")}>
          Fields and relations
        </button>
        <button type="button" role="tab" aria-selected={tab === "rules"} className={tab === "rules" ? "active" : ""} onClick={() => setTab("rules")}>
          Rules
        </button>
      </div>
      {visibleCollections.length === 0 ? (
        <div className="pb-overview-empty">
          <EmptyState label={collections.length > 0 ? "Enable system collections to show the remaining collections." : "No collections yet."} action="New collection" onAction={onCreate} />
        </div>
      ) : tab === "fields" ? (
        <div className="pb-overview-fields">
          <div className="pb-overview-canvas">
            {relationEdges.length > 0 ? (
              <div className="pb-relation-ribbons" aria-label="Relation links">
                {relationEdges.slice(0, 12).map((edge) => (
                  <button key={`${edge.sourceCollection}.${edge.sourceField}`} type="button" onClick={() => edge.targetCollection && onSelect(edge.targetCollection)}>
                    <span>{edge.sourceCollection}.{edge.sourceField}</span>
                    <ChevronRight className="h-4 w-4" />
                    <strong>{relationCardinalityLabel(edge.cardinality)} · {edge.targetCollection || "unconfigured"}</strong>
                  </button>
                ))}
              </div>
            ) : null}
            <div className="pb-overview-board" style={{ gridTemplateColumns: `repeat(auto-fit, minmax(${minNodeWidth}px, 1fr))` }}>
              {visibleCollections.map((collection) => (
                <CollectionOverviewNode key={collection.id} collection={collection} active={selected === collection.name} onSelect={onSelect} />
              ))}
            </div>
          </div>
          <RelationTree collections={visibleCollections} edges={relationEdges} onSelect={onSelect} />
        </div>
      ) : (
        <CollectionRulesOverview collections={visibleCollections} onSelect={onSelect} />
      )}
    </div>
  );
}

export function CollectionOverviewNode({ collection, active, onSelect }: { collection: Collection; active: boolean; onSelect: (name: string) => void }) {
  return (
    <article className={`pb-overview-node ${active ? "active" : ""}`}>
      <button type="button" className="pb-overview-node-head" onClick={() => onSelect(collection.name)}>
        <CollectionIcon collection={collection} />
        <strong>{collection.name}</strong>
        <span>{collection.type}</span>
      </button>
      <div className="pb-overview-field-list">
        {collection.fields.length === 0 ? <span className="pb-overview-field muted">No editable fields</span> : null}
        {collection.fields.map((field) => (
          <div key={`${collection.id}-${field.name}`} className={`pb-overview-field ${field.type === "relation" ? "relation" : ""}`}>
            <FieldTypeGlyph type={field.type} />
            <span className="field-name">{field.name}</span>
            <span className="field-type">{field.type}</span>
            {field.hidden ? <span className="pb-mini-badge danger">Hidden</span> : null}
            {field.required ? <span className="pb-mini-badge">Required</span> : null}
            {field.type === "relation" ? <span className="pb-relation-arrow">{relationCardinalityLabel(relationCardinalityType(field))} to {relationTargetName(field) || "unconfigured"}</span> : null}
            {field.type === "file" && field.options?.multiple ? <span className="pb-mini-badge">multiple</span> : null}
          </div>
        ))}
      </div>
    </article>
  );
}

export function RelationTree({ collections, edges, onSelect }: { collections: Collection[]; edges: RelationEdge[]; onSelect: (name: string) => void }) {
  const byCollection = new Map(collections.map((collection) => [collection.name, collection]));
  const grouped = collections.map((collection) => ({
    collection,
    edges: edges.filter((edge) => edge.sourceCollection === collection.name),
  }));
  return (
    <aside className="pb-relation-tree" aria-label="Relation tree">
      <header>
        <h3>Relation tree</h3>
        <span>{edges.length ? `${edges.length} links` : "No relation fields"}</span>
      </header>
      {edges.length === 0 ? (
        <div className="pb-inline-alert info">Add a relation field to connect one collection to another.</div>
      ) : (
        <div className="pb-tree-list">
          {grouped.map(({ collection, edges: collectionEdges }) => (
            <details key={collection.id} open={collectionEdges.length > 0}>
              <summary>
                <CollectionIcon collection={collection} />
                <span>{collection.name}</span>
                <em>{collectionEdges.length}</em>
              </summary>
              {collectionEdges.length === 0 ? <p>No outgoing relations.</p> : null}
              {collectionEdges.map((edge) => (
                <button key={`${edge.sourceCollection}.${edge.sourceField}`} type="button" onClick={() => edge.targetCollection && onSelect(edge.targetCollection)}>
                  <Share2 className="h-4 w-4" />
                  <span>
                    <strong>{edge.sourceField}</strong>
                    <em>
                      {relationCardinalityLabel(edge.cardinality)} to {edge.targetCollection || "unconfigured"}
                      {edge.displayField ? ` · display ${edge.displayField}` : ""}
                    </em>
                  </span>
                  {edge.targetCollection && byCollection.has(edge.targetCollection) ? <CollectionIcon collection={byCollection.get(edge.targetCollection)} /> : <Table2 className="h-4 w-4" />}
                </button>
              ))}
            </details>
          ))}
        </div>
      )}
    </aside>
  );
}

export function CollectionRulesOverview({ collections, onSelect }: { collections: Collection[]; onSelect: (name: string) => void }) {
  const ruleRows = [
    ["List", "listRule"],
    ["View", "viewRule"],
    ["Create", "createRule"],
    ["Update", "updateRule"],
    ["Delete", "deleteRule"],
  ] as const;
  return (
    <div className="pb-overview-rules">
      {collections.map((collection) => (
        <article key={collection.id} className="pb-rule-node">
          <button type="button" className="pb-overview-node-head" onClick={() => onSelect(collection.name)}>
            <CollectionIcon collection={collection} />
            <strong>{collection.name}</strong>
            <span>{collection.type}</span>
          </button>
          <dl>
            {ruleRows.map(([label, key]) => {
              const value = collection[key];
              return (
                <div key={`${collection.id}-${key}`}>
                  <dt>{label}</dt>
                  <dd>{value || "Superusers only"}</dd>
                </div>
              );
            })}
          </dl>
        </article>
      ))}
    </div>
  );
}

export function CollectionSidebar({
  collections,
  selected,
  onSelect,
  onNewCollection,
}: {
  collections: Collection[];
  selected: string;
  onSelect: (name: string) => void;
  onNewCollection: () => void;
}) {
  const [search, setSearch] = useState("");
  const filtered = useMemo(() => {
    const query = search.replaceAll(" ", "").toLowerCase();
    if (!query) return collections;
    return collections.filter((collection) => (collection.name + collection.type).toLowerCase().includes(query));
  }, [collections, search]);
  const regular = filtered.filter((collection) => !collection.system);
  const system = filtered.filter((collection) => collection.system);
  return (
    <aside className="pb-sidebar collections-sidebar">
      <div className="pb-sidebar-search">
        <Search className="h-4 w-4" />
        <input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search collections..." />
        {search ? (
          <button type="button" className="pb-icon-btn" onClick={() => setSearch("")} aria-label="Clear search">
            <X className="h-4 w-4" />
          </button>
        ) : null}
      </div>
      <nav className="pb-sidebar-content collections-list" aria-label="Collections">
        <CollectionGroup label="Collections" collections={regular} selected={selected} onSelect={onSelect} />
        <CollectionGroup label="System" collections={system} selected={selected} onSelect={onSelect} collapsed={!search} />
        {filtered.length === 0 ? <div className="pb-sidebar-empty">No collections found.</div> : null}
      </nav>
      <div className="pb-sidebar-action">
        <button type="button" className="pb-btn outline block" onClick={onNewCollection}>
          <Plus className="h-4 w-4" />
          New collection
        </button>
      </div>
    </aside>
  );
}

export function CollectionGroup({
  label,
  collections,
  selected,
  onSelect,
  collapsed,
}: {
  label: string;
  collections: Collection[];
  selected: string;
  onSelect: (name: string) => void;
  collapsed?: boolean;
}) {
  if (collections.length === 0) return null;
  return (
    <details className="pb-nav-group" open={!collapsed}>
      <summary>{label}</summary>
      {collections.map((collection) => (
        <button key={collection.id} type="button" title={collection.name} className={`pb-nav-item ${selected === collection.name ? "active" : ""}`} onClick={() => onSelect(collection.name)}>
          <CollectionIcon collection={collection} />
          <span className="txt">{collection.name}</span>
          {collection.type === "auth" ? <ShieldCheck className="h-3.5 w-3.5 hint" /> : null}
        </button>
      ))}
    </details>
  );
}
