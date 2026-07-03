-- 0001_init: control-plane schema (_dbo)
--
-- Everything dublyobase itself needs lives under the _dbo schema, kept separate
-- from user project schemas. No CREATE DATABASE, no CREATE EXTENSION here, so
-- this runs even when the app role is not a Postgres superuser.
-- gen_random_uuid() is built into Postgres 13+ (no pgcrypto extension needed).

-- Single-row instance settings blob (encrypted fields handled in the app layer).
create table if not exists _dbo.instance_settings (
    id         boolean primary key default true,
    data       jsonb   not null default '{}'::jsonb,
    updated_at timestamptz not null default now(),
    constraint instance_settings_single_row check (id)
);

-- Admin (control-plane) users — distinct from per-project app users.
create table if not exists _dbo.admins (
    id            uuid primary key default gen_random_uuid(),
    email         text unique not null,
    password_hash text not null,
    created_at    timestamptz not null default now()
);

-- Projects: each maps to its own Postgres schema (schema-per-project tenancy).
create table if not exists _dbo.projects (
    id          uuid primary key default gen_random_uuid(),
    slug        text unique not null,
    name        text not null,
    schema_name text unique not null,
    created_at  timestamptz not null default now()
);
