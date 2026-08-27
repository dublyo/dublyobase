-- 0014_org_scoped_rules: expose the caller's active organization to record
-- policies so multi-tenant rules can be written as `org = @request.auth.orgId`.
--
-- request_org_id() returns uuid (matching an org relation column) rather than
-- text, for the same reason request_auth_id() does: comparing a uuid column to
-- a text claim has no operator and would be rejected at CREATE POLICY time.

create or replace function _dbo.request_org_id()
returns uuid
language sql
stable
as $$
    select nullif(_dbo.request_claim('org'), '')::uuid
$$;

create or replace function _dbo.request_org_role()
returns text
language sql
stable
as $$
    select _dbo.request_claim('org_role')
$$;

grant execute on function _dbo.request_org_id() to public;
grant execute on function _dbo.request_org_role() to public;
