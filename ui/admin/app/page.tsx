"use client";

import { AlertCircle, Check, ChevronDown, Copy, RefreshCw, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiError, changeAdminEmail, changeAdminPassword, clearAuditLog, clearRequestLogs, createAdmin, createAPIKey, createBackupJob, createCollection, createCronJob, createFileToken, createMCPToken, createProject, createRecord, downloadRecordsCSV, createWebhook, deleteCollection, deleteRecord, deleteWebhook, discoverSchema, exportCollections, getProjectAuthSettings, getCollectionInsights, getProjectInsights, getProjectMetrics, getProjectQuotas, health, getSettings, importCollections, importSchemaTables, listAdmins, listAPIKeys, listAudit, listBackupJobs, listBackupRuns, listCollections, listCronJobs, listCronRuns, listMCPTokens, listOpsAlerts, listProjects, listRecords, listRequestLogs, listWebhookDeliveries, listWebhooks, login, logout, me, revokeAPIKey, revokeMCPToken, resolveOpsAlert, restoreBackup, runBackupJob, runCronJob, runSQL, testSMTPSettings, testStorageSettings, updateCollection, updateCORSSettings, updateLogSettings, updateProjectAuthSettings, updateProjectCORSSettings, updateProjectQuotas, updateRecord, updateSMTPSettings, updateStorageSettings, uploadFile } from "../src/lib/api";
import { SettingsWorkspace } from "./components/settings-shell";
import { LogsView } from "./components/logs";
import { InsightsWorkspace } from "./components/insights";
import { CollectionsWorkspace } from "./components/collections";
import { CollectionModal } from "./components/collection-editor";
import { APIPreviewModal, RecordModal } from "./components/record-editor";
import { AccountModal, AuthScreen, PasswordChangeScreen } from "./components/auth";
import { StatusPill } from "./components/ui";
import { canSearchField, cleanField, newDefaultField } from "./lib/fields";
import { relationExpandParam } from "./lib/relations";
import { collectionIconFromOptions, collectionImportedFromOptions, collectionManagedByDublyobase, collectionOptionsWithIcon, discoveredTableKey, extractImportItems, recordPrimaryKeyValue, stripSystemFields } from "./lib/collections";
import { isDangerousSQL, loadSQLHistory, saveSQLHistory, sqlResultToCSV } from "./lib/sql";
import { quotasToDraft, settingsToCORSDraft, settingsToLogDraft, settingsToSMTPDraft, settingsToStorageDraft, splitDraftList } from "./lib/settings-drafts";
import { emptyAdminDraft, emptyAuditFilters, emptyBackupDraft, emptyCORSDraft, emptyCollectionDraft, emptyCronDraft, emptyLogDraft, emptyMCPDraft, emptyQuotaDraft, emptyRequestFilters, emptyRules, emptySMTPDraft, emptyStorageDraft, emptyWebhookDraft, navItems, recordPageSizes, settingsItems, TOKEN_KEY } from "./lib/constants";
import type { CollectionModalMode, InsightsRangeHours, InsightsTab, Notice, RuleDraft, SettingsSection, View } from "./lib/view-types";
import { defaultInsightCollection, formatCount, formatDate, parseHeadersJSON } from "./lib/format";
import type { APIKey, Admin, ApiEnvelope, AuditEntry, BackupJob, BackupRun, Collection, CollectionExport, CollectionIconOption, CollectionImportResult, CollectionInsights, CronJob, CronRun, DiscoveredTable, Field, FieldType, Health, InstanceSettings, MCPToken, OpsAlert, Project, ProjectAuthSettings, ProjectInsights, ProjectMetrics, ProjectQuotas, RecordItem, RecordList, RequestLogEntry, RestoreJob, SchemaImportItem, SQLResult, Webhook, WebhookDelivery } from "../src/lib/types";

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
  const [collectionsProject, setCollectionsProject] = useState("");
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
  const [audit, setAudit] = useState<ApiEnvelope<AuditEntry>>({ items: [], page: 1, perPage: 25, totalItems: 0 });
  const [auditPerPage, setAuditPerPage] = useState<(typeof recordPageSizes)[number]>(25);
  const [auditFilters, setAuditFilters] = useState(emptyAuditFilters);
  const [logMode, setLogMode] = useState<"audit" | "requests">("audit");
  const [requestLogs, setRequestLogs] = useState<ApiEnvelope<RequestLogEntry>>({ items: [], page: 1, perPage: 25, totalItems: 0 });
  const [requestPerPage, setRequestPerPage] = useState<(typeof recordPageSizes)[number]>(25);
  const [requestFilters, setRequestFilters] = useState(emptyRequestFilters);
  const [logDraft, setLogDraft] = useState(emptyLogDraft);
  const [settings, setSettingsState] = useState<InstanceSettings | null>(null);
  const [authSettings, setAuthSettings] = useState<ProjectAuthSettings | null>(null);
  const [projectQuotas, setProjectQuotas] = useState<ProjectQuotas | null>(null);
  const [quotaDraft, setQuotaDraft] = useState(emptyQuotaDraft);
  const [projectMetrics, setProjectMetrics] = useState<ProjectMetrics | null>(null);
  const [insightsTab, setInsightsTab] = useState<InsightsTab>("overview");
  const [insightsRange, setInsightsRange] = useState<InsightsRangeHours>(24);
  const [projectInsights, setProjectInsights] = useState<ProjectInsights | null>(null);
  const [collectionInsights, setCollectionInsights] = useState<CollectionInsights | null>(null);
  const [insightCollection, setInsightCollection] = useState("");
  const [insightsLoading, setInsightsLoading] = useState(false);
  const [opsAlerts, setOpsAlerts] = useState<OpsAlert[]>([]);
  const [adminUsers, setAdminUsers] = useState<Admin[]>([]);
  const [adminDraft, setAdminDraft] = useState(emptyAdminDraft);
  const [oneTimeAdmin, setOneTimeAdmin] = useState<{ email: string; password: string } | null>(null);
  const [cronJobs, setCronJobs] = useState<CronJob[]>([]);
  const [cronRuns, setCronRuns] = useState<Record<string, CronRun[]>>({});
  const [backupJobs, setBackupJobs] = useState<BackupJob[]>([]);
  const [backupRuns, setBackupRuns] = useState<Record<string, BackupRun[]>>({});
  const [restoreFile, setRestoreFile] = useState<File | null>(null);
  const [restoreMode, setRestoreMode] = useState<"dry_run" | "restore">("dry_run");
  const [restoreConfirm, setRestoreConfirm] = useState("");
  const [restoreResult, setRestoreResult] = useState<RestoreJob | null>(null);
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [webhookDraft, setWebhookDraft] = useState(emptyWebhookDraft);
  const [webhookDeliveries, setWebhookDeliveries] = useState<Record<string, WebhookDelivery[]>>({});
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
  const [corsDraft, setCORSDraft] = useState(emptyCORSDraft);
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
    () => (collectionsProject === selectedProject ? collections.find((collection) => collection.name === selectedCollection) ?? collections[0] ?? null : null),
    [collections, collectionsProject, selectedCollection, selectedProject],
  );
  const insightCollectionModel = useMemo(
    () => (collectionsProject === selectedProject ? collections.find((collection) => collection.name === insightCollection) ?? null : null),
    [collections, collectionsProject, insightCollection, selectedProject],
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

  // Success confirmations can fade; errors must not. A validation message like
  // `invalid_rule: unknown field "x"` is something the operator has to read and
  // act on, and a 4.2s timer routinely expired while they were still looking at
  // the form that caused it. Errors now stay until dismissed or superseded.
  const noticeTimer = useRef<number | null>(null);
  const showNotice = useCallback((type: "success" | "error", message: string) => {
    if (noticeTimer.current !== null) {
      window.clearTimeout(noticeTimer.current);
      noticeTimer.current = null;
    }
    setNotice({ type, message });
    if (type === "success") {
      noticeTimer.current = window.setTimeout(() => {
        noticeTimer.current = null;
        setNotice(null);
      }, 4200);
    }
  }, []);

  useEffect(
    () => () => {
      if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    },
    [],
  );

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

  useEffect(() => {
    setAPIPreviewOpen(false);
    setRecordEditorOpen(false);
    setCollectionModal(null);
  }, [selectedCollection, selectedProject, settingsSection, view]);

  const loadProjectData = useCallback(
    async (authToken: string, projectSlug: string, preferredCollection?: string) => {
      if (!projectSlug) return;
      const [collectionResponse, keysResponse, auditResponse, requestResponse, authResponse, quotaResponse, metricsResponse, alertsResponse, webhookResponse] = await Promise.all([
        listCollections(authToken, projectSlug),
        listAPIKeys(authToken, projectSlug),
        listAudit(authToken, { project: projectSlug, page: 1, perPage: auditPerPage, ...auditFilters }),
        listRequestLogs(authToken, { project: projectSlug, page: 1, perPage: requestPerPage, ...requestFilters, status: Number(requestFilters.status) || undefined }),
        getProjectAuthSettings(authToken, projectSlug),
        getProjectQuotas(authToken, projectSlug),
        getProjectMetrics(authToken, projectSlug, 24),
        listOpsAlerts(authToken, projectSlug),
        listWebhooks(authToken, projectSlug),
      ]);
      setCollections(collectionResponse.items);
      setCollectionsProject(projectSlug);
      setApiKeys(keysResponse.items);
      setAudit(auditResponse);
      setRequestLogs(requestResponse);
      setAuthSettings(authResponse);
      setProjectQuotas(quotaResponse);
      setQuotaDraft(quotasToDraft(quotaResponse));
      setProjectMetrics(metricsResponse);
      setOpsAlerts(alertsResponse.items);
      setWebhooks(webhookResponse.items);
      const targetCollection = preferredCollection ?? selectedCollection;
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
          expand: relationExpandParam(nextCollectionModel),
        });
        setRecords(recordsResponse);
      } else {
        setRecords({ items: [], page: 1, perPage: recordPerPage, totalItems: 0 });
      }
    },
    [auditFilters, auditPerPage, recordFilter, recordPerPage, recordSearch, requestFilters, requestPerPage, selectedCollection],
  );

  const refreshAll = useCallback(
    async (authToken = token, preferredProject = selectedProject) => {
      if (!authToken) return;
      setBusy(true);
      try {
        const [healthResponse, projectsResponse, settingsResponse, adminResponse, cronResponse, backupResponse, mcpResponse] = await Promise.all([
          health(),
          listProjects(authToken),
          getSettings(authToken),
          listAdmins(authToken),
          listCronJobs(authToken),
          listBackupJobs(authToken),
          listMCPTokens(authToken),
        ]);
        setHealthState(healthResponse);
        setProjects(projectsResponse.items);
        setSettingsState(settingsResponse);
        setAdminUsers(adminResponse.items);
        setCronJobs(cronResponse.items);
        setBackupJobs(backupResponse.items);
        setMCPTokens(mcpResponse.items);
        setSMTPDraft(settingsToSMTPDraft(settingsResponse));
        setStorageDraft(settingsToStorageDraft(settingsResponse));
        setLogDraft(settingsToLogDraft(settingsResponse));
        const projectSlug = preferredProject || projectsResponse.items[0]?.slug || "";
        const projectModel = projectsResponse.items.find((project) => project.slug === projectSlug) ?? null;
        setCORSDraft(settingsToCORSDraft(settingsResponse, projectModel));
        setSelectedProject(projectSlug);
        if (projectSlug) {
          await loadProjectData(authToken, projectSlug);
        } else {
          setCollections([]);
          setCollectionsProject("");
          setSelectedCollection("");
          setRecords({ items: [], page: 1, perPage: recordPerPage, totalItems: 0 });
          setAudit({ items: [], page: 1, perPage: auditPerPage, totalItems: 0 });
          setRequestLogs({ items: [], page: 1, perPage: requestPerPage, totalItems: 0 });
          setAuthSettings(null);
          setProjectQuotas(null);
          setQuotaDraft(emptyQuotaDraft);
          setProjectMetrics(null);
          setProjectInsights(null);
          setCollectionInsights(null);
          setInsightCollection("");
          setOpsAlerts([]);
          setWebhooks([]);
        }
      } catch (error) {
        handleError(error);
      } finally {
        setBusy(false);
      }
    },
    [auditPerPage, handleError, loadProjectData, recordPerPage, requestPerPage, selectedProject, token],
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
    if (collectionsProject !== selectedProject) return;
    let cancelled = false;
    const { filter, search, perPage } = recordQueryRef.current;
    const activeSearch = selectedCollectionModel?.fields.some((field) => field.searchable && canSearchField(field)) ? search : "";
    listRecords(token, selectedProject, selectedCollection, { page: 1, perPage, filter, search: activeSearch, expand: relationExpandParam(selectedCollectionModel) })
      .then((response) => {
        if (!cancelled) setRecords(response);
      })
      .catch((error) => {
        if (!cancelled) handleError(error);
      });
    return () => {
      cancelled = true;
    };
  }, [collectionsProject, handleError, selectedCollection, selectedCollectionModel, selectedProject, token]);

  useEffect(() => {
    if (!token || view !== "settings" || settingsSection !== "crons") return;
    let cancelled = false;
    listCronJobs(token)
      .then((response) => {
        if (!cancelled) setCronJobs(response.items);
      })
      .catch((error) => {
        if (!cancelled) handleError(error);
      });
    return () => {
      cancelled = true;
    };
  }, [handleError, settingsSection, token, view]);

  useEffect(() => {
    if (collectionsProject !== selectedProject) return;
    const fallback = defaultInsightCollection(collections);
    if (!fallback) {
      if (insightCollection) setInsightCollection("");
      return;
    }
    if (!collections.some((collection) => collection.name === insightCollection)) {
      setInsightCollection(fallback);
    }
  }, [collections, collectionsProject, insightCollection, selectedProject]);

  useEffect(() => {
    if (!token || view !== "insights" || !selectedProject || collectionsProject !== selectedProject) return;
    let cancelled = false;
    const collectionName = insightCollection || defaultInsightCollection(collections);
    setInsightsLoading(true);
    Promise.all([
      getProjectInsights(token, selectedProject, insightsRange),
      collectionName ? getCollectionInsights(token, selectedProject, collectionName, insightsRange) : Promise.resolve(null),
    ])
      .then(([projectResponse, collectionResponse]) => {
        if (cancelled) return;
        setProjectInsights(projectResponse);
        setCollectionInsights(collectionResponse);
      })
      .catch((error) => {
        if (!cancelled) handleError(error);
      })
      .finally(() => {
        if (!cancelled) setInsightsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [collections, collectionsProject, handleError, insightCollection, insightsRange, selectedProject, token, view]);

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

  // The hash was read once on mount, which is before the session is restored
  // and the app shell replaces the login screen — so a cold deep link was read
  // and then overwritten. Re-applying when the token settles fixes the cold
  // link, and listening for hashchange makes browser back and forward work,
  // which they previously did not.
  useEffect(() => {
    const applyHash = () => {
      const hash = window.location.hash.replace("#", "");
      const [primary, secondary] = hash.split("/");
      if (navItems.some((item) => item.id === primary)) {
        setView(primary as View);
      }
      if (primary === "settings" && settingsItems.some((item) => item.id === secondary)) {
        setSettingsSection(secondary as SettingsSection);
      }
    };
    applyHash();
    window.addEventListener("hashchange", applyHash);
    return () => window.removeEventListener("hashchange", applyHash);
  }, [token]);

  function changeView(next: View) {
    setView(next);
    // pushState, not replaceState: replacing left the back button with nothing
    // to go back to, so navigation inside the panel was a one-way trip.
    if (window.location.hash !== `#${next}`) {
      window.history.pushState(null, "", `#${next}`);
    }
  }

  function changeSettings(next: SettingsSection) {
    setView("settings");
    setSettingsSection(next);
    window.history.replaceState(null, "", `#settings/${next}`);
  }

  async function refreshInsights(collectionName = insightCollection, range = insightsRange) {
    if (!token || !selectedProject) return;
    const targetCollection = collectionName || defaultInsightCollection(collections);
    setInsightsLoading(true);
    try {
      const [projectResponse, collectionResponse] = await Promise.all([
        getProjectInsights(token, selectedProject, range),
        targetCollection ? getCollectionInsights(token, selectedProject, targetCollection, range) : Promise.resolve(null),
      ]);
      setProjectInsights(projectResponse);
      setCollectionInsights(collectionResponse);
      if (targetCollection) setInsightCollection(targetCollection);
      showNotice("success", "Insights refreshed");
    } catch (error) {
      handleError(error);
    } finally {
      setInsightsLoading(false);
    }
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
    setCollectionsProject("");
    setSettingsState(null);
    setAdminUsers([]);
    setAdminDraft(emptyAdminDraft);
    setOneTimeAdmin(null);
    setAudit({ items: [], page: 1, perPage: auditPerPage, totalItems: 0 });
    setAuditFilters(emptyAuditFilters);
    setRequestLogs({ items: [], page: 1, perPage: requestPerPage, totalItems: 0 });
    setRequestFilters(emptyRequestFilters);
    setAuthSettings(null);
    setProjectQuotas(null);
    setQuotaDraft(emptyQuotaDraft);
    setProjectMetrics(null);
    setProjectInsights(null);
    setCollectionInsights(null);
    setInsightCollection("");
    setLogDraft(emptyLogDraft);
    setCORSDraft(emptyCORSDraft);
    setCronJobs([]);
    setBackupJobs([]);
    setRestoreResult(null);
    setWebhooks([]);
    setWebhookDeliveries({});
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

  async function submitEmailChange(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    const data = new FormData(event.currentTarget);
    const currentPassword = String(data.get("emailCurrentPassword") ?? "");
    const email = String(data.get("email") ?? "");
    setBusy(true);
    try {
      const response = await changeAdminEmail(token, currentPassword, email);
      setAdmin(response.admin);
      setAdminUsers((items) => items.map((item) => (item.id === response.admin.id ? response.admin : item)));
      showNotice("success", "Email changed.");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function refreshAudit(page = audit.page ?? 1, perPage = auditPerPage) {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await listAudit(token, { project: selectedProject, page, perPage, ...auditFilters });
      setAudit(response);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function refreshRequestLogs(page = requestLogs.page ?? 1, perPage = requestPerPage) {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await listRequestLogs(token, {
        project: selectedProject,
        page,
        perPage,
        search: requestFilters.search,
        method: requestFilters.method,
        status: Number.parseInt(requestFilters.status, 10) || undefined,
      });
      setRequestLogs(response);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveAuthSettings(next: ProjectAuthSettings) {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await updateProjectAuthSettings(token, selectedProject, next);
      setAuthSettings(response);
      showNotice("success", "Auth settings saved");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveQuotaSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await updateProjectQuotas(token, selectedProject, {
        enabled: quotaDraft.enabled,
        requestsPerMinute: Number.parseInt(quotaDraft.requestsPerMinute, 10) || 0,
        authRequestsPerMinute: Number.parseInt(quotaDraft.authRequestsPerMinute, 10) || 0,
        maxAppUsers: Number.parseInt(quotaDraft.maxAppUsers, 10) || 0,
        maxStorageMb: Number.parseInt(quotaDraft.maxStorageMb, 10) || 0,
      });
      setProjectQuotas(response);
      setQuotaDraft(quotasToDraft(response));
      const metrics = await getProjectMetrics(token, selectedProject, 24);
      setProjectMetrics(metrics);
      const alerts = await listOpsAlerts(token, selectedProject, true);
      setOpsAlerts(alerts.items);
      showNotice("success", "Project quotas saved");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function refreshProjectMetrics() {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const response = await getProjectMetrics(token, selectedProject, 24);
      setProjectMetrics(response);
      const alerts = await listOpsAlerts(token, selectedProject, true);
      setOpsAlerts(alerts.items);
      showNotice("success", "Metrics refreshed");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function resolveProjectAlert(id: string) {
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      await resolveOpsAlert(token, selectedProject, id);
      setOpsAlerts((items) => items.filter((item) => item.id !== id));
      showNotice("success", "Alert resolved");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function saveCORSSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const adminOrigins = splitDraftList(corsDraft.adminOrigins);
      const settingsResponse = await updateCORSSettings(token, {
        adminOrigins,
        allowWildcard: corsDraft.allowAdminWildcard,
      });
      let nextProject = selectedProjectModel;
      if (selectedProject) {
        const projectResponse = await updateProjectCORSSettings(token, selectedProject, {
          publicOrigins: splitDraftList(corsDraft.publicOrigins),
          allowWildcard: corsDraft.allowPublicWildcard,
        });
        nextProject = projectResponse;
        setProjects((items) => items.map((item) => (item.id === projectResponse.id ? projectResponse : item)));
      }
      setSettingsState(settingsResponse);
      setCORSDraft(settingsToCORSDraft(settingsResponse, nextProject));
      showNotice("success", "CORS settings saved");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitAdminUser(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const response = await createAdmin(token, {
        email: adminDraft.email,
        password: adminDraft.temporaryPassword,
      });
      setOneTimeAdmin({ email: adminDraft.email, password: adminDraft.temporaryPassword });
      setAdminDraft(emptyAdminDraft);
      showNotice("success", `Super admin ${response.admin.email} created`);
      const admins = await listAdmins(token);
      setAdminUsers(admins.items);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function clearCurrentLogs() {
    if (!token) return;
    const label = logMode === "audit" ? "audit log" : "request log";
    if (!window.confirm(`Clear the entire ${label} table? This cannot be undone.`)) return;
    setBusy(true);
    try {
      const response = logMode === "audit" ? await clearAuditLog(token) : await clearRequestLogs(token);
      if (logMode === "audit") {
        setAudit({ items: [], page: 1, perPage: auditPerPage, totalItems: 0 });
        await refreshAudit(1, auditPerPage);
      } else {
        setRequestLogs({ items: [], page: 1, perPage: requestPerPage, totalItems: 0 });
        await refreshRequestLogs(1, requestPerPage);
      }
      showNotice("success", `Cleared ${formatCount(response.deleted)} ${logMode === "audit" ? "audit entries" : "request logs"}.`);
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

  // Exports what is on screen: the same filter, search, sort and page size the
  // list is using, so the file matches the view rather than the whole table.
  async function exportRecordsCSV(format: "csv" | "xlsx" = "csv") {
    if (!token || !selectedProject || !selectedCollectionModel) return;
    setBusy(true);
    try {
      const search = selectedCollectionModel.fields.some((field) => field.searchable && canSearchField(field))
        ? recordSearch
        : "";
      await downloadRecordsCSV(
        token,
        selectedProject,
        selectedCollectionModel.name,
        { filter: recordFilter, search, sort: "-created" },
        "export",
        format,
      );
      showNotice("success", `Exported ${selectedCollectionModel.name} as ${format.toUpperCase()}`);
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
        expand: relationExpandParam(selectedCollectionModel),
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

  async function saveLogSettings(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      const response = await updateLogSettings(token, {
        retentionDays: Number.parseInt(logDraft.retentionDays, 10) || 30,
        retentionCount: Number.parseInt(logDraft.retentionCount, 10) || 100000,
      });
      setSettingsState(response);
      setLogDraft(settingsToLogDraft(response));
      showNotice("success", "Log retention saved");
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

  async function submitRestore(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !restoreFile) return;
    setBusy(true);
    try {
      const response = await restoreBackup(token, { file: restoreFile, mode: restoreMode, confirm: restoreConfirm });
      setRestoreResult(response);
      showNotice(response.status === "success" ? "success" : "error", `Restore ${response.mode} finished with ${response.status}`);
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function submitWebhook(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!token || !selectedProject) return;
    setBusy(true);
    try {
      const created = await createWebhook(token, selectedProject, {
        name: webhookDraft.name,
        url: webhookDraft.url,
        events: splitDraftList(webhookDraft.events),
        enabled: webhookDraft.enabled,
        timeoutSeconds: Number.parseInt(webhookDraft.timeoutSeconds, 10) || 10,
        maxAttempts: Number.parseInt(webhookDraft.maxAttempts, 10) || 5,
        secret: webhookDraft.secret || undefined,
      });
      setWebhookDraft(emptyWebhookDraft);
      setWebhooks((items) => [created, ...items]);
      showNotice("success", created.secret ? `Webhook created. Secret: ${created.secret}` : "Webhook created");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function removeWebhook(hook: Webhook) {
    if (!token || !selectedProject) return;
    if (!window.confirm(`Delete webhook ${hook.name}?`)) return;
    setBusy(true);
    try {
      await deleteWebhook(token, selectedProject, hook.id);
      setWebhooks((items) => items.filter((item) => item.id !== hook.id));
      showNotice("success", "Webhook deleted");
    } catch (error) {
      handleError(error);
    } finally {
      setBusy(false);
    }
  }

  async function loadWebhookRuns(hook: Webhook) {
    if (!token || !selectedProject) return;
    try {
      const response = await listWebhookDeliveries(token, selectedProject, hook.id);
      setWebhookDeliveries((current) => ({ ...current, [hook.id]: response.items }));
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
          <img className="pb-brand-mark" src="/dublyobase-logo.png" alt="" aria-hidden="true" />
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
            setCollections([]);
            setCollectionsProject("");
            setApiKeys([]);
            setAudit({ items: [], page: 1, perPage: auditPerPage, totalItems: 0 });
            setRequestLogs({ items: [], page: 1, perPage: requestPerPage, totalItems: 0 });
            setAuthSettings(null);
            setProjectQuotas(null);
            setQuotaDraft(emptyQuotaDraft);
            setProjectMetrics(null);
            setProjectInsights(null);
            setCollectionInsights(null);
            setInsightCollection("");
            setWebhooks([]);
            setWebhookDeliveries({});
            setCORSDraft(settingsToCORSDraft(settings, projects.find((project) => project.slug === slug) ?? null));
            setSelectedCollection("");
            setSelectedRecordId("");
            setRecords({ items: [], page: 1, perPage: recordPerPage, totalItems: 0 });
            if (token) loadProjectData(token, slug, "").catch(handleError);
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

      {accountOpen ? <AccountModal admin={admin} busy={busy} onEmailSubmit={submitEmailChange} onPasswordSubmit={submitPasswordChange} onClose={() => setAccountOpen(false)} onLogout={signOut} /> : null}

      {notice ? (
        <div className={`pb-toast ${notice.type === "error" ? "danger" : "success"}`} role="status" aria-live="polite">
          {notice.type === "error" ? <AlertCircle className="h-4 w-4 shrink-0" /> : <Check className="h-4 w-4 shrink-0" />}
          <span className="pb-toast-message">{notice.message}</span>
          {notice.type === "error" ? (
            <button type="button" className="pb-toast-close" onClick={() => setNotice(null)} aria-label="Dismiss error">
              <X className="h-4 w-4" />
            </button>
          ) : null}
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
          onExportCSV={exportRecordsCSV}
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

      {view === "insights" ? (
        <InsightsWorkspace
          project={selectedProjectModel}
          collections={collections}
          selectedCollection={insightCollectionModel}
          selectedCollectionName={insightCollection}
          projectInsights={projectInsights}
          collectionInsights={collectionInsights}
          tab={insightsTab}
          setTab={setInsightsTab}
          range={insightsRange}
          setRange={setInsightsRange}
          loading={insightsLoading}
          onSelectCollection={(name) => {
            setInsightCollection(name);
            void refreshInsights(name);
          }}
          onRefresh={() => void refreshInsights()}
          version={healthState?.version ?? "unknown"}
        />
      ) : null}

      {view === "logs" ? (
        <LogsView
          mode={logMode}
          setMode={setLogMode}
          audit={audit}
          auditPerPage={auditPerPage}
          filters={auditFilters}
          requestLogs={requestLogs}
          requestPerPage={requestPerPage}
          requestFilters={requestFilters}
          settings={settings}
          logDraft={logDraft}
          setLogDraft={setLogDraft}
          onFilterChange={setAuditFilters}
          onRequestFilterChange={setRequestFilters}
          onSaveLogSettings={saveLogSettings}
          onRefresh={() => void (logMode === "audit" ? refreshAudit() : refreshRequestLogs())}
          onClearLogs={() => void clearCurrentLogs()}
          onPageChange={(page) => {
            void refreshAudit(page);
          }}
          onRequestPageChange={(page) => void refreshRequestLogs(page)}
          onPageSizeChange={(pageSize) => {
            setAuditPerPage(pageSize);
            void refreshAudit(1, pageSize);
          }}
          onRequestPageSizeChange={(pageSize) => {
            setRequestPerPage(pageSize);
            void refreshRequestLogs(1, pageSize);
          }}
          onCopy={copyText}
          version={healthState?.version ?? "unknown"}
        />
      ) : null}

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
          authSettings={authSettings}
          setAuthSettings={setAuthSettings}
          onSaveAuthSettings={saveAuthSettings}
          projectQuotas={projectQuotas}
          quotaDraft={quotaDraft}
          setQuotaDraft={setQuotaDraft}
          projectMetrics={projectMetrics}
          opsAlerts={opsAlerts}
          onSaveQuotas={saveQuotaSettings}
          onRefreshMetrics={refreshProjectMetrics}
          onResolveOpsAlert={resolveProjectAlert}
          onOpenAuth={() => changeSettings("auth")}
          onOpenMail={() => changeSettings("mail")}
          onOpenFiles={() => changeSettings("files")}
          onOpenMCP={() => changeSettings("mcp")}
          smtpDraft={smtpDraft}
          setSMTPDraft={setSMTPDraft}
          storageDraft={storageDraft}
          setStorageDraft={setStorageDraft}
          corsDraft={corsDraft}
          setCORSDraft={setCORSDraft}
          onSaveCORS={saveCORSSettings}
          admin={admin}
          adminUsers={adminUsers}
          adminDraft={adminDraft}
          setAdminDraft={setAdminDraft}
          oneTimeAdmin={oneTimeAdmin}
          onCreateAdmin={submitAdminUser}
          onDismissAdminSecret={() => setOneTimeAdmin(null)}
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
          restoreFile={restoreFile}
          setRestoreFile={setRestoreFile}
          restoreMode={restoreMode}
          setRestoreMode={setRestoreMode}
          restoreConfirm={restoreConfirm}
          setRestoreConfirm={setRestoreConfirm}
          restoreResult={restoreResult}
          onSubmitRestore={submitRestore}
          webhooks={webhooks}
          webhookDraft={webhookDraft}
          setWebhookDraft={setWebhookDraft}
          webhookDeliveries={webhookDeliveries}
          onCreateWebhook={submitWebhook}
          onDeleteWebhook={removeWebhook}
          onLoadWebhookDeliveries={loadWebhookRuns}
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
          collections={collections}
          token={token}
          project={selectedProject}
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
