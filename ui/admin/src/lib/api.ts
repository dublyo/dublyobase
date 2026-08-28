import type { APIKey, Admin, ApiEnvelope, AuditEntry, BackupJob, BackupRun, Collection, CollectionExport, CollectionImportResult, CollectionInsights, CronJob, CronRun, Health, InstanceSettings, MCPToken, OpsAlert, Project, ProjectAuthSettings, ProjectInsights, ProjectMetrics, ProjectQuotas, RecordItem, RecordList, RequestLogEntry, RestoreJob, SchemaDiscoveryResult, SchemaImportItem, SQLResult, Webhook, WebhookDelivery } from "./types";

type RequestOptions = RequestInit & {
  token?: string | null;
};

type LoginResponse = {
  token: string;
  expiresAt: string;
  admin: Admin;
};

type ProjectAuthSettingsUpdateInput = {
  accessTokenMinutes?: number;
  refreshTokenDays?: number;
  verifyTokenHours?: number;
  resetTokenHours?: number;
  otpEnabled?: boolean;
  otpTokenMinutes?: number;
  mfaEnabled?: boolean;
  mfaRequired?: boolean;
  emailChangeEnabled?: boolean;
  emailChangeRequiresPassword?: boolean;
  templates?: ProjectAuthSettings["templates"];
  providers?: Record<string, unknown>;
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

export function changeAdminEmail(token: string, currentPassword: string, email: string) {
  return request<{ admin: Admin }>("/admin/api/auth/email", {
    method: "PATCH",
    token,
    body: JSON.stringify({ currentPassword, email }),
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

export function updateCORSSettings(token: string, input: { adminOrigins: string[]; allowWildcard?: boolean }) {
  return request<InstanceSettings>("/admin/api/settings/cors", {
    method: "PUT",
    token,
    body: JSON.stringify(input),
  });
}

export function updateLogSettings(token: string, input: { retentionDays: number; retentionCount: number }) {
  return request<InstanceSettings>("/admin/api/settings/logs", {
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

export function updateProjectCORSSettings(token: string, project: string, input: { publicOrigins: string[]; allowWildcard?: boolean }) {
  return request<Project>(`/admin/api/projects/${encodeURIComponent(project)}/cors`, {
    method: "PUT",
    token,
    body: JSON.stringify(input),
  });
}

export function listAdmins(token: string) {
  return request<ApiEnvelope<Admin>>("/admin/api/admins", { token }).then(normalizeEnvelope);
}

export function createAdmin(token: string, input: { email: string; password: string }) {
  return request<{ admin: Admin }>("/admin/api/admins", {
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
  expand?: string;
  skipTotal?: boolean;
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
  if (input.expand?.trim()) params.set("expand", input.expand.trim());
  if (input.skipTotal) params.set("skipTotal", "true");
  return request<RecordList>(`/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records?${params}`, { token }).then(
    normalizeRecordList,
  );
}

// The export endpoints return a file, not JSON, so they bypass `request` and
// its JSON decoding. The bearer token cannot travel in a plain link, so the
// response is fetched and handed to the browser as a blob.
export async function downloadRecordsCSV(
  token: string,
  project: string,
  collection: string,
  params: Record<string, string>,
  path: "export" | "aggregate/export" = "export",
  format: "csv" | "xlsx" = "csv",
) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value.trim()) query.set(key, value.trim());
  }
  if (format === "xlsx") query.set("format", "xlsx");
  const response = await fetch(
    `/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records/${path}?${query}`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
  if (!response.ok) {
    const errorBody = await response.json().catch(() => ({}));
    throw new ApiError(response.status, errorBody.error ?? "request_failed", errorBody.message ?? response.statusText);
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${collection}-${new Date().toISOString().slice(0, 10)}.${format}`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

export function getRecord(token: string, project: string, collection: string, id: string) {
  return request<RecordItem>(
    `/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`,
    { token },
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

export function listAudit(token: string, input: { project?: string; page?: number; perPage?: number; search?: string; action?: string; target?: string } = {}) {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    perPage: String(input.perPage ?? 30),
  });
  if (input.project) params.set("project", input.project);
  if (input.search?.trim()) params.set("search", input.search.trim());
  if (input.action?.trim()) params.set("action", input.action.trim());
  if (input.target?.trim()) params.set("target", input.target.trim());
  return request<ApiEnvelope<AuditEntry>>(`/admin/api/audit-log?${params}`, { token }).then(normalizeEnvelope);
}

export function listRequestLogs(token: string, input: { project?: string; page?: number; perPage?: number; search?: string; method?: string; status?: number } = {}) {
  const params = new URLSearchParams({
    page: String(input.page ?? 1),
    perPage: String(input.perPage ?? 30),
  });
  if (input.project) params.set("project", input.project);
  if (input.search?.trim()) params.set("search", input.search.trim());
  if (input.method?.trim()) params.set("method", input.method.trim());
  if (input.status) params.set("status", String(input.status));
  return request<ApiEnvelope<RequestLogEntry>>(`/admin/api/request-logs?${params}`, { token }).then(normalizeEnvelope);
}

export function clearAuditLog(token: string) {
  return request<{ deleted: number }>("/admin/api/audit-log", {
    method: "DELETE",
    token,
  });
}

export function clearRequestLogs(token: string) {
  return request<{ deleted: number }>("/admin/api/request-logs", {
    method: "DELETE",
    token,
  });
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

export function backupDownloadURL(id: string, runId: string) {
  return `/admin/api/backups/${encodeURIComponent(id)}/runs/${encodeURIComponent(runId)}/download`;
}

export function restoreBackup(token: string, input: { file: File; mode: "dry_run" | "restore"; confirm?: string }) {
  const form = new FormData();
  form.set("file", input.file);
  form.set("mode", input.mode);
  if (input.confirm) form.set("confirm", input.confirm);
  return request<RestoreJob>("/admin/api/restores", {
    method: "POST",
    token,
    body: form,
  });
}

export function getProjectAuthSettings(token: string, project: string) {
  return request<ProjectAuthSettings>(`/admin/api/projects/${encodeURIComponent(project)}/auth-settings`, { token });
}

export function updateProjectAuthSettings(token: string, project: string, input: Partial<ProjectAuthSettings>) {
  return request<ProjectAuthSettings>(`/admin/api/projects/${encodeURIComponent(project)}/auth-settings`, {
    method: "PUT",
    token,
    body: JSON.stringify(projectAuthSettingsUpdateInput(input)),
  });
}

function projectAuthSettingsUpdateInput(input: Partial<ProjectAuthSettings>): ProjectAuthSettingsUpdateInput {
  return {
    accessTokenMinutes: input.accessTokenMinutes,
    refreshTokenDays: input.refreshTokenDays,
    verifyTokenHours: input.verifyTokenHours,
    resetTokenHours: input.resetTokenHours,
    otpEnabled: input.otpEnabled,
    otpTokenMinutes: input.otpTokenMinutes,
    mfaEnabled: input.mfaEnabled,
    mfaRequired: input.mfaRequired,
    emailChangeEnabled: input.emailChangeEnabled,
    emailChangeRequiresPassword: input.emailChangeRequiresPassword,
    templates: input.templates,
    providers: sanitizeAuthProviderInput(input.providers),
  };
}

function sanitizeAuthProviderInput(input: unknown): Record<string, unknown> | undefined {
  if (!input || typeof input !== "object" || Array.isArray(input)) return undefined;
  const output: Record<string, unknown> = {};
  for (const [provider, value] of Object.entries(input as Record<string, unknown>)) {
    if (!value || typeof value !== "object" || Array.isArray(value)) continue;
    const providerInput: Record<string, unknown> = {};
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
      if (key === "clientSecretSet") continue;
      providerInput[key] = item;
    }
    output[provider] = providerInput;
  }
  return output;
}

export function getProjectQuotas(token: string, project: string) {
  return request<ProjectQuotas>(`/admin/api/projects/${encodeURIComponent(project)}/quotas`, { token });
}

export function updateProjectQuotas(token: string, project: string, input: Partial<ProjectQuotas>) {
  return request<ProjectQuotas>(`/admin/api/projects/${encodeURIComponent(project)}/quotas`, {
    method: "PUT",
    token,
    body: JSON.stringify(input),
  });
}

export function getProjectMetrics(token: string, project: string, hours = 24) {
  return request<ProjectMetrics>(`/admin/api/projects/${encodeURIComponent(project)}/metrics?hours=${encodeURIComponent(String(hours))}`, { token });
}

export function getProjectInsights(token: string, project: string, hours = 24) {
  return request<ProjectInsights>(`/admin/api/projects/${encodeURIComponent(project)}/insights?hours=${encodeURIComponent(String(hours))}`, { token });
}

export function getCollectionInsights(token: string, project: string, collection: string, hours = 24) {
  return request<CollectionInsights>(
    `/admin/api/projects/${encodeURIComponent(project)}/collections/${encodeURIComponent(collection)}/insights?hours=${encodeURIComponent(String(hours))}`,
    { token },
  );
}

export function listOpsAlerts(token: string, project: string, refresh = false) {
  const suffix = refresh ? "?refresh=true" : "";
  return request<ApiEnvelope<OpsAlert>>(`/admin/api/projects/${encodeURIComponent(project)}/ops/alerts${suffix}`, { token }).then(normalizeEnvelope);
}

export function resolveOpsAlert(token: string, project: string, id: string) {
  return request<void>(`/admin/api/projects/${encodeURIComponent(project)}/ops/alerts/${encodeURIComponent(id)}/resolve`, {
    method: "POST",
    token,
  });
}

export function listWebhooks(token: string, project: string) {
  return request<ApiEnvelope<Webhook>>(`/admin/api/projects/${encodeURIComponent(project)}/webhooks`, { token }).then(normalizeEnvelope);
}

export function createWebhook(token: string, project: string, input: { name: string; url: string; events: string[]; enabled: boolean; timeoutSeconds: number; maxAttempts: number; secret?: string }) {
  return request<Webhook>(`/admin/api/projects/${encodeURIComponent(project)}/webhooks`, {
    method: "POST",
    token,
    body: JSON.stringify(input),
  });
}

export function deleteWebhook(token: string, project: string, id: string) {
  return request<void>(`/admin/api/projects/${encodeURIComponent(project)}/webhooks/${encodeURIComponent(id)}`, {
    method: "DELETE",
    token,
  });
}

export function listWebhookDeliveries(token: string, project: string, id: string, limit = 30) {
  return request<ApiEnvelope<WebhookDelivery>>(`/admin/api/projects/${encodeURIComponent(project)}/webhooks/${encodeURIComponent(id)}/deliveries?limit=${limit}`, { token }).then(normalizeEnvelope);
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
