-- 0003_collections: project-scoped collection metadata.

create table if not exists _dbo.collections (
    id          uuid primary key default gen_random_uuid(),
    project_id  uuid not null references _dbo.projects(id) on delete cascade,
    name        text not null,
    type        text not null check (type in ('base', 'auth', 'view')),
    system      boolean not null default false,
    fields      jsonb not null default '[]'::jsonb,
    indexes     jsonb not null default '[]'::jsonb,
    list_rule   text null,
    view_rule   text null,
    create_rule text null,
    update_rule text null,
    delete_rule text null,
    options     jsonb not null default '{}'::jsonb,
    created_at  timestamptz not null default now(),
    updated_at  timestamptz not null default now(),
    unique(project_id, name)
);

create index if not exists collections_project_id_idx
    on _dbo.collections(project_id);
