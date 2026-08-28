"use client";

import { exportCollections, importCollections } from "../../src/lib/api";
import type { APIKey, Admin, BackupJob, BackupRun, Collection, CollectionExport, CollectionImportResult, CronJob, CronRun, DiscoveredTable, Field, Health, InstanceSettings, MCPToken, OpsAlert, Project, ProjectAuthSettings, ProjectMetrics, ProjectQuotas, RecordItem, RecordList, RestoreJob, SQLResult, Webhook, WebhookDelivery } from "../../src/lib/types";
import { emptyAdminDraft, emptyBackupDraft, emptyCORSDraft, emptyCronDraft, emptyMCPDraft, emptyQuotaDraft, emptySMTPDraft, emptyStorageDraft, emptyWebhookDraft, settingsItems } from "../lib/constants";
import type { SettingsSection } from "../lib/view-types";
import { APIKeysView, BackupsView, CronsView, DiscoverTablesView, ExportCollectionsView, FilesView, ImportCollectionsView, MCPAccessView, SQLConsoleView, WebhooksView } from "./ops-views";
import { AdminUsersPanel, ApplicationSettings, AuthSettingsPanel, CORSSettingsPanel, MailSettings, QuotasSettingsPanel, StorageSettingsPanel } from "./settings-panels";
import { PageFooter } from "./ui";
import { Activity, Archive, Code2, Database, FileUp, Globe, HardDrive, KeyRound, Mail, Settings, ShieldCheck, UploadCloud, Users } from "lucide-react";

export function SettingsWorkspace(props: {
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
  authSettings: ProjectAuthSettings | null;
  setAuthSettings: React.Dispatch<React.SetStateAction<ProjectAuthSettings | null>>;
  onSaveAuthSettings: (settings: ProjectAuthSettings) => void;
  projectQuotas: ProjectQuotas | null;
  quotaDraft: typeof emptyQuotaDraft;
  setQuotaDraft: React.Dispatch<React.SetStateAction<typeof emptyQuotaDraft>>;
  projectMetrics: ProjectMetrics | null;
  opsAlerts: OpsAlert[];
  onSaveQuotas: (event: React.FormEvent<HTMLFormElement>) => void;
  onRefreshMetrics: () => void;
  onResolveOpsAlert: (id: string) => void;
  onOpenAuth: () => void;
  onOpenMail: () => void;
  onOpenFiles: () => void;
  onOpenMCP: () => void;
  smtpDraft: typeof emptySMTPDraft;
  setSMTPDraft: React.Dispatch<React.SetStateAction<typeof emptySMTPDraft>>;
  storageDraft: typeof emptyStorageDraft;
  setStorageDraft: React.Dispatch<React.SetStateAction<typeof emptyStorageDraft>>;
  corsDraft: typeof emptyCORSDraft;
  setCORSDraft: React.Dispatch<React.SetStateAction<typeof emptyCORSDraft>>;
  onSaveCORS: (event: React.FormEvent<HTMLFormElement>) => void;
  admin: Admin | null;
  adminUsers: Admin[];
  adminDraft: typeof emptyAdminDraft;
  setAdminDraft: React.Dispatch<React.SetStateAction<typeof emptyAdminDraft>>;
  oneTimeAdmin: { email: string; password: string } | null;
  onCreateAdmin: (event: React.FormEvent<HTMLFormElement>) => void;
  onDismissAdminSecret: () => void;
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
  onEditCron: (job: CronJob) => void;
  onCancelCronEdit: () => void;
  onToggleCron: (job: CronJob) => void;
  onDeleteCron: (job: CronJob) => void;
  editingCronId: string | null;
  backupJobs: BackupJob[];
  backupRuns: Record<string, BackupRun[]>;
  backupDraft: typeof emptyBackupDraft;
  setBackupDraft: React.Dispatch<React.SetStateAction<typeof emptyBackupDraft>>;
  onCreateBackup: (event: React.FormEvent<HTMLFormElement>) => void;
  onRunBackup: (job: BackupJob) => void;
  onLoadBackupRuns: (job: BackupJob) => void;
  restoreFile: File | null;
  setRestoreFile: React.Dispatch<React.SetStateAction<File | null>>;
  restoreMode: "dry_run" | "restore";
  setRestoreMode: React.Dispatch<React.SetStateAction<"dry_run" | "restore">>;
  restoreConfirm: string;
  setRestoreConfirm: React.Dispatch<React.SetStateAction<string>>;
  restoreResult: RestoreJob | null;
  onSubmitRestore: (event: React.FormEvent<HTMLFormElement>) => void;
  webhooks: Webhook[];
  webhookDraft: typeof emptyWebhookDraft;
  setWebhookDraft: React.Dispatch<React.SetStateAction<typeof emptyWebhookDraft>>;
  webhookDeliveries: Record<string, WebhookDelivery[]>;
  onCreateWebhook: (event: React.FormEvent<HTMLFormElement>) => void;
  onDeleteWebhook: (hook: Webhook) => void;
  onLoadWebhookDeliveries: (hook: Webhook) => void;
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
          {props.section === "cors" ? <CORSSettingsPanel {...props} /> : null}
          {props.section === "quotas" ? <QuotasSettingsPanel {...props} /> : null}
          {props.section === "admins" ? <AdminUsersPanel {...props} /> : null}
          {props.section === "backups" ? <BackupsView {...props} onOpenExport={() => props.onChangeSection("exportCollections")} /> : null}
          {props.section === "crons" ? <CronsView {...props} /> : null}
          {props.section === "webhooks" ? <WebhooksView {...props} /> : null}
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

export function SettingsSidebar({ active, onChange }: { active: SettingsSection; onChange: (section: SettingsSection) => void }) {
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

export function SettingsIcon({ id }: { id: SettingsSection }) {
  if (id === "auth") return <ShieldCheck className="h-4 w-4" />;
  if (id === "mail") return <Mail className="h-4 w-4" />;
  if (id === "storage") return <HardDrive className="h-4 w-4" />;
  if (id === "cors") return <Globe className="h-4 w-4" />;
  if (id === "quotas") return <Activity className="h-4 w-4" />;
  if (id === "admins") return <Users className="h-4 w-4" />;
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
