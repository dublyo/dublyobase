package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRecordFilterBytes      = 8192
	maxRecordFilterDepth      = 16
	maxRecordFilterPredicates = 128
	maxRecordSearchBytes      = 200
)

type recordFilterBuilder struct {
	collection *Collection
	relations  *FilterRelations
	args       []any
	base       int
	predicates int
}

func CompileRecordListFilter(filter string, search string, collection *Collection) (*SQLExpression, error) {
	return CompileRecordListFilterWithRelations(filter, search, collection, nil)
}

// CompileRecordListFilterWithRelations additionally allows dotted keys that
// walk relations, e.g. "conversation.workspace.plan".
func CompileRecordListFilterWithRelations(filter string, search string, collection *Collection, relations *FilterRelations) (*SQLExpression, error) {
	filterExpr, err := compileRecordFilterWithRelations(filter, collection, relations)
	if err != nil {
		return nil, err
	}
	searchExpr, err := compileRecordSearch(search, collection, len(filterExpr.Args))
	if err != nil {
		return nil, err
	}
	return combineRecordExpressions(filterExpr, searchExpr), nil
}

func compileRecordFilter(raw string, collection *Collection) (*SQLExpression, error) {
	return compileRecordFilterWithRelations(raw, collection, nil)
}

func compileRecordFilterWithRelations(raw string, collection *Collection, relations *FilterRelations) (*SQLExpression, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &SQLExpression{}, nil
	}
	if !strings.HasPrefix(raw, "{") {
		return CompileFilter(raw, collection)
	}
	if len(raw) > maxRecordFilterBytes {
		return nil, fmt.Errorf("%w: JSON filter is too large", ErrInvalidFilter)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	var body map[string]any
	if err := dec.Decode(&body); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON filter", ErrInvalidFilter)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: invalid JSON filter", ErrInvalidFilter)
	}
	if len(body) == 0 {
		return &SQLExpression{}, nil
	}
	builder := &recordFilterBuilder{collection: collection, relations: relations}
	sql, err := builder.compileObject(body, "and", 0)
	if err != nil {
		return nil, err
	}
	return &SQLExpression{SQL: sql, Args: builder.args}, nil
}

func compileRecordSearch(raw string, collection *Collection, base int) (*SQLExpression, error) {
	search := strings.TrimSpace(raw)
	if search == "" {
		return &SQLExpression{}, nil
	}
	if len(search) > maxRecordSearchBytes {
		return nil, fmt.Errorf("%w: search is too long", ErrInvalidFilter)
	}
	builder := &recordFilterBuilder{collection: collection, base: base}
	var parts []string
	for _, field := range collection.Fields {
		if !field.Searchable || !fieldCanSearch(field) {
			continue
		}
		column := recordColumnSQL(collection, field.Name)
		switch field.Type {
		case "number":
			if n, ok := parseSearchNumber(search); ok {
				parts = append(parts, fmt.Sprintf("%s = %s", column, builder.arg(n)))
			}
		case "decimal":
			if _, err := parseDecimalText(search); err == nil {
				parts = append(parts, fmt.Sprintf("%s = %s::numeric", column, builder.arg(strings.TrimSpace(search))))
			}
		case "bool":
			if b, ok := parseSearchBool(search); ok {
				parts = append(parts, fmt.Sprintf("%s = %s", column, builder.arg(b)))
			}
		default:
			parts = append(parts, fmt.Sprintf("%s like %s", searchTextExpr(column), builder.arg("%"+strings.ToLower(search)+"%")))
		}
	}
	if len(parts) == 0 {
		return &SQLExpression{SQL: "false"}, nil
	}
	return &SQLExpression{SQL: "(" + strings.Join(parts, " or ") + ")", Args: builder.args}, nil
}

func combineRecordExpressions(filter *SQLExpression, search *SQLExpression) *SQLExpression {
	if filter == nil || filter.SQL == "" {
		if search == nil {
			return &SQLExpression{}
		}
		return search
	}
	if search == nil || search.SQL == "" {
		return filter
	}
	return &SQLExpression{
		SQL:  fmt.Sprintf("(%s) and (%s)", filter.SQL, search.SQL),
		Args: append(append([]any{}, filter.Args...), search.Args...),
	}
}

