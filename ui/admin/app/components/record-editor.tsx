"use client";

import { login, me } from "../../src/lib/api";
import type { Collection, Field } from "../../src/lib/types";
import { collectionPrimaryKeyFieldName, collectionStandardSystemColumns, pascalCase, sampleRecordPayload, sampleUpdatePayload } from "../lib/collections";
import { canSearchField, fieldIsMultiple, formatJSONInput, isRecordFormField, optionValues, parseRecordDraft, sanitizeEditorHTML, toDateTimeLocal } from "../lib/fields";
import type { RelationAnchor, View } from "../lib/view-types";
import { RelationPicker, useRelationAnchors } from "./relation-picker";
import { CollectionIcon } from "./ui";
import { Bold, Copy, Heading, Italic, Link2, List, Quote, Save, Search, Server, Type, Underline, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";

export function RecordModal({
  collection,
  collections,
  token,
  project,
  selectedRecordId,
  recordJSON,
  setRecordJSON,
  onClose,
  onSave,
}: {
  collection: Collection;
  collections: Collection[];
  token: string | null;
  project: string;
  selectedRecordId: string;
  recordJSON: string;
  setRecordJSON: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const parsed = parseRecordDraft(recordJSON);
  const draft = parsed.ok ? parsed.value : {};
  // Computed once for the whole form: every picker constrains itself from the
  // same set of anchors, and each anchor record is fetched only once.
  const anchors = useRelationAnchors(draft, collection, collections, token, project);
  const updateDraft = (field: Field, value: unknown) => {
    const next = { ...(parsed.ok ? parsed.value : {}) };
    if (value === undefined) {
      delete next[field.name];
    } else {
      next[field.name] = value;
    }
    setRecordJSON(JSON.stringify(next, null, 2));
  };
  return (
    <div className="pb-modal-layer" role="presentation">
      <section className="pb-modal record-upsert-modal" role="dialog" aria-modal="true" aria-labelledby="record-modal-title">
        <header className="pb-modal-header">
          <h2 id="record-modal-title">{selectedRecordId ? "Edit record" : "New record"}</h2>
          <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="pb-modal-content">
          <div className="pb-collection-title">
            <CollectionIcon collection={collection} />
            <div>
              <p>{collection.name}</p>
              <span>{selectedRecordId || "new record"}</span>
            </div>
          </div>
          {parsed.ok ? (
            <div className="pb-record-form">
              {collection.fields.filter(isRecordFormField).map((field) => (
                <RecordFieldInput
                  key={field.name}
                  field={field}
                  value={draft[field.name]}
                  editing={Boolean(selectedRecordId)}
                  collections={collections}
                  ownerCollection={collection}
                  anchors={anchors}
                  selfId={selectedRecordId}
                  token={token}
                  project={project}
                  onChange={(value) => updateDraft(field, value)}
                />
              ))}
            </div>
          ) : (
            <div className="pb-inline-alert danger">Fix the raw JSON before using the field form: {parsed.error}</div>
          )}
          <details className="pb-raw-json-panel" open={!parsed.ok}>
            <summary>Raw JSON</summary>
            <label className="pb-field">
              <span>Payload</span>
              <textarea value={recordJSON} onChange={(event) => setRecordJSON(event.target.value)} rows={14} className="mono json-editor" spellCheck={false} />
            </label>
          </details>
        </div>
        <footer className="pb-modal-footer">
          <button type="button" className="pb-btn transparent" onClick={onClose}>
            Close
          </button>
          <button type="button" className="pb-btn primary expanded-lg" onClick={onSave}>
            Save
          </button>
        </footer>
      </section>
    </div>
  );
}

export function RecordFieldInput({
  field,
  value,
  editing,
  onChange,
  collections,
  ownerCollection,
  anchors,
  selfId,
  token,
  project,
}: {
  field: Field;
  value: unknown;
  editing: boolean;
  onChange: (value: unknown) => void;
  collections: Collection[];
  ownerCollection: Collection;
  anchors: RelationAnchor[];
  selfId: string;
  token: string | null;
  project: string;
}) {
  const label = field.name;
  const multiple = fieldIsMultiple(field);
  if (field.type === "file") {
    return <ManagedRecordField field={field} note="Use the files upload section or API to update this field." />;
  }
  if (field.type === "autodate") {
    return <ManagedRecordField field={field} note="Managed automatically by the server." />;
  }
  if (field.type === "editor") {
    return (
      <label className="pb-field record-field full">
        <span>{label}</span>
        <RichTextEditor value={typeof value === "string" ? value : ""} onChange={onChange} />
      </label>
    );
  }
  if (field.type === "json") {
    return (
      <label className="pb-field record-field full">
        <span>{label}</span>
        <JSONFieldEditor value={value} onChange={onChange} />
      </label>
    );
  }
  if (field.type === "select") {
    const values = optionValues(field.options);
    const selected = multiple ? (Array.isArray(value) ? value.map(String) : []) : typeof value === "string" ? value : "";
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <select
          multiple={multiple}
          value={selected}
          onChange={(event) => {
            if (multiple) {
              onChange(Array.from(event.currentTarget.selectedOptions).map((option) => option.value));
            } else {
              onChange(event.currentTarget.value || undefined);
            }
          }}
        >
          {!multiple ? <option value="">Choose value</option> : null}
          {values.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
    );
  }
  if (field.type === "relation") {
    return (
      <div className="pb-field record-field full">
        <span>{label}</span>
        <RelationPicker
          field={field}
          value={value}
          collections={collections}
          collection={ownerCollection}
          anchors={anchors}
          selfId={selfId}
          token={token}
          project={project}
          onChange={onChange}
        />
      </div>
    );
  }
  if (field.type === "bool") {
    return (
      <label className="pb-checkline record-checkline">
        <input type="checkbox" checked={Boolean(value)} onChange={(event) => onChange(event.target.checked)} />
        {label}
      </label>
    );
  }
  if (field.type === "decimal") {
    // inputMode=decimal rather than type=number: the value must never round-trip
    // through a JS number, so it is kept as the typed string.
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <input
          inputMode="decimal"
          value={typeof value === "string" ? value : value === undefined || value === null ? "" : String(value)}
          onChange={(event) => onChange(event.target.value === "" ? undefined : event.target.value)}
          placeholder="0.00"
        />
      </label>
    );
  }
  if (field.type === "number") {
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <input type="number" value={typeof value === "number" ? String(value) : ""} onChange={(event) => onChange(event.target.value === "" ? undefined : Number(event.target.value))} />
      </label>
    );
  }
  if (field.type === "date") {
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <input type="datetime-local" value={toDateTimeLocal(value)} onChange={(event) => onChange(event.target.value ? new Date(event.target.value).toISOString() : undefined)} />
      </label>
    );
  }
  if (field.type === "password") {
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <input type="password" value={typeof value === "string" ? value : ""} onChange={(event) => onChange(event.target.value || (editing ? undefined : ""))} placeholder={editing ? "Leave blank to keep current" : ""} autoComplete="new-password" />
      </label>
    );
  }
  const inputType = field.type === "email" ? "email" : field.type === "url" ? "url" : "text";
  return (
    <label className="pb-field record-field">
      <span>{label}</span>
      <input type={inputType} value={typeof value === "string" ? value : ""} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}

