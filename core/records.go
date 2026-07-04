package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
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

	result := &RecordListResult{Items: make([]Record, 0), Page: opts.Page, PerPage: opts.PerPage}
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
		if records == nil {
			records = make([]Record, 0)
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
	payload = appendAutodatePayload(collection, payload, "create")
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
	payload = appendAutodatePayload(collection, payload, "update")
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

func DeleteRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string) (Record, error) {
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
	err = withRecordTx(ctx, pool, auth, "delete", func(tx pgx.Tx) error {
		query := fmt.Sprintf(`delete from %s where id = $1 returning %s`, table, selectList(columns))
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
		if isAuthUsersHiddenField(collection, field.Name) {
			continue
		}
		if field.Hidden || field.Type == "password" {
			continue
		}
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
		if isAuthUsersHiddenField(collection, field.Name) {
			continue
		}
		if field.Hidden || field.Type == "password" {
			continue
		}
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
		if isAuthUsersHiddenField(collection, name) {
			return nil, fmt.Errorf("%w: auth field %q cannot be written", ErrValidation, name)
		}
		field, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("%w: unknown field %q", ErrValidation, name)
		}
		if field.Type == "autodate" {
			return nil, fmt.Errorf("%w: autodate field %q is managed by the server", ErrValidation, field.Name)
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

func isAuthUsersHiddenField(collection *Collection, name string) bool {
	return collection != nil && collection.Type == CollectionAuth && collection.Name == authUsersCollection && isHiddenAuthColumn(name)
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
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		return v, validateStringField(field, v, 0)
	case "editor":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		if err := validateStringField(field, v, 0); err != nil {
			return nil, err
		}
		if int64(len(v)) > maxSizeOption(field, 5<<20) {
			return nil, fmt.Errorf("%w: field %q exceeds max size", ErrValidation, field.Name)
		}
		return v, nil
	case "password":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		if err := validateStringField(field, v, 71); err != nil {
			return nil, err
		}
		if v == "" {
			return "", nil
		}
		cost := bcrypt.DefaultCost
		if configured, ok := intOption(field.Options, "cost"); ok {
			cost = configured
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(v), cost)
		if err != nil {
			return nil, err
		}
		return string(hash), nil
	case "email":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		if v == "" {
			if field.Required {
				return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
			}
			return "", nil
		}
		if _, err := mail.ParseAddress(v); err != nil {
			return nil, fmt.Errorf("%w: field %q must be an email", ErrValidation, field.Name)
		}
		if err := validateEmailDomain(field, v); err != nil {
			return nil, err
		}
		return v, nil
	case "url":
		v, err := decodeStringField(field, raw)
		if err != nil {
			return nil, err
		}
		if v == "" {
			if field.Required {
				return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
			}
			return "", nil
		}
		u, err := url.ParseRequestURI(v)
		if err != nil || u.Scheme == "" {
			return nil, fmt.Errorf("%w: field %q must be a URL", ErrValidation, field.Name)
		}
		if err := validateStringField(field, v, 0); err != nil {
			return nil, err
		}
		return v, nil
	case "number":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%w: field %q must be a number", ErrValidation, field.Name)
		}
		if field.Required && v == 0 {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
		}
		if boolOption(field.Options, "onlyInt") && v != math.Trunc(v) {
			return nil, fmt.Errorf("%w: field %q must be an integer", ErrValidation, field.Name)
		}
		if min, ok := floatOption(field.Options, "min"); ok && v < min {
			return nil, fmt.Errorf("%w: field %q must be greater than or equal to %v", ErrValidation, field.Name, min)
		}
		if max, ok := floatOption(field.Options, "max"); ok && v > max {
			return nil, fmt.Errorf("%w: field %q must be less than or equal to %v", ErrValidation, field.Name, max)
		}
		return v, nil
	case "bool":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a boolean", ErrValidation, field.Name)
		}
		if field.Required && !v {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
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
		if field.Required && emptyJSONValue(v) {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
		}
		if int64(len(raw)) > maxSizeOption(field, 1<<20) {
			return nil, fmt.Errorf("%w: field %q exceeds max size", ErrValidation, field.Name)
		}
		return string(raw), nil
	case "file":
		return nil, fmt.Errorf("%w: field %q must be updated through the file upload API", ErrValidation, field.Name)
	case "autodate":
		return nil, fmt.Errorf("%w: autodate field %q is managed by the server", ErrValidation, field.Name)
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

func validateStringField(field Field, value string, defaultMax int) error {
	if value == "" {
		if field.Required {
			return fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
		}
		return nil
	}
	length := len([]rune(value))
	if min, ok := intOption(field.Options, "min"); ok && min > 0 && length < min {
		return fmt.Errorf("%w: field %q must be at least %d characters", ErrValidation, field.Name, min)
	}
	maxValue := defaultMax
	if max, ok := intOption(field.Options, "max"); ok {
		maxValue = max
	}
	if maxValue > 0 && length > maxValue {
		return fmt.Errorf("%w: field %q must be at most %d characters", ErrValidation, field.Name, maxValue)
	}
	if pattern, _ := field.Options["pattern"].(string); pattern != "" {
		matches, err := regexp.MatchString(pattern, value)
		if err != nil {
			return fmt.Errorf("%w: field %q has invalid validation pattern", ErrValidation, field.Name)
		}
		if !matches {
			return fmt.Errorf("%w: field %q has invalid format", ErrValidation, field.Name)
		}
	}
	return nil
}

func validateEmailDomain(field Field, value string) error {
	at := strings.LastIndex(value, "@")
	if at < 0 || at == len(value)-1 {
		return fmt.Errorf("%w: field %q must be an email", ErrValidation, field.Name)
	}
	domain := strings.ToLower(value[at+1:])
	if allowed := stringSlice(field.Options["onlyDomains"]); len(allowed) > 0 && !containsFold(allowed, domain) {
		return fmt.Errorf("%w: field %q domain is not allowed", ErrValidation, field.Name)
	}
	if blocked := stringSlice(field.Options["exceptDomains"]); len(blocked) > 0 && containsFold(blocked, domain) {
		return fmt.Errorf("%w: field %q domain is not allowed", ErrValidation, field.Name)
	}
	return nil
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func normalizeSelectValue(field Field, raw json.RawMessage) (any, error) {
	allowed := map[string]struct{}{}
	for _, value := range stringSlice(field.Options["values"]) {
		allowed[value] = struct{}{}
	}
	if fieldIsMultiple(field) {
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a string array", ErrValidation, field.Name)
		}
		if err := validateSelectedCount(field, len(values)); err != nil {
			return nil, err
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
	if field.Required && value == "" {
		return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
	}
	if _, ok := allowed[value]; !ok {
		return nil, fmt.Errorf("%w: field %q has unsupported select value", ErrValidation, field.Name)
	}
	return value, nil
}

func normalizeRelationValue(field Field, raw json.RawMessage) (any, error) {
	if fieldIsMultiple(field) {
		var values []string
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("%w: field %q must be a UUID array", ErrValidation, field.Name)
		}
		if err := validateSelectedCount(field, len(values)); err != nil {
			return nil, err
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
	if field.Required && value == "" {
		return nil, fmt.Errorf("%w: field %q is required", ErrValidation, field.Name)
	}
	if err := ValidateUUID(value); err != nil {
		return nil, fmt.Errorf("%w: field %q must be a UUID", ErrValidation, field.Name)
	}
	return value, nil
}

func validateSelectedCount(field Field, count int) error {
	minSelect := 0
	if configured, ok := intOption(field.Options, "minSelect"); ok {
		minSelect = configured
	}
	if field.Required && minSelect < 1 {
		minSelect = 1
	}
	if count < minSelect {
		return fmt.Errorf("%w: field %q must include at least %d value(s)", ErrValidation, field.Name, minSelect)
	}
	if maxSelect, ok := intOption(field.Options, "maxSelect"); ok && maxSelect > 0 && count > maxSelect {
		return fmt.Errorf("%w: field %q must include no more than %d value(s)", ErrValidation, field.Name, maxSelect)
	}
	return nil
}

func maxSizeOption(field Field, defaultValue int64) int64 {
	maxSize, ok := int64Option(field.Options, "maxSize")
	if !ok || maxSize <= 0 {
		return defaultValue
	}
	return maxSize
}

func emptyJSONValue(v any) bool {
	switch value := v.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case []any:
		return len(value) == 0
	case map[string]any:
		return len(value) == 0
	default:
		return false
	}
}

func appendAutodatePayload(collection *Collection, payload *normalizedPayload, operation string) *normalizedPayload {
	if payload == nil {
		payload = &normalizedPayload{}
	}
	now := time.Now().UTC()
	for _, field := range collection.Fields {
		if field.Type != "autodate" {
			continue
		}
		if operation == "create" && !boolOption(field.Options, "onCreate") {
			continue
		}
		if operation == "update" && !boolOption(field.Options, "onUpdate") {
			continue
		}
		payload.Columns = append(payload.Columns, field.Name)
		payload.Values = append(payload.Values, now)
	}
	return payload
}

func fieldRequiredOnCreate(field Field) bool {
	if !field.Required {
		return false
	}
	return field.Type != "json" && field.Type != "file" && field.Type != "autodate"
}

func fieldCanBeNull(field Field) bool {
	if field.Required {
		return false
	}
	return field.Type != "bool" && field.Type != "json" && field.Type != "file" && field.Type != "autodate"
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
	case "autodate":
		return p + "::timestamptz"
	case "json", "file":
		return p + "::jsonb"
	case "select":
		if fieldIsMultiple(field) {
			return p + "::text[]"
		}
		return p + "::text"
	case "relation":
		if fieldIsMultiple(field) {
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
	case []pgtype.UUID:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, normalizeDBValue(item))
		}
		return out
	case [16]byte:
		return formatUUIDBytes(value)
	case [][16]byte:
		out := make([]string, 0, len(value))
		for _, item := range value {
			out = append(out, formatUUIDBytes(item))
		}
		return out
	case []byte:
		if json.Valid(value) {
			var decoded any
			if err := json.Unmarshal(value, &decoded); err == nil {
				return decoded
			}
		}
		return string(value)
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, normalizeDBValue(item))
		}
		return out
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
