package core

import (
	"context"
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
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
	case [16]byte:
		return formatUUIDBytes(v)
	case pgtype.Numeric:
		return formatExactNumeric(v)
	case *pgtype.Numeric:
		if v == nil {
			return nil
		}
		return formatExactNumeric(*v)
	case pgtype.Interval:
		return formatSQLInterval(v)
	}

	// Anything left is a driver type or a container. Rendering those with
	// fmt.Sprint produced Go debug output — a uuid came back as a byte array,
	// numeric as "{2885000 -3 false finite true}", and jsonb as "map[a:1]" —
	// so containers are walked and everything else is asked for its own text.
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = sqlConsoleValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, key := range rv.MapKeys() {
			out[fmt.Sprint(key.Interface())] = sqlConsoleValue(rv.MapIndex(key).Interface())
		}
		return out
	case reflect.Pointer:
		if rv.IsNil() {
			return nil
		}
		return sqlConsoleValue(rv.Elem().Interface())
	}
	if t, ok := value.(encoding.TextMarshaler); ok {
		if text, err := t.MarshalText(); err == nil {
			return string(text)
		}
	}
	if s, ok := value.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprint(value)
}

// formatSQLInterval renders an interval the way Postgres writes one, so the
// value can be pasted back into a query.
func formatSQLInterval(v pgtype.Interval) any {
	if !v.Valid {
		return nil
	}
	parts := make([]string, 0, 3)
	if years := v.Months / 12; years != 0 {
		parts = append(parts, fmt.Sprintf("%d year%s", years, plural(int(years))))
	}
	if months := v.Months % 12; months != 0 {
		parts = append(parts, fmt.Sprintf("%d mon%s", months, plural(int(months))))
	}
	if v.Days != 0 {
		parts = append(parts, fmt.Sprintf("%d day%s", v.Days, plural(int(v.Days))))
	}
	if v.Microseconds != 0 {
		d := time.Duration(v.Microseconds) * time.Microsecond
		neg := ""
		if d < 0 {
			neg, d = "-", -d
		}
		hours := int64(d / time.Hour)
		d -= time.Duration(hours) * time.Hour
		minutes := int64(d / time.Minute)
		d -= time.Duration(minutes) * time.Minute
		seconds := float64(d) / float64(time.Second)
		parts = append(parts, fmt.Sprintf("%s%02d:%02d:%s", neg, hours, minutes, trimSeconds(seconds)))
	}
	if len(parts) == 0 {
		return "00:00:00"
	}
	return strings.Join(parts, " ")
}

func plural(n int) string {
	if n == 1 || n == -1 {
		return ""
	}
	return "s"
}

func trimSeconds(seconds float64) string {
	out := strconv.FormatFloat(seconds, 'f', -1, 64)
	if !strings.Contains(out, ".") && len(out) < 2 {
		return "0" + out
	}
	if idx := strings.Index(out, "."); idx == 1 {
		return "0" + out
	}
	return out
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
