package core

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgconn"
)

type auditExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// AuditEvent is one control-plane action. It intentionally stores generic
// metadata only; secrets and plaintext tokens must never enter audit rows.
type AuditEvent struct {
	AdminID    *string
	Action     string
	TargetType string
	TargetID   string
	IP         string
	UserAgent  string
	Data       map[string]any
}

func InsertAudit(ctx context.Context, exec auditExecer, event AuditEvent) error {
	data := []byte(`{}`)
	if event.Data != nil {
		encoded, err := json.Marshal(event.Data)
		if err != nil {
			return err
		}
		data = encoded
	}
	_, err := exec.Exec(ctx, `
		insert into _dbo.audit_log (admin_id, action, target_type, target_id, ip, user_agent, data)
		values ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		event.AdminID,
		event.Action,
		event.TargetType,
		event.TargetID,
		event.IP,
		event.UserAgent,
		data,
	)
	return err
}
