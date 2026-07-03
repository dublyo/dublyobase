-- 0006_file_upload_sessions: durable metadata for resumable local uploads.

create table if not exists _dbo.file_upload_sessions (
    id                 uuid primary key default gen_random_uuid(),
    project_id         uuid not null references _dbo.projects(id) on delete cascade,
    collection         text not null,
    record_id          uuid not null,
    field              text not null,
    file_id            uuid not null default gen_random_uuid(),
    filename           text not null,
    mode               text not null check (mode in ('replace', 'append')),
    total_size         bigint not null check (total_size > 0),
    chunk_size         bigint not null check (chunk_size > 0),
    total_chunks       integer not null check (total_chunks > 0),
    checksum_sha256    text not null default '',
    status             text not null default 'open' check (status in ('open', 'completing', 'completed', 'canceled')),
    creator_role       text not null default '',
    creator_subject    text not null default '',
    creator_collection text not null default '',
    created_at         timestamptz not null default now(),
    updated_at         timestamptz not null default now(),
    expires_at         timestamptz not null,
    completed_at       timestamptz null,
    canceled_at        timestamptz null
);

create index if not exists file_upload_sessions_project_status_idx
    on _dbo.file_upload_sessions(project_id, status, expires_at);

create table if not exists _dbo.file_upload_chunks (
    session_id      uuid not null references _dbo.file_upload_sessions(id) on delete cascade,
    chunk_index     integer not null check (chunk_index >= 0),
    size            bigint not null check (size >= 0),
    checksum_sha256 text not null,
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    primary key (session_id, chunk_index)
);
