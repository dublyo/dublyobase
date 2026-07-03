package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Record map[string]any

type RecordListOptions struct {
	Page    int
	PerPage int
	Sort    string
	Filter  string
	Fields  string
}

type RecordListResult struct {
	Items      []Record `json:"items"`
	Page       int      `json:"page"`
	PerPage    int      `json:"perPage"`
	TotalItems int      `json:"totalItems"`
}

type normalizedPayload struct {
	Columns []string
	Values  []any
}

func ListRecords(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, opts RecordListOptions) (*RecordListResult, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	opts = normalizeListOptions(opts)
	columns, err := projectionColumns(collection, opts.Fields)
	if err != nil {
		return nil, err
	}
	orderBy, err := orderByClause(collection, opts.Sort)
	if err != nil {
		return nil, err
	}
	filter, err := CompileFilter(opts.Filter, collection)
	if err != nil {
		return nil, err
	}
	table := quoteIdent(auth.Project.SchemaName, collection.Name)
	where := ""
	if filter.SQL != "" {
		where = " where " + filter.SQL
	}

	result := &RecordListResult{Page: opts.Page, PerPage: opts.PerPage}
	err = withRecordTx(ctx, pool, auth, "list", func(tx pgx.Tx) error {
		var total int
		countSQL := fmt.Sprintf(`select count(*) from %s%s`, table, where)
		if err := tx.QueryRow(ctx, countSQL, filter.Args...).Scan(&total); err != nil {
			return mapRecordDBError(err)
		}
		result.TotalItems = total

		args := append([]any{}, filter.Args...)
		limitPos := len(args) + 1
		args = append(args, opts.PerPage)
		offsetPos := len(args) + 1
		args = append(args, (opts.Page-1)*opts.PerPage)
		query := fmt.Sprintf(`select %s from %s%s order by %s limit $%d offset $%d`,
			selectList(columns),
			table,
			where,
			orderBy,
			limitPos,
			offsetPos,
		)
		records, err := queryRecords(ctx, tx, query, columns, args...)
		if err != nil {
			return err
		}
		result.Items = records
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func CreateRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, raw map[string]json.RawMessage) (Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	payload, err := normalizeCreatePayload(collection, raw)
	if err != nil {
		return nil, err
	}
	columns := allRecordColumns(collection)
	table := quoteIdent(auth.Project.SchemaName, collection.Name)

	var out Record
	err = withRecordTx(ctx, pool, auth, "create", func(tx pgx.Tx) error {
		var query string
		if len(payload.Columns) == 0 {
			query = fmt.Sprintf(`insert into %s default values returning %s`, table, selectList(columns))
		} else {
			query = fmt.Sprintf(`insert into %s (%s) values (%s) returning %s`,
				table,
				selectList(payload.Columns),
				valuePlaceholders(payload.Columns, fieldByName(collection.Fields), 1),
				selectList(columns),
			)
		}
		record, err := queryOneRecord(ctx, tx, query, columns, payload.Values...)
		if err != nil {
			return err
		}
		out = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func GetRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string) (Record, error) {
	if err := ValidateUUID(id); err != nil {
		return nil, err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	columns := allRecordColumns(collection)
	table := quoteIdent(auth.Project.SchemaName, collection.Name)

	var out Record
	err = withRecordTx(ctx, pool, auth, "view", func(tx pgx.Tx) error {
		query := fmt.Sprintf(`select %s from %s where id = $1`, selectList(columns), table)
		record, err := queryOneRecord(ctx, tx, query, columns, id)
		if err != nil {
			return err
		}
		out = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func UpdateRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string, raw map[string]json.RawMessage) (Record, error) {
	if err := ValidateUUID(id); err != nil {
		return nil, err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	payload, err := normalizePatchPayload(collection, raw)
	if err != nil {
		return nil, err
	}
	columns := allRecordColumns(collection)
	table := quoteIdent(auth.Project.SchemaName, collection.Name)

	var out Record
	err = withRecordTx(ctx, pool, auth, "update", func(tx pgx.Tx) error {
		assignments := make([]string, 0, len(payload.Columns)+1)
		args := make([]any, 0, len(payload.Values)+1)
		fields := fieldByName(collection.Fields)
		for i, column := range payload.Columns {
			assignments = append(assignments, fmt.Sprintf(`%s = %s`, quoteIdent(column), valuePlaceholder(fields[column], i+1)))
			args = append(args, payload.Values[i])
		}
		assignments = append(assignments, `updated = now()`)
		args = append(args, id)
		query := fmt.Sprintf(`update %s set %s where id = $%d returning %s`,
			table,
			strings.Join(assignments, ", "),
			len(args),
			selectList(columns),
		)
		record, err := queryOneRecord(ctx, tx, query, columns, args...)
		if err != nil {
			return err
		}
		out = record
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func DeleteRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string) error {
	if err := ValidateUUID(id); err != nil {
		return err
	}
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return err
	}
	table := quoteIdent(auth.Project.SchemaName, collection.Name)
	return withRecordTx(ctx, pool, auth, "delete", func(tx pgx.Tx) error {
		var deleted string
		err := tx.QueryRow(ctx, fmt.Sprintf(`delete from %s where id = $1 returning id`, table), id).Scan(&deleted)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordNotFound
		}
		return mapRecordDBError(err)
	})
}

func recordCollection(ctx context.Context, pool *pgxpool.Pool, projectSlug string, name string) (*Collection, error) {
	collection, err := GetCollection(ctx, pool, projectSlug, name)
	if err != nil {
		return nil, err
	}
	if collection.Type == CollectionView {
		return nil, ErrNotImplemented
	}
	if collection.Type != CollectionBase && collection.Type != CollectionAuth {
		return nil, fmt.Errorf("%w: unsupported collection type", ErrValidation)
	}
	return collection, nil
}

func normalizeListOptions(opts RecordListOptions) RecordListOptions {
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PerPage < 1 {
		opts.PerPage = 30
	}
	if opts.PerPage > 500 {
		opts.PerPage = 500
	}
	return opts
}

func allRecordColumns(collection *Collection) []string {
	columns := []string{"id", "created", "updated"}
	for _, field := range collection.Fields {
		columns = append(columns, field.Name)
	}
	return columns
}

func projectionColumns(collection *Collection, projection string) ([]string, error) {
	if strings.TrimSpace(projection) == "" {
		return allRecordColumns(collection), nil
	}
	allowed := allowedRecordColumns(collection)
	seen := map[string]struct{}{}
	var columns []string
	for _, part := range strings.Split(projection, ",") {
		name := NormalizeIdentifier(part)
		if name == "" {
			continue
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("%w: unknown projection field %q", ErrValidation, name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		columns = append(columns, name)
	}
	if len(columns) == 0 {
		return nil, fmt.Errorf("%w: fields projection is empty", ErrValidation)
	}
	return columns, nil
}

func orderByClause(collection *Collection, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return quoteIdent("created") + " desc, " + quoteIdent("id") + " desc", nil
	}
	allowed := allowedRecordColumns(collection)
	seen := map[string]struct{}{}
	var parts []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		dir := "asc"
		if strings.HasPrefix(part, "-") {
			dir = "desc"
			part = strings.TrimPrefix(part, "-")
		}
		name := NormalizeIdentifier(part)
		if _, ok := allowed[name]; !ok {
			return "", fmt.Errorf("%w: unknown sort field %q", ErrValidation, name)
		}
		key := name + ":" + dir
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		parts = append(parts, quoteIdent(name)+" "+dir)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%w: sort is empty", ErrValidation)
	}
	return strings.Join(parts, ", ") + ", " + quoteIdent("id") + " asc", nil
}

func allowedRecordColumns(collection *Collection) map[string]struct{} {
	out := map[string]struct{}{"id": {}, "created": {}, "updated": {}}
	for _, field := range collection.Fields {
		out[field.Name] = struct{}{}
	}
	return out
}

func normalizeCreatePayload(collection *Collection, raw map[string]json.RawMessage) (*normalizedPayload, error) {
	payload, err := normalizeRecordPayload(collection, raw)
	if err != nil {
		return nil, err
	}
	provided := map[string]struct{}{}
	for _, column := range payload.Columns {
		provided[column] = struct{}{}
	}
	for _, field := range collection.Fields {
		if _, ok := provided[field.Name]; ok {
			continue
		}
		if fieldRequiredOnCreate(field) {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
		}
	}
	return payload, nil
}

func normalizePatchPayload(collection *Collection, raw map[string]json.RawMessage) (*normalizedPayload, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: patch body is empty", ErrValidation)
	}
	return normalizeRecordPayload(collection, raw)
}

func normalizeRecordPayload(collection *Collection, raw map[string]json.RawMessage) (*normalizedPayload, error) {
	fields := fieldByName(collection.Fields)
	columns := make([]string, 0, len(raw))
	valuesByColumn := map[string]json.RawMessage{}
	for name, body := range raw {
		name = NormalizeIdentifier(name)
		if _, exists := valuesByColumn[name]; exists {
			return nil, fmt.Errorf("%w: duplicate field %q", ErrValidation, name)
		}
		if name == "id" || name == "created" || name == "updated" {
			return nil, fmt.Errorf("%w: system field %q cannot be written", ErrValidation, name)
		}
		field, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("%w: unknown field %q", ErrValidation, name)
		}
		if _, err := normalizeRecordValue(field, body); err != nil {
			return nil, err
		}
		valuesByColumn[name] = body
		columns = append(columns, name)
	}
	sort.SliceStable(columns, func(i, j int) bool { return columns[i] < columns[j] })
	sortedValues := make([]any, 0, len(columns))
	for _, column := range columns {
		value, err := normalizeRecordValue(fields[column], valuesByColumn[column])
		if err != nil {
			return nil, err
		}
		sortedValues = append(sortedValues, value)
	}
	return &normalizedPayload{Columns: columns, Values: sortedValues}, nil
}

func normalizeRecordValue(field Field, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		if fieldCanBeNull(field) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: field %q cannot be null", ErrValidation, field.Name)
	}
	switch field.Type {
	case "text":
		return decodeStringField(field, raw)
	case "email":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		if _, err := mail.ParseAddress(v); err != nil {
			return nil, fmt.Errorf("%w: field %q must be an email", ErrValidation, field.Name)
		}
		return v, nil
	case "url":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		u, err := url.ParseRequestURI(v)
		if err != nil || u.Scheme == "" {
			return nil, fmt.Errorf("%w: field %q must be a URL", ErrValidation, field.Name)
		}
		return v, nil
	case "number":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%w: field %q must be a number", ErrValidation, field.Name)
		}
		return v, nil
	case "bool":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a boolean", ErrValidation, field.Name)
		}
		return v, nil
	case "date":
		s, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("%w: field %q must be RFC3339", ErrValidation, field.Name)
		}
		return t, nil
	case "json":
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: field %q must be valid JSON", ErrValidation, field.Name)
		}
		return string(raw), nil
	case "select":
		return normalizeSelectValue(field, raw)
	case "relation":
		return normalizeRelationValue(field, raw)
	default:
		return nil, fmt.Errorf("%w: unsupported field type %q", ErrValidation, field.Type)
	}
}

func decodeStringField(field Field, raw json.RawMessage) (string, error) {
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("%w: field %q must be a string", ErrValidation, field.Name)
	}
	return v, nil
}

