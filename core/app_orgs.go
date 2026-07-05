package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	appOrganizationsTable = "_app_organizations"
	appOrgMembersTable    = "_app_organization_members"
	appOrgInvitesTable    = "_app_organization_invitations"

	OrgRoleOwner   = "owner"
	OrgRoleAdmin   = "admin"
	OrgRoleBilling = "billing"
	OrgRoleMember  = "member"
	OrgRoleViewer  = "viewer"
)

type CreateOrganizationInput struct {
	Name     string         `json:"name"`
	Slug     string         `json:"slug"`
	Metadata map[string]any `json:"metadata"`
}

type CreateOrganizationInvitationInput struct {
	Email        string         `json:"email"`
	Role         string         `json:"role"`
	ExpiresHours int            `json:"expiresHours"`
	Metadata     map[string]any `json:"metadata"`
}

type AcceptOrganizationInvitationInput struct {
	Token string `json:"token"`
}

type Organization struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Slug      string         `json:"slug"`
	Role      string         `json:"role,omitempty"`
	Metadata  map[string]any `json:"metadata"`
	CreatedBy string         `json:"createdBy"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type OrganizationMember struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"orgId"`
	UserID    string    `json:"userId"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type OrganizationInvitation struct {
	ID         string         `json:"id"`
	OrgID      string         `json:"orgId"`
	OrgName    string         `json:"orgName,omitempty"`
	Email      string         `json:"email"`
	Role       string         `json:"role"`
	InvitedBy  string         `json:"invitedBy"`
	AcceptedBy string         `json:"acceptedBy,omitempty"`
	Metadata   map[string]any `json:"metadata"`
	ExpiresAt  time.Time      `json:"expiresAt"`
	AcceptedAt *time.Time     `json:"acceptedAt,omitempty"`
	RevokedAt  *time.Time     `json:"revokedAt,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type OrganizationInvitationRequestResult struct {
	Invitation  OrganizationInvitation `json:"invitation"`
	DevToken    string                 `json:"devToken,omitempty"`
	Email       string                 `json:"-"`
	Token       string                 `json:"-"`
	Type        string                 `json:"-"`
	ProjectName string                 `json:"-"`
}

func EnsureProjectSaaSTables(ctx context.Context, pool *pgxpool.Pool, projectSlug string) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := ensureProjectSaaSTablesTx(ctx, tx, project); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func CreateOrganization(ctx context.Context, pool *pgxpool.Pool, project *Project, user *AppUser, input CreateOrganizationInput, ip string, userAgent string, now time.Time) (*Organization, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: organization name is required", ErrValidation)
	}
	if len(name) > 120 {
		return nil, fmt.Errorf("%w: organization name is too long", ErrValidation)
	}
	orgSlug := normalizeOrganizationSlug(input.Slug)
	if orgSlug == "" {
		orgSlug = normalizeOrganizationSlug(name)
	}
	if orgSlug == "" {
		return nil, fmt.Errorf("%w: organization slug is required", ErrValidation)
	}
	rawMetadata, err := encodeOrganizationMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := ensureProjectSaaSTablesTx(ctx, tx, project); err != nil {
		return nil, err
	}
	var org Organization
	var rawOrgMetadata []byte
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		insert into %s (name, slug, metadata, created_by)
		values ($1, $2, $3::jsonb, $4)
		returning id, name, slug, metadata, created_by, created_at, updated_at`,
		quoteIdent(project.SchemaName, appOrganizationsTable),
	), name, orgSlug, rawMetadata, user.ID).Scan(
		&org.ID,
		&org.Name,
		&org.Slug,
		&rawOrgMetadata,
		&org.CreatedBy,
		&org.CreatedAt,
		&org.UpdatedAt,
	); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, fmt.Errorf("%w: organization slug already exists", ErrValidation)
		}
		return nil, err
	}
	org.Metadata = decodeMetadata(rawOrgMetadata)
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		insert into %s (org_id, user_id, role)
		values ($1, $2, $3)`,
		quoteIdent(project.SchemaName, appOrgMembersTable),
	), org.ID, user.ID, OrgRoleOwner); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_org.create",
		TargetType: "organization",
		TargetID:   org.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "userId": user.ID, "slug": org.Slug},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	org.Role = OrgRoleOwner
	return &org, nil
}

