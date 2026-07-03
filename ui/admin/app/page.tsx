"use client";

import {
  Activity,
  AlertCircle,
  Archive,
  ChevronRight,
  Copy,
  Database,
  FileUp,
  KeyRound,
  Layers3,
  ListFilter,
  LogOut,
  Plus,
  RefreshCw,
  Save,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Table2,
  Trash2,
  UploadCloud,
  Users,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  createAPIKey,
  createCollection,
  createFileToken,
  createProject,
  createRecord,
  deleteCollection,
  deleteRecord,
  health,
  listAPIKeys,
  listAudit,
  listCollections,
  listProjects,
  listRecords,
  login,
  logout,
  me,
  revokeAPIKey,
  setup,
  updateCollection,
  updateRecord,
  uploadFile,
} from "../src/lib/api";
import type { APIKey, Admin, AuditEntry, Collection, Field, FieldType, Health, Project, RecordItem, RecordList } from "../src/lib/types";

const TOKEN_KEY = "dublyobase.adminToken.v1";
const fieldTypes: FieldType[] = ["text", "number", "bool", "date", "email", "url", "select", "json", "relation", "file"];
const navItems = [
  { id: "overview", label: "Overview", icon: Activity },
  { id: "collections", label: "Collections", icon: Layers3 },
  { id: "records", label: "Records", icon: Table2 },
  { id: "users", label: "Users", icon: Users },
  { id: "apiKeys", label: "API Keys", icon: KeyRound },
  { id: "files", label: "Files", icon: FileUp },
  { id: "logs", label: "Logs", icon: Archive },
  { id: "settings", label: "Settings", icon: Settings },
] as const;

type View = (typeof navItems)[number]["id"];
type Notice = { type: "success" | "error"; message: string } | null;

const emptyCollectionDraft = {
  name: "",
  type: "base" as Collection["type"],
  fields: '[{"name":"title","type":"text","required":true}]',
  listRule: "",
  viewRule: "",
  createRule: "",
  updateRule: "",
  deleteRule: "",
};