export function ManagedRecordField({ field, note }: { field: Field; note: string }) {
  return (
    <div className="record-managed-field">
      <span>{field.name}</span>
      <em>{note}</em>
    </div>
  );
}

export function RichTextEditor({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const editorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const editor = editorRef.current;
    if (!editor) return;
    const next = sanitizeEditorHTML(value);
    if (editor.innerHTML !== next) {
      editor.innerHTML = next;
    }
  }, [value]);
  const run = (command: string, commandValue?: string) => {
    editorRef.current?.focus();
    document.execCommand(command, false, commandValue);
    onChange(sanitizeEditorHTML(editorRef.current?.innerHTML ?? ""));
  };
  const addLink = () => {
    const href = window.prompt("URL");
    if (href) run("createLink", href);
  };
  return (
    <div className="rich-editor">
      <div className="rich-editor-toolbar" aria-label="Rich editor toolbar">
        <button type="button" onClick={() => run("bold")} aria-label="Bold">
          <Bold className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("italic")} aria-label="Italic">
          <Italic className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("underline")} aria-label="Underline">
          <Underline className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("formatBlock", "h2")} aria-label="Heading">
          <Heading className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("formatBlock", "blockquote")} aria-label="Quote">
          <Quote className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("insertUnorderedList")} aria-label="Bullet list">
          <List className="h-4 w-4" />
        </button>
        <button type="button" onClick={addLink} aria-label="Link">
          <Link2 className="h-4 w-4" />
        </button>
        <button type="button" onClick={() => run("removeFormat")} aria-label="Clear formatting">
          <Type className="h-4 w-4" />
        </button>
      </div>
      <div
        ref={editorRef}
        className="rich-editor-surface"
        contentEditable
        role="textbox"
        aria-multiline="true"
        spellCheck
        suppressContentEditableWarning
        onInput={(event) => onChange(sanitizeEditorHTML(event.currentTarget.innerHTML))}
        onBlur={(event) => onChange(sanitizeEditorHTML(event.currentTarget.innerHTML))}
      />
    </div>
  );
}