func ListOrganizationsForUser(ctx context.Context, pool *pgxpool.Pool, project *Project, user *AppUser) ([]Organization, error) {
	if err := EnsureProjectSaaSTables(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		select o.id, o.name, o.slug, o.metadata, o.created_by, o.created_at, o.updated_at, m.role
		from %s o
		join %s m on m.org_id = o.id
		where m.user_id = $1
		order by o.created_at desc`,
		quoteIdent(project.SchemaName, appOrganizationsTable),
		quoteIdent(project.SchemaName, appOrgMembersTable),
	), user.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	orgs := []Organization{}
	for rows.Next() {
		org, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, *org)
	}
	return orgs, rows.Err()
}

func ListOrganizationMembers(ctx context.Context, pool *pgxpool.Pool, project *Project, user *AppUser, orgID string) ([]OrganizationMember, error) {
	if err := ValidateUUID(orgID); err != nil {
		return nil, err
	}
	if err := EnsureProjectSaaSTables(ctx, pool, project.Slug); err != nil {
		return nil, err
	}
	role, err := getOrganizationRole(ctx, pool, project, orgID, user.ID)
	if err != nil {
		return nil, err
	}
	if !canViewOrganization(role) {
		return nil, ErrForbidden
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(`
		select m.id, m.org_id, m.user_id, u.email, m.role, m.created_at, m.updated_at
		from %s m
		join %s u on u.id = m.user_id
		where m.org_id = $1
		order by m.created_at asc`,
		quoteIdent(project.SchemaName, appOrgMembersTable),
		quoteIdent(project.SchemaName, authUsersCollection),
	), orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := []OrganizationMember{}
	for rows.Next() {
		var member OrganizationMember
		if err := rows.Scan(&member.ID, &member.OrgID, &member.UserID, &member.Email, &member.Role, &member.CreatedAt, &member.UpdatedAt); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func CreateOrganizationInvitation(ctx context.Context, pool *pgxpool.Pool, cfg *Config, project *Project, inviter *AppUser, orgID string, input CreateOrganizationInvitationInput, ip string, userAgent string, now time.Time) (*OrganizationInvitationRequestResult, error) {
	if err := ValidateUUID(orgID); err != nil {
		return nil, err
	}
	email, err := normalizeAppEmail(input.Email)
	if err != nil {
		return nil, err
	}
	role := normalizeOrganizationRole(input.Role)
	if !canInviteRole(role) {
		return nil, fmt.Errorf("%w: invalid invitation role", ErrValidation)
	}
	expiresHours := input.ExpiresHours
	if expiresHours == 0 {
		expiresHours = 168
	}
	if expiresHours < 1 || expiresHours > 2160 {
		return nil, fmt.Errorf("%w: expiresHours must be between 1 and 2160", ErrValidation)
	}
	rawMetadata, err := encodeOrganizationMetadata(input.Metadata)
	if err != nil {
		return nil, err
	}
	token, err := GenerateOrganizationInvitationToken()
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := ensureProjectSaaSTablesTx(ctx, tx, project); err != nil {
		return nil, err
	}
	inviterRole, err := getOrganizationRoleTx(ctx, tx, project, orgID, inviter.ID)
	if err != nil {
		return nil, err
	}
	if !canManageOrganization(inviterRole) {
		return nil, ErrForbidden
	}
	if invitedUser, err := getAppUserByEmailTx(ctx, tx, project, email); err == nil {
		if member, err := getOrganizationRoleTx(ctx, tx, project, orgID, invitedUser.ID); err == nil && member != "" {
			return nil, fmt.Errorf("%w: user is already a member", ErrValidation)
		}
	}
	var invite OrganizationInvitation
	var rawInviteMetadata []byte
	var acceptedAt, revokedAt sql.NullTime
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		insert into %s (org_id, email, role, token_hash, invited_by, metadata, expires_at)
		values ($1, $2, $3, $4, $5, $6::jsonb, $7)
		returning id, org_id, email, role, invited_by, coalesce(accepted_by::text, ''), metadata, expires_at, accepted_at, revoked_at, created_at`,
		quoteIdent(project.SchemaName, appOrgInvitesTable),
	), orgID, email, role, HashToken(token), inviter.ID, rawMetadata, now.UTC().Add(time.Duration(expiresHours)*time.Hour)).Scan(
		&invite.ID,
		&invite.OrgID,
		&invite.Email,
		&invite.Role,
		&invite.InvitedBy,
		&invite.AcceptedBy,
		&rawInviteMetadata,
		&invite.ExpiresAt,
		&acceptedAt,
		&revokedAt,
		&invite.CreatedAt,
	); err != nil {
		return nil, err
	}
	invite.Metadata = decodeMetadata(rawInviteMetadata)
	if acceptedAt.Valid {
		invite.AcceptedAt = &acceptedAt.Time
	}
	if revokedAt.Valid {
		invite.RevokedAt = &revokedAt.Time
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_org.invitation_create",
		TargetType: "organization",
		TargetID:   orgID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "email": email, "role": role, "invitedBy": inviter.ID},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	out := &OrganizationInvitationRequestResult{
		Invitation:  invite,
		Email:       email,
		Token:       token,
		Type:        "org_invitation",
		ProjectName: project.Name,
	}
	if cfg != nil && cfg.AuthDevTokens {
		out.DevToken = token
	}
	return out, nil
}