export default function AdminApp() {
  const [token, setToken] = useState<string | null>(null);
  const [admin, setAdmin] = useState<Admin | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);
  const [view, setView] = useState<View>("overview");
  const [healthState, setHealthState] = useState<Health | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState<string>("");
  const [collections, setCollections] = useState<Collection[]>([]);
  const [selectedCollection, setSelectedCollection] = useState<string>("");
  const [records, setRecords] = useState<RecordList>({ items: [], page: 1, perPage: 30, totalItems: 0 });
  const [recordFilter, setRecordFilter] = useState("");
  const [recordJSON, setRecordJSON] = useState("{}");
  const [selectedRecordId, setSelectedRecordId] = useState("");
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [oneTimeKey, setOneTimeKey] = useState<APIKey | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [projectDraft, setProjectDraft] = useState({ slug: "", name: "" });
  const [collectionDraft, setCollectionDraft] = useState(emptyCollectionDraft);
  const [editingFields, setEditingFields] = useState<Field[]>([]);
  const [newField, setNewField] = useState<{ name: string; type: FieldType; required: boolean; options: string }>({
    name: "",
    type: "text",
    required: false,
    options: "{}",
  });
  const [keyDraft, setKeyDraft] = useState({ name: "", type: "service" as "anon" | "service" });
  const [fileDraft, setFileDraft] = useState({ recordId: "", field: "" });
  const [fileResult, setFileResult] = useState<RecordItem | null>(null);
  const bootstrapped = useRef(false);

  const selectedProjectModel = useMemo(() => projects.find((project) => project.slug === selectedProject) ?? null, [projects, selectedProject]);
  const selectedCollectionModel = useMemo(
    () => collections.find((collection) => collection.name === selectedCollection) ?? collections[0] ?? null,
    [collections, selectedCollection],
  );
  const fileFields = useMemo(() => selectedCollectionModel?.fields.filter((field) => field.type === "file") ?? [], [selectedCollectionModel]);

  const showNotice = useCallback((type: "success" | "error", message: string) => {
    setNotice({ type, message });
    window.setTimeout(() => setNotice(null), 4200);
  }, []);

  const handleError = useCallback(
    (error: unknown) => {
      if (error instanceof ApiError) {
        if (error.status === 401) {
          sessionStorage.removeItem(TOKEN_KEY);
          setToken(null);
          setAdmin(null);
        }
        showNotice("error", `${error.code}: ${error.message}`);
        return;
      }
      showNotice("error", error instanceof Error ? error.message : "Request failed");
    },
    [showNotice],
  );

  const loadProjectData = useCallback(
    async (authToken: string, projectSlug: string) => {
      if (!projectSlug) return;
      const [collectionResponse, keysResponse, auditResponse] = await Promise.all([
        listCollections(authToken, projectSlug),
        listAPIKeys(authToken, projectSlug),
        listAudit(authToken, projectSlug),
      ]);
      setCollections(collectionResponse.items);
      setApiKeys(keysResponse.items);
      setAudit(auditResponse.items);
      const nextCollection = selectedCollection || collectionResponse.items[0]?.name || "";
      setSelectedCollection(nextCollection);
      if (nextCollection) {
        const recordsResponse = await listRecords(authToken, projectSlug, nextCollection, 1, recordFilter);
        setRecords(recordsResponse);
      } else {
        setRecords({ items: [], page: 1, perPage: 30, totalItems: 0 });
      }
    },
    [recordFilter, selectedCollection],
  );

  const refreshAll = useCallback(
    async (authToken = token, preferredProject = selectedProject) => {
      if (!authToken) return;
      setBusy(true);
      try {
        const [healthResponse, projectsResponse] = await Promise.all([health(), listProjects(authToken)]);
        setHealthState(healthResponse);
        setProjects(projectsResponse.items);
        const projectSlug = preferredProject || projectsResponse.items[0]?.slug || "";
        setSelectedProject(projectSlug);
        if (projectSlug) {
          await loadProjectData(authToken, projectSlug);
        } else {
          setCollections([]);
          setSelectedCollection("");
          setRecords({ items: [], page: 1, perPage: 30, totalItems: 0 });
        }
      } catch (error) {
        handleError(error);
      } finally {
        setBusy(false);
      }
    },
    [handleError, loadProjectData, selectedProject, token],
  );

  useEffect(() => {
    if (bootstrapped.current) return;
    bootstrapped.current = true;
    const saved = sessionStorage.getItem(TOKEN_KEY);
    if (!saved) {
      setCheckingSession(false);
      health().then(setHealthState).catch(() => undefined);
      return;
    }
    me(saved)
      .then((response) => {
        setToken(saved);
        setAdmin(response.admin);
        return refreshAll(saved);
      })
      .catch(() => {
        sessionStorage.removeItem(TOKEN_KEY);
      })
      .finally(() => setCheckingSession(false));
  }, [refreshAll]);

  useEffect(() => {
    if (!token || !selectedProject || !selectedCollection) return;
    let cancelled = false;
    listRecords(token, selectedProject, selectedCollection, 1, recordFilter)
      .then((response) => {
        if (!cancelled) {
          setRecords(response);
        }
      })
      .catch((error) => {
        if (!cancelled) {
          handleError(error);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [handleError, selectedCollection, selectedProject, token]);

  useEffect(() => {
    if (selectedCollectionModel) {
      setEditingFields(selectedCollectionModel.fields.map((field) => ({ ...field, options: field.options ?? {} })));
      setSelectedRecordId("");
      setRecordJSON("{}");
      setFileDraft((draft) => ({ ...draft, field: selectedCollectionModel.fields.find((field) => field.type === "file")?.name ?? "" }));
    } else {
      setEditingFields([]);
    }
  }, [selectedCollectionModel]);

  useEffect(() => {
    const hashView = window.location.hash.replace("#", "");
    if (navItems.some((item) => item.id === hashView)) {
      setView(hashView as View);
    }
  }, []);

  function changeView(next: View) {
    setView(next);
    window.history.replaceState(null, "", `#${next}`);
  }

  async function submitLogin(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true);
    try {
      const response = await login(String(data.get("email") ?? ""), String(data.get("password") ?? ""));
      sessionStorage.setItem(TOKEN_KEY, response.token);
      setToken(response.token);
      setAdmin(response.admin);
      showNotice("success", "Logged in");
      await refreshAll(response.token);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitSetup(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true);
    try {
      await setup(String(data.get("setupEmail") ?? ""), String(data.get("setupPassword") ?? ""));
      showNotice("success", "First admin created. Log in with that account.");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    if (token) {
      try {
        await logout(token);
      } catch {
        // Logout should always clear local state; the server may already have expired the session.
      }
    }
    sessionStorage.removeItem(TOKEN_KEY);
    setToken(null);
    setAdmin(null);
    setProjects([]);
    setCollections([]);
  }

  async function submitProject(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const created = await createProject(token, projectDraft);
      showNotice("success", `Project ${created.slug} created`);
      setProjectDraft({ slug: "", name: "" });
      await refreshAll(token, created.slug);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitCollection(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const fields = JSON.parse(collectionDraft.fields) as Field[];
      const created = await createCollection(token, selectedProject, {
        name: collectionDraft.name,
        type: collectionDraft.type,
        fields,
        listRule: collectionDraft.listRule,
        viewRule: collectionDraft.viewRule,
        createRule: collectionDraft.createRule,
        updateRule: collectionDraft.updateRule,
        deleteRule: collectionDraft.deleteRule,
      });
      showNotice("success", `Collection ${created.name} created`);
      setCollectionDraft(emptyCollectionDraft);
      setSelectedCollection(created.name);
      await loadProjectData(token, selectedProject);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveFields() {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const updated = await updateCollection(token, selectedProject, selectedCollectionModel.name, { fields: editingFields });
      showNotice("success", `Collection ${updated.name} saved`);
      await loadProjectData(token, selectedProject);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function removeCollection(collection: Collection) {
    if (!token || !selectedProject) return;
    const confirmed = window.confirm(`Delete collection "${collection.name}"? This removes its table and file storage.`);
    if (!confirmed) return;
    setBusy(true);
    try {
      await deleteCollection(token, selectedProject, collection.name);
      showNotice("success", `Collection ${collection.name} deleted`);
      setSelectedCollection("");
      await loadProjectData(token, selectedProject);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  function addField() {
    try {
      const options = JSON.parse(newField.options || "{}") as Record<string, unknown>;
      setEditingFields((fields) => [...fields, { name: newField.name, type: newField.type, required: newField.required, options }]);
      setNewField({ name: "", type: "text", required: false, options: "{}" });
    } catch {
      showNotice("error", "Field options must be valid JSON");
    }
  }

  async function refreshRecords(page = records.page) {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const response = await listRecords(token, selectedProject, selectedCollectionModel.name, page, recordFilter);
      setRecords(response);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveRecord() {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const payload = JSON.parse(recordJSON) as RecordItem;
      if (selectedRecordId) {
        await updateRecord(token, selectedProject, selectedCollectionModel.name, selectedRecordId, payload);
        showNotice("success", "Record updated");
      } else {
        await createRecord(token, selectedProject, selectedCollectionModel.name, payload);
        showNotice("success", "Record created");
      }
      setRecordJSON("{}");
      setSelectedRecordId("");
      await refreshRecords(1);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function removeRecord(record: RecordItem) {
    if (!token || !selectedProject || !selectedCollectionModel || typeof record.id !== "string") return;
    if (!window.confirm(`Delete record ${record.id}?`)) return;
    setBusy(true);
    try {
      await deleteRecord(token, selectedProject, selectedCollectionModel.name, record.id);
      showNotice("success", "Record deleted");
      await refreshRecords(records.page);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitAPIKey(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const created = await createAPIKey(token, selectedProject, keyDraft);
      setOneTimeKey(created);
      setKeyDraft({ name: "", type: "service" });
      showNotice("success", "API key created");
      const response = await listAPIKeys(token, selectedProject);
      setApiKeys(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function revokeKey(key: APIKey) {
    if (!token || !selectedProject) return;
    if (!window.confirm(`Revoke API key "${key.name}"?`)) return;
    setBusy(true);
    try {
      await revokeAPIKey(token, selectedProject, key.id);
      showNotice("success", "API key revoked");
      const response = await listAPIKeys(token, selectedProject);
      setApiKeys(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitUpload(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject || !selectedCollectionModel) return;
    const input = event.currentTarget.elements.namedItem("file") as HTMLInputElement | null;
    const file = input?.files?.[0];
    if (!file) {
      showNotice("error", "Choose a file first");
      return;
    }
    setBusy(true);
    try {
      const result = await uploadFile(token, selectedProject, selectedCollectionModel.name, fileDraft.recordId, fileDraft.field, file);
      setFileResult(result);
      showNotice("success", "File uploaded");
      await refreshRecords(records.page);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function copyText(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      showNotice("success", "Copied");
    } catch {
      showNotice("error", "Copy failed");
    }
  }

  if (checkingSession) {
    return (
      <main className="grid min-h-screen place-items-center bg-[#0c1424] text-white">
        <div className="flex items-center gap-3 text-sm">
          <RefreshCw className="h-4 w-4 animate-spin" />
          Loading admin session
        </div>
      </main>
    );
  }

  if (!token || !admin) {
    return <AuthScreen busy={busy} healthState={healthState} notice={notice} onLogin={submitLogin} onSetup={submitSetup} />;
  }

  return (
    <div className="app-shell grid min-h-screen grid-cols-1 lg:grid-cols-[250px_minmax(0,1fr)]">
      <aside className="db-sidebar flex min-h-[unset] flex-col lg:min-h-screen">
        <div className="flex h-16 items-center gap-3 border-b border-white/10 px-5">
          <div className="grid h-9 w-9 place-items-center rounded-md bg-cyan-400/15 text-cyan-200">
            <Database className="h-5 w-5" />
          </div>
          <div>
            <p className="text-sm font-semibold text-white">dublyobase</p>
            <p className="text-xs text-slate-400">Postgres control plane</p>
          </div>
        </div>
        <nav className="flex gap-1 overflow-x-auto px-3 py-3 lg:flex-1 lg:flex-col lg:overflow-visible" aria-label="Primary">
          {navItems.map((item) => {
            const Icon = item.icon;
            const active = view === item.id;
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => changeView(item.id)}
                className={`flex min-w-fit items-center gap-3 rounded-md px-3 py-2 text-left text-sm transition ${
                  active ? "bg-white text-slate-950" : "text-slate-300 hover:bg-white/10 hover:text-white"
                }`}
              >
                <Icon className="h-4 w-4" />
                {item.label}
              </button>
            );
          })}
        </nav>
        <div className="hidden border-t border-white/10 p-4 text-xs text-slate-400 lg:block">
          <p>{admin.email}</p>
          <button type="button" onClick={signOut} className="mt-3 flex items-center gap-2 rounded-md px-2 py-1.5 text-slate-200 hover:bg-white/10">
            <LogOut className="h-4 w-4" />
            Log out
          </button>
        </div>
      </aside>

      <main className="min-w-0">
        <header className="sticky top-0 z-10 flex min-h-16 flex-wrap items-center justify-between gap-3 border-b border-slate-200 bg-white/95 px-4 py-3 backdrop-blur md:px-6">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-3">
            <select
              aria-label="Select project"
              value={selectedProject}
              onChange={(event) => {
                const slug = event.target.value;
                setSelectedProject(slug);
                if (token) loadProjectData(token, slug).catch(handleError);
              }}
              className="h-10 min-w-56 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium text-slate-900"
            >
              <option value="">No project</option>
              {projects.map((project) => (
                <option key={project.id} value={project.slug}>
                  {project.name} ({project.slug})
                </option>
              ))}
            </select>
            <StatusChip label="DB" value={healthState?.db ?? "checking"} ok={healthState?.db === "ok"} />
            <StatusChip label="Storage" value={healthState?.storage ?? "checking"} ok={healthState?.storage === "ok"} />
            <span className="rounded-full border border-slate-200 bg-slate-50 px-3 py-1 text-xs font-medium text-slate-600">
              {healthState?.version ?? "version unknown"}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button type="button" onClick={() => refreshAll()} className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium text-slate-700 hover:bg-slate-50">
              <RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />
              Refresh
            </button>
            <button type="button" onClick={signOut} className="inline-flex h-10 items-center gap-2 rounded-md bg-slate-900 px-3 text-sm font-medium text-white hover:bg-slate-800 lg:hidden">
              <LogOut className="h-4 w-4" />
              Log out
            </button>
          </div>
        </header>

        {notice ? (
          <div className={`mx-4 mt-4 rounded-md border px-4 py-3 text-sm md:mx-6 ${notice.type === "error" ? "border-red-200 bg-red-50 text-red-800" : "border-emerald-200 bg-emerald-50 text-emerald-800"}`}>
            {notice.message}
          </div>
        ) : null}

        <section className="p-4 md:p-6">
          {view === "overview" ? (
            <Overview
              project={selectedProjectModel}
              projects={projects}
              collections={collections}
              records={records}
              apiKeys={apiKeys}
              audit={audit}
              projectDraft={projectDraft}
              setProjectDraft={setProjectDraft}
              onSubmitProject={submitProject}
            />
          ) : null}
          {view === "collections" ? (
            <CollectionsView
              collections={collections}
              selected={selectedCollectionModel}
              editingFields={editingFields}
              newField={newField}
              collectionDraft={collectionDraft}
              setCollectionDraft={setCollectionDraft}
              setSelectedCollection={setSelectedCollection}
              setEditingFields={setEditingFields}
              setNewField={setNewField}
              onAddField={addField}
              onSaveFields={saveFields}
              onCreateCollection={submitCollection}
              onDeleteCollection={removeCollection}
            />
          ) : null}
          {view === "records" ? (
            <RecordsView
              collections={collections}
              selectedCollection={selectedCollectionModel}
              selectedCollectionName={selectedCollection}
              setSelectedCollection={setSelectedCollection}
              records={records}
              recordFilter={recordFilter}
              setRecordFilter={setRecordFilter}
              recordJSON={recordJSON}
              setRecordJSON={setRecordJSON}
              selectedRecordId={selectedRecordId}
              setSelectedRecordId={setSelectedRecordId}
              onRefresh={() => refreshRecords(1)}
              onSaveRecord={saveRecord}
              onDeleteRecord={removeRecord}
            />
          ) : null}
          {view === "users" ? (
            <UsersView
              usersCollection={collections.find((collection) => collection.name === "users") ?? null}
              records={selectedCollection === "users" ? records : { items: [], page: 1, perPage: 30, totalItems: 0 }}
              onOpenUsers={() => {
                setSelectedCollection("users");
                changeView("records");
              }}
            />
          ) : null}
          {view === "apiKeys" ? (
            <APIKeysView
              keys={apiKeys}
              oneTimeKey={oneTimeKey}
              keyDraft={keyDraft}
              setKeyDraft={setKeyDraft}
              onCreate={submitAPIKey}
              onRevoke={revokeKey}
              onCopy={copyText}
              onDismissSecret={() => setOneTimeKey(null)}
            />
          ) : null}
          {view === "files" ? (
            <FilesView
              collections={collections}
              selectedCollection={selectedCollectionModel}
              selectedCollectionName={selectedCollection}
              setSelectedCollection={setSelectedCollection}
              fileFields={fileFields}
              fileDraft={fileDraft}
              setFileDraft={setFileDraft}
              fileResult={fileResult}
              records={records}
              token={token}
              project={selectedProject}
              onSubmitUpload={submitUpload}
              onCreateFileToken={async (recordId, field, fileId) => {
                try {
                  const response = await createFileToken(token, selectedProject, selectedCollectionModel?.name ?? "", recordId, field, fileId);
                  showNotice("success", `File token expires ${formatDate(response.expiresAt)}`);
                  return response.token;
                } catch (error) {
                  handleError(error);
                  return "";
                }
              }}
            />
          ) : null}
          {view === "logs" ? <LogsView audit={audit} onRefresh={() => token && selectedProject && listAudit(token, selectedProject).then((r) => setAudit(r.items)).catch(handleError)} /> : null}
          {view === "settings" ? <SettingsView project={selectedProjectModel} healthState={healthState} appUrl={typeof window !== "undefined" ? window.location.origin : ""} /> : null}
        </section>
      </main>
    </div>
  );
}

function AuthScreen({
  busy,
  healthState,
  notice,
  onLogin,
  onSetup,
}: {
  busy: boolean;
  healthState: Health | null;
  notice: Notice;
  onLogin: (event: React.FormEvent<HTMLFormElement>) => void;
  onSetup: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <main className="grid min-h-screen bg-[#0c1424] text-slate-950 lg:grid-cols-[minmax(360px,480px)_minmax(0,1fr)]">
      <section className="flex min-h-screen flex-col justify-center bg-white px-6 py-10 sm:px-10">
        <div className="mb-10 flex items-center gap-3">
          <div className="grid h-10 w-10 place-items-center rounded-md bg-slate-950 text-cyan-200">
            <Database className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-xl font-semibold">dublyobase</h1>
            <p className="text-sm text-slate-500">Admin panel</p>
          </div>
        </div>
        {notice ? (
          <div className={`mb-4 rounded-md border px-4 py-3 text-sm ${notice.type === "error" ? "border-red-200 bg-red-50 text-red-800" : "border-emerald-200 bg-emerald-50 text-emerald-800"}`}>
            {notice.message}
          </div>
        ) : null}
        <form onSubmit={onLogin} className="space-y-4">
          <div>
            <label htmlFor="email" className="text-sm font-medium text-slate-700">
              Admin email
            </label>
            <input id="email" name="email" type="email" autoComplete="username" required className="mt-1 h-11 w-full rounded-md border border-slate-300 px-3 text-sm" />
          </div>
          <div>
            <label htmlFor="password" className="text-sm font-medium text-slate-700">
              Password
            </label>
            <input id="password" name="password" type="password" autoComplete="current-password" required className="mt-1 h-11 w-full rounded-md border border-slate-300 px-3 text-sm" />
          </div>
          <button type="submit" disabled={busy} className="inline-flex h-11 w-full items-center justify-center gap-2 rounded-md bg-slate-950 px-4 text-sm font-semibold text-white hover:bg-slate-800 disabled:opacity-60">
            <ShieldCheck className="h-4 w-4" />
            Log in
          </button>
        </form>
        <details className="mt-8 rounded-md border border-slate-200 bg-slate-50 p-4">
          <summary className="cursor-pointer text-sm font-medium text-slate-800">Create first admin if setup is open</summary>
          <form onSubmit={onSetup} className="mt-4 space-y-3">
            <div>
              <label htmlFor="setupEmail" className="text-sm font-medium text-slate-700">
                Email
              </label>
              <input id="setupEmail" name="setupEmail" type="email" autoComplete="username" required className="mt-1 h-10 w-full rounded-md border border-slate-300 px-3 text-sm" />
            </div>
            <div>
              <label htmlFor="setupPassword" className="text-sm font-medium text-slate-700">
                Password
              </label>
              <input id="setupPassword" name="setupPassword" type="password" autoComplete="new-password" minLength={12} required className="mt-1 h-10 w-full rounded-md border border-slate-300 px-3 text-sm" />
            </div>
            <button type="submit" disabled={busy} className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium hover:bg-slate-50">
              <Plus className="h-4 w-4" />
              Create admin
            </button>
          </form>
        </details>
      </section>
      <section className="hidden min-h-screen p-8 text-white lg:block">
        <div className="mx-auto flex h-full max-w-3xl flex-col justify-center">
          <div className="rounded-lg border border-white/10 bg-white/5 p-6 shadow-2xl">
            <div className="mb-6 flex items-center justify-between border-b border-white/10 pb-4">
              <div>
                <p className="text-sm text-cyan-200">Status</p>
                <p className="text-2xl font-semibold">Postgres control plane</p>
              </div>
              <Server className="h-8 w-8 text-cyan-200" />
            </div>
            <div className="grid grid-cols-3 gap-3">
              <Metric label="DB" value={healthState?.db ?? "checking"} />
              <Metric label="Storage" value={healthState?.storage ?? "checking"} />
              <Metric label="Version" value={healthState?.version ?? "unknown"} />
            </div>
          </div>
        </div>
      </section>
    </main>
  );
}

function Overview({
  project,
  projects,
  collections,
  records,
  apiKeys,
  audit,
  projectDraft,
  setProjectDraft,
  onSubmitProject,
}: {
  project: Project | null;
  projects: Project[];
  collections: Collection[];
  records: RecordList;
  apiKeys: APIKey[];
  audit: AuditEntry[];
  projectDraft: { slug: string; name: string };
  setProjectDraft: (draft: { slug: string; name: string }) => void;
  onSubmitProject: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold text-slate-950">Project workspace</h2>
          <p className="mt-1 text-sm text-slate-600">{project ? `${project.name} uses schema ${project.schemaName}` : "Create or select a project to start."}</p>
        </div>
        <form onSubmit={onSubmitProject} className="db-panel flex flex-wrap items-end gap-3 p-3">
          <LabeledInput label="Slug" value={projectDraft.slug} onChange={(value) => setProjectDraft({ ...projectDraft, slug: value })} placeholder="myapp" />
          <LabeledInput label="Name" value={projectDraft.name} onChange={(value) => setProjectDraft({ ...projectDraft, name: value })} placeholder="My app" />
          <button type="submit" className="inline-flex h-10 items-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
            <Plus className="h-4 w-4" />
            Create project
          </button>
        </form>
      </div>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <MetricPanel label="Projects" value={projects.length} helper="Active control-plane projects" />
        <MetricPanel label="Collections" value={collections.length} helper="Schema-managed tables" />
        <MetricPanel label="Visible records" value={records.totalItems} helper="Current collection page" />
        <MetricPanel label="API keys" value={apiKeys.length} helper="Created for this project" />
      </div>
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1.4fr)_minmax(320px,0.8fr)]">
        <Panel title="Collections" icon={<Layers3 className="h-4 w-4" />}>
          <CompactTable
            headers={["Name", "Type", "Fields", "Rules"]}
            rows={collections.slice(0, 6).map((collection) => [
              collection.name,
              collection.type,
              String(collection.fields.length),
              [collection.listRule, collection.viewRule, collection.createRule, collection.updateRule, collection.deleteRule].filter((rule) => rule !== null).length + " configured",
            ])}
            empty="No collections yet"
          />
        </Panel>
        <Panel title="Recent audit" icon={<Archive className="h-4 w-4" />}>
          <div className="space-y-3">
            {audit.slice(0, 7).map((entry) => (
              <div key={entry.id} className="flex items-start gap-3 border-b border-slate-100 pb-3 last:border-0 last:pb-0">
                <span className="status-dot mt-1.5 bg-cyan-600" />
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-slate-900">{entry.action}</p>
                  <p className="text-xs text-slate-500">{formatDate(entry.createdAt)}</p>
                </div>
              </div>
            ))}
            {audit.length === 0 ? <EmptyState label="No audit entries yet" /> : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function CollectionsView(props: {
  collections: Collection[];
  selected: Collection | null;
  editingFields: Field[];
  newField: { name: string; type: FieldType; required: boolean; options: string };
  collectionDraft: typeof emptyCollectionDraft;
  setCollectionDraft: (draft: typeof emptyCollectionDraft) => void;
  setSelectedCollection: (name: string) => void;
  setEditingFields: React.Dispatch<React.SetStateAction<Field[]>>;
  setNewField: (field: { name: string; type: FieldType; required: boolean; options: string }) => void;
  onAddField: () => void;
  onSaveFields: () => void;
  onCreateCollection: (event: React.FormEvent<HTMLFormElement>) => void;
  onDeleteCollection: (collection: Collection) => void;
}) {
  const { collections, selected, editingFields, newField, collectionDraft } = props;
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
      <div className="space-y-4">
        <Panel title="Collections" icon={<Layers3 className="h-4 w-4" />}>
          <div className="mb-3 flex items-center gap-2">
            <Search className="h-4 w-4 text-slate-400" />
            <span className="text-sm text-slate-500">{collections.length} collections</span>
          </div>
          <div className="overflow-x-auto">
            <table className="db-table">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Fields</th>
                  <th>System</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {collections.map((collection) => (
                  <tr key={collection.id} className={selected?.name === collection.name ? "bg-cyan-50/60" : ""}>
                    <td>
                      <button type="button" onClick={() => props.setSelectedCollection(collection.name)} className="font-medium text-slate-950 hover:text-cyan-700">
                        {collection.name}
                      </button>
                    </td>
                    <td>{collection.type}</td>
                    <td>{collection.fields.length}</td>
                    <td>{collection.system ? "yes" : "no"}</td>
                    <td className="text-right">
                      {!collection.system ? (
                        <button type="button" onClick={() => props.onDeleteCollection(collection)} className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50">
                          <Trash2 className="h-3.5 w-3.5" />
                          Delete
                        </button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
        <Panel title="Create collection" icon={<Plus className="h-4 w-4" />}>
          <form onSubmit={props.onCreateCollection} className="grid gap-3 md:grid-cols-2">
            <LabeledInput label="Name" value={collectionDraft.name} onChange={(value) => props.setCollectionDraft({ ...collectionDraft, name: value })} placeholder="posts" />
            <label className="text-sm font-medium text-slate-700">
              Type
              <select value={collectionDraft.type} onChange={(event) => props.setCollectionDraft({ ...collectionDraft, type: event.target.value as Collection["type"] })} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm">
                <option value="base">base</option>
                <option value="auth">auth</option>
              </select>
            </label>
            <label className="md:col-span-2 text-sm font-medium text-slate-700">
              Fields JSON
              <textarea value={collectionDraft.fields} onChange={(event) => props.setCollectionDraft({ ...collectionDraft, fields: event.target.value })} rows={5} className="mt-1 w-full rounded-md border border-slate-300 p-3 font-mono text-xs" />
            </label>
            <button type="submit" className="inline-flex h-10 items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
              <Plus className="h-4 w-4" />
              Create collection
            </button>
          </form>
        </Panel>
      </div>
      <Panel title={selected ? `Field editor: ${selected.name}` : "Field editor"} icon={<ListFilter className="h-4 w-4" />}>
        {selected ? (
          <div className="space-y-4">
            <div className="space-y-2">
              {editingFields.map((field, index) => (
                <div key={`${field.name}-${index}`} className="grid grid-cols-[minmax(0,1fr)_110px_80px_40px] items-center gap-2 rounded-md border border-slate-200 p-2">
                  <input aria-label={`Field ${index + 1} name`} value={field.name} onChange={(event) => props.setEditingFields((fields) => fields.map((item, i) => (i === index ? { ...item, name: event.target.value } : item)))} className="h-9 rounded-md border border-slate-300 px-2 text-sm" />
                  <select aria-label={`Field ${field.name} type`} value={field.type} onChange={(event) => props.setEditingFields((fields) => fields.map((item, i) => (i === index ? { ...item, type: event.target.value as FieldType } : item)))} className="h-9 rounded-md border border-slate-300 bg-white px-2 text-sm">
                    {fieldTypes.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                  </select>
                  <label className="flex items-center gap-2 text-xs text-slate-600">
                    <input type="checkbox" checked={Boolean(field.required)} onChange={(event) => props.setEditingFields((fields) => fields.map((item, i) => (i === index ? { ...item, required: event.target.checked } : item)))} />
                    Required
                  </label>
                  <button type="button" aria-label={`Remove field ${field.name}`} onClick={() => props.setEditingFields((fields) => fields.filter((_, i) => i !== index))} className="grid h-9 w-9 place-items-center rounded-md text-red-700 hover:bg-red-50">
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
            <div className="rounded-md border border-slate-200 bg-slate-50 p-3">
              <div className="grid gap-2">
                <LabeledInput label="New field name" value={newField.name} onChange={(value) => props.setNewField({ ...newField, name: value })} placeholder="title" />
                <label className="text-sm font-medium text-slate-700">
                  Type
                  <select value={newField.type} onChange={(event) => props.setNewField({ ...newField, type: event.target.value as FieldType })} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm">
                    {fieldTypes.map((type) => (
                      <option key={type} value={type}>
                        {type}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="text-sm font-medium text-slate-700">
                  Options JSON
                  <textarea value={newField.options} onChange={(event) => props.setNewField({ ...newField, options: event.target.value })} rows={3} className="mt-1 w-full rounded-md border border-slate-300 p-2 font-mono text-xs" />
                </label>
                <label className="flex items-center gap-2 text-sm text-slate-700">
                  <input type="checkbox" checked={newField.required} onChange={(event) => props.setNewField({ ...newField, required: event.target.checked })} />
                  Required field
                </label>
                <button type="button" onClick={props.onAddField} className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium hover:bg-slate-50">
                  <Plus className="h-4 w-4" />
                  Add field
                </button>
              </div>
            </div>
            <button type="button" onClick={props.onSaveFields} className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
              <Save className="h-4 w-4" />
              Save schema
            </button>
          </div>
        ) : (
          <EmptyState label="Select a collection" />
        )}
      </Panel>
    </div>
  );
}

function RecordsView(props: {
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  setSelectedCollection: (name: string) => void;
  records: RecordList;
  recordFilter: string;
  setRecordFilter: (value: string) => void;
  recordJSON: string;
  setRecordJSON: (value: string) => void;
  selectedRecordId: string;
  setSelectedRecordId: (value: string) => void;
  onRefresh: () => void;
  onSaveRecord: () => void;
  onDeleteRecord: (record: RecordItem) => void;
}) {
  const columns = props.selectedCollection ? ["id", "created", "updated", ...props.selectedCollection.fields.slice(0, 5).map((field) => field.name)] : ["id"];
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
      <Panel title="Records" icon={<Table2 className="h-4 w-4" />}>
        <div className="mb-3 flex flex-wrap items-end gap-3">
          <label className="text-sm font-medium text-slate-700">
            Collection
            <select value={props.selectedCollectionName} onChange={(event) => props.setSelectedCollection(event.target.value)} className="mt-1 h-10 min-w-48 rounded-md border border-slate-300 bg-white px-3 text-sm">
              {props.collections.map((collection) => (
                <option key={collection.id} value={collection.name}>
                  {collection.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Filter" value={props.recordFilter} onChange={props.setRecordFilter} placeholder='title = "hello"' />
          <button type="button" onClick={props.onRefresh} className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium hover:bg-slate-50">
            <RefreshCw className="h-4 w-4" />
            Load
          </button>
        </div>
        <div className="overflow-auto">
          <table className="db-table min-w-[760px]">
            <thead>
              <tr>
                {columns.map((column) => (
                  <th key={column}>{column}</th>
                ))}
                <th />
              </tr>
            </thead>
            <tbody>
              {props.records.items.map((record, index) => (
                <tr key={String(record.id ?? index)}>
                  {columns.map((column) => (
                    <td key={column} className="max-w-52 truncate">
                      {renderValue(record[column])}
                    </td>
                  ))}
                  <td className="text-right">
                    <button
                      type="button"
                      onClick={() => {
                        props.setSelectedRecordId(String(record.id ?? ""));
                        props.setRecordJSON(JSON.stringify(stripSystemFields(record), null, 2));
                      }}
                      className="mr-1 rounded-md px-2 py-1 text-xs font-medium text-cyan-700 hover:bg-cyan-50"
                    >
                      Edit
                    </button>
                    <button type="button" onClick={() => props.onDeleteRecord(record)} className="rounded-md px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50">
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {props.records.items.length === 0 ? <EmptyState label="No records returned" /> : null}
        </div>
      </Panel>
      <Panel title={props.selectedRecordId ? "Edit record" : "Create record"} icon={<Save className="h-4 w-4" />}>
        <textarea value={props.recordJSON} onChange={(event) => props.setRecordJSON(event.target.value)} rows={18} className="w-full rounded-md border border-slate-300 p-3 font-mono text-xs" />
        <div className="mt-3 flex gap-2">
          <button type="button" onClick={props.onSaveRecord} className="inline-flex h-10 flex-1 items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
            <Save className="h-4 w-4" />
            {props.selectedRecordId ? "Save record" : "Create record"}
          </button>
          {props.selectedRecordId ? (
            <button type="button" onClick={() => { props.setSelectedRecordId(""); props.setRecordJSON("{}"); }} className="h-10 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium hover:bg-slate-50">
              New
            </button>
          ) : null}
        </div>
      </Panel>
    </div>
  );
}

function UsersView({ usersCollection, records, onOpenUsers }: { usersCollection: Collection | null; records: RecordList; onOpenUsers: () => void }) {
  return (
    <Panel title="Users" icon={<Users className="h-4 w-4" />}>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-slate-600">{usersCollection ? `${usersCollection.fields.length} fields in system users collection` : "The users collection is created per project."}</p>
        <button type="button" onClick={onOpenUsers} className="inline-flex h-10 items-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
          <ChevronRight className="h-4 w-4" />
          Open users records
        </button>
      </div>
      <CompactTable
        headers={["ID", "Email", "Verified", "Created"]}
        rows={records.items.map((record) => [String(record.id ?? ""), String(record.email ?? ""), String(record.verified ?? ""), String(record.created ?? "")])}
        empty="Open users records to load rows"
      />
    </Panel>
  );
}

function APIKeysView(props: {
  keys: APIKey[];
  oneTimeKey: APIKey | null;
  keyDraft: { name: string; type: "anon" | "service" };
  setKeyDraft: (draft: { name: string; type: "anon" | "service" }) => void;
  onCreate: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevoke: (key: APIKey) => void;
  onCopy: (text: string) => void;
  onDismissSecret: () => void;
}) {
  return (
    <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
      <Panel title="API keys" icon={<KeyRound className="h-4 w-4" />}>
        <div className="overflow-x-auto">
          <table className="db-table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Prefix</th>
                <th>Created</th>
                <th>Status</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {props.keys.map((key) => (
                <tr key={key.id}>
                  <td>{key.name}</td>
                  <td>{key.type}</td>
                  <td>{key.prefix}</td>
                  <td>{formatDate(key.createdAt)}</td>
                  <td>{key.revokedAt ? "revoked" : "active"}</td>
                  <td className="text-right">
                    {!key.revokedAt ? (
                      <button type="button" onClick={() => props.onRevoke(key)} className="rounded-md px-2 py-1 text-xs font-medium text-red-700 hover:bg-red-50">
                        Revoke
                      </button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Panel>
      <Panel title="Create key" icon={<Plus className="h-4 w-4" />}>
        <form onSubmit={props.onCreate} className="space-y-3">
          <LabeledInput label="Name" value={props.keyDraft.name} onChange={(value) => props.setKeyDraft({ ...props.keyDraft, name: value })} placeholder="production service" />
          <label className="text-sm font-medium text-slate-700">
            Type
            <select value={props.keyDraft.type} onChange={(event) => props.setKeyDraft({ ...props.keyDraft, type: event.target.value as "anon" | "service" })} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm">
              <option value="service">service</option>
              <option value="anon">anon</option>
            </select>
          </label>
          <button type="submit" className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
            <KeyRound className="h-4 w-4" />
            Create key
          </button>
        </form>
        {props.oneTimeKey?.key ? (
          <div className="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3">
            <p className="text-sm font-semibold text-amber-900">Copy this key now. It will not be shown again.</p>
            <div className="code-box mt-2">{props.oneTimeKey.key}</div>
            <div className="mt-2 flex gap-2">
              <button type="button" onClick={() => props.onCopy(props.oneTimeKey?.key ?? "")} className="inline-flex h-9 items-center gap-2 rounded-md border border-amber-300 bg-white px-3 text-sm font-medium">
                <Copy className="h-4 w-4" />
                Copy
              </button>
              <button type="button" onClick={props.onDismissSecret} className="h-9 rounded-md px-3 text-sm font-medium text-amber-900 hover:bg-amber-100">
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </Panel>
    </div>
  );
}

function FilesView(props: {
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  setSelectedCollection: (name: string) => void;
  fileFields: Field[];
  fileDraft: { recordId: string; field: string };
  setFileDraft: (draft: { recordId: string; field: string }) => void;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  project: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
}) {
  const availableFiles = findFiles(props.records.items, props.fileFields);
  return (
    <div className="grid gap-4 xl:grid-cols-[420px_minmax(0,1fr)]">
      <Panel title="Upload file" icon={<UploadCloud className="h-4 w-4" />}>
        <form onSubmit={props.onSubmitUpload} className="space-y-3">
          <label className="text-sm font-medium text-slate-700">
            Collection
            <select value={props.selectedCollectionName} onChange={(event) => props.setSelectedCollection(event.target.value)} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm">
              {props.collections.map((collection) => (
                <option key={collection.id} value={collection.name}>
                  {collection.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Record ID" value={props.fileDraft.recordId} onChange={(value) => props.setFileDraft({ ...props.fileDraft, recordId: value })} placeholder="uuid" />
          <label className="text-sm font-medium text-slate-700">
            File field
            <select value={props.fileDraft.field} onChange={(event) => props.setFileDraft({ ...props.fileDraft, field: event.target.value })} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm">
              {props.fileFields.map((field) => (
                <option key={field.name} value={field.name}>
                  {field.name}
                </option>
              ))}
            </select>
          </label>
          <label className="text-sm font-medium text-slate-700">
            File
            <input name="file" type="file" required className="mt-1 block w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm" />
          </label>
          <button type="submit" className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-slate-950 px-3 text-sm font-medium text-white hover:bg-slate-800">
            <UploadCloud className="h-4 w-4" />
            Upload
          </button>
        </form>
        {props.fileResult ? <pre className="code-box mt-4">{JSON.stringify(props.fileResult, null, 2)}</pre> : null}
      </Panel>
      <Panel title="Protected files" icon={<FileUp className="h-4 w-4" />}>
        <div className="overflow-x-auto">
          <table className="db-table">
            <thead>
              <tr>
                <th>Record</th>
                <th>Field</th>
                <th>File</th>
                <th>Size</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {availableFiles.map((file) => (
                <tr key={`${file.recordId}-${file.field}-${file.id}`}>
                  <td>{file.recordId}</td>
                  <td>{file.field}</td>
                  <td>{file.name}</td>
                  <td>{file.size}</td>
                  <td className="text-right">
                    <button
                      type="button"
                      onClick={async () => {
                        const fileToken = await props.onCreateFileToken(file.recordId, file.field, file.id);
                        if (fileToken) {
                          window.open(`/api/projects/${encodeURIComponent(props.project)}/files/${encodeURIComponent(props.selectedCollection?.name ?? "")}/${encodeURIComponent(file.recordId)}/${encodeURIComponent(file.field)}/${encodeURIComponent(file.id)}/${encodeURIComponent(file.name)}?token=${encodeURIComponent(fileToken)}`, "_blank", "noopener,noreferrer");
                        }
                      }}
                      className="rounded-md px-2 py-1 text-xs font-medium text-cyan-700 hover:bg-cyan-50"
                    >
                      Download
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {availableFiles.length === 0 ? <EmptyState label="Load records from a collection with file fields" /> : null}
        </div>
      </Panel>
    </div>
  );
}

function LogsView({ audit, onRefresh }: { audit: AuditEntry[]; onRefresh: () => void }) {
  return (
    <Panel title="Audit log" icon={<Archive className="h-4 w-4" />}>
      <div className="mb-3 flex justify-end">
        <button type="button" onClick={onRefresh} className="inline-flex h-10 items-center gap-2 rounded-md border border-slate-300 bg-white px-3 text-sm font-medium hover:bg-slate-50">
          <RefreshCw className="h-4 w-4" />
          Refresh logs
        </button>
      </div>
      <div className="overflow-x-auto">
        <table className="db-table min-w-[860px]">
          <thead>
            <tr>
              <th>Action</th>
              <th>Target</th>
              <th>IP</th>
              <th>Created</th>
              <th>Data</th>
            </tr>
          </thead>
          <tbody>
            {audit.map((entry) => (
              <tr key={entry.id}>
                <td>{entry.action}</td>
                <td>
                  {entry.targetType} {entry.targetId}
                </td>
                <td>{entry.ip || "-"}</td>
                <td>{formatDate(entry.createdAt)}</td>
                <td className="max-w-sm truncate">{JSON.stringify(entry.data)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  );
}

function SettingsView({ project, healthState, appUrl }: { project: Project | null; healthState: Health | null; appUrl: string }) {
  return (
    <div className="grid gap-4 xl:grid-cols-2">
      <Panel title="Project settings" icon={<Settings className="h-4 w-4" />}>
        {project ? (
          <dl className="grid gap-3 text-sm">
            <Info label="Slug" value={project.slug} />
            <Info label="Name" value={project.name} />
            <Info label="Schema" value={project.schemaName} />
            <Info label="Anon role" value={project.roles?.anon ?? ""} />
            <Info label="Authenticated role" value={project.roles?.authenticated ?? ""} />
            <Info label="Service role" value={project.roles?.service ?? ""} />
          </dl>
        ) : (
          <EmptyState label="No project selected" />
        )}
      </Panel>
      <Panel title="Runtime" icon={<Server className="h-4 w-4" />}>
        <dl className="grid gap-3 text-sm">
          <Info label="App URL" value={appUrl} />
          <Info label="Version" value={healthState?.version ?? ""} />
          <Info label="DB" value={healthState?.db ?? ""} />
          <Info label="Storage" value={healthState?.storage ?? ""} />
        </dl>
      </Panel>
    </div>
  );
}

function Panel({ title, icon, children }: { title: string; icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <section className="db-panel min-w-0">
      <div className="flex items-center gap-2 border-b border-slate-200 px-4 py-3">
        <span className="text-slate-500">{icon}</span>
        <h3 className="text-sm font-semibold text-slate-950">{title}</h3>
      </div>
      <div className="p-4">{children}</div>
    </section>
  );
}

function MetricPanel({ label, value, helper }: { label: string; value: number | string; helper: string }) {
  return (
    <section className="db-panel p-4">
      <p className="text-xs font-medium uppercase text-slate-500">{label}</p>
      <p className="mt-2 text-3xl font-semibold text-slate-950">{value}</p>
      <p className="mt-1 text-sm text-slate-500">{helper}</p>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-white/10 bg-white/5 p-4">
      <p className="text-xs uppercase text-slate-400">{label}</p>
      <p className="mt-2 truncate text-lg font-semibold text-white">{value}</p>
    </div>
  );
}

function StatusChip({ label, value, ok }: { label: string; value: string; ok: boolean }) {
  return (
    <span className="inline-flex items-center gap-2 rounded-full border border-slate-200 bg-white px-3 py-1 text-xs font-medium text-slate-700">
      <span className={`status-dot ${ok ? "bg-emerald-600" : "bg-amber-500"}`} />
      {label} {value}
    </span>
  );
}

function LabeledInput({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="text-sm font-medium text-slate-700">
      {label}
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-3 text-sm" />
    </label>
  );
}

function CompactTable({ headers, rows, empty }: { headers: string[]; rows: string[][]; empty: string }) {
  return (
    <div className="overflow-x-auto">
      <table className="db-table">
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
                <td key={cellIndex} className="max-w-64 truncate">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length === 0 ? <EmptyState label={empty} /> : null}
    </div>
  );
}

function EmptyState({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-2 rounded-md border border-dashed border-slate-300 bg-slate-50 px-4 py-8 text-sm text-slate-500">
      <AlertCircle className="h-4 w-4" />
      {label}
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[150px_minmax(0,1fr)] gap-3 border-b border-slate-100 pb-2 last:border-0">
      <dt className="text-slate-500">{label}</dt>
      <dd className="break-all font-medium text-slate-900">{value || "-"}</dd>
    </div>
  );
}

function renderValue(value: unknown) {
  if (value === null || value === undefined) return "-";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function stripSystemFields(record: RecordItem): RecordItem {
  const next: RecordItem = {};
  for (const [key, value] of Object.entries(record)) {
    if (!["id", "created", "updated"].includes(key)) {
      next[key] = value;
    }
  }
  return next;
}

function formatDate(value: string) {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

type FileMeta = { recordId: string; field: string; id: string; name: string; size: string };

function findFiles(records: RecordItem[], fields: Field[]): FileMeta[] {
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
        const name = String(raw.name ?? raw.filename ?? "file");
        if (recordId && id) {
          out.push({ recordId, field, id, name, size: String(raw.size ?? "-") });
        }
      }
    }
  }
  return out;
}
