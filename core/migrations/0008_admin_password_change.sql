-- 0008_admin_password_change: force first-login password rotation for seeded bootstrap admins.

alter table _dbo.admins
    add column if not exists must_change_password boolean not null default false;
