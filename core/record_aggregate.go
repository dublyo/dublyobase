package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxAggregateGroups  = 1000
	maxAggregateColumns = 12
)

// AggregateInput describes a grouped aggregate query: the CRM/ERP reporting
// primitive (revenue by month, pipeline by stage, stock on hand) that
// previously forced clients to page every row and total it themselves.
type AggregateInput struct {
	Aggregates []string // "sum:amount", "count:*", "avg:amount"
	GroupBy    []string
	Filter     string
	Search     string
	Limit      int
}

type AggregateRow struct {
	Group  map[string]any `json:"group,omitempty"`
	Values map[string]any `json:"values"`
}

type AggregateResult struct {
	Items     []AggregateRow `json:"items"`
	Truncated bool           `json:"truncated"`
}

var aggregateFunctions = map[string]struct{}{
	"count": {}, "sum": {}, "avg": {}, "min": {}, "max": {},
}

// AggregateRecords runs the query under the caller's role and RLS policies, so
// a tenant can only ever aggregate rows they are allowed to read.
func AggregateRecords(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, input AggregateInput) (*AggregateResult, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	if len(input.Aggregates) == 0 {
		return nil, fmt.Errorf("%w: at least one aggregate is required", ErrValidation)
	}
	if len(input.Aggregates) > maxAggregateColumns || len(input.GroupBy) > maxAggregateColumns {
		return nil, fmt.Errorf("%w: at most %d aggregates and group columns are supported", ErrValidation, maxAggregateColumns)
	}

	selects := make([]string, 0, len(input.Aggregates)+len(input.GroupBy))
	groupExprs := make([]string, 0, len(input.GroupBy))
	groupAliases := make([]string, 0, len(input.GroupBy))
	for _, raw := range input.GroupBy {
		name := NormalizeIdentifier(raw)
		field, ok := aggregatableField(collection, name)
		if !ok {
			return nil, fmt.Errorf("%w: cannot group by %q", ErrValidation, raw)
		}
		expr := recordColumnSQL(collection, field)
		groupExprs = append(groupExprs, expr)
		groupAliases = append(groupAliases, field)
		selects = append(selects, expr+" as "+quoteIdent(field))
	}

	valueAliases := make([]string, 0, len(input.Aggregates))
	for _, raw := range input.Aggregates {
		fn, target, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok {
			return nil, fmt.Errorf("%w: aggregate %q must be written as function:field", ErrValidation, raw)
		}
		fn = strings.ToLower(strings.TrimSpace(fn))
		if _, ok := aggregateFunctions[fn]; !ok {
			return nil, fmt.Errorf("%w: unsupported aggregate function %q", ErrValidation, fn)
		}
		target = strings.TrimSpace(target)
		var expr, alias string
		if target == "*" {
			if fn != "count" {
				return nil, fmt.Errorf("%w: only count supports *", ErrValidation)
			}
			expr, alias = "count(*)", "count"
		} else {
			name := NormalizeIdentifier(target)
			field, ok := aggregatableField(collection, name)
			if !ok {
				return nil, fmt.Errorf("%w: cannot aggregate %q", ErrValidation, target)
			}
			if fn == "sum" || fn == "avg" {
				if !fieldIsNumericForAggregate(collection, field) {
					return nil, fmt.Errorf("%w: %s requires a number or decimal field, got %q", ErrValidation, fn, target)
				}
			}
			if fn == "min" || fn == "max" {
				// Postgres has no min/max aggregate for boolean or uuid; without
				// this the request reaches the database and comes back as a 500
				// instead of telling the caller what is wrong.
				if !fieldSupportsMinMax(collection, field) {
					return nil, fmt.Errorf("%w: %s is not supported on field %q", ErrValidation, fn, target)
				}
			}
			expr = fn + "(" + recordColumnSQL(collection, field) + ")"
			alias = fn + "_" + field
		}
		selects = append(selects, expr+" as "+quoteIdent(alias))
		valueAliases = append(valueAliases, alias)
	}

	expr, err := CompileRecordListFilter(input.Filter, input.Search, collection)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`select %s from %s`, strings.Join(selects, ", "), table)
	if expr != nil && expr.SQL != "" {
		query += " where " + expr.SQL
	}
	if len(groupExprs) > 0 {
		query += " group by " + strings.Join(groupExprs, ", ") + " order by " + strings.Join(groupExprs, ", ")
	}
	limit := input.Limit
	if limit <= 0 || limit > maxAggregateGroups {
		limit = maxAggregateGroups
	}
	// One extra row so the caller is told the result was cut rather than
	// silently reading a partial report as if it were complete.
	query += fmt.Sprintf(" limit %d", limit+1)

	columns := append(append([]string{}, groupAliases...), valueAliases...)
	result := &AggregateResult{Items: []AggregateRow{}}
	err = withRecordTxForCollection(ctx, pool, auth, collection, "list", func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, expr.Args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			if len(result.Items) >= limit {
				result.Truncated = true
				break
			}
			row := AggregateRow{Group: map[string]any{}, Values: map[string]any{}}
			for i, column := range columns {
				if i >= len(values) {
					break
				}
				if i < len(groupAliases) {
					row.Group[column] = normalizeDBValue(values[i])
				} else {
					row.Values[column] = normalizeDBValue(values[i])
				}
			}
			if len(groupAliases) == 0 {
				row.Group = nil
			}
			result.Items = append(result.Items, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// aggregatableField resolves a requested name to a real, readable column.
func aggregatableField(collection *Collection, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if name == collectionPrimaryKeyField(collection) {
		return name, true
	}
	if name == "created" || name == "updated" {
		// Imported tables need not have the managed system columns.
		return name, collectionStandardSystemColumns(collection)
	}
	for _, field := range collection.Fields {
		if field.Name != name {
			continue
		}
		if field.Hidden || field.Type == "password" || field.Type == "file" || field.Type == "json" {
			return "", false
		}
		if isAuthUsersHiddenField(collection, field.Name) {
			return "", false
		}
		return field.Name, true
	}
	return "", false
}

// fieldSupportsMinMax mirrors the types Postgres actually provides min/max
// aggregates for. Relation columns are uuid and bool is bool: neither has one.
func fieldSupportsMinMax(collection *Collection, name string) bool {
	if name == "created" || name == "updated" {
		return true
	}
	if name == collectionPrimaryKeyField(collection) {
		return false
	}
	for _, field := range collection.Fields {
		if field.Name != name {
			continue
		}
		switch field.Type {
		case "number", "decimal", "date", "autodate", "text", "email", "url", "editor":
			return !fieldIsMultiple(field)
		default:
			return false
		}
	}
	return false
}

func fieldIsNumericForAggregate(collection *Collection, name string) bool {
	for _, field := range collection.Fields {
		if field.Name == name {
			return field.Type == "number" || field.Type == "decimal"
		}
	}
	return false
}