func AcceptOrganizationInvitation(ctx context.Context, pool *pgxpool.Pool, project *Project, user *AppUser, input AcceptOrganizationInvitationInput, ip string, userAgent string, now time.Time) (*OrganizationMember, error) {
	token := strings.TrimSpace(input.Token)
	if !strings.HasPrefix(token, orgInvitationPrefix) {
		return nil, ErrInvalidAuthToken
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := ensureProjectSaaSTablesTx(ctx, tx, project); err != nil {
		return nil, err
	}
	var invite OrganizationInvitation
	var rawMetadata []byte
	var acceptedAt, revokedAt sql.NullTime
	err = tx.QueryRow(ctx, fmt.Sprintf(`
		select id, org_id, email, role, invited_by, coalesce(accepted_by::text, ''), metadata, expires_at, accepted_at, revoked_at, created_at
		from %s
		where token_hash = $1
		for update`,
		quoteIdent(project.SchemaName, appOrgInvitesTable),
	), HashToken(token)).Scan(
		&invite.ID,
		&invite.OrgID,
		&invite.Email,
		&invite.Role,
		&invite.InvitedBy,
		&invite.AcceptedBy,
		&rawMetadata,
		&invite.ExpiresAt,
		&acceptedAt,
		&revokedAt,
		&invite.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidAuthToken
		}
		return nil, err
	}
	if invite.Email != user.Email || acceptedAt.Valid || revokedAt.Valid || !invite.ExpiresAt.After(now.UTC()) {
		return nil, ErrInvalidAuthToken
	}
	var member OrganizationMember
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		insert into %s (org_id, user_id, role)
		values ($1, $2, $3)
		on conflict (org_id, user_id) do update
		set role = excluded.role,
			updated_at = now()
		returning id, org_id, user_id, $4::text, role, created_at, updated_at`,
		quoteIdent(project.SchemaName, appOrgMembersTable),
	), invite.OrgID, user.ID, invite.Role, user.Email).Scan(
		&member.ID,
		&member.OrgID,
		&member.UserID,
		&member.Email,
		&member.Role,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		update %s
		set accepted_by = $1, accepted_at = $2
		where id = $3`,
		quoteIdent(project.SchemaName, appOrgInvitesTable),
	), user.ID, now.UTC(), invite.ID); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		Action:     "app_org.invitation_accept",
		TargetType: "organization",
		TargetID:   invite.OrgID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "userId": user.ID, "inviteId": invite.ID},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &member, nil
}

