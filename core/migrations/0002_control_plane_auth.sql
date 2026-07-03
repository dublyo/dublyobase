-- 0002_control_plane_auth: admin sessions, API keys, audit log and project hardening.

alter table _dbo.admins
    add column if not exists updated_at timestamptz not null default now(),
    add column if not exists disabled_at timestamptz null;

alter table _dbo.projects
    add column if not exists updated_at timestamptz not null default now(),
    add column if not exists disabled_at timestamptz null;

create table if not exists _dbo.admin_sessions (
    id           uuid primary key default gen_random_uuid(),
    admin_id     uuid not null references _dbo.admins(id) on delete cascade,
    token_hash   text unique not null,
    user_agent   text not null default '',
    ip           text not null default '',
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null,
    revoked_at   timestamptz null
);

create index if not exists admin_sessions_admin_id_idx
    on _dbo.admin_sessions(admin_id);

create index if not exists admin_sessions_active_idx
    on _dbo.admin_sessions(token_hash)
    where revoked_at is null;

create table if not exists _dbo.audit_log (
    id          uuid primary key default gen_random_uuid(),
    admin_id    uuid null references _dbo.admins(id) on delete set null,
    action      text not null,
    target_type text not null,
    target_id   text not null default '',
    ip          text not null default '',
    user_agent  text not null default '',
    data        jsonb not null default '{}'::jsonb,
    created_at  timestamptz not null default now()
);

create index if not exists audit_log_created_at_idx
    on _dbo.audit_log(created_at desc);

create index if not exists audit_log_admin_id_idx
    on _dbo.audit_log(admin_id);

create table if not exists _dbo.api_keys (
    id         uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    name       text not null,
    type       text not null check (type in ('anon', 'service')),
    key_hash   text unique not null,
    prefix     text not null,
    created_at timestamptz not null default now(),
    revoked_at timestamptz null
);

create index if not exists api_keys_project_id_idx
    on _dbo.api_keys(project_id);
