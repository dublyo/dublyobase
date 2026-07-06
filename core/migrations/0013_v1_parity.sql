-- 0013_v1_parity: OAuth runtime, MFA, realtime channels, schema versions and ops alerts.

alter table _dbo.project_auth_settings
    add column if not exists mfa_enabled boolean not null default false,
    add column if not exists mfa_required boolean not null default false;

create table if not exists _dbo.oauth_states (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    provider text not null,
    state_hash text unique not null,
    redirect_url text not null default '',
    code_verifier text not null default '',
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    used_at timestamptz null
);

create index if not exists oauth_states_project_provider_idx
    on _dbo.oauth_states(project_id, provider, created_at desc);

create table if not exists _dbo.oauth_accounts (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    collection text not null default 'users',
    user_id uuid not null,
    provider text not null,
    provider_user_id text not null,
    email text not null default '',
    email_verified boolean not null default false,
    display_name text not null default '',
    avatar_url text not null default '',
    raw_profile jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique(project_id, provider, provider_user_id)
);

create index if not exists oauth_accounts_user_idx
    on _dbo.oauth_accounts(project_id, collection, user_id);

create table if not exists _dbo.mfa_factors (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    collection text not null default 'users',
    user_id uuid not null,
    type text not null check (type in ('totp')),
    name text not null default 'Authenticator app',
    secret_cipher text not null,
    enabled boolean not null default false,
    verified_at timestamptz null,
    last_used_at timestamptz null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index if not exists mfa_factors_user_type_enabled_idx
    on _dbo.mfa_factors(project_id, collection, user_id, type)
    where enabled;

create table if not exists _dbo.mfa_recovery_codes (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    collection text not null default 'users',
    user_id uuid not null,
    code_hash text not null,
    used_at timestamptz null,
    created_at timestamptz not null default now()
);

create index if not exists mfa_recovery_codes_user_idx
    on _dbo.mfa_recovery_codes(project_id, collection, user_id, created_at desc);

create table if not exists _dbo.mfa_challenges (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    collection text not null default 'users',
    user_id uuid not null,
    token_hash text unique not null,
    refresh_family_id uuid not null default gen_random_uuid(),
    ip text not null default '',
    user_agent text not null default '',
    created_at timestamptz not null default now(),
    expires_at timestamptz not null,
    used_at timestamptz null
);

create index if not exists mfa_challenges_user_idx
    on _dbo.mfa_challenges(project_id, collection, user_id, created_at desc);

create table if not exists _dbo.realtime_presence (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    project_slug text not null,
    channel text not null,
    user_id text not null default '',
    session_id text not null,
    state jsonb not null default '{}'::jsonb,
    last_seen_at timestamptz not null default now(),
    expires_at timestamptz not null,
    unique(project_id, channel, session_id)
);

create index if not exists realtime_presence_project_channel_idx
    on _dbo.realtime_presence(project_id, channel, expires_at desc);

create table if not exists _dbo.realtime_broadcasts (
    id bigserial primary key,
    source_id text not null,
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    project_slug text not null,
    channel text not null,
    event text not null,
    user_id text not null default '',
    payload jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);

create index if not exists realtime_broadcasts_project_channel_idx
    on _dbo.realtime_broadcasts(project_id, channel, id desc);
create index if not exists realtime_broadcasts_id_source_idx
    on _dbo.realtime_broadcasts(id, source_id);

create table if not exists _dbo.schema_versions (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    version integer not null,
    label text not null default '',
    snapshot jsonb not null,
    created_by_admin_id uuid null references _dbo.admins(id) on delete set null,
    created_at timestamptz not null default now(),
    unique(project_id, version)
);

create index if not exists schema_versions_project_created_idx
    on _dbo.schema_versions(project_id, created_at desc);

create table if not exists _dbo.ops_alerts (
    id uuid primary key default gen_random_uuid(),
    project_id uuid null references _dbo.projects(id) on delete cascade,
    severity text not null check (severity in ('info', 'warning', 'critical')),
    code text not null,
    message text not null,
    metadata jsonb not null default '{}'::jsonb,
    resolved_at timestamptz null,
    created_at timestamptz not null default now()
);

create index if not exists ops_alerts_project_created_idx
    on _dbo.ops_alerts(project_id, created_at desc);

create table if not exists _dbo.usage_buckets (
    id uuid primary key default gen_random_uuid(),
    project_id uuid not null references _dbo.projects(id) on delete cascade,
    principal_type text not null check (principal_type in ('anon', 'api_key', 'app_user', 'ip')),
    principal_id text not null,
    metric text not null,
    bucket_started_at timestamptz not null,
    count integer not null default 0,
    unique(project_id, principal_type, principal_id, metric, bucket_started_at)
);

create index if not exists usage_buckets_project_metric_idx
    on _dbo.usage_buckets(project_id, metric, bucket_started_at desc);
