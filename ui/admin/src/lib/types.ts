export type Admin = {
  id: string;
  email: string;
  role: "owner" | "super_admin";
  mustChangePassword: boolean;
  createdAt?: string;
  updatedAt?: string;
};

export type Project = {
  id: string;
  slug: string;
  name: string;
  schemaName: string;
  roles?: {
    anon: string;
    authenticated: string;
    service: string;
  };
  cors: ProjectCORSSettings;
};

export type ProjectCORSSettings = {
  publicOrigins: string[];
  source: string;
  wildcard: boolean;
};

export type Field = {
  name: string;
  type: FieldType;
  required?: boolean;
  hidden?: boolean;
  presentable?: boolean;
  searchable?: boolean;
  help?: string;
  options?: Record<string, unknown>;
};

export type FieldType =
  | "autodate"
  | "text"
  | "editor"
  | "password"
  | "number"
  | "bool"
  | "date"
  | "email"
  | "url"
  | "select"
  | "json"
  | "relation"
  | "file";

export type Collection = {
  id: string;
  projectId: string;
  name: string;
  type: "base" | "auth" | "view";
  system: boolean;
  fields: Field[];
  listRule: string | null;
  viewRule: string | null;
  createRule: string | null;
  updateRule: string | null;
  deleteRule: string | null;
  options?: CollectionOptions;
};

export type CollectionSchemaItem = {
  name: string;
  type: Collection["type"];
  system?: boolean;
  fields: Field[];
  listRule: string | null;
  viewRule: string | null;
  createRule: string | null;
  updateRule: string | null;
  deleteRule: string | null;
  options?: CollectionOptions;
};

export type CollectionIconOption =
  | {
      type: "lucide";
      name: string;
    }
  | {
      type: "emoji";
      value: string;
    };

export type CollectionOptions = {
  icon?: CollectionIconOption | string;
  [key: string]: unknown;
};

export type CollectionExport = {
  project: string;
  exportedAt: string;
  items: CollectionSchemaItem[];
};

export type CollectionImportResult = {
  items: Array<{
    name: string;
    action: string;
    status: string;
    message?: string;
  }>;
  created: number;
  updated: number;
  skipped: number;
  dryRun: boolean;
};

export type DiscoveredPrimaryKey = {
  column: string;
  field: string;
  type: string;
};

export type DiscoveredColumn = {
  name: string;
  fieldName?: string;
  dataType: string;
  udtName: string;
  nullable: boolean;
  hasDefault: boolean;
  primaryKey: boolean;
  supported: boolean;
  reason?: string;
};

export type DiscoveredForeignKey = {
  column: string;
  targetSchema: string;
  targetTable: string;
  targetColumn: string;
  onDelete?: string;
};

export type DiscoveredTable = {
  schema: string;
  table: string;
  suggestedName: string;
  existingCollection?: string;
  imported: boolean;
  canImport: boolean;
  canManage: boolean;
  reason?: string;
  primaryKey?: DiscoveredPrimaryKey;
  standardSystemColumns: boolean;
  columns: DiscoveredColumn[];
  fields: Field[];
  foreignKeys: DiscoveredForeignKey[];
};

export type SchemaDiscoveryResult = {
  items: DiscoveredTable[];
};

export type SchemaImportItem = {
  schema: string;
  table: string;
  name?: string;
};

export type SQLResult = {
  columns: Array<{ name: string; typeOid: number }>;
  rows: unknown[][];
  command: string;
  affectedRows: number;
  durationMs: number;
  maxRows: number;
  truncated: boolean;
  readOnly: boolean;
};

export type APIKey = {
  id: string;
  projectId: string;
  name: string;
  type: "anon" | "service";
  prefix: string;
  key?: string;
  createdAt: string;
  revokedAt?: string;
};

