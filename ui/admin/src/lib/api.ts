import type { APIKey, Admin, ApiEnvelope, AuditEntry, BackupJob, BackupRun, Collection, CollectionExport, CollectionImportResult, CronJob, CronRun, Health, InstanceSettings, MCPToken, Project, RecordItem, RecordList, SchemaDiscoveryResult, SchemaImportItem, SQLResult } from "./types";

type RequestOptions = RequestInit & {
  token?: string | null;
};

type LoginResponse = {
  token: string;
  expiresAt: string;
  admin: Admin;
};

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");
  if (options.body && !(options.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
  }
  if (options.token) {
    headers.set("Authorization", `Bearer ${options.token}`);
  }

  const response = await fetch(path, {
    ...options,
    headers,
  });

  if (response.status === 204) {
    return undefined as T;
  }

  const text = await response.text();
  const body = text ? safeJSON(text) : {};

  if (!response.ok) {
    const errorBody = body as { error?: string; message?: string };
    throw new ApiError(response.status, errorBody.error ?? "request_failed", errorBody.message ?? response.statusText);
  }

  return body as T;
}

function safeJSON(text: string): unknown {
  try {
    return JSON.parse(text);
  } catch {
    return {};
  }
}

export function login(email: string, password: string) {
  return request<LoginResponse>("/admin/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function setup(email: string, password: string) {
  return request<{ admin: Admin }>("/setup", {
    method: "POST",
    body: JSON.stringify({ email, password }),
  });
}

export function logout(token: string) {
  return request<void>("/admin/api/auth/logout", { method: "POST", token });
}

export function changeAdminPassword(token: string, currentPassword: string, newPassword: string) {
  return request<{ admin: Admin }>("/admin/api/auth/change-password", {
    method: "POST",
    token,
    body: JSON.stringify({ currentPassword, newPassword }),
  });
}

export function me(token: string) {
  return request<{ admin: Admin; session: { id: string; expiresAt: string } }>("/admin/api/me", { token });
}

export function health() {
  return requestHealth();
}

async function requestHealth(): Promise<Health> {
  const response = await fetch("/health", {
    headers: { Accept: "application/json" },
  });
  const text = await response.text();
  const body = text ? safeJSON(text) : {};
  if (!response.ok && response.status !== 503) {
    const errorBody = body as { error?: string; message?: string };
    throw new ApiError(response.status, errorBody.error ?? "request_failed", errorBody.message ?? response.statusText);
  }
  return body as Health;
}

export function getSettings(token: string) {
  return request<InstanceSettings>("/admin/api/settings", { token });
}

export function updateSMTPSettings(
  token: string,
  input: {
    enabled: boolean;
    host: string;
    port: string;
    from: string;
    username: string;
    password?: string;
    clearPassword?: boolean;
  },
) {
  return request<InstanceSettings>("/admin/api/settings/smtp", {
    method: "PUT",
    token,
    body: JSON.stringify(input),
  });
}

export function testSMTPSettings(token: string, to: string) {
  return request<{ status: string }>("/admin/api/settings/smtp/test", {
    method: "POST",
    token,
    body: JSON.stringify({ to }),
  });
}

export function updateStorageSettings(
  token: string,
  input: {
    type: "local" | "s3";
    s3: {
      endpoint: string;
      bucket: string;
      region: string;
      accessKey: string;
      secretKey?: string;
      clearSecretKey?: boolean;
      prefix: string;
      useSSL: boolean;
      forcePathStyle: boolean;
    };
  },
) {
  return request<InstanceSettings>("/admin/api/settings/storage", {
    method: "PUT",
    token,
    body: JSON.stringify(input),
  });
}

export function testStorageSettings(token: string) {
  return request<{ status: string }>("/admin/api/settings/storage/test", {
    method: "POST",
    token,
    body: JSON.stringify({}),
  });
}

export function listProjects(token: string) {
  return request<ApiEnvelope<Project>>("/admin/api/projects", { token }).then(normalizeEnvelope);
}

export function createProject(token: string, input: { slug: string; name: string }) {
  return request<Project>("/admin/api/projects", {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function listCollections(token: string, project: string) {
  return request<ApiEnvelope<Collection>>(`/api/projects/${encodeURIComponent(project)}/collections`, { token }).then(normalizeEnvelope);
}

export function createCollection(token: string, project: string, input: unknown) {
  return request<Collection>(`/api/projects/${encodeURIComponent(project)}/collections`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function updateCollection(token: string, project: string, name: string, input: unknown) {
  return request<Collection>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(name)}`, {
    method: "PATCH",
    token,
    body: JSON.stringify(input),
  });
}

export function deleteCollection(token: string, project: string, name: string) {
  return request<void>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(name)}?confirm=${encodeURIComponent(name)}`, {
    method: "DELETE",
    token,
  });
}

export function exportCollections(token: string, project: string) {
  return request<CollectionExport>(`/admin/api/projects/${encodeURIComponent(project)}/collections/export`, { token });
}

export function importCollections(
  token: string,
  project: string,
  input: {
    items: unknown[];
    mode: "create_missing" | "upsert";
    dryRun: boolean;
    dropMissingFields: boolean;
  },
) {
  return request<CollectionImportResult>(`/admin/api/projects/${encodeURIComponent(project)}/collections/import`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function discoverSchema(token: string, project: string, input: { schema?: string; table?: string } = {}) {
  const params = new URLSearchParams();
  if (input.schema?.trim()) params.set("schema", input.schema.trim());
  if (input.table?.trim()) params.set("table", input.table.trim());
  const suffix = params.toString() ? `?${params}` : "";
  return request<SchemaDiscoveryResult>(`/admin/api/projects/${encodeURIComponent(project)}/schema/discover${suffix}`, { token });
}

export function importSchemaTables(token: string, project: string, input: { items: SchemaImportItem[]; dryRun: boolean }) {
  return request<CollectionImportResult>(`/admin/api/projects/${encodeURIComponent(project)}/schema/import`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function runSQL(token: string, project: string, input: { query: string; maxRows: number }) {
  return request<SQLResult>(`/admin/api/projects/${encodeURIComponent(project)}/sql`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export type RecordListParams = {
  page?: number;
  perPage?: number;
  offset?: number;
  filter?: string;
  search?: string;
  sort?: string;
  fields?: string;
};

export function listRecords(token: string, project: string, collection: string, input: RecordListParams = {}) {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    perPage: String(input.perPage ?? 25),
    sort: input.sort ?? "-created",
  });
  if (typeof input.offset === "number" && Number.isFinite(input.offset)) params.set("offset", String(input.offset));
  if (input.filter?.trim()) params.set("filter", input.filter.trim());
  if (input.search?.trim()) params.set("search", input.search.trim());
  if (input.fields?.trim()) params.set("fields", input.fields.trim());
  return request<RecordList>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records?${params}`, { token }).then(
    normalizeRecordList,
  );
}

export function createRecord(token: string, project: string, collection: string, input: RecordItem) {
  return request<RecordItem>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function updateRecord(token: string, project: string, collection: string, id: string, input: RecordItem) {
  return request<RecordItem>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, {
    method: "PATCH",
    token,
    body: JSON.stringify(input),
  });
}

export function deleteRecord(token: string, project: string, collection: string, id: string) {
  return request<void>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, {
    method: "DELETE",
    token,
  });
}

export function listAPIKeys(token: string, project: string) {
  return request<ApiEnvelope<APIKey>>(`/admin/api/projects/${encodeURIComponent(project)}/api-keys`, { token }).then(normalizeEnvelope);
}

export function createAPIKey(token: string, project: string, input: { name: string; type: "anon" | "service" }) {
  return request<APIKey>(`/admin/api/projects/${encodeURIComponent(project)}/api-keys`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function revokeAPIKey(token: string, project: string, id: string) {
  return request<void>(`/admin/api/projects/${encodeURIComponent(project)}/api-keys/${encodeURIComponent(id)}`, {
    method: "DELETE",
    token,
  });
}

export function listAudit(token: string, project?: string) {
  const params = new URLSearchParams({ page: "1", perPage: "30" });
  if (project) params.set("project", project);
  return request<ApiEnvelope<AuditEntry>>(`/admin/api/audit-log?${params}`, { token }).then(normalizeEnvelope);
}

function normalizeEnvelope<T>(response: ApiEnvelope<T>): ApiEnvelope<T> {
  return {
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  };
}

function normalizeRecordList(response: RecordList): RecordList {
  return {
    ...response,
    items: Array.isArray(response.items) ? response.items : [],
  };
}

export async function uploadFile(token: string, project: string, collection: string, recordId: string, field: string, file: File) {
  const form = new FormData();
  form.set("file", file);
  return request<RecordItem>(`/api/projects/${encodeURIComponent(project)}/files/${encodeURIComponent(collection)}/${encodeURIComponent(recordId)}/${encodeURIComponent(field)}`, {
    method: "POST",
    token,
    body: form,
  });
}

export function createFileToken(token: string, project: string, collection: string, recordId: string, field: string, fileId: string) {
  return request<{ token: string; expiresAt: string }>(
    `/api/projects/${encodeURIComponent(project)}/files/${encodeURIComponent(collection)}/${encodeURIComponent(recordId)}/${encodeURIComponent(field)}/${encodeURIComponent(fileId)}/token`,
    {
      method: "POST",
      token,
      body: JSON.stringify({}),
    },
  );
}

export function listCronJobs(token: string) {
  return request<ApiEnvelope<CronJob>>("/admin/api/cron-jobs", { token }).then(normalizeEnvelope);
}

export function createCronJob(
  token: string,
  input: {
    projectSlug?: string;
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
  },
) {
  return request<CronJob>("/admin/api/cron-jobs", {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function listCronRuns(token: string, id: string) {
  return request<ApiEnvelope<CronRun>>(`/admin/api/cron-jobs/${encodeURIComponent(id)}/runs`, { token }).then(normalizeEnvelope);
}

export function runCronJob(token: string, id: string) {
  return request<CronRun>(`/admin/api/cron-jobs/${encodeURIComponent(id)}/run`, {
    method: "POST",
    token,
    body: JSON.stringify({}),
  });
}

export function listBackupJobs(token: string) {
  return request<ApiEnvelope<BackupJob>>("/admin/api/backups", { token }).then(normalizeEnvelope);
}

export function createBackupJob(
  token: string,
  input: {
    name: string;
    scope: "full" | "project";
    projectSlug?: string;
    schedule: string;
    timezone: string;
    enabled: boolean;
    retentionDays: number;
    retentionCount: number;
  },
) {
  return request<BackupJob>("/admin/api/backups", {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function listBackupRuns(token: string, id: string) {
  return request<ApiEnvelope<BackupRun>>(`/admin/api/backups/${encodeURIComponent(id)}/runs`, { token }).then(normalizeEnvelope);
}

export function runBackupJob(token: string, id: string) {
  return request<BackupRun>(`/admin/api/backups/${encodeURIComponent(id)}/run`, {
    method: "POST",
    token,
    body: JSON.stringify({}),
  });
}

export function listMCPTokens(token: string) {
  return request<ApiEnvelope<MCPToken>>("/admin/api/mcp/tokens", { token }).then(normalizeEnvelope);
}

export function createMCPToken(
  token: string,
  input: {
    scope: "admin" | "project";
    projectSlug?: string;
    name: string;
    allowedTools: string[];
    expiresAt?: string;
  },
) {
  return request<MCPToken>("/admin/api/mcp/tokens", {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function revokeMCPToken(token: string, id: string) {
  return request<void>(`/admin/api/mcp/tokens/${encodeURIComponent(id)}`, {
    method: "DELETE",
    token,
  });
}
