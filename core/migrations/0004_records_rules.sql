-- 0004_records_rules: request-local helpers for record RLS policies.

grant usage on schema _dbo to public;

create or replace function _dbo.request_claim(name text)
returns text
language sql
stable
as $$
    select nullif(coalesce(current_setting('request.jwt.claims', true), '{}'), '')::jsonb ->> name
$$;

create or replace function _dbo.request_role()
returns text
language sql
stable
as $$
    select coalesce(_dbo.request_claim('role'), 'anon')
$$;

create or replace function _dbo.request_auth_id()
returns uuid
language sql
stable
as $$
    select nullif(_dbo.request_claim('sub'), '')::uuid
$$;

create or replace function _dbo.request_operation()
returns text
language sql
stable
as $$
    select nullif(current_setting('request.operation', true), '')
$$;

grant execute on function _dbo.request_claim(text) to public;
grant execute on function _dbo.request_role() to public;
grant execute on function _dbo.request_auth_id() to public;
grant execute on function _dbo.request_operation() to public;
