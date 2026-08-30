package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Record map[string]any

type RecordListOptions struct {
	Page      int
	PerPage   int
	Offset    int
	Sort      string
	Filter    string
	Search    string
	Fields    string
	Expand    string
	SkipTotal bool

	// NearField and NearVector ask for nearest-neighbour order. They ride the
	// normal list path so a similarity search inherits the same row-level
	// security, filters, projection and paging as any other read.
	NearField  string
	NearVector []float64
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
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	opts = normalizeListOptions(opts)
	columns, err := projectionColumns(collection, opts.Fields)
	if err != nil {
		return nil, err
	}
	// Resolution has to happen before the order clause, because sorting can walk
	// relations too.
	relations, err := filterRelationsFor(ctx, pool, auth, collection, opts.Filter+" "+opts.Sort)
	if err != nil {
		return nil, err
	}
	orderBy, err := orderByClause(collection, opts.Sort, relations)
	if err != nil {
		return nil, err
	}
	filter, err := CompileRecordListFilterWithRelations(opts.Filter, opts.Search, collection, relations)
	if err != nil {
		return nil, err
	}
	// The subqueries a relation filter or sort builds qualify the outer columns,
	// so the outer table has to carry the alias they expect.
	if relations != nil {
		table += " as " + quoteIdent(filterRootAlias)
	}
	// The query vector is a bind parameter, never interpolated, and its
	// placeholder is numbered after the filter's own arguments.
	var nearArg any
	if opts.NearField != "" {
		field, literal, expr, err := compileVectorOrder(collection, opts.NearField, opts.NearVector, len(filter.Args)+1)
		if err != nil {
			return nil, err
		}
		_ = field
		nearArg = literal
		orderBy = expr
	}
	where := ""
	if filter.SQL != "" {
		where = " where " + filter.SQL
	}

	result := &RecordListResult{Items: make([]Record, 0), Page: opts.Page, PerPage: opts.PerPage}
	err = withRecordTxForCollection(ctx, pool, auth, collection, "list", func(tx pgx.Tx) error {
		if opts.SkipTotal {
			result.TotalItems = -1
		} else {
			var total int
			countSQL := fmt.Sprintf(`select count(*) from %s%s`, table, where)
			if err := tx.QueryRow(ctx, countSQL, filter.Args...).Scan(&total); err != nil {
				return mapRecordDBError(err)
			}
			result.TotalItems = total
		}

		args := append([]any{}, filter.Args...)
		if nearArg != nil {
			args = append(args, nearArg)
		}
		limitPos := len(args) + 1
		args = append(args, opts.PerPage)
		offsetPos := len(args) + 1
		args = append(args, opts.Offset)
		query := fmt.Sprintf(`select %s from %s%s order by %s limit $%d offset $%d`,
			recordSelectList(collection, columns),
			table,
			where,
			orderBy,
			limitPos,
			offsetPos,
		)
		records, err := queryRecords(ctx, tx, query, columns, columnFormats(collection), args...)
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
	if err := expandRecords(ctx, pool, auth, collection, result.Items, opts.Expand); err != nil {
		return nil, err
	}
	return result, nil
}

func CreateRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, raw map[string]json.RawMessage) (Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	var out Record
	err = withRecordTxForCollection(ctx, pool, auth, collection, "create", func(tx pgx.Tx) error {
		record, err := createRecordInTx(ctx, tx, auth, collection, raw)
		out = record
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// createRecordInTx is the transaction-scoped core, split out so a batch can run
// many operations inside one transaction instead of one transaction each.
func createRecordInTx(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, raw map[string]json.RawMessage) (Record, error) {
	payload, err := normalizeCreatePayload(collection, raw)
	if err != nil {
		return nil, err
	}
	payload = appendAutodatePayload(collection, payload, "create")
	columns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	var query string
	if len(payload.Columns) == 0 {
		query = fmt.Sprintf(`insert into %s default values returning %s`, table, recordSelectList(collection, columns))
	} else {
		query = fmt.Sprintf(`insert into %s (%s) values (%s) returning %s`,
			table,
			recordColumnList(collection, payload.Columns),
			valuePlaceholders(collection, payload.Columns, fieldByName(collection.Fields), 1),
			recordSelectList(collection, columns),
		)
	}
	return queryOneRecord(ctx, tx, query, columns, columnFormats(collection), payload.Values...)
}

// assertRecordVersion locks the row and compares its current version. The
// SELECT ... FOR UPDATE matters: without it two callers could both read the
// same version, both pass this check, and both write.
func assertRecordVersion(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, id string, expected string) error {
	table, err := recordTable(auth, collection)
	if err != nil {
		return err
	}
	where, args, err := recordPrimaryKeyWhere(collection, id, 1)
	if err != nil {
		return err
	}
	var current int64
	query := fmt.Sprintf(`select xmin::text::bigint from %s where %s for update`, table, where)
	if err := tx.QueryRow(ctx, query, args...).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRecordNotFound
		}
		return mapRecordDBError(err)
	}
	if fmt.Sprint(current) != strings.TrimSpace(expected) {
		return fmt.Errorf("%w: the record changed since you loaded it (version %d)", ErrRecordConflict, current)
	}
	return nil
}

func getRecordInTx(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, id string) (Record, error) {
	if err := validateRecordKey(collection, id); err != nil {
		return nil, err
	}
	columns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	where, whereArgs, err := recordPrimaryKeyWhere(collection, id, 1)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`select %s from %s where %s`, recordSelectList(collection, columns), table, where)
	return queryOneRecord(ctx, tx, query, columns, columnFormats(collection), whereArgs...)
}

func GetRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string) (Record, error) {
	return GetRecordWithOptions(ctx, pool, auth, collectionName, id, "")
}

