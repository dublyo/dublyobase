-- 0007_ops_mcp: native cron jobs, database backups, and scoped MCP access.

create table if not exists _dbo.cron_jobs (
    id                uuid primary key default gen_random_uuid(),
    project_id        uuid null references _dbo.projects(id) on delete cascade,
    name              text not null,
    type              text not null check (type in ('http')),
    schedule          text not null,
    timezone          text not null default 'UTC',
    enabled           boolean not null default true,
    timeout_seconds   integer not null default 30 check (timeout_seconds between 1 and 600),
    retry_count       integer not null default 0 check (retry_count between 0 and 10),
    method            text not null default 'GET',
    url               text not null,
    headers           jsonb not null default '{}'::jsonb,
    body              text not null default '',
    last_run_at       timestamptz null,
    next_run_at       timestamptz null,
    created_by_admin_id uuid null references _dbo.admins(id) on delete set null,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);

create index if not exists cron_jobs_due_idx
    on _dbo.cron_jobs(enabled, next_run_at)
    where enabled and next_run_at is not null;

create table if not exists _dbo.cron_runs (
    id          uuid primary key default gen_random_uuid(),
    job_id      uuid not null references _dbo.cron_jobs(id) on delete cascade,
    status      text not null check (status in ('running', 'success', 'error')),
    attempt     integer not null default 1,
    started_at  timestamptz not null default now(),
    finished_at timestamptz null,
    status_code integer null,
    error       text not null default '',
    output      text not null default ''
);

create index if not exists cron_runs_job_started_idx
    on _dbo.cron_runs(job_id, started_at desc);

create table if not exists _dbo.backup_jobs (
    id                uuid primary key default gen_random_uuid(),
    project_id        uuid null references _dbo.projects(id) on delete cascade,
    name              text not null,
    scope             text not null check (scope in ('full', 'project')),
    schedule          text not null,
    timezone          text not null default 'UTC',
    enabled           boolean not null default true,
    retention_days    integer not null default 14 check (retention_days between 1 and 3650),
    retention_count   integer not null default 10 check (retention_count between 1 and 1000),
    last_run_at       timestamptz null,
    next_run_at       timestamptz null,
    created_by_admin_id uuid null references _dbo.admins(id) on delete set null,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now(),
    constraint backup_project_scope_requires_project check (
        (scope = 'project' and project_id is not null) or
        (scope = 'full' and project_id is null)
    )
);

create index if not exists backup_jobs_due_idx
    on _dbo.backup_jobs(enabled, next_run_at)
    where enabled and next_run_at is not null;

create table if not exists _dbo.backup_runs (
    id          uuid primary key default gen_random_uuid(),
    job_id      uuid not null references _dbo.backup_jobs(id) on delete cascade,
    status      text not null check (status in ('running', 'success', 'error')),
    started_at  timestamptz not null default now(),
    finished_at timestamptz null,
    storage_key text not null default '',
    size_bytes  bigint not null default 0,
    error       text not null default ''
);

create index if not exists backup_runs_job_started_idx
    on _dbo.backup_runs(job_id, started_at desc);

create table if not exists _dbo.mcp_tokens (
    id                uuid primary key default gen_random_uuid(),
    scope             text not null check (scope in ('admin', 'project')),
    project_id        uuid null references _dbo.projects(id) on delete cascade,
    name              text not null,
    token_hash        text unique not null,
    prefix            text not null,
    allowed_tools     text[] not null default '{}',
    created_by_admin_id uuid null references _dbo.admins(id) on delete set null,
    created_at        timestamptz not null default now(),
    expires_at        timestamptz null,
    revoked_at        timestamptz null,
    constraint mcp_project_scope_requires_project check (
        (scope = 'project' and project_id is not null) or
        (scope = 'admin' and project_id is null)
    )
);

create index if not exists mcp_tokens_project_idx
    on _dbo.mcp_tokens(project_id);
