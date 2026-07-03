export type Admin = {
  id: string;
  email: string;
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
  options?: Record<string, unknown>;
};

export type FieldType =
  | "text"
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

export type ApiEnvelope<T> = {
  items: T[];
  page?: number;
  perPage?: number;
  totalItems?: number;
};
