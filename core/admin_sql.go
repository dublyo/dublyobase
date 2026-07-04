package core

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultSQLMaxRows = 250
	maxSQLMaxRows     = 1000
)

type AdminSQLInput struct {
	Query   string `json:"query"`
	MaxRows int    `json:"maxRows"`
}

type AdminSQLColumn struct {
	Name    string `json:"name"`
	TypeOID uint32 `json:"typeOid"`
}

type AdminSQLResult struct {
	Columns      []AdminSQLColumn `json:"columns"`
	Rows         [][]any          `json:"rows"`
	Command      string           `json:"command"`
	AffectedRows int64            `json:"affectedRows"`
	DurationMs   int64            `json:"durationMs"`
	MaxRows      int              `json:"maxRows"`
	Truncated    bool             `json:"truncated"`
	ReadOnly     bool             `json:"readOnly"`
}

func ExecuteAdminSQL(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input AdminSQLInput, ip string, userAgent string) (*AdminSQLResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	query, err := validateAdminSQL(input.Query)
	if err != nil {
		return nil, err
	}
	maxRows := input.MaxRows
	if maxRows < 1 {
		maxRows = defaultSQLMaxRows
	}
	if maxRows > maxSQLMaxRows {
		maxRows = maxSQLMaxRows
	}

	start := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `set local statement_timeout = '15s'`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`set local search_path = %s, public`, quoteIdent(project.SchemaName))); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	fields := rows.FieldDescriptions()
	result := &AdminSQLResult{
		Columns:  make([]AdminSQLColumn, 0, len(fields)),
		Rows:     make([][]any, 0),
		MaxRows:  maxRows,
		ReadOnly: isReadOnlySQL(query),
	}
	for _, field := range fields {
		result.Columns = append(result.Columns, AdminSQLColumn{Name: field.Name, TypeOID: field.DataTypeOID})
	}
	for rows.Next() {
		if len(result.Rows) >= maxRows {
			result.Truncated = true
			break
		}
		values, err := rows.Values()
		if err != nil {
			rows.Close()
			return nil, err
		}
		out := make([]any, len(values))
		for i, value := range values {
			out[i] = sqlConsoleValue(value)
		}
		result.Rows = append(result.Rows, out)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tag := rows.CommandTag()
	result.Command = tag.String()
	result.AffectedRows = tag.RowsAffected()
	result.DurationMs = time.Since(start).Milliseconds()

	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "sql.execute",
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"project":      project.Slug,
			"readOnly":     result.ReadOnly,
			"command":      firstSQLWord(query),
			"durationMs":   result.DurationMs,
			"affectedRows": result.AffectedRows,
			"returnedRows": len(result.Rows),
			"truncated":    result.Truncated,
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

func validateAdminSQL(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("%w: SQL query is required", ErrValidation)
	}
	if strings.ContainsRune(query, '\x00') {
		return "", fmt.Errorf("%w: SQL query contains invalid bytes", ErrValidation)
	}
	if strings.HasPrefix(query, `\`) {
		return "", fmt.Errorf("%w: psql meta commands are not supported", ErrValidation)
	}
	withoutTrailing := strings.TrimSpace(strings.TrimSuffix(query, ";"))
	if strings.Contains(withoutTrailing, ";") {
		return "", fmt.Errorf("%w: run one SQL statement at a time", ErrValidation)
	}
	lower := strings.ToLower(withoutTrailing)
	compact := strings.Join(strings.Fields(lower), " ")
	switch firstSQLWord(compact) {
	case "begin", "commit", "rollback", "savepoint", "release":
		return "", fmt.Errorf("%w: transaction control is managed by Dublyobase", ErrValidation)
	}
	if strings.Contains(compact, "copy ") && strings.Contains(compact, " program") {
		return "", fmt.Errorf("%w: COPY PROGRAM is not allowed from the admin SQL console", ErrValidation)
	}
	if strings.HasPrefix(compact, "set role") || strings.HasPrefix(compact, "reset role") {
		return "", fmt.Errorf("%w: role switching is not allowed from the admin SQL console", ErrValidation)
	}
	return query, nil
}

func sqlConsoleValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return v
	case []byte:
		return string(v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case bool:
		return v
	case int:
		return v
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return uint64(v)
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	case float32:
		return float64(v)
	case float64:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func isReadOnlySQL(query string) bool {
	switch firstSQLWord(query) {
	case "select", "with", "show", "explain", "values":
		return true
	default:
		return false
	}
}

func firstSQLWord(query string) string {
	query = strings.TrimSpace(strings.ToLower(query))
	for _, prefix := range []string{"--", "/*"} {
		if strings.HasPrefix(query, prefix) {
			return ""
		}
	}
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], ";")
}
