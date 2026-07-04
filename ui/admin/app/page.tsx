"use client";

import {
  Activity,
  AlertCircle,
  Archive,
  Check,
  ChevronDown,
  Code2,
  Copy,
  Database,
  Eye,
  EyeOff,
  FileUp,
  HardDrive,
  KeyRound,
  Layers3,
  ListFilter,
  LogOut,
  Mail,
  MoreHorizontal,
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
  X,
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
  getSettings,
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
  testSMTPSettings,
  testStorageSettings,
  updateCollection,
  updateRecord,
  updateSMTPSettings,
  updateStorageSettings,
  uploadFile,
} from "../src/lib/api";
import type { APIKey, Admin, AuditEntry, Collection, Field, FieldType, Health, InstanceSettings, Project, RecordItem, RecordList } from "../src/lib/types";

const TOKEN_KEY = "dublyobase.adminToken.v1";
const fieldTypes: FieldType[] = ["text", "number", "bool", "date", "email", "url", "select", "json", "relation", "file"];
const navItems = [
  { id: "collections", label: "Collections", icon: Layers3 },
  { id: "logs", label: "Logs", icon: Archive },
  { id: "settings", label: "Settings", icon: Settings },
] as const;
const settingsItems = [
  { id: "application", label: "Application", group: "System" },
  { id: "mail", label: "Mail settings", group: "System" },
  { id: "storage", label: "Files storage", group: "System" },
  { id: "apiKeys", label: "API keys", group: "Project" },
  { id: "files", label: "File uploads", group: "Project" },
] as const;

type View = (typeof navItems)[number]["id"];
type SettingsSection = (typeof settingsItems)[number]["id"];
type Notice = { type: "success" | "error"; message: string } | null;
type CollectionModalMode = "create" | "settings" | null;

type CollectionDraft = {
  name: string;
  type: Collection["type"];
  fields: Field[];
  listRule: string;
  viewRule: string;
  createRule: string;
  updateRule: string;
  deleteRule: string;
};

type RuleDraft = Pick<CollectionDraft, "listRule" | "viewRule" | "createRule" | "updateRule" | "deleteRule">;

const emptyRules: RuleDraft = {
  listRule: "",
  viewRule: "",
  createRule: "",
  updateRule: "",
  deleteRule: "",
};

const emptyCollectionDraft: CollectionDraft = {
  name: "",
  type: "base",
  fields: [{ name: "title", type: "text", required: true, options: {} }],
  ...emptyRules,
};

const emptySMTPDraft = {
  enabled: false,
  host: "",
  port: "587",
  from: "",
  username: "",
  password: "",
  clearPassword: false,
  testTo: "",
};

const emptyStorageDraft = {
  type: "local" as "local" | "s3",
  endpoint: "",
  bucket: "",
  region: "us-east-1",
  accessKey: "",
  secretKey: "",
  clearSecretKey: false,
  prefix: "",
  useSSL: true,
  forcePathStyle: true,
};

