-- 0010_v1_productization: request logs, webhooks, project auth settings and restore jobs.

create table if not exists _dbo.request_logs (
    id uuid primary key default gen_random_uuid(),
    project_id uuid references _dbo.projects(id) on delete set null,
    project_slug text not null default '',
    method text not null,
    path text not null,
    status integer not null,
    duration_ms integer not null default 0,
    ip text not null default '',
    user_agent text not null default '',
    request_id text not null default '',
    error text not null default '',
    metadata jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);

create index if not exists request_logs_created_at_idx on _dbo.request_logs (created_at desc);
create index if not exists request_logs_project_created_idx on _dbo.request_logs (project_id, created_at desc);
create index if not exists request_logs_status_idx on _dbo.request_logs (status);
create index if not exists request_logs_method_idx on _dbo.request_logs (method);

create table if not exists _dbo.project_auth_settings (
    project_id uuid primary key references _dbo.projects(id) on delete cascade,
    access_token_minutes integer not null default 60,
    refresh_token_days integer not null default 7,
    verify_token_hours integer not null default 24,
    reset_token_hours integer not null default 1,
    email_change_enabled boolean not null default true,
    templates jsonb not null default '{}'::jsonb,
    providers jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

alter table _dbo.auth_tokens
    add column if not exists data jsonb not null default '{}'::jsonb;

create table if not exists _dbo.webhooks (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    name text not null,
    url text not null,
    events text[] not null default '{}'::text[],
    enabled boolean not null default true,
    secret_cipher text not null default '',
    timeout_seconds integer not null default 10,
    max_attempts integer not null default 5,
    created_by_admin_id uuid references _dbo.admins(id) on delete set null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (project_id, name)
);

create index if not exists webhooks_project_idx on _dbo.webhooks (project_id, enabled);

create table if not exists _dbo.webhook_deliveries (
    id uuid primary key default gen_random_uuid(),
    webhook_id uuid not null references _dbo.webhooks(id) on delete cascade,
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    event text not null,
    status text not null default 'pending' check (status in ('pending', 'success', 'error')),
    attempts integer not null default 0,
    next_attempt_at timestamptz not null default now(),
    last_status_code integer,
    error text not null default '',
    request_body jsonb not null default '{}'::jsonb,
    response_body text not null default '',
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index if not exists webhook_deliveries_due_idx on _dbo.webhook_deliveries (status, next_attempt_at);
create index if not exists webhook_deliveries_webhook_created_idx on _dbo.webhook_deliveries (webhook_id, created_at desc);
create index if not exists webhook_deliveries_project_created_idx on _dbo.webhook_deliveries (project_id, created_at desc);

create table if not exists _dbo.realtime_events (
    id bigserial primary key,
    source_id text not null,
    project_slug text not null,
    collection text not null,
    action text not null,
    record_id text not null default '',
    record jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);

create index if not exists realtime_events_created_at_idx on _dbo.realtime_events (created_at desc);
create index if not exists realtime_events_id_source_idx on _dbo.realtime_events (id, source_id);

create table if not exists _dbo.restore_jobs (
    id uuid primary key default gen_random_uuid(),
    admin_id uuid references _dbo.admins(id) on delete set null,
    mode text not null check (mode in ('dry_run', 'restore')),
    source text not null default 'upload',
    file_name text not null default '',
    status text not null default 'running' check (status in ('running', 'success', 'error')),
    output text not null default '',
    error text not null default '',
    created_at timestamptz not null default now(),
    finished_at timestamptz
);

create index if not exists restore_jobs_created_at_idx on _dbo.restore_jobs (created_at desc);
