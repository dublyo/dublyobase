import type { APIKey, Admin, ApiEnvelope, AuditEntry, Collection, Health, Project, RecordItem, RecordList } from "./types";

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

export function me(token: string) {
  return request<{ admin: Admin; session: { id: string; expiresAt: string } }>("/admin/api/me", { token });
}

export function health() {
  return request<Health>("/health");
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

export function listRecords(token: string, project: string, collection: string, page = 1, filter = "") {
  const params = new URLSearchParams({ page: String(page), perPage: "30", sort: "-created" });
  if (filter.trim()) params.set("filter", filter.trim());
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