func normalizeSelectValue(field Field, raw json.RawMessage) (any, error) {
	allowed := map[string]struct{}{}
	for _, value := range stringSlice(field.Options["values"]) {
		allowed[value] = struct{}{}
	}
	if boolOption(field.Options, "multi") {
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a string array", ErrValidation, field.Name)
		}
		for _, value := range values {
			if _, ok := allowed[value]; !ok {
				return nil, fmt.Errorf("%w: field %q has unsupported select value", ErrValidation, field.Name)
			}
		}
		return values, nil
	}
	value, err := decodeStringField(field, raw)
	if err != nil {
		return nil, err
	}
	if _, ok := allowed[value]; !ok {
		return nil, fmt.Errorf("%w: field %q has unsupported select value", ErrValidation, field.Name)
	}
	return value, nil
}

func normalizeRelationValue(field Field, raw json.RawMessage) (any, error) {
	if boolOption(field.Options, "multi") {
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a UUID array", ErrValidation, field.Name)
		}
		for _, value := range values {
			if err := ValidateUUID(value); err != nil {
				return nil, fmt.Errorf("%w: field %q must contain UUIDs", ErrValidation, field.Name)
			}
		}
		return values, nil
	}
	value, err := decodeStringField(field, raw)
	if err != nil {
		return nil, err
	}
	if err := ValidateUUID(value); err != nil {
		return nil, fmt.Errorf("%w: field %q must be a UUID", ErrValidation, field.Name)
	}
	return value, nil
}

