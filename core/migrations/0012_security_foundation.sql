-- 0012_security_foundation: app auth hardening, SaaS org primitives and quotas.

alter table _dbo.project_auth_settings
    add column if not exists otp_enabled boolean not null default true,
    add column if not exists otp_token_minutes integer not null default 10,
    add column if not exists email_change_requires_password boolean not null default true;

alter table _dbo.sessions
    add column if not exists device_name text not null default '';

alter table _dbo.auth_tokens
    drop constraint if exists auth_tokens_type_check;

alter table _dbo.auth_tokens
    add constraint auth_tokens_type_check
    check (type in ('verify_email', 'password_reset', 'email_change', 'login_otp', 'org_invitation'));

create table if not exists _dbo.project_quotas (
    project_id uuid primary key references _dbo.projects(id) on delete cascade,
    enabled boolean not null default false,
    requests_per_minute integer not null default 0,
    auth_requests_per_minute integer not null default 0,
    max_app_users integer not null default 0,
    max_storage_mb integer not null default 0,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table if not exists _dbo.project_usage_snapshots (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    app_users integer not null default 0,
    active_sessions integer not null default 0,
    organizations integer not null default 0,
    storage_bytes bigint not null default 0,
    request_count integer not null default 0,
    error_count integer not null default 0,
    window_started_at timestamptz not null,
    window_ended_at timestamptz not null,
    created_at timestamptz not null default now()
);

create index if not exists project_usage_snapshots_project_created_idx
    on _dbo.project_usage_snapshots(project_id, created_at desc);