func (b *recordFilterBuilder) compileObject(body map[string]any, logical string, depth int) (string, error) {
	if depth > maxRecordFilterDepth {
		return "", fmt.Errorf("%w: JSON filter nesting is too deep", ErrInvalidFilter)
	}
	keys := sortedMapKeys(body)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := body[key]
		switch key {
		case "_and", "_or":
			group, err := b.compileLogicalArray(key, value, depth+1)
			if err != nil {
				return "", err
			}
			if group != "" {
				parts = append(parts, group)
			}
		case "_not":
			sub, ok := value.(map[string]any)
			if !ok {
				return "", fmt.Errorf("%w: _not requires an object", ErrInvalidFilter)
			}
			sql, err := b.compileObject(sub, "and", depth+1)
			if err != nil {
				return "", err
			}
			if sql != "" {
				parts = append(parts, "not ("+sql+")")
			}
		default:
			sql, err := b.compileField(key, value, depth+1)
			if err != nil {
				return "", err
			}
			if sql != "" {
				parts = append(parts, sql)
			}
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " "+logical+" ") + ")", nil
}

func (b *recordFilterBuilder) compileLogicalArray(operator string, value any, depth int) (string, error) {
	items, ok := value.([]any)
	if !ok {
		return "", fmt.Errorf("%w: %s requires an array", ErrInvalidFilter, operator)
	}
	logical := "and"
	if operator == "_or" {
		logical = "or"
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		body, ok := item.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%w: %s entries must be objects", ErrInvalidFilter, operator)
		}
		sql, err := b.compileObject(body, "and", depth)
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " "+logical+" ") + ")", nil
}

func (b *recordFilterBuilder) compileField(rawName string, rawValue any, depth int) (string, error) {
	if strings.Contains(rawName, ".") {
		return b.compileRelationPath(rawName, rawValue, depth)
	}
	name := NormalizeIdentifier(rawName)
	field, ok := filterableRecordField(b.collection, name)
	if !ok {
		return "", fmt.Errorf("%w: unknown filter field %q", ErrInvalidFilter, name)
	}
	ops, ok := rawValue.(map[string]any)
	if !ok || len(ops) == 0 {
		return b.compilePredicate(field, "_eq", rawValue)
	}
	keys := sortedMapKeys(ops)
	parts := make([]string, 0, len(keys))
	for _, op := range keys {
		sql, err := b.compilePredicate(field, op, ops[op])
		if err != nil {
			return "", err
		}
		if sql != "" {
			parts = append(parts, sql)
		}
	}
	if len(parts) == 0 {
		return "", nil
	}
	if depth > maxRecordFilterDepth {
		return "", fmt.Errorf("%w: JSON filter nesting is too deep", ErrInvalidFilter)
	}
	return "(" + strings.Join(parts, " and ") + ")", nil
}

func (b *recordFilterBuilder) compilePredicate(field Field, operator string, value any) (string, error) {
	return b.compilePredicateColumn(recordColumnSQL(b.collection, field.Name), field, operator, value)
}

func (b *recordFilterBuilder) compilePredicateColumn(column string, field Field, operator string, value any) (string, error) {
	b.predicates++
	if b.predicates > maxRecordFilterPredicates {
		return "", fmt.Errorf("%w: JSON filter has too many predicates", ErrInvalidFilter)
	}
	if field.Type == "decimal" {
		// Handled before normalizeFilterJSONValue, which would turn a
		// json.Number into float64 and reintroduce the rounding that decimal
		// fields exist to prevent.
		return b.compileDecimalPredicate(column, operator, value)
	}
	value = normalizeFilterJSONValue(value)
	// A multi-value field is an array column. Comparing it to a single value
	// with = made Postgres try to parse that value as an array literal, and the
	// caller got "malformed array literal" — a database internal, for a filter
	// that reads perfectly reasonably. On an array these mean membership.
	if fieldIsMultiple(field) {
		switch operator {
		case "_eq", "_neq":
			if value == nil {
				if operator == "_eq" {
					return column + " is null", nil
				}
				return column + " is not null", nil
			}
			member := b.arg(value) + multiValueElementCast(field) + " = any(" + column + ")"
			if operator == "_neq" {
				return "not coalesce(" + member + ", false)", nil
			}
			return member, nil
		case "_in", "_nin":
			return b.compileMultiValueListPredicate(column, field, operator, value)
		}
	}
	switch operator {
	case "_eq":
		if value == nil {
			return column + " is null", nil
		}
		return column + " = " + b.arg(value), nil
	case "_neq":
		if value == nil {
			return column + " is not null", nil
		}
		return column + " <> " + b.arg(value), nil
	case "_gt", "_gte", "_lt", "_lte":
		sqlOp := map[string]string{"_gt": ">", "_gte": ">=", "_lt": "<", "_lte": "<="}[operator]
		return column + " " + sqlOp + " " + b.arg(value), nil
	case "_contains", "_ncontains", "_icontains", "_nicontains", "_starts_with", "_nstarts_with", "_istarts_with", "_nistarts_with", "_ends_with", "_nends_with", "_iends_with", "_niends_with":
		return b.compileTextPredicate(column, operator, value)
	case "_in", "_nin":
		return b.compileListPredicate(column, operator, value)
	case "_null":
		if filterBool(value) == false {
			return column + " is not null", nil
		}
		return column + " is null", nil
	case "_nnull":
		if filterBool(value) == false {
			return column + " is null", nil
		}
		return column + " is not null", nil
	case "_empty":
		if filterBool(value) == false {
			return fmt.Sprintf("(%s is not null and %s::text <> '')", column, column), nil
		}
		return fmt.Sprintf("(%s is null or %s::text = '')", column, column), nil
	case "_nempty":
		if filterBool(value) == false {
			return fmt.Sprintf("(%s is null or %s::text = '')", column, column), nil
		}
		return fmt.Sprintf("(%s is not null and %s::text <> '')", column, column), nil
	default:
		return "", fmt.Errorf("%w: unsupported filter operator %q", ErrInvalidFilter, operator)
	}
}