func fieldRequiredOnCreate(field Field) bool {
	if !field.Required {
		return false
	}
	return field.Type != "json"
}

func fieldCanBeNull(field Field) bool {
	if field.Required {
		return false
	}
	return field.Type != "bool" && field.Type != "json"
}

func selectList(columns []string) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		parts[i] = quoteIdent(column)
	}
	return strings.Join(parts, ", ")
}

func valuePlaceholders(columns []string, fields map[string]Field, start int) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		parts[i] = valuePlaceholder(fields[column], start+i)
	}
	return strings.Join(parts, ", ")
}

func valuePlaceholder(field Field, pos int) string {
	p := fmt.Sprintf("$%d", pos)
	switch field.Type {
	case "number":
		return p + "::double precision"
	case "bool":
		return p + "::boolean"
	case "date":
		return p + "::timestamptz"
	case "json":
		return p + "::jsonb"
	case "select":
		if boolOption(field.Options, "multi") {
			return p + "::text[]"
		}
		return p + "::text"
	case "relation":
		if boolOption(field.Options, "multi") {
			return p + "::uuid[]"
		}
		return p + "::uuid"
	default:
		return p + "::text"
	}
}

func queryOneRecord(ctx context.Context, tx pgx.Tx, query string, columns []string, args ...any) (Record, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, mapRecordDBError(err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, mapRecordDBError(err)
		}
		return nil, ErrRecordNotFound
	}
	record, err := scanRecordValues(rows, columns)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, fmt.Errorf("%w: expected one record", ErrValidation)
	}
	return record, mapRecordDBError(rows.Err())
}