export default function AdminApp() {
  const [token, setToken] = useState<string | null>(null);
  const [admin, setAdmin] = useState<Admin | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);
  const [view, setView] = useState<View>("collections");
  const [settingsSection, setSettingsSection] = useState<SettingsSection>("application");
  const [healthState, setHealthState] = useState<Health | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [selectedProject, setSelectedProject] = useState("");
  const [collections, setCollections] = useState<Collection[]>([]);
  const [selectedCollection, setSelectedCollection] = useState("");
  const [records, setRecords] = useState<RecordList>({ items: [], page: 1, perPage: 30, totalItems: 0 });
  const [recordFilter, setRecordFilter] = useState("");
  const [recordJSON, setRecordJSON] = useState("{}");
  const [selectedRecordId, setSelectedRecordId] = useState("");
  const [recordEditorOpen, setRecordEditorOpen] = useState(false);
  const [apiPreviewOpen, setAPIPreviewOpen] = useState(false);
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [oneTimeKey, setOneTimeKey] = useState<APIKey | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [settings, setSettingsState] = useState<InstanceSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [projectDraft, setProjectDraft] = useState({ slug: "", name: "" });
  const [collectionDraft, setCollectionDraft] = useState(emptyCollectionDraft);
  const [collectionModal, setCollectionModal] = useState<CollectionModalMode>(null);
  const [editingFields, setEditingFields] = useState<Field[]>([]);
  const [editingRules, setEditingRules] = useState<RuleDraft>(emptyRules);
  const [smtpDraft, setSMTPDraft] = useState(emptySMTPDraft);
  const [storageDraft, setStorageDraft] = useState(emptyStorageDraft);
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
    async (authToken: string, projectSlug: string, preferredCollection = "") => {
      if (!projectSlug) return;
      const [collectionResponse, keysResponse, auditResponse] = await Promise.all([
        listCollections(authToken, projectSlug),
        listAPIKeys(authToken, projectSlug),
        listAudit(authToken, projectSlug),
      ]);
      setCollections(collectionResponse.items);
      setApiKeys(keysResponse.items);
      setAudit(auditResponse.items);
      const targetCollection = preferredCollection || selectedCollection;
      const currentExists = collectionResponse.items.some((collection) => collection.name === targetCollection);
      const nextCollection = currentExists ? targetCollection : collectionResponse.items[0]?.name || "";
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
        const [healthResponse, projectsResponse, settingsResponse] = await Promise.all([health(), listProjects(authToken), getSettings(authToken)]);
        setHealthState(healthResponse);
        setProjects(projectsResponse.items);
        setSettingsState(settingsResponse);
        setSMTPDraft(settingsToSMTPDraft(settingsResponse));
        setStorageDraft(settingsToStorageDraft(settingsResponse));
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
        if (!cancelled) setRecords(response);
      })
      .catch((error) => {
        if (!cancelled) handleError(error);
      });
    return () => {
      cancelled = true;
    };
  }, [handleError, recordFilter, selectedCollection, selectedProject, token]);

  useEffect(() => {
    if (!selectedCollectionModel) {
      setEditingFields([]);
      setEditingRules(emptyRules);
      return;
    }
    setEditingFields(selectedCollectionModel.fields.map((field) => ({ ...field, options: field.options ?? {} })));
    setEditingRules({
      listRule: selectedCollectionModel.listRule ?? "",
      viewRule: selectedCollectionModel.viewRule ?? "",
      createRule: selectedCollectionModel.createRule ?? "",
      updateRule: selectedCollectionModel.updateRule ?? "",
      deleteRule: selectedCollectionModel.deleteRule ?? "",
    });
    setSelectedRecordId("");
    setRecordJSON("{}");
    setFileDraft((draft) => ({ ...draft, field: selectedCollectionModel.fields.find((field) => field.type === "file")?.name ?? "" }));
  }, [selectedCollectionModel]);

  useEffect(() => {
    const hash = window.location.hash.replace("#", "");
    const [primary, secondary] = hash.split("/");
    if (navItems.some((item) => item.id === primary)) {
      setView(primary as View);
    }
    if (primary === "settings" && settingsItems.some((item) => item.id === secondary)) {
      setSettingsSection(secondary as SettingsSection);
    }
  }, []);

  function changeView(next: View) {
    setView(next);
    window.history.replaceState(null, "", `#${next}`);
  }

  function changeSettings(next: SettingsSection) {
    setView("settings");
    setSettingsSection(next);
    window.history.replaceState(null, "", `#settings/${next}`);
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
        // Local state must be cleared even if the server already expired the session.
      }
    }
    sessionStorage.removeItem(TOKEN_KEY);
    setToken(null);
    setAdmin(null);
    setProjects([]);
    setCollections([]);
    setSettingsState(null);
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
      const created = await createCollection(token, selectedProject, {
        name: collectionDraft.name,
        type: collectionDraft.type,
        fields: collectionDraft.fields.map(cleanField),
        listRule: collectionDraft.listRule,
        viewRule: collectionDraft.viewRule,
        createRule: collectionDraft.createRule,
        updateRule: collectionDraft.updateRule,
        deleteRule: collectionDraft.deleteRule,
      });
      setCollectionDraft(emptyCollectionDraft);
      setCollectionModal(null);
      setSelectedCollection(created.name);
      showNotice("success", `Collection ${created.name} created`);
      await loadProjectData(token, selectedProject, created.name);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveCollectionSettings() {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const updated = await updateCollection(token, selectedProject, selectedCollectionModel.name, {
        fields: editingFields.map(cleanField),
        ...editingRules,
      });
      showNotice("success", `Collection ${updated.name} saved`);
      setCollectionModal(null);
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

  function addDraftField() {
    setCollectionDraft((draft) => ({ ...draft, fields: [...draft.fields, newDefaultField()] }));
  }

  function addEditingField() {
    setEditingFields((fields) => [...fields, newDefaultField()]);
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
      setRecordEditorOpen(false);
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

  async function saveSMTPSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const payload = {
        enabled: smtpDraft.enabled,
        host: smtpDraft.host,
        port: smtpDraft.port,
        from: smtpDraft.from,
        username: smtpDraft.username,
        clearPassword: smtpDraft.clearPassword,
        ...(smtpDraft.password ? { password: smtpDraft.password } : {}),
      };
      const response = await updateSMTPSettings(token, payload);
      setSettingsState(response);
      setSMTPDraft(settingsToSMTPDraft(response));
      showNotice("success", "SMTP settings saved");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function sendSMTPTest() {
    if (!token || !smtpDraft.testTo.trim()) return;
    setBusy(true);
    try {
      await testSMTPSettings(token, smtpDraft.testTo);
      showNotice("success", "SMTP test sent");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveStorageSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const response = await updateStorageSettings(token, {
        type: storageDraft.type,
        s3: {
          endpoint: storageDraft.endpoint,
          bucket: storageDraft.bucket,
          region: storageDraft.region,
          accessKey: storageDraft.accessKey,
          clearSecretKey: storageDraft.clearSecretKey,
          ...(storageDraft.secretKey ? { secretKey: storageDraft.secretKey } : {}),
          prefix: storageDraft.prefix,
          useSSL: storageDraft.useSSL,
          forcePathStyle: storageDraft.forcePathStyle,
        },
      });
      setSettingsState(response);
      setStorageDraft(settingsToStorageDraft(response));
      showNotice("success", "Storage settings saved");
      const healthResponse = await health();
      setHealthState(healthResponse);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function runStorageTest() {
    if (!token) return;
    setBusy(true);
    try {
      await testStorageSettings(token);
      showNotice("success", "Storage test passed");
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
      <main className="pb-loading">
        <RefreshCw className="h-4 w-4 animate-spin" />
        Loading admin session
      </main>
    );
  }

  if (!token || !admin) {
    return <AuthScreen busy={busy} healthState={healthState} notice={notice} onLogin={submitLogin} onSetup={submitSetup} />;
  }

  return (
    <main className="pb-app">
      <header className="pb-app-header accent-surface">
        <button type="button" className="pb-logo" onClick={() => changeView("collections")} aria-label="Open collections">
          <Database className="h-4 w-4" />
        </button>
        <nav className="pb-main-nav" aria-label="Primary">
          {navItems.map((item) => {
            const Icon = item.icon;
            return (
              <button key={item.id} type="button" onClick={() => changeView(item.id)} className={`pb-header-link ${view === item.id ? "active" : ""}`}>
                <Icon className="h-4 w-4" />
                {item.label}
              </button>
            );
          })}
        </nav>
        <div className="pb-header-spacer" />
        <select
          aria-label="Select project"
          value={selectedProject}
          onChange={(event) => {
            const slug = event.target.value;
            setSelectedProject(slug);
            if (token) loadProjectData(token, slug).catch(handleError);
          }}
          className="pb-project-select"
        >
          <option value="">No project</option>
          {projects.map((project) => (
            <option key={project.id} value={project.slug}>
              {project.name}
            </option>
          ))}
        </select>
        <StatusPill label="DB" value={healthState?.db ?? "checking"} ok={healthState?.db === "ok"} />
        <StatusPill label="Storage" value={healthState?.storage ?? "checking"} ok={healthState?.storage === "ok"} />
        <button type="button" onClick={() => refreshAll()} className="pb-header-link" aria-label="Refresh">
          <RefreshCw className={`h-4 w-4 ${busy ? "animate-spin" : ""}`} />
        </button>
        <button type="button" onClick={signOut} className="pb-header-link logged-user" title="Log out">
          <span className="truncate">{admin.email}</span>
          <LogOut className="h-4 w-4" />
        </button>
      </header>

      {notice ? (
        <div className={`pb-toast ${notice.type === "error" ? "danger" : "success"}`}>
          {notice.type === "error" ? <AlertCircle className="h-4 w-4" /> : <Check className="h-4 w-4" />}
          {notice.message}
        </div>
      ) : null}

      {view === "collections" ? (
        <CollectionsWorkspace
          project={selectedProject}
          collections={collections}
          selectedCollection={selectedCollectionModel}
          selectedCollectionName={selectedCollection}
          records={records}
          recordFilter={recordFilter}
          setRecordFilter={setRecordFilter}
          onSelectCollection={setSelectedCollection}
          onRefresh={() => refreshRecords(1)}
          onOpenCreateCollection={() => setCollectionModal("create")}
          onOpenCollectionSettings={() => selectedCollectionModel && setCollectionModal("settings")}
          onOpenAPIPreview={() => selectedCollectionModel && setAPIPreviewOpen(true)}
          onOpenNewRecord={() => {
            setSelectedRecordId("");
            setRecordJSON("{}");
            setRecordEditorOpen(true);
          }}
          onEditRecord={(record) => {
            setSelectedRecordId(String(record.id ?? ""));
            setRecordJSON(JSON.stringify(stripSystemFields(record), null, 2));
            setRecordEditorOpen(true);
          }}
          onDeleteRecord={removeRecord}
          onDeleteCollection={removeCollection}
          version={healthState?.version ?? "unknown"}
        />
      ) : null}

      {view === "logs" ? <LogsView audit={audit} onRefresh={() => token && selectedProject && listAudit(token, selectedProject).then((r) => setAudit(r.items)).catch(handleError)} version={healthState?.version ?? "unknown"} /> : null}

      {view === "settings" ? (
        <SettingsWorkspace
          section={settingsSection}
          onChangeSection={changeSettings}
          project={selectedProjectModel}
          projects={projects}
          projectDraft={projectDraft}
          setProjectDraft={setProjectDraft}
          onSubmitProject={submitProject}
          healthState={healthState}
          appUrl={typeof window !== "undefined" ? window.location.origin : ""}
          settings={settings}
          smtpDraft={smtpDraft}
          setSMTPDraft={setSMTPDraft}
          storageDraft={storageDraft}
          setStorageDraft={setStorageDraft}
          onSaveSMTP={saveSMTPSettings}
          onTestSMTP={sendSMTPTest}
          onSaveStorage={saveStorageSettings}
          onTestStorage={runStorageTest}
          apiKeys={apiKeys}
          oneTimeKey={oneTimeKey}
          keyDraft={keyDraft}
          setKeyDraft={setKeyDraft}
          onCreateKey={submitAPIKey}
          onRevokeKey={revokeKey}
          onCopy={copyText}
          onDismissSecret={() => setOneTimeKey(null)}
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
          projectSlug={selectedProject}
          onSubmitUpload={submitUpload}
          version={healthState?.version ?? "unknown"}
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

      {collectionModal === "create" ? (
        <CollectionModal
          mode="create"
          collections={collections}
          draft={collectionDraft}
          setDraft={setCollectionDraft}
          fields={collectionDraft.fields}
          setFields={(fields) => setCollectionDraft((draft) => ({ ...draft, fields }))}
          rules={collectionDraft}
          setRules={(rules) => setCollectionDraft((draft) => ({ ...draft, ...rules }))}
          onAddField={addDraftField}
          onClose={() => setCollectionModal(null)}
          onSubmit={submitCollection}
        />
      ) : null}

      {collectionModal === "settings" && selectedCollectionModel ? (
        <CollectionModal
          mode="settings"
          collection={selectedCollectionModel}
          collections={collections}
          fields={editingFields}
          setFields={setEditingFields}
          rules={editingRules}
          setRules={setEditingRules}
          onAddField={addEditingField}
          onClose={() => setCollectionModal(null)}
          onSave={saveCollectionSettings}
        />
      ) : null}

      {recordEditorOpen && selectedCollectionModel ? (
        <RecordModal
          collection={selectedCollectionModel}
          selectedRecordId={selectedRecordId}
          recordJSON={recordJSON}
          setRecordJSON={setRecordJSON}
          onClose={() => setRecordEditorOpen(false)}
          onSave={saveRecord}
        />
      ) : null}

      {apiPreviewOpen && selectedCollectionModel ? (
        <APIPreviewModal project={selectedProject} collection={selectedCollectionModel} onClose={() => setAPIPreviewOpen(false)} onCopy={copyText} />
      ) : null}
    </main>
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
  const [showPassword, setShowPassword] = useState(false);
  return (
    <main className="pb-login-screen">
      <section className="pb-login-card" aria-labelledby="login-title">
        <div className="pb-login-logo">
          <Database className="h-6 w-6" />
        </div>
        <h1 id="login-title">Superuser login</h1>
        {notice ? (
          <div className={`pb-inline-alert ${notice.type === "error" ? "danger" : "success"}`}>
            {notice.message}
          </div>
        ) : null}
        <form onSubmit={onLogin} className="pb-form-stack">
          <label className="pb-field">
            <span>Email</span>
            <input id="login_identity" name="email" type="email" autoComplete="username" required />
          </label>
          <label className="pb-field password-field">
            <span>Password</span>
            <input id="login_pass" name="password" type={showPassword ? "text" : "password"} autoComplete="current-password" required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowPassword((value) => !value)} aria-label={showPassword ? "Hide password" : "Show password"}>
              {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <button type="button" className="pb-link-hint">
            Forgotten password
          </button>
          <button type="submit" disabled={busy} className="pb-btn primary lg block">
            {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
            Login
          </button>
        </form>
        <details className="pb-setup-details">
          <summary>Create first admin if setup is open</summary>
          <form onSubmit={onSetup} className="pb-form-stack compact">
            <label className="pb-field">
              <span>Email</span>
              <input id="setupEmail" name="setupEmail" type="email" autoComplete="username" required />
            </label>
            <label className="pb-field">
              <span>Password</span>
              <input id="setupPassword" name="setupPassword" type="password" autoComplete="new-password" minLength={12} required />
            </label>
            <button type="submit" disabled={busy} className="pb-btn secondary">
              <Plus className="h-4 w-4" />
              Create admin
            </button>
          </form>
        </details>
        <div className="pb-login-status">
          <span>DB {healthState?.db ?? "checking"}</span>
          <span>Storage {healthState?.storage ?? "checking"}</span>
          <span>{healthState?.version ?? "unknown"}</span>
        </div>
      </section>
    </main>
  );
}

function CollectionsWorkspace({
  project,
  collections,
  selectedCollection,
  selectedCollectionName,
  records,
  recordFilter,
  setRecordFilter,
  onSelectCollection,
  onRefresh,
  onOpenCreateCollection,
  onOpenCollectionSettings,
  onOpenAPIPreview,
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
  recordFilter: string;
  setRecordFilter: (value: string) => void;
  onSelectCollection: (name: string) => void;
  onRefresh: () => void;
  onOpenCreateCollection: () => void;
  onOpenCollectionSettings: () => void;
  onOpenAPIPreview: () => void;
  onOpenNewRecord: () => void;
  onEditRecord: (record: RecordItem) => void;
  onDeleteRecord: (record: RecordItem) => void;
  onDeleteCollection: (collection: Collection) => void;
  version: string;
}) {
  const columns = selectedCollection ? Array.from(new Set(["id", ...selectedCollection.fields.map((field) => field.name), "created", "updated"])) : ["id"];
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

        <form
          className="pb-searchbar"
          onSubmit={(event) => {
            event.preventDefault();
            onRefresh();
          }}
        >
          <button type="button" className="pb-btn sm pill secondary transparent" title="Search history">
            <Search className="h-4 w-4" />
          </button>
          <input value={recordFilter} onChange={(event) => setRecordFilter(event.target.value)} placeholder='Search records, for example title = "hello"' />
          <button type="submit" className="pb-btn sm secondary">
            Load
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
              {records.items.map((record, index) => (
                <tr key={String(record.id ?? index)} onDoubleClick={() => onEditRecord(record)}>
                  <td className="col-bulk">
                    <input type="checkbox" aria-label={`Select record ${String(record.id ?? index)}`} />
                  </td>
                  {columns.map((column) => (
                    <td key={column} className="truncate-cell">
                      {renderValue(record[column])}
                    </td>
                  ))}
                  <td className="row-actions">
                    <button type="button" className="pb-btn sm transparent secondary" onClick={() => onEditRecord(record)}>
                      Edit
                    </button>
                    <button type="button" className="pb-btn sm transparent danger" onClick={() => onDeleteRecord(record)}>
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
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

        <PageFooter left={selectedCollection ? `Total: ${records.totalItems}` : "No collection selected"} version={version} />
      </div>
    </section>
  );
}

function CollectionSidebar({
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

function CollectionGroup({
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
          <CollectionIcon type={collection.type} />
          <span className="txt">{collection.name}</span>
          {collection.type === "auth" ? <ShieldCheck className="h-3.5 w-3.5 hint" /> : null}
        </button>
      ))}
    </details>
  );
}

function CollectionIcon({ type }: { type: Collection["type"] }) {
  if (type === "auth") return <ShieldCheck className="h-4 w-4" />;
  if (type === "view") return <Eye className="h-4 w-4" />;
  return <Table2 className="h-4 w-4" />;
}

function CollectionModal({
  mode,
  collection,
  collections,
  draft,
  setDraft,
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
  fields: Field[];
  setFields: (fields: Field[]) => void;
  rules: RuleDraft;
  setRules: (rules: RuleDraft) => void;
  onAddField: () => void;
  onClose: () => void;
  onSubmit?: (event: React.FormEvent<HTMLFormElement>) => void;
  onSave?: () => void;
}) {
  const [tab, setTab] = useState<"fields" | "rules">("fields");
  const title = mode === "create" ? "Create collection" : "Collection settings";
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
              <CollectionIcon type={collection.type} />
              <div>
                <p>{collection.name}</p>
                <span>{collection.type} collection</span>
              </div>
            </div>
          ) : null}

          <div className="pb-tabs" role="tablist" aria-label="Collection editor">
            <button type="button" role="tab" aria-selected={tab === "fields"} className={`pb-tab-item ${tab === "fields" ? "active" : ""}`} onClick={() => setTab("fields")}>
              Fields
            </button>
            <button type="button" role="tab" aria-selected={tab === "rules"} className={`pb-tab-item ${tab === "rules" ? "active" : ""}`} onClick={() => setTab("rules")}>
              API rules
            </button>
          </div>

          {tab === "fields" ? <FieldRows fields={fields} collections={collections} onChange={setFields} onAdd={onAddField} /> : null}
          {tab === "rules" ? <RuleInputs rules={rules} onChange={setRules} /> : null}
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

function FieldRows({
  fields,
  collections,
  onChange,
  onAdd,
}: {
  fields: Field[];
  collections: Collection[];
  onChange: (fields: Field[]) => void;
  onAdd: () => void;
}) {
  const update = (index: number, field: Field) => onChange(fields.map((item, i) => (i === index ? field : item)));
  return (
    <div className="pb-fields-editor">
      {fields.length === 0 ? <EmptyState label="No fields yet" /> : null}
      {fields.map((field, index) => (
        <div key={`${field.name}-${index}`} className="pb-field-row">
          <div className="pb-field-row-main">
            <label className="pb-field-type">
              <span className="sr-only">Type</span>
              <select value={field.type} onChange={(event) => update(index, fieldWithType(field, event.target.value as FieldType))}>
                {fieldTypes.map((type) => (
                  <option key={type} value={type}>
                    {type}
                  </option>
                ))}
              </select>
            </label>
            <input value={field.name} onChange={(event) => update(index, { ...field, name: event.target.value })} placeholder="Field name*" />
            <label className="pb-checkline sm">
              <input type="checkbox" checked={Boolean(field.required)} onChange={(event) => update(index, { ...field, required: event.target.checked })} />
              Required
            </label>
            <button type="button" aria-label={`Remove field ${field.name || index + 1}`} className="pb-btn sm circle transparent danger" onClick={() => onChange(fields.filter((_, i) => i !== index))}>
              <Trash2 className="h-4 w-4" />
            </button>
          </div>
          <FieldOptionsEditor field={field} collections={collections} onChange={(next) => update(index, next)} />
        </div>
      ))}
      <button type="button" className="pb-btn outline block" onClick={onAdd}>
        <Plus className="h-4 w-4" />
        New field
      </button>
    </div>
  );
}

function FieldOptionsEditor({ field, collections, onChange }: { field: Field; collections: Collection[]; onChange: (field: Field) => void }) {
  if (field.type === "select") {
    return (
      <div className="pb-field-options two">
        <label className="pb-field">
          <span>Values</span>
          <textarea value={optionValuesText(field.options)} onChange={(event) => onChange(setFieldOption(field, "values", splitOptionValues(event.target.value)))} rows={3} placeholder={"draft\npublished"} />
        </label>
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.multi)} onChange={(event) => onChange(setFieldOption(field, "multi", event.target.checked))} />
          Multiple
        </label>
      </div>
    );
  }
  if (field.type === "relation") {
    return (
      <div className="pb-field-options two">
        <label className="pb-field">
          <span>Target collection</span>
          <select value={String(field.options?.collection ?? "")} onChange={(event) => onChange(setFieldOption(field, "collection", event.target.value))}>
            <option value="">Choose collection</option>
            {collections.map((collection) => (
              <option key={collection.id} value={collection.name}>
                {collection.name}
              </option>
            ))}
          </select>
        </label>
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.multi)} onChange={(event) => onChange(setFieldOption(field, "multi", event.target.checked))} />
          Multiple
        </label>
      </div>
    );
  }
  if (field.type === "file") {
    return (
      <div className="pb-field-options">
        <label className="pb-checkline">
          <input type="checkbox" checked={Boolean(field.options?.multiple)} onChange={(event) => onChange(setFieldOption(field, "multiple", event.target.checked))} />
          Allow multiple files
        </label>
      </div>
    );
  }
  return null;
}

function RuleInputs({ rules, onChange }: { rules: RuleDraft; onChange: (rules: RuleDraft) => void }) {
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

function RuleTextarea({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="pb-field">
      <span>{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.target.value)} rows={4} placeholder='@request.auth.id != ""' className="mono" />
    </label>
  );
}

function RecordModal({
  collection,
  selectedRecordId,
  recordJSON,
  setRecordJSON,
  onClose,
  onSave,
}: {
  collection: Collection;
  selectedRecordId: string;
  recordJSON: string;
  setRecordJSON: (value: string) => void;
  onClose: () => void;
  onSave: () => void;
}) {
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
            <CollectionIcon type={collection.type} />
            <div>
              <p>{collection.name}</p>
              <span>{selectedRecordId || "new record"}</span>
            </div>
          </div>
          <label className="pb-field">
            <span>JSON</span>
            <textarea value={recordJSON} onChange={(event) => setRecordJSON(event.target.value)} rows={20} className="mono json-editor" />
          </label>
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

function APIPreviewModal({ project, collection, onClose, onCopy }: { project: string; collection: Collection; onClose: () => void; onCopy: (text: string) => void }) {
  const basePath = `/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection.name)}`;
  const sample = `GET ${basePath}/records\nPOST ${basePath}/records\nPATCH ${basePath}/records/{id}\nDELETE ${basePath}/records/{id}`;
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
            <CollectionIcon type={collection.type} />
            <div>
              <p>{collection.name}</p>
              <span>{collection.fields.length} fields</span>
            </div>
          </div>
          <pre className="pb-code-box">{sample}</pre>
          <button type="button" className="pb-btn secondary" onClick={() => onCopy(sample)}>
            <Copy className="h-4 w-4" />
            Copy
          </button>
        </div>
      </section>
    </div>
  );
}

function LogsView({ audit, onRefresh, version }: { audit: AuditEntry[]; onRefresh: () => void; version: string }) {
  return (
    <section className="pb-page single">
      <div className="pb-page-content full-height">
        <header className="pb-page-header">
          <nav className="pb-breadcrumbs" aria-label="Breadcrumb">
            <span>Logs</span>
          </nav>
          <div className="pb-header-primary-btns">
            <button type="button" onClick={onRefresh} className="pb-btn outline">
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
          </div>
        </header>
        <div className="pb-table-wrap">
          <table className="pb-records-table">
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
                  <td className="truncate-cell">{JSON.stringify(entry.data)}</td>
                </tr>
              ))}
              {audit.length === 0 ? (
                <tr>
                  <td colSpan={5} className="pb-empty-cell">
                    <EmptyState label="No audit entries yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <PageFooter left={`Total: ${audit.length}`} version={version} />
      </div>
    </section>
  );
}

function SettingsWorkspace(props: {
  section: SettingsSection;
  onChangeSection: (section: SettingsSection) => void;
  project: Project | null;
  projects: Project[];
  projectDraft: { slug: string; name: string };
  setProjectDraft: (draft: { slug: string; name: string }) => void;
  onSubmitProject: (event: React.FormEvent<HTMLFormElement>) => void;
  healthState: Health | null;
  appUrl: string;
  settings: InstanceSettings | null;
  smtpDraft: typeof emptySMTPDraft;
  setSMTPDraft: React.Dispatch<React.SetStateAction<typeof emptySMTPDraft>>;
  storageDraft: typeof emptyStorageDraft;
  setStorageDraft: React.Dispatch<React.SetStateAction<typeof emptyStorageDraft>>;
  onSaveSMTP: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestSMTP: () => void;
  onSaveStorage: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestStorage: () => void;
  apiKeys: APIKey[];
  oneTimeKey: APIKey | null;
  keyDraft: { name: string; type: "anon" | "service" };
  setKeyDraft: React.Dispatch<React.SetStateAction<{ name: string; type: "anon" | "service" }>>;
  onCreateKey: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeKey: (key: APIKey) => void;
  onCopy: (text: string) => void;
  onDismissSecret: () => void;
  collections: Collection[];
  selectedCollection: Collection | null;
  selectedCollectionName: string;
  setSelectedCollection: (name: string) => void;
  fileFields: Field[];
  fileDraft: { recordId: string; field: string };
  setFileDraft: React.Dispatch<React.SetStateAction<{ recordId: string; field: string }>>;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  projectSlug: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
  version: string;
}) {
  const active = settingsItems.find((item) => item.id === props.section);
  return (
    <section className="pb-page">
      <SettingsSidebar active={props.section} onChange={props.onChangeSection} />
      <div className="pb-page-content full-height">
        <header className="pb-page-header">
          <nav className="pb-breadcrumbs" aria-label="Breadcrumb">
            <span>Settings</span>
            {active ? <span>{active.label}</span> : null}
          </nav>
        </header>
        <div className="pb-wrapper">
          {props.section === "application" ? <ApplicationSettings {...props} /> : null}
          {props.section === "mail" ? <MailSettings {...props} /> : null}
          {props.section === "storage" ? <StorageSettingsPanel {...props} /> : null}
          {props.section === "apiKeys" ? <APIKeysView {...props} /> : null}
          {props.section === "files" ? <FilesView {...props} /> : null}
        </div>
        <PageFooter left="Settings" version={props.version} />
      </div>
    </section>
  );
}

function SettingsSidebar({ active, onChange }: { active: SettingsSection; onChange: (section: SettingsSection) => void }) {
  const grouped = settingsItems.reduce<Record<string, typeof settingsItems[number][]>>((acc, item) => {
    acc[item.group] = [...(acc[item.group] ?? []), item];
    return acc;
  }, {});
  return (
    <aside className="pb-sidebar settings-sidebar">
      <nav className="pb-sidebar-content" aria-label="Settings">
        {Object.entries(grouped).map(([group, items]) => (
          <details key={group} className="pb-nav-group" open>
            <summary>{group}</summary>
            {items.map((item) => (
              <button key={item.id} type="button" className={`pb-nav-item ${active === item.id ? "active" : ""}`} onClick={() => onChange(item.id)}>
                <SettingsIcon id={item.id} />
                <span className="txt">{item.label}</span>
              </button>
            ))}
          </details>
        ))}
      </nav>
    </aside>
  );
}

function SettingsIcon({ id }: { id: SettingsSection }) {
  if (id === "mail") return <Mail className="h-4 w-4" />;
  if (id === "storage") return <HardDrive className="h-4 w-4" />;
  if (id === "apiKeys") return <KeyRound className="h-4 w-4" />;
  if (id === "files") return <UploadCloud className="h-4 w-4" />;
  return <Settings className="h-4 w-4" />;
}

function ApplicationSettings({
  project,
  projects,
  projectDraft,
  setProjectDraft,
  onSubmitProject,
  healthState,
  appUrl,
}: {
  project: Project | null;
  projects: Project[];
  projectDraft: { slug: string; name: string };
  setProjectDraft: (draft: { slug: string; name: string }) => void;
  onSubmitProject: (event: React.FormEvent<HTMLFormElement>) => void;
  healthState: Health | null;
  appUrl: string;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Application</h2>
        <div className="pb-info-grid">
          <Info label="Application URL" value={appUrl} />
          <Info label="Version" value={healthState?.version ?? ""} />
          <Info label="DB" value={healthState?.db ?? ""} />
          <Info label="Storage" value={healthState?.storage ?? ""} />
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Project</h2>
        {project ? (
          <div className="pb-info-grid">
            <Info label="Slug" value={project.slug} />
            <Info label="Name" value={project.name} />
            <Info label="Schema" value={project.schemaName} />
            <Info label="Anon role" value={project.roles?.anon ?? ""} />
            <Info label="Authenticated role" value={project.roles?.authenticated ?? ""} />
            <Info label="Service role" value={project.roles?.service ?? ""} />
          </div>
        ) : (
          <EmptyState label="No project selected." />
        )}
      </section>
      <section className="pb-settings-block">
        <h2>Projects</h2>
        <form onSubmit={onSubmitProject} className="pb-grid-form">
          <LabeledInput label="Slug" value={projectDraft.slug} onChange={(value) => setProjectDraft({ ...projectDraft, slug: value })} placeholder="myapp" />
          <LabeledInput label="Name" value={projectDraft.name} onChange={(value) => setProjectDraft({ ...projectDraft, name: value })} placeholder="My app" />
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create project
          </button>
        </form>
        <CompactTable headers={["Name", "Slug", "Schema"]} rows={projects.map((item) => [item.name, item.slug, item.schemaName])} empty="No projects yet." />
      </section>
    </div>
  );
}

function MailSettings({
  settings,
  smtpDraft,
  setSMTPDraft,
  onSaveSMTP,
  onTestSMTP,
}: {
  settings: InstanceSettings | null;
  smtpDraft: typeof emptySMTPDraft;
  setSMTPDraft: React.Dispatch<React.SetStateAction<typeof emptySMTPDraft>>;
  onSaveSMTP: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestSMTP: () => void;
}) {
  return (
    <form onSubmit={onSaveSMTP} className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Mail settings</h2>
        <p className="pb-muted-copy">Configure common settings for sending emails.</p>
        <div className="pb-grid-form two">
          <LabeledInput label="Sender address" value={smtpDraft.from} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, from: value }))} placeholder="Support <support@example.com>" />
          <label className="pb-checkline switchline">
            <input type="checkbox" checked={smtpDraft.enabled} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Use SMTP mail server <strong>(recommended)</strong>
          </label>
        </div>
        {smtpDraft.enabled ? (
          <div className="pb-grid-form smtp-grid">
            <LabeledInput label="SMTP server host" value={smtpDraft.host} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, host: value }))} placeholder="smtp.example.com" />
            <LabeledInput label="Port" value={smtpDraft.port} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, port: value }))} placeholder="587" />
            <LabeledInput label="Username" value={smtpDraft.username} onChange={(value) => setSMTPDraft((draft) => ({ ...draft, username: value }))} />
            <label className="pb-field">
              <span>Password {settings?.smtp.passwordSet ? <em>(saved)</em> : null}</span>
              <input type="password" value={smtpDraft.password} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, password: event.target.value, clearPassword: false }))} placeholder={settings?.smtp.passwordSet ? "* * * * * *" : ""} />
            </label>
            <label className="pb-checkline">
              <input type="checkbox" checked={smtpDraft.clearPassword} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, clearPassword: event.target.checked, password: "" }))} />
              Clear password
            </label>
          </div>
        ) : null}
      </section>
      <section className="pb-settings-actions">
        <label className="pb-field test-recipient">
          <span>Test recipient</span>
          <input value={smtpDraft.testTo} onChange={(event) => setSMTPDraft((draft) => ({ ...draft, testTo: event.target.value }))} placeholder="you@example.com" />
        </label>
        <button type="button" onClick={onTestSMTP} className="pb-btn outline expanded-lg">
          <Mail className="h-4 w-4" />
          Send test email
        </button>
        <button type="submit" className="pb-btn primary expanded-lg">
          Save changes
        </button>
      </section>
    </form>
  );
}