export type CronJob = {
  id: string;
  projectId?: string | null;
  name: string;
  type: "http";
  schedule: string;
  timezone: string;
  enabled: boolean;
  timeoutSeconds: number;
  retryCount: number;
  method: string;
  url: string;
  headers: Record<string, string>;
  body: string;
  lastRunAt?: string | null;
  nextRunAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type CronRun = {
  id: string;
  jobId: string;
  status: "running" | "success" | "error";
  attempt: number;
  startedAt: string;
  finishedAt?: string | null;
  statusCode?: number | null;
  error: string;
  output: string;
};

export type BackupJob = {
  id: string;
  projectId?: string | null;
  projectSlug?: string;
  name: string;
  scope: "full" | "project";
  schedule: string;
  timezone: string;
  enabled: boolean;
  retentionDays: number;
  retentionCount: number;
  lastRunAt?: string | null;
  nextRunAt?: string | null;
  createdAt: string;
  updatedAt: string;
};

export type BackupRun = {
  id: string;
  jobId: string;
  status: "running" | "success" | "error";
  startedAt: string;
  finishedAt?: string | null;
  storageKey: string;
  sizeBytes: number;
  error: string;
};

export type RestoreJob = {
  id: string;
  adminId?: string | null;
  mode: "dry_run" | "restore";
  source: string;
  fileName: string;
  status: "running" | "success" | "error";
  output: string;
  error: string;
  createdAt: string;
  finishedAt?: string | null;
};

export type ProjectAuthSettings = {
  projectId: string;
  projectSlug: string;
  accessTokenMinutes: number;
  refreshTokenDays: number;
  verifyTokenHours: number;
  resetTokenHours: number;
  emailChangeEnabled: boolean;
  templates: {
    verifySubject?: string;
    verifyBody?: string;
    resetSubject?: string;
    resetBody?: string;
    emailChangeSubject?: string;
    emailChangeBody?: string;
  };
  providers: Record<string, unknown>;
};

export type Webhook = {
  id: string;
  projectId: string;
  projectSlug: string;
  name: string;
  url: string;
  events: string[];
  enabled: boolean;
  secretSet: boolean;
  secret?: string;
  timeoutSeconds: number;
  maxAttempts: number;
  createdAt: string;
  updatedAt: string;
};

export type WebhookDelivery = {
  id: string;
  webhookId: string;
  projectId: string;
  event: string;
  status: "pending" | "success" | "error";
  attempts: number;
  nextAttemptAt: string;
  lastStatusCode?: number | null;
  error: string;
  requestBody: Record<string, unknown>;
  responseBody: string;
  createdAt: string;
  updatedAt: string;
};

export type MCPToken = {
  id: string;
  scope: "admin" | "project";
  projectId?: string | null;
  projectSlug?: string;
  name: string;
  prefix: string;
  allowedTools: string[];
  createdByAdminId?: string | null;
  createdAt: string;
  expiresAt?: string | null;
  revokedAt?: string | null;
  token?: string;
};

export type RecordItem = Record<string, unknown>;

export type RecordList = {
  items: RecordItem[];
  page: number;
  perPage: number;
  totalItems: number;
};

export type AuditEntry = {
  id: string;
  adminId?: string | null;
  action: string;
  targetType: string;
  targetId: string;
  ip: string;
  userAgent: string;
  data: Record<string, unknown>;
  createdAt: string;
};

export type RequestLogEntry = {
  id: string;
  projectId?: string | null;
  projectSlug: string;
  method: string;
  path: string;
  status: number;
  durationMs: number;
  ip: string;
  userAgent: string;
  requestId: string;
  error: string;
  metadata: Record<string, unknown>;
  createdAt: string;
};

export type Health = {
  status: string;
  db: string;
  storage: string;
  version: string;
};

export type InstanceSettings = {
  smtp: SMTPSettings;
  storage: StorageSettings;
  cors: CORSSettings;
  logs: LogSettings;
};

export type LogSettings = {
  retentionDays: number;
  retentionCount: number;
  source: string;
};

export type CORSSettings = {
  adminOrigins: string[];
  source: string;
  wildcard: boolean;
};

export type SMTPSettings = {
  enabled: boolean;
  host: string;
  port: string;
  username: string;
  passwordSet: boolean;
  from: string;
  source: string;
};

export type StorageSettings = {
  type: "local" | "s3";
  localPath: string;
  source: string;
  s3: S3Settings;
};

export type S3Settings = {
  endpoint: string;
  bucket: string;
  region: string;
  accessKey: string;
  secretKeySet: boolean;
  prefix: string;
  useSSL: boolean;
  forcePathStyle: boolean;
};

export type ApiEnvelope<T> = {
  items: T[];
  page?: number;
  perPage?: number;
  totalItems?: number;
};