func queryRecords(ctx context.Context, tx pgx.Tx, query string, columns []string, args ...any) ([]Record, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, mapRecordDBError(err)
	}
	defer rows.Close()
	var records []Record
	for rows.Next() {
		record, err := scanRecordValues(rows, columns)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, mapRecordDBError(rows.Err())
}

func scanRecordValues(rows pgx.Rows, columns []string) (Record, error) {
	values, err := rows.Values()
	if err != nil {
		return nil, err
	}
	record := Record{}
	for i, column := range columns {
		if i >= len(values) {
			return nil, fmt.Errorf("%w: record scan mismatch", ErrSchemaDrift)
		}
		record[column] = normalizeDBValue(values[i])
	}
	return record, nil
}

func normalizeDBValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano)
	case pgtype.UUID:
		if !value.Valid {
			return nil
		}
		return formatUUIDBytes(value.Bytes)
	case [16]byte:
		return formatUUIDBytes(value)
	case []byte:
		if json.Valid(value) {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err == nil {
				return decoded
			}
		}
		return string(value)
	default:
		return value
	}
}

func formatUUIDBytes(b [16]byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}

func mapRecordDBError(err error) error {
	if err == nil {
		return nil
	}
	switch pgErrCode(err) {
	case "42501":
		return ErrRLSDenied
	default:
		return err
	}
}
