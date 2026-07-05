-- 0011_email_change_auth_tokens: allow app-user email change auth tokens.

alter table _dbo.auth_tokens
    drop constraint if exists auth_tokens_type_check;

alter table _dbo.auth_tokens
    add constraint auth_tokens_type_check
    check (type in ('verify_email', 'password_reset', 'email_change'));
