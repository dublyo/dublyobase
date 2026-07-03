-- 0005_app_auth: per-project app user sessions and one-use auth tokens.

create table if not exists _dbo.sessions (
    id           uuid primary key default gen_random_uuid(),
    project_id   uuid not null references _dbo.projects(id) on delete cascade,
    collection   text not null default 'users',
    user_id      uuid not null,
    token_hash   text unique not null,
    family_id    uuid not null,
    user_agent   text not null default '',
    ip           text not null default '',
    created_at   timestamptz not null default now(),
    last_seen_at timestamptz not null default now(),
    expires_at   timestamptz not null,
    rotated_at   timestamptz null,
    revoked_at   timestamptz null,
    replaced_by  uuid null
);

create index if not exists sessions_user_idx
    on _dbo.sessions(project_id, collection, user_id);

create index if not exists sessions_family_idx
    on _dbo.sessions(project_id, family_id);

create index if not exists sessions_active_token_idx
    on _dbo.sessions(token_hash)
    where revoked_at is null;

create table if not exists _dbo.auth_tokens (
    id          uuid primary key default gen_random_uuid(),
    project_id  uuid not null references _dbo.projects(id) on delete cascade,
    collection  text not null default 'users',
    user_id     uuid not null,
    type        text not null check (type in ('verify_email', 'password_reset')),
    token_hash  text unique not null,
    created_at  timestamptz not null default now(),
    expires_at  timestamptz not null,
    used_at     timestamptz null
);

create index if not exists auth_tokens_user_type_idx
    on _dbo.auth_tokens(project_id, collection, user_id, type, created_at desc);

create index if not exists auth_tokens_active_hash_idx
    on _dbo.auth_tokens(token_hash)
    where used_at is null;