export function JSONFieldEditor({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
  const [text, setText] = useState(formatJSONInput(value));
  const [error, setError] = useState("");
  useEffect(() => {
    setText(formatJSONInput(value));
    setError("");
  }, [value]);
  return (
    <>
      <textarea
        value={text}
        onChange={(event) => {
          setText(event.target.value);
          try {
            onChange(event.target.value.trim() ? JSON.parse(event.target.value) : undefined);
            setError("");
          } catch {
            setError("Invalid JSON");
          }
        }}
        rows={5}
        className="mono"
        spellCheck={false}
      />
      {error ? <span className="pb-field-error">{error}</span> : null}
    </>
  );
}

export function APIPreviewModal({ project, collection, onClose, onCopy }: { project: string; collection: Collection; onClose: () => void; onCopy: (text: string) => void }) {
  const [tab, setTab] = useState("list");
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [onClose]);
  const basePath = `/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection.name)}`;
  const authBase = `/api/projects/${encodeURIComponent(project)}/auth`;
  const realtimePath = `/api/projects/${encodeURIComponent(project)}/realtime?collection=${encodeURIComponent(collection.name)}&events=create,update,delete`;
  const realtimeWSPath = `/api/projects/${encodeURIComponent(project)}/realtime/ws?collection=${encodeURIComponent(collection.name)}&events=create,update,delete&channel=${encodeURIComponent(collection.name)}`;
  const primaryKeyField = collectionPrimaryKeyFieldName(collection);
  const hasStandardColumns = collectionStandardSystemColumns(collection);
  const searchableField = collection.fields.find((field) => field.searchable && canSearchField(field));
  const searchField = searchableField?.name ?? primaryKeyField;
  const sortField = hasStandardColumns ? "-created" : primaryKeyField;
  const projectionFields = Array.from(new Set([primaryKeyField, searchField, ...(hasStandardColumns ? ["created"] : [])])).join(",");
  const searchLine = searchableField ? ` \\
  --data-urlencode "search=hello"` : "";
  const relationField = collection.fields.find((field) => field.type === "relation")?.name ?? "";
  const expandLine = relationField ? ` \\
  --data-urlencode "expand=${relationField}"` : "";
  const filterObject = searchableField ? { [searchField]: { _icontains: "hello" } } : { [primaryKeyField]: { _eq: "record-id" } };
  const sampleBody = sampleRecordPayload(collection);
  const updateBody = sampleUpdatePayload(collection);
  const fileField = collection.fields.find((field) => field.type === "file")?.name ?? "attachment";
  const previewNotes = [
    collection.system ? "System collections are protected; use dedicated auth, org, or admin APIs when a normal collection route is unavailable." : "",
    "List requests return only rows visible to the caller. RLS-hidden private rows produce an empty list; hidden single-record reads return 404.",
  ].filter(Boolean);
  const examples: Record<string, { title: string; detail: string; code: string; params?: string[] }> = {
    list: {
      title: "List/Search records",
      detail: "Supports page/perPage, selected-field search, Directus-style JSON filters, fields projection, expand, skipTotal, and sort.",
      code: `curl -G "${basePath}/records" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN" \\
  --data-urlencode "page=1" \\
  --data-urlencode "perPage=25" \\
  --data-urlencode "sort=${sortField}"${searchLine} \\
  --data-urlencode 'filter=${JSON.stringify(filterObject)}' \\
  --data-urlencode "fields=${projectionFields}"${expandLine} \\
  --data-urlencode "skipTotal=false"`,
      params: ["page", "perPage", "sort", ...(searchableField ? ["search"] : []), "filter", "fields", ...(relationField ? ["expand"] : []), "skipTotal"],
    },
    view: {
      title: "View one record",
      detail: "Reads a single record by primary key while preserving collection RLS rules. Use expand for first-level relation records.",
      code: `curl -G "${basePath}/records/{${primaryKeyField}}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN"${expandLine}`,
      params: [primaryKeyField, ...(relationField ? ["expand"] : [])],
    },
    create: {
      title: "Create record",
      detail: "Writable fields are validated by collection field options before insert.",
      code: `curl -X POST "${basePath}/records" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN" \\
  -H "Content-Type: application/json" \\
  --data '${JSON.stringify(sampleBody, null, 2)}'`,
    },
    update: {
      title: "Update record",
      detail: "Patch accepts partial JSON. Hidden, primary-key, and system fields remain protected.",
      code: `curl -X PATCH "${basePath}/records/{${primaryKeyField}}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN" \\
  -H "Content-Type: application/json" \\
  --data '${JSON.stringify(updateBody, null, 2)}'`,
    },
    delete: {
      title: "Delete record",
      detail: "Deletes the record only when delete rules allow the caller.",
      code: `curl -X DELETE "${basePath}/records/{${primaryKeyField}}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN"`,
    },
    batch: {
      title: "Batch records",
      detail: "Runs bounded record operations sequentially. Atomic database transactions are intentionally disabled until transaction-scoped rules are complete.",
      code: `curl -X POST "${`/api/projects/${encodeURIComponent(project)}/batch`}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN" \\
  -H "Content-Type: application/json" \\
	  --data '{
	    "requests": [
      { "method": "POST", "collection": "${collection.name}", "body": ${JSON.stringify(sampleBody)} },
      { "method": "PATCH", "collection": "${collection.name}", "id": "{${primaryKeyField}}", "body": ${JSON.stringify(updateBody)} },
      { "method": "GET", "collection": "${collection.name}", "id": "{${primaryKeyField}}" }
    ]
  }'`,
	      params: ["requests", "method", "collection", primaryKeyField, "body"],
    },
    realtime: {
      title: "Realtime records",
      detail: "Server-Sent Events stream create/update/delete events and re-check record visibility before sending payloads.",
      code: `const source = new EventSource("${realtimePath}&token=" + encodeURIComponent(accessToken));
source.addEventListener("ready", (event) => console.log(JSON.parse(event.data)));
source.addEventListener("record.create", (event) => console.log(JSON.parse(event.data)));
source.addEventListener("record.update", (event) => console.log(JSON.parse(event.data)));
source.addEventListener("record.delete", (event) => console.log(JSON.parse(event.data)));`,
      params: ["collection", "events", "token or Authorization"],
    },
    websocket: {
      title: "WebSocket realtime",
      detail: "WebSocket supports record events, channel broadcasts, presence updates, and Postgres-backed fanout across replicas.",
      code: `const ws = new WebSocket("${realtimeWSPath}&token=" + encodeURIComponent(accessToken));
ws.addEventListener("message", (event) => {
  const message = JSON.parse(event.data);
  if (message.type === "ready") {
    ws.send(JSON.stringify({ type: "presence.update", state: { status: "online" } }));
  }
  if (message.type === "record.create") console.log("created", message.data);
  if (message.type === "broadcast") console.log("broadcast", message.event, message.payload);
});

ws.addEventListener("open", () => {
  ws.send(JSON.stringify({
    type: "broadcast",
    event: "client.ready",
    payload: { collection: "${collection.name}" }
  }));
});`,
      params: ["channel", "collection", "events", "presence.update", "broadcast"],
    },
    files: {
      title: "Files",
      detail: "Upload to file fields with multipart form data; protected files use short-lived file tokens.",
      code: `curl -X POST "${`/api/projects/${encodeURIComponent(project)}/files/${encodeURIComponent(collection.name)}/{recordId}/${encodeURIComponent(fileField)}`}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN" \\
  -F "file=@./image.png"

curl -X POST "${`/api/projects/${encodeURIComponent(project)}/files/${encodeURIComponent(collection.name)}/{recordId}/${encodeURIComponent(fileField)}/{fileId}/token`}" \\
  -H "Authorization: Bearer $DUBLYO_TOKEN"`,
    },
    auth: {
      title: "App auth",
      detail: "Email/password auth uses the project's system users collection and SMTP-backed verification/reset flows.",
      code: `POST ${authBase}/signup
{"email":"user@example.com","password":"password-123"}

	POST ${authBase}/login
	{"email":"user@example.com","password":"password-123"}

	POST ${authBase}/request-otp
	{"email":"user@example.com"}

		POST ${authBase}/login-otp
		{"email":"user@example.com","token":"dbo_otp_..."}

		GET ${authBase}/oauth/google/start?format=json
		GET ${authBase}/oauth/facebook/start?format=json
		GET ${authBase}/oauth/github/start?format=json

		POST ${authBase}/mfa/enroll
		Authorization: Bearer $ACCESS_TOKEN
		{"name":"Authenticator app"}

		POST ${authBase}/mfa/confirm
		Authorization: Bearer $ACCESS_TOKEN
		{"factorId":"...","code":"123456"}

		POST ${authBase}/mfa/verify
		{"mfaToken":"dbo_mfa_...","code":"123456"}

		POST ${authBase}/mfa/recovery
		{"mfaToken":"dbo_mfa_...","code":"xxxx-yyyy-zzzz-1111"}

		POST ${authBase}/refresh
		{"refreshToken":"..."}

		GET ${authBase}/sessions
		Authorization: Bearer $ACCESS_TOKEN

		GET ${authBase}/me
		Authorization: Bearer $ACCESS_TOKEN

POST ${authBase}/request-verification
{"email":"user@example.com"}

POST ${authBase}/request-password-reset
{"email":"user@example.com"}

		POST ${authBase}/request-email-change
		Authorization: Bearer $ACCESS_TOKEN
		{"newEmail":"new@example.com","password":"current-password"}

		POST ${authBase}/confirm-email-change
		{"token":"dbo_email_change_..."}

	POST /api/projects/${encodeURIComponent(project)}/orgs
	Authorization: Bearer $ACCESS_TOKEN
	{"name":"Acme Inc","slug":"acme"}

	POST /api/projects/${encodeURIComponent(project)}/orgs/{orgId}/invitations
	Authorization: Bearer $ACCESS_TOKEN
	{"email":"member@example.com","role":"admin"}`,
	    },
    sdk: {
      title: "JavaScript fetch",
      detail: "Drop-in browser/server example with list, view, create, update, delete, realtime, and batch helpers. Use service keys only on trusted servers.",
      code: `const base = "${typeof window !== "undefined" ? window.location.origin : ""}/api/projects/${project}";

export async function list${pascalCase(collection.name)}(token) {
  const params = new URLSearchParams({
    page: "1",
    perPage: "25",
    filter: JSON.stringify(${JSON.stringify(filterObject)}),
    ${relationField ? `expand: "${relationField}",` : ""}
  });
  const res = await fetch(\`\${base}/collections/${collection.name}/records?\${params}\`, {
    headers: { Authorization: \`Bearer \${token}\` },
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function create${pascalCase(collection.name)}(token, data) {
  const res = await fetch(\`\${base}/collections/${collection.name}/records\`, {
    method: "POST",
    headers: { Authorization: \`Bearer \${token}\`, "Content-Type": "application/json" },
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export function subscribe${pascalCase(collection.name)}(token, onEvent) {
  const url = \`\${base}/realtime?collection=${collection.name}&events=create,update,delete&token=\${encodeURIComponent(token)}\`;
  const source = new EventSource(url);
  ["record.create", "record.update", "record.delete"].forEach((event) => {
    source.addEventListener(event, (message) => onEvent(event, JSON.parse(message.data)));
  });
  return () => source.close();
}`,
    },
    typedSdk: {
      title: "Generated TypeScript SDK",
      detail: "Download project-specific collection types and a small fetch client generated from the current schema.",
      code: `curl "${`/admin/api/projects/${encodeURIComponent(project)}/sdk/typescript`}" \\
  -H "Authorization: Bearer $DUBLYO_ADMIN_TOKEN" \\
  -o dublyobase-client.ts

import { DublyobaseClient } from "./dublyobase-client";

const client = new DublyobaseClient("https://your-dublyobase.app", "${project}", accessToken);
const records = await client.list("${collection.name}", {
  page: 1,
  perPage: 25,
  filter: ${JSON.stringify(filterObject)}
});`,
    },
    responses: {
      title: "Response shapes",
      detail: "Core response envelopes returned by the REST API.",
      code: `// List
{
  "items": [${JSON.stringify(sampleBody)}],
  "page": 1,
  "perPage": 25,
  "totalItems": 1
}

// Record event
{
  "project": "${project}",
  "collection": "${collection.name}",
  "action": "create",
  "id": "{id}",
  "record": ${JSON.stringify(sampleBody, null, 2)},
  "ts": "2026-07-04T00:00:00Z"
}

// Error
{
  "error": "validation_failed",
  "message": "validation failed: field is required"
}

// Batch
{
  "results": [
    { "status": 200, "body": ${JSON.stringify(sampleBody)} }
  ]
}`,
    },
  };
  const active = examples[tab] ?? examples.list;
  return (
    <div className="pb-modal-layer" role="presentation">
      <section className="pb-modal api-preview-modal" role="dialog" aria-modal="true" aria-labelledby="api-preview-title">
        <header className="pb-modal-header">
          <h2 id="api-preview-title">API preview</h2>
          <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </header>
        <div className="pb-modal-content">
          <div className="pb-collection-title">
            <CollectionIcon collection={collection} />
            <div>
              <p>{collection.name}</p>
              <span>{collection.fields.length} fields</span>
            </div>
          </div>
          <div className="pb-api-preview-layout">
            <nav className="pb-api-tabs" aria-label="API preview operations">
              {Object.entries(examples).map(([key, item]) => (
                <button key={key} type="button" className={key === tab ? "active" : ""} onClick={() => setTab(key)}>
                  {item.title}
                </button>
              ))}
            </nav>
            <div className="pb-api-preview-panel">
              <div className="pb-section-title-row">
                <div>
                  <h3>{active.title}</h3>
                  <p className="pb-muted-copy">{active.detail}</p>
                </div>
                <button type="button" className="pb-btn secondary" onClick={() => onCopy(active.code)}>
                  <Copy className="h-4 w-4" />
                  Copy
                </button>
              </div>
              {active.params?.length ? (
                <div className="pb-chip-row api-param-row">
                  {active.params.map((param) => (
                    <span key={param} className="pb-chip">
                      {param}
                    </span>
                  ))}
                </div>
              ) : null}
              {previewNotes.length ? (
                <div className="pb-inline-alert info">
                  {previewNotes.map((note) => (
                    <p key={note}>{note}</p>
                  ))}
                </div>
              ) : null}
              <pre className="pb-code-box api-code">{active.code}</pre>
            </div>
          </div>
        </div>
      </section>
    </div>
  );
}
