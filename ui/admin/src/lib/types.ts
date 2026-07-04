export type Admin = {
  id: string;
  email: string;
  mustChangePassword: boolean;
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
};

export type Field = {
  name: string;
  type: FieldType;
  required?: boolean;
  hidden?: boolean;
  presentable?: boolean;
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
  options?: unknown;
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
  options?: unknown;
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

export type Health = {
  status: string;
  db: string;
  storage: string;
  version: string;
};

export type InstanceSettings = {
  smtp: SMTPSettings;
  storage: StorageSettings;
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
