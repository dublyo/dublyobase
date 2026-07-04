"use client";

import {
  Activity,
  AlertCircle,
  Archive,
  Bold,
  Bell,
  Braces,
  BookOpen,
  Boxes,
  BriefcaseBusiness,
  Calendar,
  CalendarCheck2,
  Check,
  ChevronDown,
  Code2,
  Copy,
  CreditCard,
  Database,
  Download,
  Eye,
  EyeOff,
  FileText,
  FileUp,
  Folder,
  Globe,
  HardDrive,
  Hash,
  Heading,
  Image,
  Italic,
  KeyRound,
  Layers3,
  Link2,
  List,
  ListFilter,
  LogOut,
  Mail,
  MapPin,
  MoreHorizontal,
  MessageSquare,
  Package,
  PencilLine,
  Plus,
  Quote,
  RefreshCw,
  Save,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Share2,
  ShoppingCart,
  Star,
  Table2,
  Tag,
  ToggleLeft,
  Trash2,
  Type,
  Underline,
  UploadCloud,
  User,
  Users,
  X,
  type LucideIcon,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ApiError,
  changeAdminPassword,
  createAPIKey,
  createBackupJob,
  createCollection,
  createCronJob,
  createFileToken,
  createMCPToken,
  createProject,
  createRecord,
  deleteCollection,
  deleteRecord,
  discoverSchema,
  exportCollections,
  health,
  getSettings,
  importCollections,
  importSchemaTables,
  listAPIKeys,
  listAudit,
  listBackupJobs,
  listBackupRuns,
  listCollections,
  listCronJobs,
  listCronRuns,
  listMCPTokens,
  listProjects,
  listRecords,
  login,
  logout,
  me,
  revokeAPIKey,
  revokeMCPToken,
  runBackupJob,
  runCronJob,
  runSQL,
  testSMTPSettings,
  testStorageSettings,
  updateCollection,
  updateRecord,
  updateSMTPSettings,
  updateStorageSettings,
  uploadFile,
} from "../src/lib/api";
import type { APIKey, Admin, AuditEntry, BackupJob, BackupRun, Collection, CollectionExport, CollectionIconOption, CollectionImportResult, CollectionOptions, CronJob, CronRun, DiscoveredTable, Field, FieldType, Health, InstanceSettings, MCPToken, Project, RecordItem, RecordList, SchemaImportItem, SQLResult } from "../src/lib/types";