func ensureProjectSaaSTablesTx(ctx context.Context, tx pgx.Tx, project *Project) error {
	if _, err := ensureAuthUsersCollectionTx(ctx, tx, project); err != nil {
		return err
	}
	statements := []string{
		fmt.Sprintf(`create table if not exists %s (
			id uuid primary key default gen_random_uuid(),
			name text not null,
			slug text not null,
			metadata jsonb not null default '{}'::jsonb,
			created_by uuid not null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (slug)
		)`, quoteIdent(project.SchemaName, appOrganizationsTable)),
		fmt.Sprintf(`create table if not exists %s (
			id uuid primary key default gen_random_uuid(),
			org_id uuid not null references %s(id) on delete cascade,
			user_id uuid not null references %s(id) on delete cascade,
			role text not null check (role in ('owner', 'admin', 'billing', 'member', 'viewer')),
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			unique (org_id, user_id)
		)`,
			quoteIdent(project.SchemaName, appOrgMembersTable),
			quoteIdent(project.SchemaName, appOrganizationsTable),
			quoteIdent(project.SchemaName, authUsersCollection),
		),
		fmt.Sprintf(`create table if not exists %s (
			id uuid primary key default gen_random_uuid(),
			org_id uuid not null references %s(id) on delete cascade,
			email text not null,
			role text not null check (role in ('admin', 'billing', 'member', 'viewer')),
			token_hash text unique not null,
			invited_by uuid not null references %s(id) on delete cascade,
			accepted_by uuid references %s(id) on delete set null,
			metadata jsonb not null default '{}'::jsonb,
			expires_at timestamptz not null,
			accepted_at timestamptz null,
			revoked_at timestamptz null,
			created_at timestamptz not null default now()
		)`,
			quoteIdent(project.SchemaName, appOrgInvitesTable),
			quoteIdent(project.SchemaName, appOrganizationsTable),
			quoteIdent(project.SchemaName, authUsersCollection),
			quoteIdent(project.SchemaName, authUsersCollection),
		),
		fmt.Sprintf(`create index if not exists %s on %s (org_id, role)`, quoteIdent(appOrgMembersTable+"_org_role_idx"), quoteIdent(project.SchemaName, appOrgMembersTable)),
		fmt.Sprintf(`create index if not exists %s on %s (user_id)`, quoteIdent(appOrgMembersTable+"_user_idx"), quoteIdent(project.SchemaName, appOrgMembersTable)),
		fmt.Sprintf(`create index if not exists %s on %s (email, created_at desc)`, quoteIdent(appOrgInvitesTable+"_email_idx"), quoteIdent(project.SchemaName, appOrgInvitesTable)),
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func getOrganizationRole(ctx context.Context, pool *pgxpool.Pool, project *Project, orgID string, userID string) (string, error) {
	return getOrganizationRoleRow(ctx, pool, project, orgID, userID)
}

func getOrganizationRoleTx(ctx context.Context, tx pgx.Tx, project *Project, orgID string, userID string) (string, error) {
	return getOrganizationRoleRow(ctx, tx, project, orgID, userID)
}

func getOrganizationRoleRow(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, project *Project, orgID string, userID string) (string, error) {
	var role string
	err := q.QueryRow(ctx, fmt.Sprintf(`
		select role
		from %s
		where org_id = $1 and user_id = $2`,
		quoteIdent(project.SchemaName, appOrgMembersTable),
	), orgID, userID).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrForbidden
		}
		return "", err
	}
	return role, nil
}

func scanOrganization(row rowScanner) (*Organization, error) {
	var org Organization
	var rawMetadata []byte
	if err := row.Scan(&org.ID, &org.Name, &org.Slug, &rawMetadata, &org.CreatedBy, &org.CreatedAt, &org.UpdatedAt, &org.Role); err != nil {
		return nil, err
	}
	org.Metadata = decodeMetadata(rawMetadata)
	return &org, nil
}

func canViewOrganization(role string) bool {
	switch role {
	case OrgRoleOwner, OrgRoleAdmin, OrgRoleBilling, OrgRoleMember, OrgRoleViewer:
		return true
	default:
		return false
	}
}

func canManageOrganization(role string) bool {
	return role == OrgRoleOwner || role == OrgRoleAdmin
}

func canInviteRole(role string) bool {
	switch role {
	case OrgRoleAdmin, OrgRoleBilling, OrgRoleMember, OrgRoleViewer:
		return true
	default:
		return false
	}
}

func normalizeOrganizationRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return OrgRoleMember
	}
	return role
}

func normalizeOrganizationSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 80 {
		out = strings.Trim(out[:80], "-")
	}
	return out
}

func encodeOrganizationMetadata(metadata map[string]any) ([]byte, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(redactAuditData(metadata))
	if err != nil {
		return nil, fmt.Errorf("%w: metadata must be JSON serializable", ErrValidation)
	}
	if len(raw) > 16*1024 {
		return nil, fmt.Errorf("%w: metadata is too large", ErrValidation)
	}
	return raw, nil
}

func decodeMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return redactAuditData(out)
}