function StorageSettingsPanel({
  settings,
  storageDraft,
  setStorageDraft,
  onSaveStorage,
  onTestStorage,
}: {
  settings: InstanceSettings | null;
  storageDraft: typeof emptyStorageDraft;
  setStorageDraft: React.Dispatch<React.SetStateAction<typeof emptyStorageDraft>>;
  onSaveStorage: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestStorage: () => void;
}) {
  const s3Enabled = storageDraft.type === "s3";
  return (
    <form onSubmit={onSaveStorage} className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>File storage</h2>
        <p className="pb-muted-copy">By default Dublyobase uses and recommends the local file system to store uploaded files because it is faster to manage and backup.</p>
        <p className="pb-muted-copy">Alternatively, if you have limited disk space available, you could opt to an S3 compatible external storage.</p>
        <label className="pb-checkline switchline">
          <input type="checkbox" checked={s3Enabled} onChange={(event) => setStorageDraft((draft) => ({ ...draft, type: event.target.checked ? "s3" : "local" }))} />
          Use S3 storage
        </label>
        <div className="pb-info-grid compact">
          <Info label="Source" value={settings?.storage.source ?? ""} />
          <Info label="Local path" value={settings?.storage.localPath ?? ""} />
        </div>
        {s3Enabled ? (
          <>
            <div className="pb-inline-alert info">
              If you have existing uploaded files, migrate them manually from local storage to S3 storage. Useful tools include{" "}
              <a href="https://github.com/rclone/rclone" target="_blank" rel="noreferrer">
                rclone
              </a>{" "}
              and{" "}
              <a href="https://github.com/peak/s5cmd" target="_blank" rel="noreferrer">
                s5cmd
              </a>
              .
            </div>
            <div className="pb-grid-form s3-grid">
              <LabeledInput label="Endpoint" value={storageDraft.endpoint} onChange={(value) => setStorageDraft((draft) => ({ ...draft, endpoint: value }))} placeholder="https://s3.example.com" />
              <LabeledInput label="Bucket" value={storageDraft.bucket} onChange={(value) => setStorageDraft((draft) => ({ ...draft, bucket: value }))} placeholder="dublyobase" />
              <LabeledInput label="Region" value={storageDraft.region} onChange={(value) => setStorageDraft((draft) => ({ ...draft, region: value }))} placeholder="auto" />
              <LabeledInput label="Prefix" value={storageDraft.prefix} onChange={(value) => setStorageDraft((draft) => ({ ...draft, prefix: value }))} placeholder="prod" />
              <LabeledInput label="Access key" value={storageDraft.accessKey} onChange={(value) => setStorageDraft((draft) => ({ ...draft, accessKey: value }))} />
              <label className="pb-field">
                <span>Secret {settings?.storage.s3.secretKeySet ? <em>(saved)</em> : null}</span>
                <input type="password" value={storageDraft.secretKey} onChange={(event) => setStorageDraft((draft) => ({ ...draft, secretKey: event.target.value, clearSecretKey: false }))} placeholder={settings?.storage.s3.secretKeySet ? "* * * * * *" : ""} />
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.forcePathStyle} onChange={(event) => setStorageDraft((draft) => ({ ...draft, forcePathStyle: event.target.checked }))} />
                Force path-style addressing
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.useSSL} onChange={(event) => setStorageDraft((draft) => ({ ...draft, useSSL: event.target.checked }))} />
                HTTPS
              </label>
              <label className="pb-checkline">
                <input type="checkbox" checked={storageDraft.clearSecretKey} onChange={(event) => setStorageDraft((draft) => ({ ...draft, clearSecretKey: event.target.checked, secretKey: "" }))} />
                Clear secret
              </label>
            </div>
          </>
        ) : null}
      </section>
      <section className="pb-settings-actions">
        <button type="button" onClick={onTestStorage} className="pb-btn outline expanded-lg">
          Test storage
        </button>
        <button type="submit" className="pb-btn primary expanded-lg">
          Save changes
        </button>
      </section>
    </form>
  );
}