func (b *recordFilterBuilder) compileTextPredicate(column string, operator string, value any) (string, error) {
	text := fmt.Sprint(value)
	negated := strings.HasPrefix(operator, "_n")
	insensitive := strings.HasPrefix(operator, "_i") || strings.HasPrefix(operator, "_ni")
	pattern := text
	switch {
	case strings.Contains(operator, "contains"):
		pattern = "%" + text + "%"
	case strings.Contains(operator, "starts_with"):
		pattern = text + "%"
	case strings.Contains(operator, "ends_with"):
		pattern = "%" + text
	}
	target := column + "::text"
	op := "like"
	if negated {
		op = "not like"
	}
	if insensitive {
		target = "lower(coalesce(" + target + ", ''))"
		pattern = strings.ToLower(pattern)
	}
	return target + " " + op + " " + b.arg(pattern), nil
}

func (b *recordFilterBuilder) compileListPredicate(column string, operator string, value any) (string, error) {
	values := filterList(value)
	if len(values) == 0 {
		if operator == "_nin" {
			return "true", nil
		}
		return "false", nil
	}
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, b.arg(value))
	}
	op := "in"
	if operator == "_nin" {
		op = "not in"
	}
	return column + " " + op + " (" + strings.Join(placeholders, ", ") + ")", nil
}

func (b *recordFilterBuilder) arg(value any) string {
	b.args = append(b.args, value)
	return fmt.Sprintf("$%d", b.base+len(b.args))
}

func filterableRecordField(collection *Collection, name string) (Field, bool) {
	if name == collectionPrimaryKeyField(collection) {
		return Field{Name: name, Type: "text"}, true
	}
	if collectionStandardSystemColumns(collection) && (name == "created" || name == "updated") {
		return Field{Name: name, Type: "text"}, true
	}
	for _, field := range collection.Fields {
		if field.Name == name && !field.Hidden && field.Type != "password" && !isAuthUsersHiddenField(collection, field.Name) {
			return field, true
		}
	}
	return Field{}, false
}

// searchTextExpr is the expression a text search compares against. The trigram
// index is built on this exact expression, so the two must stay identical — an
// index on anything else is simply never used, and the search silently goes back
// to scanning the table.
func searchTextExpr(column string) string {
	return fmt.Sprintf("lower(coalesce(%s::text, ''))", column)
}

// fieldTrigramIndexable reports whether a trigram index is worth building for a
// field. Search casts every type to text, but only the free-text types benefit;
// indexing a uuid or a timestamp that way costs writes and saves nothing.
func fieldTrigramIndexable(field Field) bool {
	if !field.Searchable || !fieldCanSearch(field) {
		return false
	}
	switch field.Type {
	case "text", "editor", "email", "url", "select":
		return true
	default:
		return false
	}
}

