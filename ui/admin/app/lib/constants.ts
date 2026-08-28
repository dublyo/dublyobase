import { Activity, Archive, Bell, BookOpen, Boxes, Braces, BriefcaseBusiness, Calendar, CalendarCheck2, CreditCard, Database, Eye, FileText, Folder, Globe, Hash, Image, Layers3, Link2, List, Mail, MapPin, MessageSquare, Package, PencilLine, Settings, Share2, ShieldCheck, ShoppingCart, Star, Table2, Tag, ToggleLeft, Type, User, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { FieldType } from "../../src/lib/types";
import type { CollectionDraft, InsightsRangeHours, RuleDraft } from "./view-types";

// Static choices, defaults and empty form drafts, split out of page.tsx.

export const TOKEN_KEY = "dublyobase.adminToken.v1";

export const SQL_HISTORY_KEY = "dublyobase.sqlHistory.v1";

export const recordPageSizes = [10, 25, 100, 250, 500] as const;

export const fieldTypes: FieldType[] = ["text", "editor", "password", "number", "decimal", "bool", "date", "autodate", "email", "url", "select", "json", "relation", "file"];

export const reservedDataFieldNames = new Set(["cmax", "cmin", "created", "ctid", "id", "information_schema", "oid", "public", "tableoid", "updated", "xmax", "xmin"]);

export type FieldTypeChoice = {
  type?: FieldType;
  label: string;
  icon: LucideIcon;
  disabled?: boolean;
};

export const fieldTypeChoices: FieldTypeChoice[] = [
  { type: "text", label: "Plain text", icon: Type },
  { type: "editor", label: "Rich editor", icon: PencilLine },
  { type: "number", label: "Number", icon: Hash },
  { type: "decimal", label: "Decimal (exact)", icon: Hash },
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

export type CollectionIconChoice = {
  name: string;
  label: string;
  icon: LucideIcon;
};

export const collectionIconChoices: CollectionIconChoice[] = [
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

export const collectionIconMap = Object.fromEntries(collectionIconChoices.map((choice) => [choice.name, choice.icon])) as Record<string, LucideIcon>;

export const navItems = [
  { id: "collections", label: "Collections", icon: Layers3 },
  { id: "insights", label: "Insights", icon: Activity },
  { id: "logs", label: "Logs", icon: Archive },
  { id: "settings", label: "Settings", icon: Settings },
] as const;

export const settingsItems = [
  { id: "application", label: "Application", group: "System" },
  { id: "auth", label: "Auth settings", group: "System" },
  { id: "mail", label: "Mail settings", group: "System" },
  { id: "storage", label: "Files storage", group: "System" },
  { id: "cors", label: "CORS origins", group: "System" },
  { id: "quotas", label: "Quotas and metrics", group: "System" },
  { id: "admins", label: "Admin users", group: "System" },
  { id: "backups", label: "Backups", group: "System" },
  { id: "crons", label: "Crons", group: "System" },
  { id: "webhooks", label: "Webhooks", group: "System" },
  { id: "mcp", label: "MCP access", group: "System" },
  { id: "exportCollections", label: "Export collections", group: "Sync" },
  { id: "importCollections", label: "Import collections", group: "Sync" },
  { id: "discoverTables", label: "Discover tables", group: "Sync" },
  { id: "sqlConsole", label: "SQL console", group: "Debug" },
  { id: "apiKeys", label: "API keys", group: "Project" },
  { id: "files", label: "File uploads", group: "Project" },
] as const;

export const emptyRules: RuleDraft = {
  listRule: "",
  viewRule: "",
  createRule: "",
  updateRule: "",
  deleteRule: "",
};

export const emptyCollectionDraft: CollectionDraft = {
  name: "",
  type: "base",
  icon: { type: "lucide", name: "table" },
  fields: [{ name: "title", type: "text", required: true, options: {} }],
  ...emptyRules,
};

export const emptySMTPDraft = {
  enabled: false,
  host: "",
  port: "587",
  from: "",
  username: "",
  password: "",
  clearPassword: false,
  testTo: "",
};

export const emptyStorageDraft = {
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

export const emptyCORSDraft = {
  adminOrigins: "",
  publicOrigins: "",
  allowAdminWildcard: false,
  allowPublicWildcard: false,
};

export const emptyAdminDraft = {
  email: "",
  temporaryPassword: "",
};

export const emptyCronDraft = {
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

export const emptyBackupDraft = {
  name: "",
  scope: "project" as "full" | "project",
  projectSlug: "",
  schedule: "0 2 * * *",
  timezone: "UTC",
  enabled: true,
  retentionDays: "14",
  retentionCount: "10",
};

export const emptyMCPDraft = {
  name: "",
  scope: "project" as "admin" | "project",
  projectSlug: "",
  allowedTools: "",
  expiresAt: "",
};

export const emptyAuditFilters = {
  search: "",
  action: "",
  target: "",
};

export const emptyRequestFilters = {
  search: "",
  method: "",
  status: "",
};

export const emptyLogDraft = {
  retentionDays: "30",
  retentionCount: "100000",
};

export const emptyQuotaDraft = {
  enabled: false,
  requestsPerMinute: "0",
  authRequestsPerMinute: "0",
  maxAppUsers: "0",
  maxStorageMb: "0",
};

export const emptyWebhookDraft = {
  name: "",
  url: "",
  events: "records.*",
  enabled: true,
  timeoutSeconds: "10",
  maxAttempts: "5",
  secret: "",
};

export const insightRanges: Array<{ hours: InsightsRangeHours; label: string }> = [
  { hours: 1, label: "1h" },
  { hours: 24, label: "24h" },
  { hours: 168, label: "7d" },
  { hours: 720, label: "30d" },
];