function APIKeysView(props: {
  apiKeys: APIKey[];
  oneTimeKey: APIKey | null;
  keyDraft: { name: string; type: "anon" | "service" };
  setKeyDraft: React.Dispatch<React.SetStateAction<{ name: string; type: "anon" | "service" }>>;
  onCreateKey: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeKey: (key: APIKey) => void;
  onCopy: (text: string) => void;
  onDismissSecret: () => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>API keys</h2>
        <CompactTable
          headers={["Name", "Type", "Prefix", "Created", "Status", ""]}
          rows={props.apiKeys.map((key) => [key.name, key.type, key.prefix, formatDate(key.createdAt), key.revokedAt ? "revoked" : "active", ""])}
          empty="No API keys yet."
        />
        <div className="pb-table-actions">
          {props.apiKeys.map((key) =>
            !key.revokedAt ? (
              <button key={key.id} type="button" onClick={() => props.onRevokeKey(key)} className="pb-btn sm transparent danger">
                Revoke {key.name}
              </button>
            ) : null,
          )}
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create key</h2>
        <form onSubmit={props.onCreateKey} className="pb-grid-form">
          <LabeledInput label="Name" value={props.keyDraft.name} onChange={(value) => props.setKeyDraft((draft) => ({ ...draft, name: value }))} placeholder="production service" />
          <label className="pb-field">
            <span>Type</span>
            <select value={props.keyDraft.type} onChange={(event) => props.setKeyDraft((draft) => ({ ...draft, type: event.target.value as "anon" | "service" }))}>
              <option value="service">service</option>
              <option value="anon">anon</option>
            </select>
          </label>
          <button type="submit" className="pb-btn primary">
            <KeyRound className="h-4 w-4" />
            Create key
          </button>
        </form>
        {props.oneTimeKey?.key ? (
          <div className="pb-inline-alert warning">
            <p>Copy this key now. It will not be shown again.</p>
            <pre className="pb-code-box">{props.oneTimeKey.key}</pre>
            <div className="pb-row-actions">
              <button type="button" onClick={() => props.onCopy(props.oneTimeKey?.key ?? "")} className="pb-btn secondary">
                <Copy className="h-4 w-4" />
                Copy
              </button>
              <button type="button" onClick={props.onDismissSecret} className="pb-btn transparent">
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </section>
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
  setFileDraft: React.Dispatch<React.SetStateAction<{ recordId: string; field: string }>>;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  projectSlug: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
}) {
  const availableFiles = findFiles(props.records.items, props.fileFields);
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Upload file</h2>
        <form onSubmit={props.onSubmitUpload} className="pb-grid-form">
          <label className="pb-field">
            <span>Collection</span>
            <select value={props.selectedCollectionName} onChange={(event) => props.setSelectedCollection(event.target.value)}>
              {props.collections.map((collection) => (
                <option key={collection.id} value={collection.name}>
                  {collection.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Record ID" value={props.fileDraft.recordId} onChange={(value) => props.setFileDraft((draft) => ({ ...draft, recordId: value }))} placeholder="uuid" />
          <label className="pb-field">
            <span>File field</span>
            <select value={props.fileDraft.field} onChange={(event) => props.setFileDraft((draft) => ({ ...draft, field: event.target.value }))}>
              {props.fileFields.map((field) => (
                <option key={field.name} value={field.name}>
                  {field.name}
                </option>
              ))}
            </select>
          </label>
          <label className="pb-field">
            <span>File</span>
            <input name="file" type="file" required />
          </label>
          <button type="submit" className="pb-btn primary">
            <UploadCloud className="h-4 w-4" />
            Upload
          </button>
        </form>
        {props.fileResult ? <pre className="pb-code-box">{JSON.stringify(props.fileResult, null, 2)}</pre> : null}
      </section>
      <section className="pb-settings-block">
        <h2>Protected files</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table">
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
                  <td className="row-actions">
                    <button
                      type="button"
                      onClick={async () => {
                        const fileToken = await props.onCreateFileToken(file.recordId, file.field, file.id);
                        if (fileToken) {
                          window.open(`/api/projects/${encodeURIComponent(props.projectSlug)}/files/${encodeURIComponent(props.selectedCollection?.name ?? "")}/${encodeURIComponent(file.recordId)}/${encodeURIComponent(file.field)}/${encodeURIComponent(file.id)}/${encodeURIComponent(file.name)}?token=${encodeURIComponent(fileToken)}`, "_blank", "noopener,noreferrer");
                        }
                      }}
                      className="pb-btn sm secondary"
                    >
                      Download
                    </button>
                  </td>
                </tr>
              ))}
              {availableFiles.length === 0 ? (
                <tr>
                  <td colSpan={5} className="pb-empty-cell">
                    <EmptyState label="Load records from a collection with file fields." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function LabeledInput({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="pb-field">
      <span>{label}</span>
      <input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} />
    </label>
  );
}

function CompactTable({ headers, rows, empty }: { headers: string[]; rows: string[][]; empty: string }) {
  return (
    <div className="pb-table-wrap">
      <table className="pb-records-table compact">
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
                <td key={cellIndex} className="truncate-cell">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
          {rows.length === 0 ? (
            <tr>
              <td colSpan={headers.length} className="pb-empty-cell">
                <EmptyState label={empty} />
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
  );
}

function EmptyState({ label, action, onAction }: { label: string; action?: string; onAction?: () => void }) {
  return (
    <div className="pb-empty-state">
      <AlertCircle className="h-4 w-4" />
      <span>{label}</span>
      {action && onAction ? (
        <button type="button" className="pb-btn secondary expanded-lg" onClick={onAction}>
          {action}
        </button>
      ) : null}
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div className="pb-info-row">
      <dt>{label}</dt>
      <dd>{value || "-"}</dd>
    </div>
  );
}

function StatusPill({ label, value, ok }: { label: string; value: string; ok: boolean }) {
  return (
    <span className="pb-status-pill">
      <span className={`status-dot ${ok ? "success" : "warning"}`} />
      {label} {value}
    </span>
  );
}

function PageFooter({ left, version }: { left: string; version: string }) {
  return (
    <footer className="pb-page-footer">
      <span>{left}</span>
      <span className="credits">
        <a href="https://github.com/dublyo/dublyobase" target="_blank" rel="noreferrer">
          Docs
        </a>
        <a href="https://github.com/dublyo/dublyobase/releases" target="_blank" rel="noreferrer">
          Dublyobase {version}
        </a>
      </span>
    </footer>
  );
}

function renderValue(value: unknown) {
  if (value === null || value === undefined || value === "") return "-";
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

function newDefaultField(): Field {
  return { name: "", type: "text", required: false, options: {} };
}

function fieldWithType(field: Field, type: FieldType): Field {
  return {
    ...field,
    type,
    options: defaultOptionsForType(type, field.options),
  };
}

function cleanField(field: Field): Field {
  const name = field.name.trim();
  const type = field.type;
  return {
    name,
    type,
    required: Boolean(field.required),
    options: defaultOptionsForType(type, field.options),
  };
}

function defaultOptionsForType(type: FieldType, options: Record<string, unknown> = {}): Record<string, unknown> {
  if (type === "select") {
    return {
      values: splitOptionValues(optionValuesText(options)),
      ...(Boolean(options.multi) ? { multi: true } : {}),
    };
  }
  if (type === "relation") {
    return {
      collection: typeof options.collection === "string" ? options.collection : "",
      ...(Boolean(options.multi) ? { multi: true } : {}),
    };
  }
  if (type === "file") {
    return Boolean(options.multiple) ? { multiple: true } : {};
  }
  return {};
}

function setFieldOption(field: Field, key: string, value: unknown): Field {
  const options = { ...(field.options ?? {}), [key]: value };
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

function optionValuesText(options: Record<string, unknown> = {}) {
  const values = options.values;
  if (Array.isArray(values)) {
    return values.map((value) => String(value)).join("\n");
  }
  return "";
}

function splitOptionValues(raw: string) {
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

function settingsToSMTPDraft(settings: InstanceSettings): typeof emptySMTPDraft {
  return {
    enabled: settings.smtp.enabled,
    host: settings.smtp.host,
    port: String(settings.smtp.port || 587),
    from: settings.smtp.from,
    username: settings.smtp.username,
    password: "",
    clearPassword: false,
    testTo: "",
  };
}

function settingsToStorageDraft(settings: InstanceSettings): typeof emptyStorageDraft {
  return {
    type: settings.storage.type,
    endpoint: settings.storage.s3.endpoint,
    bucket: settings.storage.s3.bucket,
    region: settings.storage.s3.region || "us-east-1",
    accessKey: settings.storage.s3.accessKey,
    secretKey: "",
    clearSecretKey: false,
    prefix: settings.storage.s3.prefix,
    useSSL: settings.storage.s3.useSSL,
    forcePathStyle: settings.storage.s3.forcePathStyle,
  };
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