const TOKEN_KEY = "dublyobase.adminToken.v1";
const SQL_HISTORY_KEY = "dublyobase.sqlHistory.v1";
const recordPageSizes = [10, 25, 100, 250, 500] as const;
const fieldTypes: FieldType[] = ["text", "editor", "password", "number", "bool", "date", "autodate", "email", "url", "select", "json", "relation", "file"];
type FieldTypeChoice = {
  type?: FieldType;
  label: string;
  icon: LucideIcon;
  disabled?: boolean;
};
const fieldTypeChoices: FieldTypeChoice[] = [
  { type: "text", label: "Plain text", icon: Type },
  { type: "editor", label: "Rich editor", icon: PencilLine },
  { type: "number", label: "Number", icon: Hash },
  { type: "bool", label: "Bool", icon: ToggleLeft },
  { type: "email", label: "Email", icon: Mail },
  { type: "url", label: "URL", icon: Link2 },
  { type: "date", label: "Datetime", icon: Calendar },
  { type: "autodate", label: "Autodate", icon: CalendarCheck2 },
  { type: "file", label: "File", icon: Image },
  { type: "relation", label: "Relation", icon: Share2 },
  { type: "select", label: "Select", icon: List },
  { type: "json", label: "JSON", icon: Braces },
  { label: "Geo Point", icon: MapPin, disabled: true },
];
type CollectionIconChoice = {
  name: string;
  label: string;
  icon: LucideIcon;
};
const collectionIconChoices: CollectionIconChoice[] = [
  { name: "table", label: "Table", icon: Table2 },
  { name: "shield", label: "Auth", icon: ShieldCheck },
  { name: "eye", label: "View", icon: Eye },
  { name: "book-open", label: "Content", icon: BookOpen },
  { name: "boxes", label: "Catalog", icon: Boxes },
  { name: "users", label: "Users", icon: Users },
  { name: "user", label: "Profile", icon: User },
  { name: "package", label: "Package", icon: Package },
  { name: "shopping-cart", label: "Orders", icon: ShoppingCart },
  { name: "file-text", label: "Documents", icon: FileText },
  { name: "folder", label: "Folder", icon: Folder },
  { name: "message-square", label: "Messages", icon: MessageSquare },
  { name: "globe", label: "Global", icon: Globe },
  { name: "tag", label: "Tags", icon: Tag },
  { name: "star", label: "Featured", icon: Star },
  { name: "bell", label: "Alerts", icon: Bell },
  { name: "credit-card", label: "Billing", icon: CreditCard },
  { name: "briefcase", label: "Business", icon: BriefcaseBusiness },
  { name: "database", label: "Data", icon: Database },
];
const collectionIconMap = Object.fromEntries(collectionIconChoices.map((choice) => [choice.name, choice.icon])) as Record<string, LucideIcon>;
const navItems = [
  { id: "collections", label: "Collections", icon: Layers3 },
  { id: "logs", label: "Logs", icon: Archive },
  { id: "settings", label: "Settings", icon: Settings },
] as const;
const settingsItems = [
  { id: "application", label: "Application", group: "System" },
  { id: "auth", label: "Auth settings", group: "System" },
  { id: "mail", label: "Mail settings", group: "System" },
  { id: "storage", label: "Files storage", group: "System" },
  { id: "backups", label: "Backups", group: "System" },
  { id: "crons", label: "Crons", group: "System" },
  { id: "mcp", label: "MCP access", group: "System" },
  { id: "exportCollections", label: "Export collections", group: "Sync" },
  { id: "importCollections", label: "Import collections", group: "Sync" },
  { id: "discoverTables", label: "Discover tables", group: "Sync" },
  { id: "sqlConsole", label: "SQL console", group: "Debug" },
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
  icon: CollectionIconOption;
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
  icon: { type: "lucide", name: "table" },
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

const emptyCronDraft = {
  projectSlug: "",
  name: "",
  schedule: "@every 5m",
  timezone: "UTC",
  enabled: true,
  timeoutSeconds: "30",
  retryCount: "0",
  method: "GET",
  url: "",
  headersJSON: "{}",
  body: "",
};

const emptyBackupDraft = {
  name: "",
  scope: "project" as "full" | "project",
  projectSlug: "",
  schedule: "0 2 * * *",
  timezone: "UTC",
  enabled: true,
  retentionDays: "14",
  retentionCount: "10",
};

const emptyMCPDraft = {
  name: "",
  scope: "project" as "admin" | "project",
  projectSlug: "",
  allowedTools: "",
  expiresAt: "",
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
  const [records, setRecords] = useState<RecordList>({ items: [], page: 1, perPage: 25, totalItems: 0 });
  const [recordSearch, setRecordSearch] = useState("");
  const [recordFilter, setRecordFilter] = useState("");
  const [recordPerPage, setRecordPerPage] = useState<(typeof recordPageSizes)[number]>(25);
  const [recordJSON, setRecordJSON] = useState("{}");
  const [selectedRecordId, setSelectedRecordId] = useState("");
  const [recordEditorOpen, setRecordEditorOpen] = useState(false);
  const [apiPreviewOpen, setAPIPreviewOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [oneTimeKey, setOneTimeKey] = useState<APIKey | null>(null);
  const [audit, setAudit] = useState<AuditEntry[]>([]);
  const [settings, setSettingsState] = useState<InstanceSettings | null>(null);
  const [cronJobs, setCronJobs] = useState<CronJob[]>([]);
  const [cronRuns, setCronRuns] = useState<Record<string, CronRun[]>>({});
  const [backupJobs, setBackupJobs] = useState<BackupJob[]>([]);
  const [backupRuns, setBackupRuns] = useState<Record<string, BackupRun[]>>({});
  const [mcpTokens, setMCPTokens] = useState<MCPToken[]>([]);
  const [oneTimeMCPToken, setOneTimeMCPToken] = useState<MCPToken | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<Notice>(null);
  const [projectDraft, setProjectDraft] = useState({ slug: "", name: "" });
  const [collectionDraft, setCollectionDraft] = useState(emptyCollectionDraft);
  const [collectionModal, setCollectionModal] = useState<CollectionModalMode>(null);
  const [editingFields, setEditingFields] = useState<Field[]>([]);
  const [editingRules, setEditingRules] = useState<RuleDraft>(emptyRules);
  const [editingIcon, setEditingIcon] = useState<CollectionIconOption>({ type: "lucide", name: "table" });
  const [editingManaged, setEditingManaged] = useState(false);
  const [smtpDraft, setSMTPDraft] = useState(emptySMTPDraft);
  const [storageDraft, setStorageDraft] = useState(emptyStorageDraft);
  const [cronDraft, setCronDraft] = useState(emptyCronDraft);
  const [backupDraft, setBackupDraft] = useState(emptyBackupDraft);
  const [mcpDraft, setMCPDraft] = useState(emptyMCPDraft);
  const [keyDraft, setKeyDraft] = useState({ name: "", type: "service" as "anon" | "service" });
  const [fileDraft, setFileDraft] = useState({ recordId: "", field: "" });
  const [selectedUploadFile, setSelectedUploadFile] = useState<File | null>(null);
  const [fileResult, setFileResult] = useState<RecordItem | null>(null);
  const [collectionExport, setCollectionExport] = useState<CollectionExport | null>(null);
  const [exportSelection, setExportSelection] = useState<string[]>([]);
  const [importJSON, setImportJSON] = useState("");
  const [importMode, setImportMode] = useState<"create_missing" | "upsert">("create_missing");
  const [importDropMissingFields, setImportDropMissingFields] = useState(false);
  const [importResult, setImportResult] = useState<CollectionImportResult | null>(null);
  const [discoveredTables, setDiscoveredTables] = useState<DiscoveredTable[]>([]);
  const [schemaSelection, setSchemaSelection] = useState<string[]>([]);
  const [schemaFilters, setSchemaFilters] = useState({ schema: "", table: "" });
  const [schemaImportNames, setSchemaImportNames] = useState<Record<string, string>>({});
  const [schemaImportResult, setSchemaImportResult] = useState<CollectionImportResult | null>(null);
  const [sqlQuery, setSQLQuery] = useState("select * from users limit 25");
  const [sqlMaxRows, setSQLMaxRows] = useState("250");
  const [sqlResult, setSQLResult] = useState<SQLResult | null>(null);
  const [sqlHistory, setSQLHistory] = useState<string[]>([]);
  const bootstrapped = useRef(false);
  const recordQueryRef = useRef({ search: "", filter: "", perPage: 25 });

  const selectedProjectModel = useMemo(() => projects.find((project) => project.slug === selectedProject) ?? null, [projects, selectedProject]);
  const selectedCollectionModel = useMemo(
    () => collections.find((collection) => collection.name === selectedCollection) ?? collections[0] ?? null,
    [collections, selectedCollection],
  );
  const fileFields = useMemo(() => selectedCollectionModel?.fields.filter((field) => field.type === "file") ?? [], [selectedCollectionModel]);
  const selectedExportItems = useMemo(() => {
    if (!collectionExport) return [];
    const selected = new Set(exportSelection);
    return collectionExport.items.filter((item) => selected.has(item.name));
  }, [collectionExport, exportSelection]);
  const exportPreview = useMemo(
    () =>
      JSON.stringify(
        {
          project: collectionExport?.project ?? selectedProject,
          exportedAt: collectionExport?.exportedAt ?? new Date().toISOString(),
          items: selectedExportItems,
        },
        null,
        2,
      ),
    [collectionExport, selectedExportItems, selectedProject],
  );

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

  useEffect(() => {
    recordQueryRef.current = { search: recordSearch, filter: recordFilter, perPage: recordPerPage };
  }, [recordFilter, recordPerPage, recordSearch]);

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
        const nextCollectionModel = collectionResponse.items.find((collection) => collection.name === nextCollection);
        const activeSearch = nextCollectionModel?.fields.some((field) => field.searchable && canSearchField(field)) ? recordSearch : "";
        const recordsResponse = await listRecords(authToken, projectSlug, nextCollection, {
          page: 1,
          perPage: recordPerPage,
          filter: recordFilter,
          search: activeSearch,
        });
        setRecords(recordsResponse);
      } else {
        setRecords({ items: [], page: 1, perPage: recordPerPage, totalItems: 0 });
      }
    },
    [recordFilter, recordPerPage, recordSearch, selectedCollection],
  );

  const refreshAll = useCallback(
    async (authToken = token, preferredProject = selectedProject) => {
      if (!authToken) return;
      setBusy(true);
      try {
        const [healthResponse, projectsResponse, settingsResponse, cronResponse, backupResponse, mcpResponse] = await Promise.all([
          health(),
          listProjects(authToken),
          getSettings(authToken),
          listCronJobs(authToken),
          listBackupJobs(authToken),
          listMCPTokens(authToken),
        ]);
        setHealthState(healthResponse);
        setProjects(projectsResponse.items);
        setSettingsState(settingsResponse);
        setCronJobs(cronResponse.items);
        setBackupJobs(backupResponse.items);
        setMCPTokens(mcpResponse.items);
        setSMTPDraft(settingsToSMTPDraft(settingsResponse));
        setStorageDraft(settingsToStorageDraft(settingsResponse));
        const projectSlug = preferredProject || projectsResponse.items[0]?.slug || "";
        setSelectedProject(projectSlug);
        if (projectSlug) {
          await loadProjectData(authToken, projectSlug);
        } else {
          setCollections([]);
          setSelectedCollection("");
          setRecords({ items: [], page: 1, perPage: recordPerPage, totalItems: 0 });
        }
      } catch (error) {
        handleError(error);
      } finally {
        setBusy(false);
      }
    },
    [handleError, loadProjectData, recordPerPage, selectedProject, token],
  );

  useEffect(() => {
    if (bootstrapped.current) return;
    bootstrapped.current = true;
    setSQLHistory(loadSQLHistory());
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
        if (response.admin.mustChangePassword) {
          health().then(setHealthState).catch(() => undefined);
          return undefined;
        }
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
    const { filter, search, perPage } = recordQueryRef.current;
    const activeSearch = selectedCollectionModel?.fields.some((field) => field.searchable && canSearchField(field)) ? search : "";
    listRecords(token, selectedProject, selectedCollection, { page: 1, perPage, filter, search: activeSearch })
      .then((response) => {
        if (!cancelled) setRecords(response);
      })
      .catch((error) => {
        if (!cancelled) handleError(error);
      });
    return () => {
      cancelled = true;
    };
  }, [handleError, selectedCollection, selectedCollectionModel, selectedProject, token]);

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
    setEditingIcon(collectionIconFromOptions(selectedCollectionModel));
    setEditingManaged(collectionManagedByDublyobase(selectedCollectionModel));
    if (!selectedCollectionModel.fields.some((field) => field.searchable && canSearchField(field))) {
      setRecordSearch("");
    }
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
      if (response.admin.mustChangePassword) {
        showNotice("success", "Change the bootstrap password to continue.");
        return;
      }
      await refreshAll(response.token);
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
    setCronJobs([]);
    setBackupJobs([]);
    setMCPTokens([]);
    setSelectedUploadFile(null);
    setOneTimeMCPToken(null);
    setAccountOpen(false);
  }

  async function submitPasswordChange(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    const data = new FormData(event.currentTarget);
    const currentPassword = String(data.get("currentPassword") ?? "");
    const newPassword = String(data.get("newPassword") ?? "");
    const confirmPassword = String(data.get("confirmPassword") ?? "");
    if (newPassword !== confirmPassword) {
      showNotice("error", "New passwords do not match");
      return;
    }
    setBusy(true);
    try {
      const wasForced = admin?.mustChangePassword;
      const response = await changeAdminPassword(token, currentPassword, newPassword);
      setAdmin(response.admin);
      setAccountOpen(false);
      showNotice("success", wasForced ? "Password changed. Admin access unlocked." : "Password changed.");
      await refreshAll(token);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
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
        options: collectionOptionsWithIcon(undefined, collectionDraft.icon),
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
      const imported = collectionImportedFromOptions(selectedCollectionModel);
      const options = collectionOptionsWithIcon(selectedCollectionModel.options, editingIcon);
      if (imported) {
        options.managed = editingManaged;
      }
      const payload: Record<string, unknown> = {
        ...editingRules,
        options,
      };
      if (!imported || editingManaged) {
        payload.fields = editingFields.map(cleanField);
      }
      const updated = await updateCollection(token, selectedProject, selectedCollectionModel.name, payload);
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

  function addDraftField(type: FieldType = "text") {
    setCollectionDraft((draft) => ({ ...draft, fields: [...draft.fields, newDefaultField(type)] }));
  }

  function addEditingField(type: FieldType = "text") {
    setEditingFields((fields) => [...fields, newDefaultField(type)]);
  }

  async function refreshRecords(page = records.page, perPage = recordPerPage) {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const response = await listRecords(token, selectedProject, selectedCollectionModel.name, {
        page,
        perPage,
        filter: recordFilter,
        search: selectedCollectionModel.fields.some((field) => field.searchable && canSearchField(field)) ? recordSearch : "",
      });
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
    if (!token || !selectedProject || !selectedCollectionModel) return;
    const recordId = recordPrimaryKeyValue(selectedCollectionModel, record);
    if (!recordId) return;
    if (!window.confirm(`Delete record ${recordId}?`)) return;
    setBusy(true);
    try {
      await deleteRecord(token, selectedProject, selectedCollectionModel.name, recordId);
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
    const file = selectedUploadFile ?? input?.files?.[0];
    if (!file) {
      showNotice("error", "Choose a file first");
      return;
    }
    setBusy(true);
    try {
      const result = await uploadFile(token, selectedProject, selectedCollectionModel.name, fileDraft.recordId, fileDraft.field, file);
      setFileResult(result);
      setSelectedUploadFile(null);
      if (input) input.value = "";
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

  async function submitCronJob(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    let headers: Record<string, string>;
    try {
      headers = parseHeadersJSON(cronDraft.headersJSON);
    } catch (error) {
      handleError(error);
      return;
    }
    setBusy(true);
    try {
      const projectSlug = cronDraft.projectSlug || selectedProject;
      const created = await createCronJob(token, {
        projectSlug,
        name: cronDraft.name,
        type: "http",
        schedule: cronDraft.schedule,
        timezone: cronDraft.timezone,
        enabled: cronDraft.enabled,
        timeoutSeconds: Number.parseInt(cronDraft.timeoutSeconds, 10) || 30,
        retryCount: Number.parseInt(cronDraft.retryCount, 10) || 0,
        method: cronDraft.method,
        url: cronDraft.url,
        headers,
        body: cronDraft.body,
      });
      setCronDraft({ ...emptyCronDraft, projectSlug });
      showNotice("success", `Cron ${created.name} created`);
      const response = await listCronJobs(token);
      setCronJobs(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function runCron(job: CronJob) {
    if (!token) return;
    setBusy(true);
    try {
      const run = await runCronJob(token, job.id);
      setCronRuns((current) => ({ ...current, [job.id]: [run, ...(current[job.id] ?? [])].slice(0, 5) }));
      const jobs = await listCronJobs(token);
      setCronJobs(jobs.items);
      showNotice(run.status === "success" ? "success" : "error", `Cron ${job.name} finished with ${run.status}`);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function loadCronRuns(job: CronJob) {
    if (!token) return;
    try {
      const response = await listCronRuns(token, job.id);
      setCronRuns((current) => ({ ...current, [job.id]: response.items }));
    } catch (error) {
      handleError(error);
    }
  }

  async function submitBackupJob(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const projectSlug = backupDraft.scope === "project" ? backupDraft.projectSlug || selectedProject : undefined;
      const created = await createBackupJob(token, {
        name: backupDraft.name,
        scope: backupDraft.scope,
        projectSlug,
        schedule: backupDraft.schedule,
        timezone: backupDraft.timezone,
        enabled: backupDraft.enabled,
        retentionDays: Number.parseInt(backupDraft.retentionDays, 10) || 14,
        retentionCount: Number.parseInt(backupDraft.retentionCount, 10) || 10,
      });
      setBackupDraft({ ...emptyBackupDraft, projectSlug: projectSlug ?? "" });
      showNotice("success", `Backup ${created.name} created`);
      const response = await listBackupJobs(token);
      setBackupJobs(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function runBackup(job: BackupJob) {
    if (!token) return;
    setBusy(true);
    try {
      const run = await runBackupJob(token, job.id);
      setBackupRuns((current) => ({ ...current, [job.id]: [run, ...(current[job.id] ?? [])].slice(0, 5) }));
      const jobs = await listBackupJobs(token);
      setBackupJobs(jobs.items);
      showNotice(run.status === "success" ? "success" : "error", `Backup ${job.name} finished with ${run.status}`);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function loadBackupRuns(job: BackupJob) {
    if (!token) return;
    try {
      const response = await listBackupRuns(token, job.id);
      setBackupRuns((current) => ({ ...current, [job.id]: response.items }));
    } catch (error) {
      handleError(error);
    }
  }

  async function submitMCPToken(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const projectSlug = mcpDraft.scope === "project" ? mcpDraft.projectSlug || selectedProject : undefined;
      const created = await createMCPToken(token, {
        name: mcpDraft.name,
        scope: mcpDraft.scope,
        projectSlug,
        allowedTools: mcpDraft.allowedTools
          .split(/[\n,]/)
          .map((item) => item.trim())
          .filter(Boolean),
        ...(mcpDraft.expiresAt ? { expiresAt: new Date(mcpDraft.expiresAt).toISOString() } : {}),
      });
      setOneTimeMCPToken(created);
      setMCPDraft({ ...emptyMCPDraft, projectSlug: projectSlug ?? "" });
      showNotice("success", "MCP token created");
      const response = await listMCPTokens(token);
      setMCPTokens(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function revokeMCP(item: MCPToken) {
    if (!token) return;
    if (!window.confirm(`Revoke MCP token "${item.name}"?`)) return;
    setBusy(true);
    try {
      await revokeMCPToken(token, item.id);
      showNotice("success", "MCP token revoked");
      const response = await listMCPTokens(token);
      setMCPTokens(response.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function loadCollectionExport() {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await exportCollections(token, selectedProject);
      setCollectionExport(response);
      setExportSelection(response.items.map((item) => item.name));
      showNotice("success", "Collection schema loaded");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  function toggleExportCollection(name: string) {
    setExportSelection((current) => (current.includes(name) ? current.filter((item) => item !== name) : [...current, name]));
  }

  function downloadCollectionExport() {
    const blob = new Blob([exportPreview], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${selectedProject || "dublyobase"}-collections.json`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  function useExportForImport() {
    setImportJSON(exportPreview);
    changeSettings("importCollections");
  }

  async function submitCollectionImport(dryRun: boolean) {
    if (!token || !selectedProject) return;
    let items: unknown[];
    try {
      items = extractImportItems(importJSON);
    } catch (error) {
      handleError(error);
      return;
    }
    setBusy(true);
    try {
      const response = await importCollections(token, selectedProject, {
        items,
        mode: importMode,
        dryRun,
        dropMissingFields: importDropMissingFields,
      });
      setImportResult(response);
      showNotice("success", dryRun ? "Import preview ready" : "Collections imported");
      if (!dryRun) {
        await loadProjectData(token, selectedProject);
      }
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function scanSchemaTables() {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await discoverSchema(token, selectedProject, schemaFilters);
      setDiscoveredTables(response.items);
      setSchemaSelection(response.items.filter((item) => item.canImport && !item.existingCollection).map(discoveredTableKey));
      setSchemaImportNames(Object.fromEntries(response.items.map((item) => [discoveredTableKey(item), item.suggestedName])));
      setSchemaImportResult(null);
      showNotice("success", `Found ${response.items.length} tables`);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  function toggleSchemaTable(table: DiscoveredTable) {
    const key = discoveredTableKey(table);
    setSchemaSelection((current) => (current.includes(key) ? current.filter((item) => item !== key) : [...current, key]));
  }

  function setSchemaImportName(table: DiscoveredTable, name: string) {
    setSchemaImportNames((current) => ({ ...current, [discoveredTableKey(table)]: name }));
  }

  async function submitSchemaImport(dryRun: boolean) {
    if (!token || !selectedProject) return;
    const items = discoveredTables.filter((table) => schemaSelection.includes(discoveredTableKey(table))).map<SchemaImportItem>((table) => ({
      schema: table.schema,
      table: table.table,
      name: schemaImportNames[discoveredTableKey(table)] || table.suggestedName,
    }));
    if (items.length === 0) {
      showNotice("error", "Choose at least one CRUD-ready table");
      return;
    }
    setBusy(true);
    try {
      const response = await importSchemaTables(token, selectedProject, { items, dryRun });
      setSchemaImportResult(response);
      showNotice("success", dryRun ? "Schema import preview ready" : "Tables imported as collections");
      if (!dryRun) {
        await loadProjectData(token, selectedProject);
        await scanSchemaTables();
      }
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function executeSQL(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject) return;
    const query = sqlQuery.trim();
    if (!query) return;
    if (isDangerousSQL(query) && !window.confirm("This query may change data or schema. Execute it?")) {
      return;
    }
    setBusy(true);
    try {
      const response = await runSQL(token, selectedProject, {
        query,
        maxRows: Number.parseInt(sqlMaxRows, 10) || 250,
      });
      setSQLResult(response);
      const history = saveSQLHistory(query, sqlHistory);
      setSQLHistory(history);
      showNotice("success", response.columns.length > 0 ? `${response.rows.length} rows returned` : response.command || "SQL executed");
      if (!response.readOnly) {
        await loadProjectData(token, selectedProject);
      }
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
    return <AuthScreen busy={busy} healthState={healthState} notice={notice} onLogin={submitLogin} />;
  }

  if (admin.mustChangePassword) {
    return <PasswordChangeScreen admin={admin} busy={busy} healthState={healthState} notice={notice} onSubmit={submitPasswordChange} onLogout={signOut} />;
  }

  return (
    <main className="pb-app">
      <header className="pb-app-header accent-surface">
        <button type="button" className="pb-logo" onClick={() => changeView("collections")} aria-label="Open collections">
          <img className="pb-brand-mark" src="/icon.png" alt="" aria-hidden="true" />
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
        <button type="button" onClick={() => setAccountOpen(true)} className="pb-header-link logged-user" title="Account settings">
          <span className="truncate">{admin.email}</span>
          <ChevronDown className="h-4 w-4" />
        </button>
      </header>

      {accountOpen ? <AccountModal admin={admin} busy={busy} onSubmit={submitPasswordChange} onClose={() => setAccountOpen(false)} onLogout={signOut} /> : null}

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
          recordSearch={recordSearch}
          setRecordSearch={setRecordSearch}
          recordFilter={recordFilter}
          setRecordFilter={setRecordFilter}
          recordPerPage={recordPerPage}
          onSelectCollection={setSelectedCollection}
          onRefresh={() => refreshRecords(1)}
          onPageChange={(page) => {
            void refreshRecords(page);
          }}
          onPageSizeChange={(pageSize) => {
            setRecordPerPage(pageSize);
            void refreshRecords(1, pageSize);
          }}
          onOpenCreateCollection={() => setCollectionModal("create")}
          onOpenCollectionSettings={() => selectedCollectionModel && setCollectionModal("settings")}
          onOpenAPIPreview={() => selectedCollectionModel && setAPIPreviewOpen(true)}
          onOpenNewRecord={() => {
            setSelectedRecordId("");
            setRecordJSON("{}");
            setRecordEditorOpen(true);
          }}
          onEditRecord={(record) => {
            setSelectedRecordId(selectedCollectionModel ? recordPrimaryKeyValue(selectedCollectionModel, record) : "");
            setRecordJSON(JSON.stringify(stripSystemFields(record, selectedCollectionModel), null, 2));
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
          onOpenAuth={() => changeSettings("auth")}
          onOpenMail={() => changeSettings("mail")}
          onOpenFiles={() => changeSettings("files")}
          onOpenMCP={() => changeSettings("mcp")}
          smtpDraft={smtpDraft}
          setSMTPDraft={setSMTPDraft}
          storageDraft={storageDraft}
          setStorageDraft={setStorageDraft}
          onSaveSMTP={saveSMTPSettings}
          onTestSMTP={sendSMTPTest}
          onSaveStorage={saveStorageSettings}
          onTestStorage={runStorageTest}
          cronJobs={cronJobs}
          cronRuns={cronRuns}
          cronDraft={cronDraft}
          setCronDraft={setCronDraft}
          onCreateCron={submitCronJob}
          onRunCron={runCron}
          onLoadCronRuns={loadCronRuns}
          backupJobs={backupJobs}
          backupRuns={backupRuns}
          backupDraft={backupDraft}
          setBackupDraft={setBackupDraft}
          onCreateBackup={submitBackupJob}
          onRunBackup={runBackup}
          onLoadBackupRuns={loadBackupRuns}
          mcpTokens={mcpTokens}
          oneTimeMCPToken={oneTimeMCPToken}
          mcpDraft={mcpDraft}
          setMCPDraft={setMCPDraft}
          onCreateMCPToken={submitMCPToken}
          onRevokeMCPToken={revokeMCP}
          onDismissMCPSecret={() => setOneTimeMCPToken(null)}
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
          selectedUploadFile={selectedUploadFile}
          setSelectedUploadFile={setSelectedUploadFile}
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
          collectionExport={collectionExport}
          exportSelection={exportSelection}
          exportPreview={exportPreview}
          selectedExportItems={selectedExportItems}
          onLoadCollectionExport={loadCollectionExport}
          onToggleExportCollection={toggleExportCollection}
          onCopyExport={() => copyText(exportPreview)}
          onDownloadExport={downloadCollectionExport}
          onUseExportForImport={useExportForImport}
          importJSON={importJSON}
          setImportJSON={setImportJSON}
          importMode={importMode}
          setImportMode={setImportMode}
          importDropMissingFields={importDropMissingFields}
          setImportDropMissingFields={setImportDropMissingFields}
          importResult={importResult}
          onPreviewImport={() => submitCollectionImport(true)}
          onApplyImport={() => submitCollectionImport(false)}
          discoveredTables={discoveredTables}
          schemaSelection={schemaSelection}
          schemaFilters={schemaFilters}
          setSchemaFilters={setSchemaFilters}
          setSchemaSelection={setSchemaSelection}
          schemaImportNames={schemaImportNames}
          schemaImportResult={schemaImportResult}
          onScanSchema={scanSchemaTables}
          onToggleSchemaTable={toggleSchemaTable}
          onSetSchemaImportName={setSchemaImportName}
          onPreviewSchemaImport={() => submitSchemaImport(true)}
          onApplySchemaImport={() => submitSchemaImport(false)}
          sqlQuery={sqlQuery}
          setSQLQuery={setSQLQuery}
          sqlMaxRows={sqlMaxRows}
          setSQLMaxRows={setSQLMaxRows}
          sqlResult={sqlResult}
          sqlHistory={sqlHistory}
          onExecuteSQL={executeSQL}
          onCopySQLCSV={() => {
            if (sqlResult) void copyText(sqlResultToCSV(sqlResult));
          }}
        />
      ) : null}

      {collectionModal === "create" ? (
        <CollectionModal
          mode="create"
          collections={collections}
          draft={collectionDraft}
          setDraft={setCollectionDraft}
          icon={collectionDraft.icon}
          setIcon={(icon) => setCollectionDraft((next) => ({ ...next, icon }))}
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
          icon={editingIcon}
          setIcon={setEditingIcon}
          managed={editingManaged}
          setManaged={setEditingManaged}
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
}: {
  busy: boolean;
  healthState: Health | null;
  notice: Notice;
  onLogin: (event: React.FormEvent<HTMLFormElement>) => void;
}) {
  const [showPassword, setShowPassword] = useState(false);
  return (
    <main className="pb-login-screen">
      <section className="pb-login-card" aria-labelledby="login-title">
        <div className="pb-login-logo">
          <img className="pb-login-mark" src="/icon.png" alt="" aria-hidden="true" />
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
        <div className="pb-login-status">
          <span>DB {healthState?.db ?? "checking"}</span>
          <span>Storage {healthState?.storage ?? "checking"}</span>
          <span>{healthState?.version ?? "unknown"}</span>
        </div>
      </section>
    </main>
  );
}

function PasswordChangeScreen({
  admin,
  busy,
  healthState,
  notice,
  onSubmit,
  onLogout,
}: {
  admin: Admin;
  busy: boolean;
  healthState: Health | null;
  notice: Notice;
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onLogout: () => void;
}) {
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  return (
    <main className="pb-login-screen">
      <section className="pb-login-card" aria-labelledby="password-change-title">
        <div className="pb-login-logo">
          <img className="pb-login-mark" src="/icon.png" alt="" aria-hidden="true" />
        </div>
        <h1 id="password-change-title">Change admin password</h1>
        <p className="pb-muted-copy">Signed in as {admin.email}. Set a new password before opening the control panel.</p>
        {notice ? (
          <div className={`pb-inline-alert ${notice.type === "error" ? "danger" : "success"}`}>
            {notice.message}
          </div>
        ) : null}
        <form onSubmit={onSubmit} className="pb-form-stack">
          <label className="pb-field password-field">
            <span>Current password</span>
            <input name="currentPassword" type={showCurrent ? "text" : "password"} autoComplete="current-password" required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowCurrent((value) => !value)} aria-label={showCurrent ? "Hide current password" : "Show current password"}>
              {showCurrent ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field password-field">
            <span>New password</span>
            <input name="newPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowNew((value) => !value)} aria-label={showNew ? "Hide new password" : "Show new password"}>
              {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field">
            <span>Confirm new password</span>
            <input name="confirmPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
          </label>
          <button type="submit" disabled={busy} className="pb-btn primary lg block">
            {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            Save password
          </button>
          <button type="button" onClick={onLogout} disabled={busy} className="pb-btn secondary block">
            <LogOut className="h-4 w-4" />
            Log out
          </button>
        </form>
        <div className="pb-login-status">
          <span>DB {healthState?.db ?? "checking"}</span>
          <span>Storage {healthState?.storage ?? "checking"}</span>
          <span>{healthState?.version ?? "unknown"}</span>
        </div>
      </section>
    </main>
  );
}

function AccountModal({
  admin,
  busy,
  onSubmit,
  onClose,
  onLogout,
}: {
  admin: Admin;
  busy: boolean;
  onSubmit: (event: React.FormEvent<HTMLFormElement>) => void;
  onClose: () => void;
  onLogout: () => void;
}) {
  const [showCurrent, setShowCurrent] = useState(false);
  const [showNew, setShowNew] = useState(false);
  return (
    <div className="pb-modal-layer" role="presentation">
      <section className="pb-modal account-modal" role="dialog" aria-modal="true" aria-labelledby="account-title">
        <header className="pb-modal-header">
          <h2 id="account-title">Account</h2>
          <button type="button" className="pb-icon-btn" onClick={onClose} aria-label="Close account settings">
            <X className="h-4 w-4" />
          </button>
        </header>
        <form onSubmit={onSubmit} className="pb-modal-content account-form">
          <div className="pb-info-grid compact">
            <Info label="Signed in as" value={admin.email} />
          </div>
          <h3>Change password</h3>
          <label className="pb-field password-field">
            <span>Current password</span>
            <input name="currentPassword" type={showCurrent ? "text" : "password"} autoComplete="current-password" required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowCurrent((value) => !value)} aria-label={showCurrent ? "Hide current password" : "Show current password"}>
              {showCurrent ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field password-field">
            <span>New password</span>
            <input name="newPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
            <button type="button" className="pb-icon-btn password-toggle" onClick={() => setShowNew((value) => !value)} aria-label={showNew ? "Hide new password" : "Show new password"}>
              {showNew ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </button>
          </label>
          <label className="pb-field">
            <span>Confirm new password</span>
            <input name="confirmPassword" type={showNew ? "text" : "password"} autoComplete="new-password" minLength={12} required />
          </label>
          <div className="account-actions">
            <button type="button" onClick={onLogout} disabled={busy} className="pb-btn secondary">
              <LogOut className="h-4 w-4" />
              Log out
            </button>
            <button type="submit" disabled={busy} className="pb-btn primary">
              {busy ? <RefreshCw className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              Save password
            </button>
          </div>
        </form>
      </section>
    </div>
  );
}

function CollectionsWorkspace({
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
  onOpenNewRecord: () => void;
  onEditRecord: (record: RecordItem) => void;
  onDeleteRecord: (record: RecordItem) => void;
  onDeleteCollection: (collection: Collection) => void;
  version: string;
}) {
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

        <PageFooter left={selectedCollection ? `Showing ${records.items.length} of ${records.totalItems}` : "No collection selected"} version={version} />
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
          <CollectionIcon collection={collection} />
          <span className="txt">{collection.name}</span>
          {collection.type === "auth" ? <ShieldCheck className="h-3.5 w-3.5 hint" /> : null}
        </button>
      ))}
    </details>
  );
}

function CollectionIcon({ collection, icon, type }: { collection?: Collection; icon?: CollectionIconOption; type?: Collection["type"] }) {
  const resolvedType = type ?? collection?.type ?? "base";
  const resolved = icon ?? (collection ? collectionIconFromOptions(collection) : defaultCollectionIcon(resolvedType));
  if (resolved.type === "emoji") {
    return (
      <span className="pb-collection-emoji" aria-hidden="true">
        {resolved.value || "📁"}
      </span>
    );
  }
  const fallback = defaultCollectionIcon(resolvedType);
  const Icon = collectionIconMap[resolved.name] ?? (fallback.type === "lucide" ? collectionIconMap[fallback.name] : undefined) ?? Table2;
  return <Icon className="h-4 w-4" />;
}

function CollectionIconPicker({ icon, onChange }: { icon: CollectionIconOption; onChange: (icon: CollectionIconOption) => void }) {
  const currentName = icon.type === "lucide" ? icon.name : "table";
  const currentEmoji = icon.type === "emoji" ? icon.value : "";
  return (
    <fieldset className="pb-icon-picker">
      <legend>Icon</legend>
      <div className="pb-icon-picker-head">
        <div className="pb-icon-preview" aria-hidden="true">
          <CollectionIcon icon={icon} />
        </div>
        <div className="pb-segmented-control" role="radiogroup" aria-label="Collection icon type">
          <button type="button" role="radio" aria-checked={icon.type === "lucide"} className={icon.type === "lucide" ? "active" : ""} onClick={() => onChange({ type: "lucide", name: currentName })}>
            Lucide
          </button>
          <button type="button" role="radio" aria-checked={icon.type === "emoji"} className={icon.type === "emoji" ? "active" : ""} onClick={() => onChange({ type: "emoji", value: currentEmoji || "◆" })}>
            Emoji
          </button>
        </div>
      </div>
      {icon.type === "lucide" ? (
        <div className="pb-icon-grid" role="list" aria-label="Lucide collection icons">
          {collectionIconChoices.map((choice) => {
            const Icon = choice.icon;
            const selected = currentName === choice.name;
            return (
              <button key={choice.name} type="button" className={selected ? "active" : ""} aria-pressed={selected} title={choice.label} onClick={() => onChange({ type: "lucide", name: choice.name })}>
                <Icon className="h-4 w-4" />
                <span>{choice.label}</span>
              </button>
            );
          })}
        </div>
      ) : (
        <label className="pb-field emoji-field">
          <span>Emoji</span>
          <input value={currentEmoji} maxLength={8} onChange={(event) => onChange({ type: "emoji", value: sanitizeCollectionEmoji(event.target.value) })} placeholder="◆" />
        </label>
      )}
    </fieldset>
  );
}

function CollectionModal({
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
  const [tab, setTab] = useState<"fields" | "rules">("fields");
  const title = mode === "create" ? "Create collection" : "Collection settings";
  const imported = collection ? collectionImportedFromOptions(collection) : false;
  const manageReady = collection ? collectionStandardSystemColumns(collection) : false;
  const schemaLocked = imported && !managed;
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
            <button type="button" role="tab" aria-selected={tab === "rules"} className={`pb-tab-item ${tab === "rules" ? "active" : ""}`} onClick={() => setTab("rules")}>
              API rules
            </button>
          </div>

          {tab === "fields" ? (
            <>
              {schemaLocked ? <div className="pb-inline-alert warning">This imported table is staged for CRUD. Enable managed takeover before editing columns or field definitions.</div> : null}
              <FieldRows fields={fields} collections={collections} onChange={setFields} onAdd={onAddField} readOnly={schemaLocked} />
            </>
          ) : null}
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
        const open = openRows[rowKey] ?? field.name === "";
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

function FieldOptionsEditor({ field, collections, onChange, readOnly }: { field: Field; collections: Collection[]; onChange: (field: Field) => void; readOnly?: boolean }) {
  const searchSupported = canSearchField(field);
  const relationTarget = field.type === "relation" ? collections.find((collection) => collection.name === field.options?.collection) : undefined;
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
        <label className="pb-field">
          <span>Display field</span>
          <select value={String(field.options?.displayField ?? "")} onChange={(event) => onChange(setFieldOption(field, "displayField", event.target.value))} disabled={!relationTarget}>
            <option value="">Auto</option>
            {relationTarget?.fields.filter((item) => !item.hidden && item.type !== "password").map((item) => (
              <option key={item.name} value={item.name}>
                {item.name}
              </option>
            ))}
          </select>
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
          <label className="pb-checkline">
            <input type="checkbox" checked={Boolean(field.options?.cascadeDelete)} onChange={(event) => onChange(setFieldOption(field, "cascadeDelete", event.target.checked))} />
            Cascade delete
          </label>
        </div>
        <label className="pb-field">
          <span>Reverse field name</span>
          <input value={String(field.options?.reverseName ?? "")} onChange={(event) => onChange(setFieldOption(field, "reverseName", event.target.value))} placeholder={`${field.name || "field"}_records`} />
        </label>
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
  const parsed = parseRecordDraft(recordJSON);
  const draft = parsed.ok ? parsed.value : {};
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
                <RecordFieldInput key={field.name} field={field} value={draft[field.name]} editing={Boolean(selectedRecordId)} onChange={(value) => updateDraft(field, value)} />
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

function RecordFieldInput({ field, value, editing, onChange }: { field: Field; value: unknown; editing: boolean; onChange: (value: unknown) => void }) {
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
    if (multiple) {
      return (
        <label className="pb-field record-field full">
          <span>{label}</span>
          <textarea value={Array.isArray(value) ? value.map(String).join("\n") : ""} onChange={(event) => onChange(splitOptionValues(event.target.value))} rows={3} placeholder="One record id per line" />
        </label>
      );
    }
    return (
      <label className="pb-field record-field">
        <span>{label}</span>
        <input value={typeof value === "string" ? value : ""} onChange={(event) => onChange(event.target.value || undefined)} placeholder="Record id" />
      </label>
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

function ManagedRecordField({ field, note }: { field: Field; note: string }) {
  return (
    <div className="record-managed-field">
      <span>{field.name}</span>
      <em>{note}</em>
    </div>
  );
}

function RichTextEditor({ value, onChange }: { value: string; onChange: (value: string) => void }) {
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

function JSONFieldEditor({ value, onChange }: { value: unknown; onChange: (value: unknown) => void }) {
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

function APIPreviewModal({ project, collection, onClose, onCopy }: { project: string; collection: Collection; onClose: () => void; onCopy: (text: string) => void }) {
  const basePath = `/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection.name)}`;
  const searchField = collection.fields.find((field) => field.searchable && canSearchField(field))?.name ?? "title";
  const filterSample = encodeURIComponent(JSON.stringify({ [searchField]: { _icontains: "hello" } }));
  const sample = `GET ${basePath}/records?page=1&perPage=25&search=hello
GET ${basePath}/records?filter=${filterSample}
GET ${basePath}/records?filter[${searchField}][_icontains]=hello
POST ${basePath}/records
PATCH ${basePath}/records/{id}
DELETE ${basePath}/records/{id}`;
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
  onOpenAuth: () => void;
  onOpenMail: () => void;
  onOpenFiles: () => void;
  onOpenMCP: () => void;
  smtpDraft: typeof emptySMTPDraft;
  setSMTPDraft: React.Dispatch<React.SetStateAction<typeof emptySMTPDraft>>;
  storageDraft: typeof emptyStorageDraft;
  setStorageDraft: React.Dispatch<React.SetStateAction<typeof emptyStorageDraft>>;
  onSaveSMTP: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestSMTP: () => void;
  onSaveStorage: (event: React.FormEvent<HTMLFormElement>) => void;
  onTestStorage: () => void;
  cronJobs: CronJob[];
  cronRuns: Record<string, CronRun[]>;
  cronDraft: typeof emptyCronDraft;
  setCronDraft: React.Dispatch<React.SetStateAction<typeof emptyCronDraft>>;
  onCreateCron: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunCron: (job: CronJob) => void;
  onLoadCronRuns: (job: CronJob) => void;
  backupJobs: BackupJob[];
  backupRuns: Record<string, BackupRun[]>;
  backupDraft: typeof emptyBackupDraft;
  setBackupDraft: React.Dispatch<React.SetStateAction<typeof emptyBackupDraft>>;
  onCreateBackup: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunBackup: (job: BackupJob) => void;
  onLoadBackupRuns: (job: BackupJob) => void;
  mcpTokens: MCPToken[];
  oneTimeMCPToken: MCPToken | null;
  mcpDraft: typeof emptyMCPDraft;
  setMCPDraft: React.Dispatch<React.SetStateAction<typeof emptyMCPDraft>>;
  onCreateMCPToken: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeMCPToken: (token: MCPToken) => void;
  onDismissMCPSecret: () => void;
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
  selectedUploadFile: File | null;
  setSelectedUploadFile: React.Dispatch<React.SetStateAction<File | null>>;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  projectSlug: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
  collectionExport: CollectionExport | null;
  exportSelection: string[];
  exportPreview: string;
  selectedExportItems: CollectionExport["items"];
  onLoadCollectionExport: () => void;
  onToggleExportCollection: (name: string) => void;
  onCopyExport: () => void;
  onDownloadExport: () => void;
  onUseExportForImport: () => void;
  importJSON: string;
  setImportJSON: React.Dispatch<React.SetStateAction<string>>;
  importMode: "create_missing" | "upsert";
  setImportMode: React.Dispatch<React.SetStateAction<"create_missing" | "upsert">>;
  importDropMissingFields: boolean;
  setImportDropMissingFields: React.Dispatch<React.SetStateAction<boolean>>;
  importResult: CollectionImportResult | null;
  onPreviewImport: () => void;
  onApplyImport: () => void;
  discoveredTables: DiscoveredTable[];
  schemaSelection: string[];
  schemaFilters: { schema: string; table: string };
  setSchemaFilters: React.Dispatch<React.SetStateAction<{ schema: string; table: string }>>;
  setSchemaSelection: React.Dispatch<React.SetStateAction<string[]>>;
  schemaImportNames: Record<string, string>;
  schemaImportResult: CollectionImportResult | null;
  onScanSchema: () => void;
  onToggleSchemaTable: (table: DiscoveredTable) => void;
  onSetSchemaImportName: (table: DiscoveredTable, name: string) => void;
  onPreviewSchemaImport: () => void;
  onApplySchemaImport: () => void;
  sqlQuery: string;
  setSQLQuery: React.Dispatch<React.SetStateAction<string>>;
  sqlMaxRows: string;
  setSQLMaxRows: React.Dispatch<React.SetStateAction<string>>;
  sqlResult: SQLResult | null;
  sqlHistory: string[];
  onExecuteSQL: (event: React.FormEvent<HTMLFormElement>) => void;
  onCopySQLCSV: () => void;
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
          {props.section === "auth" ? <AuthSettingsPanel {...props} /> : null}
          {props.section === "mail" ? <MailSettings {...props} /> : null}
          {props.section === "storage" ? <StorageSettingsPanel {...props} /> : null}
          {props.section === "backups" ? <BackupsView {...props} onOpenExport={() => props.onChangeSection("exportCollections")} /> : null}
          {props.section === "crons" ? <CronsView {...props} /> : null}
          {props.section === "mcp" ? <MCPAccessView {...props} /> : null}
          {props.section === "exportCollections" ? <ExportCollectionsView {...props} /> : null}
          {props.section === "importCollections" ? <ImportCollectionsView {...props} /> : null}
          {props.section === "discoverTables" ? <DiscoverTablesView {...props} /> : null}
          {props.section === "sqlConsole" ? <SQLConsoleView {...props} /> : null}
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
  if (id === "auth") return <ShieldCheck className="h-4 w-4" />;
  if (id === "mail") return <Mail className="h-4 w-4" />;
  if (id === "storage") return <HardDrive className="h-4 w-4" />;
  if (id === "backups") return <Archive className="h-4 w-4" />;
  if (id === "crons") return <Activity className="h-4 w-4" />;
  if (id === "mcp") return <KeyRound className="h-4 w-4" />;
  if (id === "exportCollections") return <FileUp className="h-4 w-4" />;
  if (id === "importCollections") return <UploadCloud className="h-4 w-4" />;
  if (id === "discoverTables") return <Database className="h-4 w-4" />;
  if (id === "sqlConsole") return <Code2 className="h-4 w-4" />;
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
  onOpenAuth,
  onOpenMail,
  onOpenFiles,
  onOpenMCP,
}: {
  project: Project | null;
  projects: Project[];
  projectDraft: { slug: string; name: string };
  setProjectDraft: (draft: { slug: string; name: string }) => void;
  onSubmitProject: (event: React.FormEvent<HTMLFormElement>) => void;
  healthState: Health | null;
  appUrl: string;
  onOpenAuth: () => void;
  onOpenMail: () => void;
  onOpenFiles: () => void;
  onOpenMCP: () => void;
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
        <h2>Core services</h2>
        <div className="pb-service-grid">
          <button type="button" className="pb-service-tile" onClick={onOpenAuth}>
            <ShieldCheck className="h-5 w-5" />
            <span>
              <strong>Auth</strong>
              <em>Email/password users, tokens, verification, reset</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenMail}>
            <Mail className="h-5 w-5" />
            <span>
              <strong>Email</strong>
              <em>SMTP delivery for auth and test messages</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenFiles}>
            <UploadCloud className="h-5 w-5" />
            <span>
              <strong>Files</strong>
              <em>Upload, protected tokens, thumbnails</em>
            </span>
          </button>
          <button type="button" className="pb-service-tile" onClick={onOpenMCP}>
            <KeyRound className="h-5 w-5" />
            <span>
              <strong>MCP</strong>
              <em>Scoped AI tool access to the live backend</em>
            </span>
          </button>
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

function AuthSettingsPanel({
  project,
  appUrl,
  settings,
  onOpenMail,
}: {
  project: Project | null;
  appUrl: string;
  settings: InstanceSettings | null;
  onOpenMail: () => void;
}) {
  const projectSlug = project?.slug || "{project}";
  const base = `${appUrl}/api/projects/${projectSlug}`;
  const routes = [
    ["POST", `${base}/auth/signup`, "Create an app user"],
    ["POST", `${base}/auth/login`, "Email/password login"],
    ["POST", `${base}/auth/refresh`, "Rotate refresh token"],
    ["GET", `${base}/auth/me`, "Current app user"],
    ["POST", `${base}/auth/request-verification`, "Send verification email"],
    ["POST", `${base}/auth/confirm-verification`, "Confirm verification token"],
    ["POST", `${base}/auth/request-password-reset`, "Send password reset email"],
    ["POST", `${base}/auth/confirm-password-reset`, "Set a new password"],
  ];
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Auth settings</h2>
        <div className="pb-inline-alert info">
          Email/password auth is available per project through the system `users` collection. Verification and reset emails use the SMTP settings.
        </div>
        <div className="pb-info-grid compact">
          <Info label="Project" value={project?.slug ?? "No project selected"} />
          <Info label="Users collection" value={project ? "users" : ""} />
          <Info label="SMTP" value={settings?.smtp.enabled ? "enabled" : "not enabled"} />
        </div>
        {!settings?.smtp.enabled ? (
          <div className="pb-row-actions">
            <button type="button" className="pb-btn secondary" onClick={onOpenMail}>
              <Mail className="h-4 w-4" />
              Configure SMTP
            </button>
          </div>
        ) : null}
      </section>
      <section className="pb-settings-block">
        <h2>Auth API</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Method</th>
                <th>Endpoint</th>
                <th>Use</th>
              </tr>
            </thead>
            <tbody>
              {routes.map(([method, route, use]) => (
                <tr key={`${method}-${route}`}>
                  <td>{method}</td>
                  <td className="truncate-cell">
                    <code>{route}</code>
                  </td>
                  <td>{use}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Client payloads</h2>
        <div className="pb-sync-grid">
          <pre className="pb-code-box">{`POST ${base}/auth/signup
{
  "email": "user@example.com",
  "password": "password-123"
}`}</pre>
          <pre className="pb-code-box">{`POST ${base}/auth/login
{
  "email": "user@example.com",
  "password": "password-123"
}`}</pre>
        </div>
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

function BackupsView(props: {
  project: Project | null;
  projects: Project[];
  backupJobs: BackupJob[];
  backupRuns: Record<string, BackupRun[]>;
  backupDraft: typeof emptyBackupDraft;
  setBackupDraft: React.Dispatch<React.SetStateAction<typeof emptyBackupDraft>>;
  onCreateBackup: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunBackup: (job: BackupJob) => void;
  onLoadBackupRuns: (job: BackupJob) => void;
  onLoadCollectionExport: () => void;
  onOpenExport: () => void;
}) {
  const schema = props.project?.schemaName ?? "proj_app";
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Backups</h2>
        <div className="pb-inline-alert info">Backups run with pg_dump and are stored in the configured file storage, including S3-compatible targets.</div>
        <div className="pb-info-grid compact">
          <Info label="Project schema" value={schema} />
          <Info label="Backup engine" value="pg_dump custom format" />
        </div>
        <div className="pb-row-actions">
          <button type="button" className="pb-btn secondary" onClick={props.onLoadCollectionExport}>
            <FileUp className="h-4 w-4" />
            Export schema JSON
          </button>
          <button type="button" className="pb-btn outline" onClick={props.onOpenExport}>
            Open export
          </button>
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create backup job</h2>
        <form onSubmit={props.onCreateBackup} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.backupDraft.name} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, name: value }))} placeholder="nightly project backup" />
          <label className="pb-field">
            <span>Scope</span>
            <select value={props.backupDraft.scope} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, scope: event.target.value as "full" | "project" }))}>
              <option value="project">Project</option>
              <option value="full">Full database</option>
            </select>
          </label>
          {props.backupDraft.scope === "project" ? (
            <label className="pb-field">
              <span>Project</span>
              <select value={props.backupDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
                <option value="">Select project</option>
                {props.projects.map((project) => (
                  <option key={project.id} value={project.slug}>
                    {project.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <LabeledInput label="Schedule" value={props.backupDraft.schedule} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, schedule: value }))} placeholder="0 2 * * *" />
          <LabeledInput label="Timezone" value={props.backupDraft.timezone} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, timezone: value }))} placeholder="UTC" />
          <LabeledInput label="Retention days" value={props.backupDraft.retentionDays} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, retentionDays: value }))} />
          <LabeledInput label="Retention count" value={props.backupDraft.retentionCount} onChange={(value) => props.setBackupDraft((draft) => ({ ...draft, retentionCount: value }))} />
          <label className="pb-checkline">
            <input type="checkbox" checked={props.backupDraft.enabled} onChange={(event) => props.setBackupDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enabled
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create backup
          </button>
        </form>
      </section>
      <section className="pb-settings-block">
        <h2>Backup jobs</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Scope</th>
                <th>Schedule</th>
                <th>Next run</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.backupJobs.map((job) => {
                const latest = props.backupRuns[job.id]?.[0];
                return (
                  <tr key={job.id}>
                    <td>{job.name}</td>
                    <td>{job.scope === "project" ? job.projectSlug || "project" : "full"}</td>
                    <td>{job.schedule}</td>
                    <td>{formatDate(job.nextRunAt ?? "")}</td>
                    <td>{latest ? `${latest.status}${latest.storageKey ? ` · ${formatBytes(latest.sizeBytes)}` : ""}` : job.enabled ? "enabled" : "paused"}</td>
                    <td>
                      <div className="pb-row-actions tight">
                        <button type="button" className="pb-btn sm secondary" onClick={() => props.onLoadBackupRuns(job)}>
                          Runs
                        </button>
                        <button type="button" className="pb-btn sm primary" onClick={() => props.onRunBackup(job)}>
                          Run
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {props.backupJobs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No backup jobs yet." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <RunHistory runs={Object.values(props.backupRuns).flat()} />
      </section>
    </div>
  );
}

function CronsView(props: {
  projects: Project[];
  project: Project | null;
  cronJobs: CronJob[];
  cronRuns: Record<string, CronRun[]>;
  cronDraft: typeof emptyCronDraft;
  setCronDraft: React.Dispatch<React.SetStateAction<typeof emptyCronDraft>>;
  onCreateCron: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunCron: (job: CronJob) => void;
  onLoadCronRuns: (job: CronJob) => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Crons</h2>
        <div className="pb-inline-alert info">HTTP jobs support cron expressions and @every durations. Retries and response snippets are stored in the run log.</div>
        <form onSubmit={props.onCreateCron} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.cronDraft.name} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, name: value }))} placeholder="refresh cache" />
          <label className="pb-field">
            <span>Project</span>
            <select value={props.cronDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
              <option value="">Global</option>
              {props.projects.map((project) => (
                <option key={project.id} value={project.slug}>
                  {project.name}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="Schedule" value={props.cronDraft.schedule} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, schedule: value }))} placeholder="@every 5m" />
          <LabeledInput label="Timezone" value={props.cronDraft.timezone} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, timezone: value }))} placeholder="UTC" />
          <label className="pb-field">
            <span>Method</span>
            <select value={props.cronDraft.method} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, method: event.target.value }))}>
              {["GET", "POST", "PUT", "PATCH", "DELETE"].map((method) => (
                <option key={method} value={method}>
                  {method}
                </option>
              ))}
            </select>
          </label>
          <LabeledInput label="URL" value={props.cronDraft.url} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, url: value }))} placeholder="https://example.com/api/job" />
          <LabeledInput label="Timeout seconds" value={props.cronDraft.timeoutSeconds} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, timeoutSeconds: value }))} />
          <LabeledInput label="Retries" value={props.cronDraft.retryCount} onChange={(value) => props.setCronDraft((draft) => ({ ...draft, retryCount: value }))} />
          <label className="pb-field wide-field">
            <span>Headers JSON</span>
            <textarea value={props.cronDraft.headersJSON} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, headersJSON: event.target.value }))} rows={4} />
          </label>
          <label className="pb-field wide-field">
            <span>Body</span>
            <textarea value={props.cronDraft.body} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, body: event.target.value }))} rows={4} />
          </label>
          <label className="pb-checkline">
            <input type="checkbox" checked={props.cronDraft.enabled} onChange={(event) => props.setCronDraft((draft) => ({ ...draft, enabled: event.target.checked }))} />
            Enabled
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create cron
          </button>
        </form>
      </section>
      <section className="pb-settings-block">
        <h2>Cron jobs</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Method</th>
                <th>Schedule</th>
                <th>Next run</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.cronJobs.map((job) => {
                const latest = props.cronRuns[job.id]?.[0];
                return (
                  <tr key={job.id}>
                    <td>{job.name}</td>
                    <td>{job.method}</td>
                    <td>{job.schedule}</td>
                    <td>{formatDate(job.nextRunAt ?? "")}</td>
                    <td>{latest ? `${latest.status}${latest.statusCode ? ` · ${latest.statusCode}` : ""}` : job.enabled ? "enabled" : "paused"}</td>
                    <td>
                      <div className="pb-row-actions tight">
                        <button type="button" className="pb-btn sm secondary" onClick={() => props.onLoadCronRuns(job)}>
                          Runs
                        </button>
                        <button type="button" className="pb-btn sm primary" onClick={() => props.onRunCron(job)}>
                          Run
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
              {props.cronJobs.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No cron jobs registered." />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
        <RunHistory runs={Object.values(props.cronRuns).flat()} />
      </section>
    </div>
  );
}

function MCPAccessView(props: {
  project: Project | null;
  projects: Project[];
  mcpTokens: MCPToken[];
  oneTimeMCPToken: MCPToken | null;
  mcpDraft: typeof emptyMCPDraft;
  setMCPDraft: React.Dispatch<React.SetStateAction<typeof emptyMCPDraft>>;
  onCreateMCPToken: (event: React.FormEvent<HTMLFormElement>) => void;
  onRevokeMCPToken: (token: MCPToken) => void;
  onDismissMCPSecret: () => void;
  onCopy: (text: string) => void;
}) {
  const endpoint = typeof window !== "undefined" ? `${window.location.origin}/mcp` : "/mcp";
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>MCP access</h2>
        <div className="pb-inline-alert info">Create scoped tokens for AI tools. Admin tokens can manage the instance; project tokens are restricted to one project and their allowlist.</div>
        <div className="pb-info-grid compact">
          <Info label="Remote endpoint" value={endpoint} />
          <Info label="Protocol" value="HTTP JSON-RPC MCP" />
        </div>
      </section>
      <section className="pb-settings-block">
        <h2>Create MCP token</h2>
        <form onSubmit={props.onCreateMCPToken} className="pb-grid-form ops-grid">
          <LabeledInput label="Name" value={props.mcpDraft.name} onChange={(value) => props.setMCPDraft((draft) => ({ ...draft, name: value }))} placeholder="codex project agent" />
          <label className="pb-field">
            <span>Scope</span>
            <select value={props.mcpDraft.scope} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, scope: event.target.value as "admin" | "project" }))}>
              <option value="project">Project</option>
              <option value="admin">Admin</option>
            </select>
          </label>
          {props.mcpDraft.scope === "project" ? (
            <label className="pb-field">
              <span>Project</span>
              <select value={props.mcpDraft.projectSlug || props.project?.slug || ""} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, projectSlug: event.target.value }))}>
                <option value="">Select project</option>
                {props.projects.map((project) => (
                  <option key={project.id} value={project.slug}>
                    {project.name}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <label className="pb-field">
            <span>Expires at</span>
            <input type="datetime-local" value={props.mcpDraft.expiresAt} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, expiresAt: event.target.value }))} />
          </label>
          <label className="pb-field wide-field">
            <span>Allowed tools</span>
            <textarea value={props.mcpDraft.allowedTools} onChange={(event) => props.setMCPDraft((draft) => ({ ...draft, allowedTools: event.target.value }))} rows={5} placeholder="Leave empty for safe defaults, or enter comma/newline separated tool names." />
          </label>
          <button type="submit" className="pb-btn primary">
            <Plus className="h-4 w-4" />
            Create token
          </button>
        </form>
        {props.oneTimeMCPToken?.token ? (
          <div className="pb-secret-box">
            <strong>Copy this token now. It will not be shown again.</strong>
            <code>{props.oneTimeMCPToken.token}</code>
            <div className="pb-row-actions">
              <button type="button" className="pb-btn secondary" onClick={() => props.onCopy(props.oneTimeMCPToken?.token ?? "")}>
                <Copy className="h-4 w-4" />
                Copy token
              </button>
              <button type="button" className="pb-btn transparent" onClick={props.onDismissMCPSecret}>
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </section>
      <section className="pb-settings-block">
        <h2>Tokens</h2>
        <div className="pb-table-wrap">
          <table className="pb-records-table compact">
            <thead>
              <tr>
                <th>Name</th>
                <th>Scope</th>
                <th>Prefix</th>
                <th>Tools</th>
                <th>Status</th>
                <th aria-label="Actions" />
              </tr>
            </thead>
            <tbody>
              {props.mcpTokens.map((token) => (
                <tr key={token.id}>
                  <td>{token.name}</td>
                  <td>{token.scope === "project" ? token.projectSlug || "project" : "admin"}</td>
                  <td>{token.prefix}</td>
                  <td>{token.allowedTools.length ? token.allowedTools.join(", ") : "default"}</td>
                  <td>{token.revokedAt ? "revoked" : token.expiresAt ? `expires ${formatDate(token.expiresAt)}` : "active"}</td>
                  <td>
                    <button type="button" className="pb-btn sm transparent danger" onClick={() => props.onRevokeMCPToken(token)} disabled={Boolean(token.revokedAt)}>
                      Revoke
                    </button>
                  </td>
                </tr>
              ))}
              {props.mcpTokens.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="No MCP tokens yet." />
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

function RunHistory({ runs }: { runs: Array<CronRun | BackupRun> }) {
  const recent = runs.slice(0, 8);
  if (recent.length === 0) return null;
  return (
    <div className="pb-run-history">
      <h3>Recent runs</h3>
      <div className="pb-table-wrap">
        <table className="pb-records-table compact">
          <thead>
            <tr>
              <th>Status</th>
              <th>Started</th>
              <th>Result</th>
              <th>Error</th>
            </tr>
          </thead>
          <tbody>
            {recent.map((run) => {
              const result = "statusCode" in run ? String(run.statusCode ?? "-") : "storageKey" in run ? `${formatBytes(run.sizeBytes)} ${run.storageKey}` : "-";
              return (
                <tr key={run.id}>
                  <td>{run.status}</td>
                  <td>{formatDate(run.startedAt)}</td>
                  <td>{result}</td>
                  <td>{run.error || "-"}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function ExportCollectionsView(props: {
  collectionExport: CollectionExport | null;
  exportSelection: string[];
  selectedExportItems: CollectionExport["items"];
  exportPreview: string;
  onLoadCollectionExport: () => void;
  onToggleExportCollection: (name: string) => void;
  onCopyExport: () => void;
  onDownloadExport: () => void;
  onUseExportForImport: () => void;
}) {
  const items = props.collectionExport?.items ?? [];
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Export collections</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn outline" onClick={props.onLoadCollectionExport}>
              <RefreshCw className="h-4 w-4" />
              Refresh
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onCopyExport} disabled={items.length === 0}>
              <Copy className="h-4 w-4" />
              Copy
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onDownloadExport} disabled={items.length === 0}>
              <Download className="h-4 w-4" />
              Download JSON
            </button>
          </div>
        </div>
        <div className="pb-sync-grid">
          <div className="pb-sync-list" aria-label="Collections to export">
            {items.map((item) => (
              <label key={item.name} className="pb-sync-item">
                <input type="checkbox" checked={props.exportSelection.includes(item.name)} onChange={() => props.onToggleExportCollection(item.name)} />
                <span>
                  <strong>{item.name}</strong>
                  <em>{item.system ? "system" : item.type}</em>
                </span>
              </label>
            ))}
            {items.length === 0 ? <EmptyState label="Load collection schema from the selected project." action="Load schema" onAction={props.onLoadCollectionExport} /> : null}
          </div>
          <div className="pb-sync-preview">
            <div className="pb-sync-preview-bar">
              <span>{props.selectedExportItems.length} selected</span>
              <button type="button" className="pb-btn sm primary" onClick={props.onUseExportForImport} disabled={props.selectedExportItems.length === 0}>
                Use for import
              </button>
            </div>
            <pre className="pb-code-box schema-preview">{props.exportPreview}</pre>
          </div>
        </div>
      </section>
    </div>
  );
}

function ImportCollectionsView(props: {
  importJSON: string;
  setImportJSON: React.Dispatch<React.SetStateAction<string>>;
  importMode: "create_missing" | "upsert";
  setImportMode: React.Dispatch<React.SetStateAction<"create_missing" | "upsert">>;
  importDropMissingFields: boolean;
  setImportDropMissingFields: React.Dispatch<React.SetStateAction<boolean>>;
  importResult: CollectionImportResult | null;
  onPreviewImport: () => void;
  onApplyImport: () => void;
}) {
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Import collections</h2>
        <label className="pb-field">
          <span>Collections JSON</span>
          <textarea className="mono import-json-editor" value={props.importJSON} onChange={(event) => props.setImportJSON(event.target.value)} placeholder='{"items":[{"name":"posts","type":"base","fields":[]}]}' />
        </label>
        <div className="pb-grid-form import-options">
          <label className="pb-field">
            <span>Mode</span>
            <select value={props.importMode} onChange={(event) => props.setImportMode(event.target.value as "create_missing" | "upsert")}>
              <option value="create_missing">Create missing</option>
              <option value="upsert">Create and update</option>
            </select>
          </label>
          <label className="pb-checkline switchline">
            <input type="checkbox" checked={props.importDropMissingFields} onChange={(event) => props.setImportDropMissingFields(event.target.checked)} />
            Drop fields missing from JSON
          </label>
          <div className="pb-row-actions import-actions">
            <button type="button" className="pb-btn outline expanded-lg" onClick={props.onPreviewImport}>
              Preview import
            </button>
            <button
              type="button"
              className="pb-btn primary expanded-lg"
              onClick={() => {
                if (props.importDropMissingFields && !window.confirm("Drop fields that are missing from the import JSON?")) return;
                props.onApplyImport();
              }}
            >
              Apply import
            </button>
          </div>
        </div>
      </section>
      {props.importResult ? (
        <section className="pb-settings-block">
          <h2>{props.importResult.dryRun ? "Import preview" : "Import result"}</h2>
          <div className="pb-info-grid compact">
            <Info label="Create" value={String(props.importResult.created)} />
            <Info label="Update" value={String(props.importResult.updated)} />
            <Info label="Skip" value={String(props.importResult.skipped)} />
          </div>
          <CompactTable headers={["Collection", "Action", "Status", "Message"]} rows={props.importResult.items.map((item) => [item.name, item.action, item.status, item.message ?? ""])} empty="No import changes." />
        </section>
      ) : null}
    </div>
  );
}

function DiscoverTablesView(props: {
  discoveredTables: DiscoveredTable[];
  schemaSelection: string[];
  schemaFilters: { schema: string; table: string };
  setSchemaFilters: React.Dispatch<React.SetStateAction<{ schema: string; table: string }>>;
  setSchemaSelection: React.Dispatch<React.SetStateAction<string[]>>;
  schemaImportNames: Record<string, string>;
  schemaImportResult: CollectionImportResult | null;
  onScanSchema: () => void;
  onToggleSchemaTable: (table: DiscoveredTable) => void;
  onSetSchemaImportName: (table: DiscoveredTable, name: string) => void;
  onPreviewSchemaImport: () => void;
  onApplySchemaImport: () => void;
}) {
  const selectedKeys = new Set(props.schemaSelection);
  const eligibleTables = props.discoveredTables.filter((table) => table.canImport && !table.existingCollection);
  const selectedTables = props.discoveredTables.filter((table) => selectedKeys.has(discoveredTableKey(table)));
  const selectedCount = props.schemaSelection.length;
  const schemaCount = new Set(props.discoveredTables.map((table) => table.schema)).size;
  const importedCount = props.discoveredTables.filter((table) => table.existingCollection).length;
  const readOnlyCount = props.discoveredTables.filter((table) => !table.canImport && !table.existingCollection).length;
  const relationCount = props.discoveredTables.reduce((total, table) => total + table.foreignKeys.length, 0);
  const importNameIssues = schemaImportNameIssues(selectedTables, props.schemaImportNames);
  const canSubmit = selectedCount > 0 && importNameIssues.length === 0;

  function selectEligibleTables() {
    props.setSchemaSelection(eligibleTables.map(discoveredTableKey));
  }

  function clearSelection() {
    props.setSchemaSelection([]);
  }

  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Discover existing tables</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn secondary" onClick={selectEligibleTables} disabled={eligibleTables.length === 0}>
              Select CRUD-ready
            </button>
            <button type="button" className="pb-btn outline" onClick={props.onScanSchema}>
              <RefreshCw className="h-4 w-4" />
              Scan database
            </button>
          </div>
        </div>
        <div className="pb-inline-alert info">
          Discovery is read-only. Tables can be imported for admin CRUD only when they have a single usable primary key. Field edits stay locked until the table has `id uuid`, `created`, and `updated` and is marked managed.
        </div>
        <div className="pb-discovery-stats" aria-label="Schema discovery summary">
          <Info label="Schemas" value={String(schemaCount)} />
          <Info label="Tables" value={String(props.discoveredTables.length)} />
          <Info label="CRUD-ready" value={String(eligibleTables.length)} />
          <Info label="Relations" value={String(relationCount)} />
          <Info label="Read-only" value={String(readOnlyCount)} />
          <Info label="Configured" value={String(importedCount)} />
        </div>
        <div className="pb-grid-form import-options">
          <LabeledInput label="Schema filter" value={props.schemaFilters.schema} onChange={(value) => props.setSchemaFilters((current) => ({ ...current, schema: value }))} placeholder="public" />
          <LabeledInput label="Table search" value={props.schemaFilters.table} onChange={(value) => props.setSchemaFilters((current) => ({ ...current, table: value }))} placeholder="users" />
        </div>
      </section>

      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>Tables</h2>
          <div className="pb-row-actions tight">
            <button type="button" className="pb-btn outline" onClick={clearSelection} disabled={selectedCount === 0}>
              Clear
            </button>
            <button type="button" className="pb-btn secondary" onClick={props.onPreviewSchemaImport} disabled={!canSubmit}>
              Preview import
            </button>
            <button type="button" className="pb-btn primary" onClick={props.onApplySchemaImport} disabled={!canSubmit}>
              Import selected
            </button>
          </div>
        </div>
        {importNameIssues.length > 0 ? (
          <div className="pb-inline-alert danger">
            {importNameIssues[0]}
          </div>
        ) : selectedCount > 0 ? (
          <div className="pb-inline-alert success">
            {selectedCount} table{selectedCount === 1 ? "" : "s"} selected for import.
          </div>
        ) : null}
        <div className="pb-table-wrap">
          <table className="pb-records-table compact schema-discovery-table">
            <thead>
              <tr>
                <th aria-label="Select" />
                <th>Source table</th>
                <th>Collection</th>
                <th>Primary key</th>
                <th>Fields</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {props.discoveredTables.map((table) => {
                const key = discoveredTableKey(table);
                const selected = props.schemaSelection.includes(key);
                const disabled = !table.canImport || Boolean(table.existingCollection);
                const relationSummary = tableRelationSummary(table, props.discoveredTables, props.schemaImportNames);
                return (
                  <tr key={key}>
                    <td>
                      <input type="checkbox" checked={selected} disabled={disabled} onChange={() => props.onToggleSchemaTable(table)} aria-label={`Import ${table.schema}.${table.table}`} />
                    </td>
                    <td>
                      <div className="pb-table-identity">
                        <strong>{table.table}</strong>
                        <span>{table.schema}</span>
                      </div>
                    </td>
                    <td>
                      <input
                        className="pb-inline-input"
                        value={props.schemaImportNames[key] ?? table.suggestedName}
                        disabled={disabled}
                        onChange={(event) => props.onSetSchemaImportName(table, event.target.value)}
                      />
                    </td>
                    <td>{table.primaryKey ? `${table.primaryKey.column} · ${table.primaryKey.type}` : "-"}</td>
                    <td>
                      {table.fields.length}/{table.columns.length} supported
                      {relationSummary.length > 0 ? (
                        <div className="pb-relation-chip-list">
                          {relationSummary.slice(0, 3).map((relation) => (
                            <span key={`${key}-${relation.column}`}>{relation.label}</span>
                          ))}
                          {relationSummary.length > 3 ? <span>+{relationSummary.length - 3}</span> : null}
                        </div>
                      ) : null}
                    </td>
                    <td>
                      <div className="pb-chip-row">
                        <SchemaStatusChip table={table} />
                        {table.standardSystemColumns ? <span className="pb-chip success">managed-ready</span> : <span className="pb-chip">staged</span>}
                      </div>
                      {table.reason ? <small className="pb-muted-inline">{table.reason}</small> : null}
                    </td>
                  </tr>
                );
              })}
              {props.discoveredTables.length === 0 ? (
                <tr>
                  <td colSpan={6} className="pb-empty-cell">
                    <EmptyState label="Scan the database to preview existing tables." action="Scan database" onAction={props.onScanSchema} />
                  </td>
                </tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </section>

      {props.discoveredTables.length > 0 ? (
        <section className="pb-settings-block">
          <h2>Preview fields and relations</h2>
          <div className="pb-discovery-preview-grid">
            {(selectedTables.length > 0 ? selectedTables : props.discoveredTables.slice(0, 12)).map((table) => (
              <details key={`preview-${discoveredTableKey(table)}`} className="pb-discovery-card">
                <summary>
                  <span>{table.schema}.{table.table}</span>
                  <em>{table.fields.length} fields · {table.foreignKeys.length} relations</em>
                </summary>
                <div className="pb-discovery-card-meta">
                  <Info label="Collection" value={props.schemaImportNames[discoveredTableKey(table)] ?? table.suggestedName} />
                  <Info label="Primary key" value={table.primaryKey ? `${table.primaryKey.field} (${table.primaryKey.type})` : "none"} />
                </div>
                {table.foreignKeys.length > 0 ? (
                  <div className="pb-discovery-relations">
                    {tableRelationSummary(table, props.discoveredTables, props.schemaImportNames).map((relation) => (
                      <span key={`${table.schema}.${table.table}.${relation.column}`}>{relation.label}</span>
                    ))}
                  </div>
                ) : null}
                <div className="pb-discovery-fields">
                  {table.columns.map((column) => (
                    <span key={column.name} className={`${column.supported ? "" : "muted"} ${column.primaryKey ? "primary-key" : ""}`} title={column.reason || column.udtName}>
                      {column.fieldName || column.name}
                      <em>{column.primaryKey ? "pk" : column.udtName}</em>
                    </span>
                  ))}
                </div>
              </details>
            ))}
          </div>
        </section>
      ) : null}

      {props.schemaImportResult ? (
        <section className="pb-settings-block">
          <h2>{props.schemaImportResult.dryRun ? "Import preview" : "Import result"}</h2>
          <div className="pb-info-grid compact">
            <Info label="Import" value={String(props.schemaImportResult.created)} />
            <Info label="Skip" value={String(props.schemaImportResult.skipped)} />
          </div>
          <CompactTable headers={["Collection", "Action", "Status", "Message"]} rows={props.schemaImportResult.items.map((item) => [item.name, item.action, item.status, item.message ?? ""])} empty="No table import changes." />
        </section>
      ) : null}
    </div>
  );
}

function SchemaStatusChip({ table }: { table: DiscoveredTable }) {
  if (table.existingCollection) return <span className="pb-chip">configured as {table.existingCollection}</span>;
  if (table.canImport) return <span className="pb-chip success">CRUD-ready</span>;
  return <span className="pb-chip warning">read-only</span>;
}

function SQLConsoleView(props: {
  sqlQuery: string;
  setSQLQuery: React.Dispatch<React.SetStateAction<string>>;
  sqlMaxRows: string;
  setSQLMaxRows: React.Dispatch<React.SetStateAction<string>>;
  sqlResult: SQLResult | null;
  sqlHistory: string[];
  onExecuteSQL: (event: React.FormEvent<HTMLFormElement>) => void;
  onCopySQLCSV: () => void;
}) {
  const hasRows = Boolean(props.sqlResult?.columns.length);
  return (
    <form onSubmit={props.onExecuteSQL} className="pb-settings-stack">
      <section className="pb-settings-block">
        <div className="pb-section-title-row">
          <h2>SQL console</h2>
          <div className="pb-row-actions tight">
            <label className="pb-field sql-history-field">
              <span>History</span>
              <select value="" onChange={(event) => event.target.value && props.setSQLQuery(event.target.value)}>
                <option value="">Recent queries</option>
                {props.sqlHistory.map((query) => (
                  <option key={query} value={query}>
                    {query}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
        <label className="pb-field">
          <span>Query</span>
          <textarea className="mono sql-editor" value={props.sqlQuery} onChange={(event) => props.setSQLQuery(event.target.value)} spellCheck={false} />
        </label>
        <div className="pb-settings-actions sql-actions">
          <label className="pb-field max-rows-field">
            <span>Max rows</span>
            <input value={props.sqlMaxRows} onChange={(event) => props.setSQLMaxRows(event.target.value)} inputMode="numeric" />
          </label>
          {hasRows ? (
            <button type="button" className="pb-btn outline expanded-lg" onClick={props.onCopySQLCSV}>
              <Copy className="h-4 w-4" />
              Copy CSV
            </button>
          ) : null}
          <button type="submit" className="pb-btn primary expanded-lg">
            <Code2 className="h-4 w-4" />
            Execute
          </button>
        </div>
      </section>
      {props.sqlResult ? (
        <section className="pb-settings-block">
          <div className={`pb-inline-alert ${props.sqlResult.readOnly ? "info" : "warning"}`}>
            {props.sqlResult.command || "SQL"} in {props.sqlResult.durationMs}ms. {props.sqlResult.columns.length > 0 ? `${props.sqlResult.rows.length} rows returned.` : `${props.sqlResult.affectedRows} rows affected.`}
            {props.sqlResult.truncated ? " Result truncated by max rows." : ""}
          </div>
          {hasRows ? <SQLResultTable result={props.sqlResult} /> : null}
        </section>
      ) : null}
    </form>
  );
}

function SQLResultTable({ result }: { result: SQLResult }) {
  return (
    <div className="pb-table-wrap sql-result-table">
      <table className="pb-records-table compact">
        <thead>
          <tr>
            {result.columns.map((column, index) => (
              <th key={`${column.name}-${index}`}>{column.name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {result.rows.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {result.columns.map((column, columnIndex) => (
                <td key={`${column.name}-${columnIndex}`} className="truncate-cell">
                  {formatSQLValue(row[columnIndex])}
                </td>
              ))}
            </tr>
          ))}
          {result.rows.length === 0 ? (
            <tr>
              <td colSpan={Math.max(result.columns.length, 1)} className="pb-empty-cell">
                <EmptyState label="No rows returned." />
              </td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </div>
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
  selectedUploadFile: File | null;
  setSelectedUploadFile: React.Dispatch<React.SetStateAction<File | null>>;
  fileResult: RecordItem | null;
  records: RecordList;
  token: string;
  projectSlug: string;
  onSubmitUpload: (event: React.FormEvent<HTMLFormElement>) => void;
  onCreateFileToken: (recordId: string, field: string, fileId: string) => Promise<string>;
}) {
  const availableFiles = findFiles(props.records.items, props.fileFields);
  const [dragActive, setDragActive] = useState(false);
  const dragDepthRef = useRef(0);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const pickFile = (files: FileList | null) => {
    const file = files?.[0] ?? null;
    props.setSelectedUploadFile(file);
  };
  return (
    <div className="pb-settings-stack">
      <section className="pb-settings-block">
        <h2>Upload file</h2>
        <form onSubmit={props.onSubmitUpload} className="pb-grid-form file-upload-form">
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
          <div
            className={`pb-dropzone ${dragActive ? "active" : ""} ${props.selectedUploadFile ? "has-file" : ""}`}
            onDragEnter={(event) => {
              event.preventDefault();
              dragDepthRef.current += 1;
              setDragActive(true);
            }}
            onDragOver={(event) => {
              event.preventDefault();
              setDragActive(true);
            }}
            onDragLeave={(event) => {
              event.preventDefault();
              dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
              if (dragDepthRef.current === 0) setDragActive(false);
            }}
            onDrop={(event) => {
              event.preventDefault();
              dragDepthRef.current = 0;
              setDragActive(false);
              pickFile(event.dataTransfer.files);
            }}
          >
            <input ref={fileInputRef} name="file" type="file" className="sr-only" onChange={(event) => pickFile(event.currentTarget.files)} />
            <UploadCloud className="h-6 w-6" />
            <div>
              <strong>{props.selectedUploadFile ? props.selectedUploadFile.name : "Drop file here"}</strong>
              <span>{props.selectedUploadFile ? `${formatBytes(props.selectedUploadFile.size)} · use Browse to replace` : "or use Browse to select from your computer"}</span>
            </div>
            <button type="button" className="pb-btn secondary" onClick={() => fileInputRef.current?.click()}>
              Browse
            </button>
            {props.selectedUploadFile ? (
              <button
                type="button"
                className="pb-icon-btn"
                aria-label="Clear selected file"
                onClick={() => {
                  props.setSelectedUploadFile(null);
                  if (fileInputRef.current) fileInputRef.current.value = "";
                }}
              >
                <X className="h-4 w-4" />
              </button>
            ) : null}
          </div>
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

function collectionIconFromOptions(collection: Collection): CollectionIconOption {
  return normalizeCollectionIcon(normalizeCollectionOptions(collection.options).icon, collection.type);
}

function collectionOptionsWithIcon(options: CollectionOptions | undefined, icon: CollectionIconOption): CollectionOptions {
  return {
    ...normalizeCollectionOptions(options),
    icon: normalizeCollectionIcon(icon, "base"),
  };
}

function collectionImportedFromOptions(collection: Collection): boolean {
  return Boolean(normalizeCollectionOptions(collection.options).imported);
}

function collectionManagedByDublyobase(collection: Collection): boolean {
  const options = normalizeCollectionOptions(collection.options);
  return !options.imported || Boolean(options.managed);
}

function collectionStandardSystemColumns(collection: Collection): boolean {
  const options = normalizeCollectionOptions(collection.options);
  return !options.imported || Boolean(options.standardSystemColumns);
}

function collectionSourceTable(collection: Collection): string {
  const options = normalizeCollectionOptions(collection.options);
  const schema = typeof options.sourceSchema === "string" ? options.sourceSchema : "";
  const table = typeof options.sourceTable === "string" ? options.sourceTable : "";
  return [schema, table].filter(Boolean).join(".");
}

function collectionPrimaryKeyFieldName(collection: Collection): string {
  const options = normalizeCollectionOptions(collection.options);
  return typeof options.primaryKeyField === "string" && options.primaryKeyField ? options.primaryKeyField : "id";
}

function recordPrimaryKeyValue(collection: Collection, record: RecordItem): string {
  const value = record[collectionPrimaryKeyFieldName(collection)];
  return value === null || value === undefined ? "" : String(value);
}

function normalizeCollectionOptions(options: unknown): CollectionOptions {
  if (!options || typeof options !== "object" || Array.isArray(options)) return {};
  return { ...(options as CollectionOptions) };
}

function normalizeCollectionIcon(raw: unknown, collectionType: Collection["type"]): CollectionIconOption {
  const fallback = defaultCollectionIcon(collectionType);
  if (raw && typeof raw === "object" && !Array.isArray(raw)) {
    const body = raw as { type?: unknown; name?: unknown; value?: unknown };
    if (body.type === "lucide" && typeof body.name === "string") {
      const name = normalizeLucideIconName(body.name);
      return collectionIconMap[name] ? { type: "lucide", name } : fallback;
    }
    if (body.type === "emoji" && typeof body.value === "string") {
      return { type: "emoji", value: sanitizeCollectionEmoji(body.value) || "◆" };
    }
  }
  if (typeof raw === "string") {
    const value = raw.trim();
    if (value.startsWith("lucide:")) {
      const name = normalizeLucideIconName(value.slice("lucide:".length));
      return collectionIconMap[name] ? { type: "lucide", name } : fallback;
    }
    if (value.startsWith("emoji:")) {
      return { type: "emoji", value: sanitizeCollectionEmoji(value.slice("emoji:".length)) || "◆" };
    }
    const name = normalizeLucideIconName(value);
    if (collectionIconMap[name]) return { type: "lucide", name };
    if (value) return { type: "emoji", value: sanitizeCollectionEmoji(value) || "◆" };
  }
  return fallback;
}

function defaultCollectionIcon(type: Collection["type"]): CollectionIconOption {
  if (type === "auth") return { type: "lucide", name: "shield" };
  if (type === "view") return { type: "lucide", name: "eye" };
  return { type: "lucide", name: "table" };
}

function normalizeLucideIconName(value: string) {
  return value.trim().replaceAll("_", "-").replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
}

function sanitizeCollectionEmoji(value: string) {
  return Array.from(value.trim()).slice(0, 4).join("");
}

function extractImportItems(raw: string): unknown[] {
  const parsed = JSON.parse(raw);
  if (Array.isArray(parsed)) return parsed;
  if (parsed && typeof parsed === "object") {
    const body = parsed as { items?: unknown; collections?: unknown };
    if (Array.isArray(body.items)) return body.items;
    if (Array.isArray(body.collections)) return body.collections;
  }
  throw new Error("Import JSON must be an array or include an items array");
}

function discoveredTableKey(table: Pick<DiscoveredTable, "schema" | "table">) {
  return `${table.schema}.${table.table}`;
}

function schemaImportNameIssues(tables: DiscoveredTable[], names: Record<string, string>) {
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

function isValidCollectionName(name: string) {
  return /^[a-z][a-z0-9_]{0,58}$/.test(name) && !name.startsWith("pg_") && name !== "id" && name !== "created" && name !== "updated";
}

function tableRelationSummary(table: DiscoveredTable, tables: DiscoveredTable[], names: Record<string, string>) {
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

function isDangerousSQL(query: string) {
  const first = query.trim().split(/\s+/, 1)[0]?.toLowerCase().replace(/;$/, "") ?? "";
  return ["alter", "replace", "insert", "create", "update", "delete", "drop", "truncate", "grant", "revoke"].includes(first);
}

function loadSQLHistory() {
  try {
    const raw = window.localStorage.getItem(SQL_HISTORY_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string").slice(0, 20) : [];
  } catch {
    return [];
  }
}

function saveSQLHistory(query: string, current: string[]) {
  const next = [query, ...current.filter((item) => item !== query)].slice(0, 20);
  try {
    window.localStorage.setItem(SQL_HISTORY_KEY, JSON.stringify(next));
  } catch {
    // Query history is a convenience; failing to persist it must not block SQL execution.
  }
  return next;
}

function formatSQLValue(value: unknown) {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") return String(value);
  return JSON.stringify(value);
}

function sqlResultToCSV(result: SQLResult) {
  const rows = [result.columns.map((column) => column.name), ...result.rows.map((row) => result.columns.map((_, index) => formatSQLValue(row[index])))];
  return rows.map((row) => row.map(csvCell).join(",")).join("\n");
}

function csvCell(value: string) {
  if (/[",\n\r]/.test(value)) {
    return `"${value.replaceAll('"', '""')}"`;
  }
  return value;
}

function stripSystemFields(record: RecordItem, collection?: Collection | null): RecordItem {
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

function parseRecordDraft(raw: string): { ok: true; value: RecordItem } | { ok: false; error: string } {
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

function isRecordFormField(field: Field): boolean {
  if (field.type === "password") return true;
  return !field.hidden && field.type !== "file" && field.type !== "autodate";
}

function fieldIsMultiple(field: Field): boolean {
  if (Boolean(field.options?.multiple) || Boolean(field.options?.multi)) return true;
  const maxSelect = field.options?.maxSelect;
  return typeof maxSelect === "number" && maxSelect > 1;
}

function toDateTimeLocal(value: unknown) {
  if (typeof value !== "string" || !value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function formatJSONInput(value: unknown) {
  if (value === undefined || value === null || value === "") return "";
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return "";
  }
}

function sanitizeEditorHTML(value: string) {
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

function newDefaultField(type: FieldType = "text"): Field {
  return fieldWithType({ name: "", type, required: false, options: {} }, type);
}

function isVisibleRecordField(field: Field): boolean {
  return !field.hidden && field.type !== "password";
}

function canSearchField(field: Field): boolean {
  if (field.hidden || field.type === "password") return false;
  return ["text", "editor", "email", "url", "select", "number", "bool", "date", "autodate", "relation"].includes(field.type);
}

function fieldMetaSummary(field: Field) {
  const parts = [];
  if (field.hidden) parts.push("hidden");
  if (field.presentable) parts.push("presentable");
  if (field.searchable) parts.push("searchable");
  if (typeof field.options?.sourceColumn === "string") parts.push(`source: ${field.options.sourceColumn}`);
  if (field.type === "relation" && field.options?.collection) parts.push(`to ${field.options.collection}`);
  if (field.type === "file" && field.options?.multiple) parts.push("multiple");
  return parts.length ? parts.join(" · ") : "default";
}

function fieldWithType(field: Field, type: FieldType): Field {
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

function cleanField(field: Field): Field {
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

function defaultOptionsForType(type: FieldType, options: Record<string, unknown> = {}): Record<string, unknown> {
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
  if (type === "number") {
    return {
      ...sourceOptions,
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
    return {
      ...sourceOptions,
      collection: typeof options.collection === "string" ? options.collection : "",
      ...withString("displayField"),
      ...withString("reverseName"),
      ...withString("targetSchema"),
      ...withString("targetTable"),
      ...withString("targetColumn"),
      ...withString("onDelete"),
      ...withNumber("minSelect"),
      ...withNumber("maxSelect"),
      ...withBool("cascadeDelete"),
    };
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

function setFieldOption(field: Field, key: string, value: unknown): Field {
  const options = { ...(field.options ?? {}), [key]: value };
  return { ...field, options: defaultOptionsForType(field.type, options) };
}

function setNumberFieldOption(field: Field, key: string, value: string): Field {
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

function numberOptionValue(options: Record<string, unknown> = {}, key: string) {
  const value = options[key];
  return typeof value === "number" && Number.isFinite(value) ? String(value) : "";
}

function optionValuesText(options: Record<string, unknown> = {}, key = "values") {
  return optionValues(options, key).join("\n");
}

function optionValues(options: Record<string, unknown> = {}, key = "values") {
  const values = options[key];
  if (Array.isArray(values)) {
    return values.map((value) => String(value).trim()).filter(Boolean);
  }
  return [];
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

function formatBytes(value: number) {
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

function parseHeadersJSON(value: string): Record<string, string> {
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