func GetRecordWithOptions(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string, expand string) (Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	var out Record
	err = withRecordTxForCollection(ctx, pool, auth, collection, "view", func(tx pgx.Tx) error {
		record, err := getRecordInTx(ctx, tx, auth, collection, id)
		out = record
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := expandRecords(ctx, pool, auth, collection, []Record{out}, expand); err != nil {
		return nil, err
	}
	return out, nil
}

func expandRecords(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collection *Collection, records []Record, rawExpand string) error {
	if strings.TrimSpace(rawExpand) == "" || len(records) == 0 {
		return nil
	}
	requested := map[string]struct{}{}
	all := false
	for _, part := range strings.Split(rawExpand, ",") {
		name := NormalizeIdentifier(part)
		if strings.TrimSpace(part) == "*" {
			all = true
			continue
		}
		if name != "" {
			requested[name] = struct{}{}
		}
	}
	if !all && len(requested) == 0 {
		return nil
	}
	for _, field := range collection.Fields {
		if field.Type != "relation" || field.Hidden {
			continue
		}
		if !all {
			if _, ok := requested[field.Name]; !ok {
				continue
			}
		}
		targetName, _ := field.Options["collection"].(string)
		targetName = NormalizeIdentifier(targetName)
		if targetName == "" {
			continue
		}

		// Collect every id this page references before touching the database.
		// The previous implementation fetched one related record at a time, and
		// each fetch opened its own transaction with SET LOCAL ROLE and three
		// set_config calls — so a 500-row page with one relation column cost 500
		// transactions. Now it is one query per relation field per page.
		multiple := fieldIsMultiple(field)
		unique := make([]string, 0, len(records))
		seen := map[string]struct{}{}
		for _, record := range records {
			value, ok := record[field.Name]
			if !ok || value == nil {
				continue
			}
			var ids []string
			if multiple {
				ids = relationIDSlice(value)
			} else if id, ok := value.(string); ok && id != "" {
				ids = []string{id}
			}
			for _, id := range ids {
				if id == "" {
					continue
				}
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				unique = append(unique, id)
			}
		}
		if len(unique) == 0 {
			continue
		}
		byID, err := getRecordsByIDs(ctx, pool, auth, targetName, unique)
		if err != nil {
			return err
		}
		for _, record := range records {
			value, ok := record[field.Name]
			if !ok || value == nil {
				continue
			}
			expanded := ensureRecordExpand(record)
			if multiple {
				items := make([]Record, 0)
				for _, id := range relationIDSlice(value) {
					if related, ok := byID[id]; ok {
						items = append(items, related)
					}
				}
				expanded[field.Name] = items
				continue
			}
			id, ok := value.(string)
			if !ok || id == "" {
				continue
			}
			if related, ok := byID[id]; ok {
				expanded[field.Name] = related
			}
		}
	}
	return nil
}

// getRecordsByIDs loads many records from one collection in a single query,
// under the caller's role so row-level security still decides what comes back:
// an id the caller may not read is simply absent from the result, exactly as it
// was when each row was fetched separately.
func getRecordsByIDs(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, ids []string) (map[string]Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		// A relation pointing at a collection that no longer exists should not
		// fail the whole list request; the expand is simply omitted.
		if errors.Is(err, ErrCollectionNotFound) {
			return map[string]Record{}, nil
		}
		return nil, err
	}
	if collectionHasCompositePrimaryKey(collection) {
		// Composite keys cannot be matched with a single = ANY, and they are
		// rare enough that the per-record path stays correct here.
		out := make(map[string]Record, len(ids))
		for _, id := range ids {
			related, err := GetRecordWithOptions(ctx, pool, auth, collectionName, id, "")
			if err == nil {
				out[id] = related
			}
		}
		return out, nil
	}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if err := validateRecordKey(collection, id); err == nil {
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return map[string]Record{}, nil
	}
	columns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	pk := recordColumnSQL(collection, collectionPrimaryKeyField(collection))
	query := fmt.Sprintf(`select %s from %s where %s::text = any($1)`,
		recordSelectList(collection, columns), table, pk)

	out := make(map[string]Record, len(valid))
	err = withRecordTxForCollection(ctx, pool, auth, collection, "view", func(tx pgx.Tx) error {
		rows, err := queryRecords(ctx, tx, query, columns, columnFormats(collection), valid)
		if err != nil {
			return err
		}
		pkField := collectionPrimaryKeyField(collection)
		for _, row := range rows {
			if key, ok := row[pkField].(string); ok {
				out[key] = row
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func ensureRecordExpand(record Record) map[string]any {
	existing, _ := record["expand"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
		record["expand"] = existing
	}
	return existing
}

func relationIDSlice(value any) []string {
	switch raw := value.(type) {
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if id, ok := item.(string); ok && id != "" {
				out = append(out, id)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(raw))
		for _, id := range raw {
			if strings.TrimSpace(id) != "" {
				out = append(out, id)
			}
		}
		return out
	default:
		return nil
	}
}

func UpdateRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string, raw map[string]json.RawMessage) (Record, error) {
	return UpdateRecordVersioned(ctx, pool, auth, collectionName, id, raw, "")
}

// UpdateRecordVersioned applies the patch only if the row still carries
// expectedVersion. An empty expectedVersion keeps the previous last-write-wins
// behaviour, so existing clients are unaffected.
func UpdateRecordVersioned(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string, raw map[string]json.RawMessage, expectedVersion string) (Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	var out Record
	err = withRecordTxForCollection(ctx, pool, auth, collection, "update", func(tx pgx.Tx) error {
		if expectedVersion != "" {
			if err := assertRecordVersion(ctx, tx, auth, collection, id, expectedVersion); err != nil {
				return err
			}
		}
		record, err := updateRecordInTx(ctx, tx, auth, collection, id, raw)
		out = record
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func updateRecordInTx(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, id string, raw map[string]json.RawMessage) (Record, error) {
	if err := validateRecordKey(collection, id); err != nil {
		return nil, err
	}
	payload, err := normalizePatchPayload(collection, raw)
	if err != nil {
		return nil, err
	}
	payload = appendAutodatePayload(collection, payload, "update")
	columns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	assignments := make([]string, 0, len(payload.Columns)+1)
	args := make([]any, 0, len(payload.Values)+1)
	fields := fieldByName(collection.Fields)
	for i, column := range payload.Columns {
		assignments = append(assignments, fmt.Sprintf(`%s = %s`, recordColumnSQL(collection, column), valuePlaceholder(fields[column], i+1)))
		args = append(args, payload.Values[i])
	}
	if collectionStandardSystemColumns(collection) {
		assignments = append(assignments, `updated = now()`)
	}
	if len(assignments) == 0 {
		return nil, fmt.Errorf("%w: patch body is empty", ErrValidation)
	}
	where, whereArgs, err := recordPrimaryKeyWhere(collection, id, len(args)+1)
	if err != nil {
		return nil, err
	}
	args = append(args, whereArgs...)
	query := fmt.Sprintf(`update %s set %s where %s returning %s`,
		table,
		strings.Join(assignments, ", "),
		where,
		recordSelectList(collection, columns),
	)
	return queryOneRecord(ctx, tx, query, columns, columnFormats(collection), args...)
}

func DeleteRecord(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string) (Record, error) {
	return DeleteRecordVersioned(ctx, pool, auth, collectionName, id, "")
}

func DeleteRecordVersioned(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, id string, expectedVersion string) (Record, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return nil, err
	}
	var out Record
	err = withRecordTxForCollection(ctx, pool, auth, collection, "delete", func(tx pgx.Tx) error {
		if expectedVersion != "" {
			if err := assertRecordVersion(ctx, tx, auth, collection, id, expectedVersion); err != nil {
				return err
			}
		}
		record, err := deleteRecordInTx(ctx, tx, auth, collection, id)
		out = record
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func deleteRecordInTx(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, id string) (Record, error) {
	if err := validateRecordKey(collection, id); err != nil {
		return nil, err
	}
	columns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return nil, err
	}
	where, whereArgs, err := recordPrimaryKeyWhere(collection, id, 1)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`delete from %s where %s returning %s`, table, where, recordSelectList(collection, columns))
	return queryOneRecord(ctx, tx, query, columns, columnFormats(collection), whereArgs...)
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
		opts.PerPage = 25
	}
	if opts.PerPage > 500 {
		opts.PerPage = 500
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	if opts.Offset == 0 {
		opts.Offset = (opts.Page - 1) * opts.PerPage
	} else {
		opts.Page = (opts.Offset / opts.PerPage) + 1
	}
	return opts
}

func recordTable(auth *RecordAuth, collection *Collection) (string, error) {
	schemaName, tableName, err := collectionPhysicalTable(&auth.Project, collection)
	if err != nil {
		return "", err
	}
	return quoteIdent(schemaName, tableName), nil
}

func withRecordTxForCollection(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collection *Collection, operation string, fn func(pgx.Tx) error) error {
	bypassRole := auth.Role == RecordRoleService && collectionIsImported(collection)
	return withRecordTxOptions(ctx, pool, auth, operation, bypassRole, fn)
}

func allRecordColumns(collection *Collection) []string {
	pkField := collectionPrimaryKeyField(collection)
	columns := []string{pkField}
	if collectionStandardSystemColumns(collection) {
		columns = append(columns, "created", "updated")
	}
	for _, field := range collection.Fields {
		if isAuthUsersHiddenField(collection, field.Name) {
			continue
		}
		if field.Hidden || field.Type == "password" {
			continue
		}
		if field.Name == pkField {
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

func orderByClause(collection *Collection, raw string, relations *FilterRelations) (string, error) {
	pkField := collectionPrimaryKeyField(collection)
	if strings.TrimSpace(raw) == "" {
		if collectionStandardSystemColumns(collection) {
			return recordColumnSQL(collection, "created") + " desc, " + recordColumnSQL(collection, pkField) + " desc", nil
		}
		return recordColumnSQL(collection, pkField) + " asc", nil
	}
	if !collectionStandardSystemColumns(collection) && strings.TrimSpace(raw) == "-created" {
		return recordColumnSQL(collection, pkField) + " asc", nil
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
		// A dotted key sorts by a field on a related row, the same paths filters
		// walk. It compiles to a correlated subquery, which runs under the same
		// role, so a row the caller cannot read sorts as null rather than
		// leaking its value.
		if strings.Contains(part, ".") {
			expr, err := relationOrderExpr(collection, part, relations)
			if err != nil {
				return "", err
			}
			key := part + ":" + dir
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			parts = append(parts, expr+" "+dir)
			continue
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
		parts = append(parts, recordColumnSQL(collection, name)+" "+dir)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%w: sort is empty", ErrValidation)
	}
	return strings.Join(parts, ", ") + ", " + recordColumnSQL(collection, pkField) + " asc", nil
}

func allowedRecordColumns(collection *Collection) map[string]struct{} {
	out := map[string]struct{}{collectionPrimaryKeyField(collection): {}}
	if collectionStandardSystemColumns(collection) {
		out["created"] = struct{}{}
		out["updated"] = struct{}{}
	}
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
	payload, err := normalizeRecordPayload(collection, raw, collectionIsImported(collection) && !collectionHasCompositePrimaryKey(collection))
	if err != nil {
		return nil, err
	}
	provided := map[string]struct{}{}
	for _, column := range payload.Columns {
		provided[column] = struct{}{}
	}
	pkField := collectionPrimaryKeyField(collection)
	if collectionIsImported(collection) && !collectionHasCompositePrimaryKey(collection) && !collectionStandardSystemColumns(collection) && !collectionPrimaryKeyHasDefault(collection) {
		if _, ok := provided[pkField]; !ok {
			return nil, fmt.Errorf("%w: field %q is required", ErrValidation, pkField)
		}
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
	return normalizeRecordPayload(collection, raw, false)
}

func normalizeRecordPayload(collection *Collection, raw map[string]json.RawMessage, allowPrimaryKey bool) (*normalizedPayload, error) {
	fields := fieldByName(collection.Fields)
	columns := make([]string, 0, len(raw))
	valuesByColumn := map[string]any{}
	pkField := collectionPrimaryKeyField(collection)
	hasStandardColumns := collectionStandardSystemColumns(collection)
	for name, body := range raw {
		name = NormalizeIdentifier(name)
		if _, exists := valuesByColumn[name]; exists {
			return nil, fmt.Errorf("%w: duplicate field %q", ErrValidation, name)
		}
		if name == pkField {
			if !allowPrimaryKey {
				return nil, fmt.Errorf("%w: system field %q cannot be written", ErrValidation, name)
			}
			value, err := normalizePrimaryKeyCreateValue(collection, body)
			if err != nil {
				return nil, err
			}
			valuesByColumn[name] = value
			columns = append(columns, name)
			continue
		}
		if hasStandardColumns && (name == "created" || name == "updated") {
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
		value, err := normalizeRecordValue(field, body)
		if err != nil {
			return nil, err
		}
		valuesByColumn[name] = value
		columns = append(columns, name)
	}
	sort.SliceStable(columns, func(i, j int) bool { return columns[i] < columns[j] })
	sortedValues := make([]any, 0, len(columns))
	for _, column := range columns {
		sortedValues = append(sortedValues, valuesByColumn[column])
	}
	return &normalizedPayload{Columns: columns, Values: sortedValues}, nil
}

func normalizePrimaryKeyCreateValue(collection *Collection, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		return nil, fmt.Errorf("%w: record id is required", ErrValidation)
	}
	switch collectionPrimaryKeyType(collection) {
	case "int2", "int4", "int8":
		var number json.Number
		if err := json.Unmarshal(raw, &number); err == nil {
			value, err := number.Int64()
			if err != nil {
				return nil, fmt.Errorf("%w: invalid record id", ErrValidation)
			}
			return value, nil
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("%w: invalid record id", ErrValidation)
		}
		value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid record id", ErrValidation)
		}
		return value, nil
	default:
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("%w: record id must be a string", ErrValidation)
		}
		if err := validateRecordKey(collection, text); err != nil {
			return nil, err
		}
		return text, nil
	}
}

func isAuthUsersHiddenField(collection *Collection, name string) bool {
	return collection != nil && collection.Type == CollectionAuth && collection.Name == authUsersCollection && isHiddenAuthColumn(name)
}

func normalizeRecordValue(field Field, raw json.RawMessage) (any, error) {
	if FieldIsComputed(field) {
		return nil, fmt.Errorf("%w: field %q is computed by the database and cannot be written", ErrValidation, field.Name)
	}
	if FieldIsRollup(field) {
		return nil, fmt.Errorf("%w: field %q is a rollup maintained by the database and cannot be written", ErrValidation, field.Name)
	}
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
	case "decimal":
		return normalizeDecimalInput(field, raw)
	case "vector":
		return normalizeVectorInput(field, raw)
	case "number":
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, fmt.Errorf("%w: field %q must be a number", ErrValidation, field.Name)
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
			return nil, fmt.Errorf("%w: field %q must be a string array", ErrValidation, field.Name)
		}
		if err := validateSelectedCount(field, len(values)); err != nil {
			return nil, err
		}
		for _, value := range values {
			if err := validateRelationIdentifier(field, value); err != nil {
				return nil, err
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
	if value == "" {
		return value, nil
	}
	if err := validateRelationIdentifier(field, value); err != nil {
		return nil, err
	}
	return value, nil
}

func validateRelationIdentifier(field Field, value string) error {
	switch fieldSourceType(field) {
	case "", "uuid":
		if err := ValidateUUID(value); err != nil {
			return fmt.Errorf("%w: field %q must be a UUID", ErrValidation, field.Name)
		}
	case "text", "varchar", "bpchar", "citext":
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: field %q must be a non-empty id", ErrValidation, field.Name)
		}
	default:
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: field %q must be a non-empty id", ErrValidation, field.Name)
		}
	}
	return nil
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

// versionColumn is the name the row version is published under.
const versionColumn = "_version"

func recordSelectList(collection *Collection, columns []string) string {
	return recordSelectListWithVersion(collection, columns, true)
}

func recordSelectListWithVersion(collection *Collection, columns []string, withVersion bool) string {
	list := recordSelectListBase(collection, columns)
	if withVersion {
		// cast through text because xid has no direct bigint cast
		list += `, xmin::text::bigint as ` + quoteIdent(versionColumn)
	}
	return list
}

func recordSelectListBase(collection *Collection, columns []string) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		expr := recordSelectExpr(collection, column)
		alias := quoteIdent(column)
		if expr == alias {
			parts[i] = expr
		} else {
			parts[i] = expr + " as " + alias
		}
	}
	return strings.Join(parts, ", ")
}

// recordSelectExpr pins the wire type of a column to its declared field type,
// and is used for SELECT only so filters and ORDER BY still hit the bare column
// and its indexes.
//
// This matters for imported tables: a Postgres numeric column mapped to a
// `number` field would otherwise be scanned as pgtype.Numeric and emitted as an
// exact string, while writes to the same field still go through float64 and the
// generated SDK still declares it a number. Casting here keeps read, write and
// SDK agreeing on one type per field.
func recordSelectExpr(collection *Collection, column string) string {
	expr := recordColumnSQL(collection, column)
	for _, field := range collection.Fields {
		if field.Name != column {
			continue
		}
		// decimal is deliberately NOT cast here. Casting it to text produced an
		// output column alias that shadows the real column, and SQL resolves a
		// bare ORDER BY name against the output alias first — so `sort=-total`
		// ordered money lexicographically (95, 450, 400, 2885, 220). The exact
		// string still reaches the client: normalizeDBValue renders
		// pgtype.Numeric digit-for-digit.
		if field.Type == "number" {
			// An imported numeric column mapped to `number` would otherwise
			// scan as pgtype.Numeric and be emitted as a string while writes
			// go through float64. A double-precision alias still sorts
			// numerically, so this cast is safe for ORDER BY.
			return expr + "::double precision"
		}
		break
	}
	return expr
}

func recordColumnList(collection *Collection, columns []string) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		parts[i] = recordColumnSQL(collection, column)
	}
	return strings.Join(parts, ", ")
}

func recordColumnSQL(collection *Collection, name string) string {
	if name == collectionPrimaryKeyField(collection) {
		if collectionHasCompositePrimaryKey(collection) {
			return compositeRecordIDSQL(collection)
		}
		return quoteIdent(collectionPrimaryKeySource(collection))
	}
	if collectionStandardSystemColumns(collection) && (name == "created" || name == "updated") {
		return quoteIdent(name)
	}
	if field, ok := fieldByName(collection.Fields)[name]; ok {
		return quoteIdent(fieldSourceColumn(field))
	}
	return quoteIdent(name)
}

func fieldSourceColumn(field Field) string {
	if field.Options != nil {
		if source, _ := field.Options["sourceColumn"].(string); strings.TrimSpace(source) != "" {
			return strings.TrimSpace(source)
		}
	}
	return field.Name
}

func fieldSourceType(field Field) string {
	if field.Options != nil {
		if sourceType, _ := field.Options["sourceType"].(string); strings.TrimSpace(sourceType) != "" {
			return strings.ToLower(strings.TrimSpace(sourceType))
		}
	}
	return ""
}

func valuePlaceholders(collection *Collection, columns []string, fields map[string]Field, start int) string {
	parts := make([]string, len(columns))
	for i, column := range columns {
		if column == collectionPrimaryKeyField(collection) {
			parts[i] = recordKeyPlaceholder(collection, start+i)
			continue
		}
		parts[i] = valuePlaceholder(fields[column], start+i)
	}
	return strings.Join(parts, ", ")
}

func valuePlaceholder(field Field, pos int) string {
	p := fmt.Sprintf("$%d", pos)
	if cast := postgresPlaceholderCast(fieldSourceType(field)); cast != "" {
		return p + "::" + cast
	}
	switch field.Type {
	case "number":
		return p + "::double precision"
	case "decimal":
		return p + "::numeric"
	case "vector":
		return p + "::public.vector"
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

func compositeRecordIDSQL(collection *Collection) string {
	parts := collectionCompositePrimaryKey(collection)
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, quoteIdent(part.Column)+"::text")
	}
	if len(values) == 0 {
		return quoteIdent(defaultRecordPrimaryKey)
	}
	return "translate(replace(replace(rtrim(encode(convert_to(json_build_array(" + strings.Join(values, ", ") + ")::text, 'UTF8'), 'base64'), '='), E'\\n', ''), E'\\r', ''), '+/', '-_')"
}

func recordPrimaryKeyWhere(collection *Collection, id string, start int) (string, []any, error) {
	if !collectionHasCompositePrimaryKey(collection) {
		return fmt.Sprintf(`%s = %s`, recordColumnSQL(collection, collectionPrimaryKeyField(collection)), recordKeyPlaceholder(collection, start)), []any{id}, nil
	}
	parts := collectionCompositePrimaryKey(collection)
	values, err := decodeCompositeRecordID(id)
	if err != nil {
		return "", nil, err
	}
	if len(values) != len(parts) {
		return "", nil, fmt.Errorf("%w: invalid composite record id", ErrValidation)
	}
	clauses := make([]string, 0, len(parts))
	args := make([]any, 0, len(parts))
	for i, part := range parts {
		value, err := normalizeCompositeRecordKeyPart(part, values[i])
		if err != nil {
			return "", nil, err
		}
		placeholder := fmt.Sprintf("$%d", start+i)
		if cast := postgresPlaceholderCast(part.Type); cast != "" {
			placeholder += "::" + cast
		}
		clauses = append(clauses, fmt.Sprintf(`%s = %s`, quoteIdent(part.Column), placeholder))
		args = append(args, value)
	}
	return strings.Join(clauses, " and "), args, nil
}

func normalizeCompositeRecordKeyPart(part collectionPrimaryKeyPart, raw string) (any, error) {
	switch strings.ToLower(strings.TrimSpace(part.Type)) {
	case "int2", "int4", "int8":
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid composite record id", ErrValidation)
		}
		return value, nil
	default:
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("%w: invalid composite record id", ErrValidation)
		}
		return raw, nil
	}
}

func recordKeyPlaceholder(collection *Collection, pos int) string {
	p := fmt.Sprintf("$%d", pos)
	if cast := postgresPlaceholderCast(collectionPrimaryKeyType(collection)); cast != "" {
		return p + "::" + cast
	}
	return p
}

func postgresPlaceholderCast(udtName string) string {
	switch strings.ToLower(strings.TrimSpace(udtName)) {
	case "uuid":
		return "uuid"
	case "bool":
		return "boolean"
	case "int2":
		return "smallint"
	case "int4":
		return "integer"
	case "int8":
		return "bigint"
	case "float4":
		return "real"
	case "float8":
		return "double precision"
	case "numeric":
		return "numeric"
	case "timestamptz":
		return "timestamptz"
	case "timestamp":
		return "timestamp"
	case "date":
		return "date"
	case "json":
		return "json"
	case "jsonb":
		return "jsonb"
	case "text", "varchar", "bpchar", "citext":
		return "text"
	default:
		return ""
	}
}

func validateRecordKey(collection *Collection, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%w: record id is required", ErrValidation)
	}
	switch collectionPrimaryKeyType(collection) {
	case "uuid":
		return ValidateUUID(id)
	case "int2", "int4", "int8":
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return fmt.Errorf("%w: invalid record id", ErrValidation)
		}
		return nil
	default:
		if len(id) > 512 {
			return fmt.Errorf("%w: record id is too long", ErrValidation)
		}
		return nil
	}
}

func queryOneRecord(ctx context.Context, tx pgx.Tx, query string, columns []string, formats map[string]columnFormat, args ...any) (Record, error) {
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
	record, err := scanRecordValues(rows, columns, formats)
	if err != nil {
		return nil, err
	}
	if rows.Next() {
		return nil, fmt.Errorf("%w: expected one record", ErrValidation)
	}
	return record, mapRecordDBError(rows.Err())
}

func queryRecords(ctx context.Context, tx pgx.Tx, query string, columns []string, formats map[string]columnFormat, args ...any) ([]Record, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, mapRecordDBError(err)
	}
	defer rows.Close()
	var records []Record
	for rows.Next() {
		record, err := scanRecordValues(rows, columns, formats)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, mapRecordDBError(rows.Err())
}

func scanRecordValues(rows pgx.Rows, columns []string, formats map[string]columnFormat) (Record, error) {
	values, err := rows.Values()
	if err != nil {
		return nil, err
	}
	record := Record{}
	for i, column := range columns {
		if i >= len(values) {
			return nil, fmt.Errorf("%w: record scan mismatch", ErrSchemaDrift)
		}
		value := normalizeDBValue(values[i])
		if format, ok := formats[column]; ok {
			value = applyColumnFormat(value, format)
		}
		record[column] = value
	}
	// the version is appended after the projected columns
	if len(values) > len(columns) {
		record[versionColumn] = normalizeDBValue(values[len(columns)])
	}
	return record, nil
}

func normalizeDBValue(v any) any {
	switch value := v.(type) {
	case nil:
		return nil
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano)
	case pgtype.Numeric:
		return formatExactNumeric(value)
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

// castErrorMessage returns the database's own explanation of a failed cast,
// which already names the type and the value, falling back to a generic line if
// the driver gave us nothing useful.
func castErrorMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.TrimSpace(pgErr.Message) != "" {
		return pgErr.Message
	}
	return "value does not match the field type"
}

func mapRecordDBError(err error) error {
	if err == nil {
		return nil
	}
	switch pgErrCode(err) {
	case "42501":
		return ErrRLSDenied
	// A value the caller supplied did not cast to the column's type — a relation
	// filtered against "", a number against a word, a malformed timestamp. These
	// arrive straight from user input, so an unfilled search box bound to a
	// filter used to return 500. Postgres is the authority on what casts, and
	// its message names the type and the offending value.
	case "22P02", "22007", "22008", "22003", "22001":
		return fmt.Errorf("%w: %s", ErrValidation, castErrorMessage(err))
	case "23505":
		return fmt.Errorf("%w: duplicate unique field value", ErrRecordConflict)
	case "23P01":
		// An exclusion constraint: this row conflicts with another row that is
		// already there, which is a conflict rather than bad input.
		return fmt.Errorf("%w: %s", ErrRecordConflict, constraintMessage(err))
	case "23514":
		// A collection check the operator authored. Naming it is the whole
		// point — "internal server error" tells them nothing about which rule
		// their record broke.
		return fmt.Errorf("%w: %s", ErrValidation, constraintMessage(err))
	// 23001 is restrict_violation: a RESTRICT foreign key refusing to let a
	// referenced row go. PostgreSQL 18 raises it where 16 raised 23503 for the
	// same delete, so both codes have to mean the same thing here.
	case "23001":
		return fmt.Errorf("%w: record is still referenced by other records", ErrRecordConflict)
	case "23503":
		// One code, two very different situations. Postgres distinguishes them
		// in the message prefix: "update or delete on table …" means the row
		// being removed is still pointed at; "insert or update on table …"
		// means the reference itself is bad. (The friendlier "is still
		// referenced from" text lives in Detail, which is not surfaced because
		// it echoes key values.)
		if strings.HasPrefix(pgErrMessage(err), "update or delete on table") {
			return fmt.Errorf("%w: record is still referenced by other records", ErrRecordConflict)
		}
		return fmt.Errorf("%w: referenced record does not exist", ErrValidation)
	case "23502":
		return fmt.Errorf("%w: %s", ErrValidation, constraintMessage(err))
	default:
		return err
	}
}

// constraintMessage renders a violated constraint in the operator's own terms.
// Dublyobase-owned constraints carry a dbo_ck_<collection>_<name> prefix, so the
// name they typed in the editor is recovered rather than echoed with plumbing.
func constraintMessage(err error) string {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "record violates a database constraint"
	}
	name := pgErr.ConstraintName
	for _, prefix := range []string{"dbo_ck_", "dbo_cs_", "dbo_ex_"} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if _, after, ok := strings.Cut(strings.TrimPrefix(name, prefix), "_"); ok {
			return fmt.Sprintf("record violates the %q rule on this collection", after)
		}
	}
	if pgErr.ColumnName != "" {
		return fmt.Sprintf("field %q violates a database constraint", pgErr.ColumnName)
	}
	if name != "" {
		return fmt.Sprintf("record violates constraint %q", name)
	}
	return "record violates a database constraint"
}
