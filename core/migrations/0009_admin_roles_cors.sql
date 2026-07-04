-- 0009_admin_roles_cors: owner/super-admin roles and runtime CORS controls.

alter table _dbo.admins
    add column if not exists role text not null default 'super_admin';

update _dbo.admins
set role = 'owner'
where id = (
    select id
    from _dbo.admins
    order by created_at asc, id asc
    limit 1
)
and not exists (
    select 1
    from _dbo.admins
    where role = 'owner'
);

do $$
begin
    if not exists (
        select 1
        from pg_constraint
        where conname = 'admins_role_check'
          and conrelid = '_dbo.admins'::regclass
    ) then
        alter table _dbo.admins
            add constraint admins_role_check check (role in ('owner', 'super_admin'));
    end if;
end $$;

create unique index if not exists admins_single_owner_idx
    on _dbo.admins ((role))
    where role = 'owner';

alter table _dbo.projects
    add column if not exists public_cors_origins text[] null;