func fieldCanSearch(field Field) bool {
	if field.Hidden || field.Type == "password" {
		return false
	}
	switch field.Type {
	case "text", "editor", "email", "url", "select", "number", "decimal", "bool", "date", "autodate", "relation":
		return true
	default:
		return false
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// decimalFilterText extracts the exact literal from a filter value without
// letting it pass through float64.
func decimalFilterText(value any) (string, bool) {
	switch v := value.(type) {
	case json.Number:
		return v.String(), true
	case string:
		return strings.TrimSpace(v), true
	default:
		return "", false
	}
}

func (b *recordFilterBuilder) decimalArg(value any) (string, error) {
	text, ok := decimalFilterText(value)
	if !ok {
		return "", fmt.Errorf("%w: decimal filter value must be a number or decimal string", ErrInvalidFilter)
	}
	if _, err := parseDecimalText(text); err != nil {
		return "", fmt.Errorf("%w: decimal filter value must be a number or decimal string", ErrInvalidFilter)
	}
	return b.arg(text) + "::numeric", nil
}

func (b *recordFilterBuilder) compileDecimalPredicate(column string, operator string, value any) (string, error) {
	switch operator {
	case "_null", "_nnull":
		isNull := filterBool(value)
		if operator == "_nnull" {
			isNull = !isNull
		}
		if isNull {
			return column + " is null", nil
		}
		return column + " is not null", nil
	case "_in", "_nin":
		items, ok := value.([]any)
		if !ok {
			return "", fmt.Errorf("%w: %s requires an array", ErrInvalidFilter, operator)
		}
		if len(items) == 0 {
			if operator == "_in" {
				return "false", nil
			}
			return "true", nil
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			arg, err := b.decimalArg(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, arg)
		}
		op := "in"
		if operator == "_nin" {
			op = "not in"
		}
		return column + " " + op + " (" + strings.Join(parts, ", ") + ")", nil
	}
	if value == nil {
		if operator == "_eq" {
			return column + " is null", nil
		}
		if operator == "_neq" {
			return column + " is not null", nil
		}
	}
	sqlOp, ok := map[string]string{"_eq": "=", "_neq": "<>", "_gt": ">", "_gte": ">=", "_lt": "<", "_lte": "<="}[operator]
	if !ok {
		return "", fmt.Errorf("%w: operator %q is not supported on decimal fields", ErrInvalidFilter, operator)
	}
	arg, err := b.decimalArg(value)
	if err != nil {
		return "", err
	}
	return column + " " + sqlOp + " " + arg, nil
}

func normalizeFilterJSONValue(value any) any {
	switch v := value.(type) {
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i
		}
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeFilterJSONValue(item))
		}
		return out
	default:
		return value
	}
}

func filterList(value any) []any {
	value = normalizeFilterJSONValue(value)
	switch v := value.(type) {
	case []any:
		return v
	case string:
		parts := strings.Split(v, ",")
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		if value == nil {
			return nil
		}
		return []any{value}
	}
}

func filterBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || v == "1"
	default:
		return true
	}
}

func parseSearchNumber(search string) (float64, bool) {
	n, err := strconv.ParseFloat(search, 64)
	return n, err == nil
}

func parseSearchBool(search string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(search)) {
	case "true", "1", "yes":
		return true, true
	case "false", "0", "no":
		return false, true
	default:
		return false, false
	}
}

// filterRelationsFor prepares relation resolution, but only when the filter
// actually contains a dotted key: walking the schema costs a query per related
// collection and most filters never need it.
func filterRelationsFor(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collection *Collection, filter string) (*FilterRelations, error) {
	if !filterMentionsRelationPath(filter) {
		return nil, nil
	}
	reachable, err := reachableCollections(ctx, pool, auth, collection)
	if err != nil {
		return nil, err
	}
	return &FilterRelations{
		Collections: reachable,
		Table:       func(c *Collection) (string, error) { return recordTable(auth, c) },
	}, nil
}

// filterMentionsRelationPath reports whether any key in the filter is dotted.
// It is a cheap scan of the raw JSON rather than a parse, so a filter that
// never walks a relation costs nothing.
func filterMentionsRelationPath(filter string) bool {
	trimmed := strings.TrimSpace(filter)
	if trimmed == "" || !strings.Contains(trimmed, ".") {
		return false
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(trimmed), &body); err != nil {
		return strings.Contains(trimmed, ".")
	}
	return objectHasDottedKey(body, 0)
}

func objectHasDottedKey(node any, depth int) bool {
	if depth > maxRecordFilterDepth {
		return false
	}
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if strings.Contains(key, ".") && !strings.HasPrefix(key, "_") {
				return true
			}
			if objectHasDottedKey(child, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if objectHasDottedKey(child, depth+1) {
				return true
			}
		}
	}
	return false
}

// multiValueElementCast returns the cast an element of an array column needs,
// e.g. "::uuid" for a multi-relation, so the bound text lands as the right type.
func multiValueElementCast(field Field) string {
	if field.Type == "relation" {
		return "::uuid"
	}
	if cast := postgresPlaceholderCast(fieldSourceType(field)); cast != "" {
		return "::" + strings.TrimSuffix(cast, "[]")
	}
	return ""
}

// compileMultiValueListPredicate asks whether an array column overlaps a list.
func (b *recordFilterBuilder) compileMultiValueListPredicate(column string, field Field, operator string, value any) (string, error) {
	items, ok := value.([]any)
	if !ok {
		return "", fmt.Errorf("%w: %s expects a list", ErrInvalidFilter, operator)
	}
	if len(items) == 0 {
		if operator == "_nin" {
			return "true", nil
		}
		return "false", nil
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, b.arg(normalizeFilterJSONValue(item))+multiValueElementCast(field)+" = any("+column+")")
	}
	joined := "(" + strings.Join(parts, " or ") + ")"
	if operator == "_nin" {
		return "not coalesce(" + joined + ", false)", nil
	}
	return joined, nil
}
