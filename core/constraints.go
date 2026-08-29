package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Computed columns and check constraints put an operator-authored expression
// directly into DDL, so it never reaches Postgres as raw text: it is parsed by
// the same grammar the access rules use, every identifier is resolved against
// the collection's own fields, and only immutable constructs survive.
//
// Postgres itself enforces the rest. A generated column may only reference the
// row it belongs to, and both generated columns and CHECK constraints reject
// non-immutable functions — so even if this compiler were bypassed, a
// subquery or a call to now() would be refused at CREATE TABLE time.

const (
	maxComputedExpressionBytes = 1024
	maxCollectionChecks        = 24
	maxCollectionIndexes       = 24
)

// CollectionCheck is a named row-level invariant, e.g. discount <= subtotal.
type CollectionCheck struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
}

// CollectionIndex is a uniqueness or lookup index over one or more fields.
type CollectionIndex struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
	Unique bool     `json:"unique"`
}

// compileImmutableExpr compiles an expression that will live inside DDL. It is
// deliberately stricter than a rule: @request references read a per-request
// setting, which is neither immutable nor meaningful at write time, so they are
// refused rather than silently evaluated against an empty claim set.
func compileImmutableExpr(expr string, collection *Collection, kind string) (string, error) {
	return compileImmutableExprExact(expr, collection, kind, false)
}

// compileImmutableExprExact compiles with numeric (not float) arithmetic when
// the value being produced is a decimal.
func compileImmutableExprExact(expr string, collection *Collection, kind string, exact bool) (string, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", fmt.Errorf("%w: %s expression is required", ErrValidation, kind)
	}
	if len(expr) > maxComputedExpressionBytes {
		return "", fmt.Errorf("%w: %s expression is too long", ErrValidation, kind)
	}
	node, err := parseRuleExpression(expr)
	if err != nil {
		return "", fmt.Errorf("%w: %s expression: %s", ErrValidation, kind,
			strings.TrimPrefix(err.Error(), ErrInvalidRule.Error()+": "))
	}
	if err := assertImmutableNode(node, kind); err != nil {
		return "", err
	}
	compiler := &ruleCompiler{collection: collection, mode: compilePolicy, exactArithmetic: exact}
	sql, err := compiler.compile(node)
	if err != nil {
		return "", fmt.Errorf("%w: %s expression: %s", ErrValidation, kind,
			strings.TrimPrefix(err.Error(), ErrInvalidRule.Error()+": "))
	}
	return sql, nil
}

func assertImmutableNode(node ruleNode, kind string) error {
	switch n := node.(type) {
	case requestNode:
		return fmt.Errorf("%w: %s expression cannot reference %s — it must depend only on this row's own columns", ErrValidation, kind, n.name)
	case arithNode:
		if err := assertImmutableNode(n.left, kind); err != nil {
			return err
		}
		return assertImmutableNode(n.right, kind)
	case binaryNode:
		if err := assertImmutableNode(n.left, kind); err != nil {
			return err
		}
		return assertImmutableNode(n.right, kind)
	case compareNode:
		if err := assertImmutableNode(n.left, kind); err != nil {
			return err
		}
		return assertImmutableNode(n.right, kind)
	}
	return nil
}

// ComputedColumnSQL returns the GENERATED ALWAYS AS clause for a field, or ""
// when the field is not computed.
func ComputedColumnSQL(collection *Collection, field Field) (string, error) {
	raw, ok := field.Options["computed"].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", nil
	}
	switch field.Type {
	case "decimal", "number", "bool", "text":
	default:
		return "", fmt.Errorf("%w: computed is not supported on %q fields", ErrValidation, field.Type)
	}
	if field.Required {
		return "", fmt.Errorf("%w: computed field %q cannot also be required — the database supplies its value", ErrValidation, field.Name)
	}
	sql, err := compileImmutableExprExact(raw, collection, "computed", field.Type == "decimal")
	if err != nil {
		return "", err
	}
	// A generated column may not reference itself.
	if referencesField(raw, field.Name) {
		return "", fmt.Errorf("%w: computed field %q cannot reference itself", ErrValidation, field.Name)
	}
	return fmt.Sprintf("generated always as (%s) stored", sql), nil
}

func referencesField(expr string, name string) bool {
	node, err := parseRuleExpression(expr)
	if err != nil {
		return false
	}
	found := false
	var walk func(ruleNode)
	walk = func(n ruleNode) {
		switch v := n.(type) {
		case identNode:
			if v.name == name {
				found = true
			}
		case arithNode:
			walk(v.left)
			walk(v.right)
		case binaryNode:
			walk(v.left)
			walk(v.right)
		case compareNode:
			walk(v.left)
			walk(v.right)
		}
	}
	walk(node)
	return found
}

// FieldIsComputed reports whether the database owns this field's value.
func FieldIsComputed(field Field) bool {
	raw, ok := field.Options["computed"].(string)
	return ok && strings.TrimSpace(raw) != ""
}

// collectionConstraintOptions decodes only the two keys this file owns, so an
// unrelated option cannot be misread as a constraint.
type collectionConstraintOptions struct {
	Checks  []CollectionCheck `json:"checks"`
	Indexes []CollectionIndex `json:"indexes"`
}

func parseConstraintOptions(raw json.RawMessage) collectionConstraintOptions {
	var opts collectionConstraintOptions
	if len(raw) == 0 {
		return opts
	}
	_ = json.Unmarshal(raw, &opts)
	return opts
}

func collectionChecks(collection *Collection) []CollectionCheck {
	if collection == nil {
		return nil
	}
	out := parseConstraintOptions(collection.Options).Checks
	for i := range out {
		out[i].Name = NormalizeIdentifier(out[i].Name)
	}
	return out
}

func collectionIndexes(collection *Collection) []CollectionIndex {
	if collection == nil {
		return nil
	}
	out := parseConstraintOptions(collection.Options).Indexes
	for i := range out {
		out[i].Name = NormalizeIdentifier(out[i].Name)
		for j := range out[i].Fields {
			out[i].Fields[j] = NormalizeIdentifier(out[i].Fields[j])
		}
	}
	return out
}

// ValidateCollectionConstraints checks everything that can be checked without
// touching the database, so a bad definition is a 422 at save time rather than
// a DDL failure mid-migration.
func ValidateCollectionConstraints(collection *Collection) error {
	checks := collectionChecks(collection)
	if len(checks) > maxCollectionChecks {
		return fmt.Errorf("%w: at most %d checks are supported", ErrValidation, maxCollectionChecks)
	}
	seen := map[string]struct{}{}
	for _, check := range checks {
		if err := ValidateDataIdentifier("check name", check.Name); err != nil {
			return err
		}
		if _, dup := seen[check.Name]; dup {
			return fmt.Errorf("%w: duplicate check %q", ErrValidation, check.Name)
		}
		seen[check.Name] = struct{}{}
		if _, err := compileImmutableExpr(check.Expression, collection, "check"); err != nil {
			return err
		}
	}
	indexes := collectionIndexes(collection)
	if len(indexes) > maxCollectionIndexes {
		return fmt.Errorf("%w: at most %d indexes are supported", ErrValidation, maxCollectionIndexes)
	}
	names := map[string]struct{}{}
	for _, idx := range indexes {
		if err := ValidateDataIdentifier("index name", idx.Name); err != nil {
			return err
		}
		if _, dup := names[idx.Name]; dup {
			return fmt.Errorf("%w: duplicate index %q", ErrValidation, idx.Name)
		}
		names[idx.Name] = struct{}{}
		if len(idx.Fields) == 0 {
			return fmt.Errorf("%w: index %q needs at least one field", ErrValidation, idx.Name)
		}
		for _, name := range idx.Fields {
			if !indexableField(collection, name) {
				return fmt.Errorf("%w: index %q references unknown field %q", ErrValidation, idx.Name, name)
			}
		}
	}
	for _, field := range collection.Fields {
		if _, err := ComputedColumnSQL(collection, field); err != nil {
			return err
		}
	}
	return nil
}

func indexableField(collection *Collection, name string) bool {
	if name == collectionPrimaryKeyField(collection) {
		return true
	}
	if (name == "created" || name == "updated") && collectionStandardSystemColumns(collection) {
		return true
	}
	for _, field := range collection.Fields {
		if field.Name == name {
			return field.Type != "file" && field.Type != "json"
		}
	}
	return false
}

// syncCollectionConstraints brings the table's checks and indexes in line with
// the collection definition. Constraints Dublyobase owns are prefixed so a
// hand-written constraint on the same table is never dropped.
func syncCollectionConstraints(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection) error {
	if err := ValidateCollectionConstraints(collection); err != nil {
		return err
	}
	table := quoteIdent(project.SchemaName, collection.Name)

	existing, err := ownedConstraintNames(ctx, tx, project.SchemaName, collection.Name)
	if err != nil {
		return err
	}
	desired := map[string]struct{}{}
	for _, check := range collectionChecks(collection) {
		name := "dbo_ck_" + collection.Name + "_" + check.Name
		desired[name] = struct{}{}
		sql, err := compileImmutableExpr(check.Expression, collection, "check")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, table, quoteIdent(name))); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s add constraint %s check (%s)`, table, quoteIdent(name), sql)); err != nil {
			return mapConstraintError(err, check.Name)
		}
	}
	for name := range existing {
		if _, keep := desired[name]; !keep {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, table, quoteIdent(name))); err != nil {
				return err
			}
		}
	}

	existingIdx, err := ownedIndexNames(ctx, tx, project.SchemaName, collection.Name)
	if err != nil {
		return err
	}
	desiredIdx := map[string]struct{}{}
	for _, idx := range collectionIndexes(collection) {
		name := "dbo_ix_" + collection.Name + "_" + idx.Name
		desiredIdx[name] = struct{}{}
		cols := make([]string, 0, len(idx.Fields))
		for _, field := range idx.Fields {
			cols = append(cols, recordColumnSQL(collection, field))
		}
		unique := ""
		if idx.Unique {
			unique = "unique "
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop index if exists %s`, quoteIdent(project.SchemaName, name))); err != nil {
			return err
		}
		stmt := fmt.Sprintf(`create %sindex %s on %s (%s)`, unique, quoteIdent(name), table, strings.Join(cols, ", "))
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return mapConstraintError(err, idx.Name)
		}
	}
	// A text search compiles to `... like '%term%'`, a leading wildcard no btree
	// index can serve, so every search read the whole table. A GIN trigram index
	// on the same expression makes that query fast without changing what it
	// means or how it is written.
	if trigramAvailable(ctx, tx) {
		for _, field := range collection.Fields {
			if !fieldTrigramIndexable(field) {
				continue
			}
			name := "dbo_ix_" + collection.Name + "_" + field.Name + "_trgm"
			desiredIdx[name] = struct{}{}
			if _, err := tx.Exec(ctx, fmt.Sprintf(`drop index if exists %s`, quoteIdent(project.SchemaName, name))); err != nil {
				return err
			}
			stmt := fmt.Sprintf(`create index %s on %s using gin ((%s) gin_trgm_ops)`,
				quoteIdent(name), table, searchTextExpr(recordColumnSQL(collection, field.Name)))
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return mapConstraintError(err, field.Name+" search index")
			}
		}
	}

	// A vector column is useless without an index: nearest-neighbour search over
	// a sequential scan is exactly the full scan people adopt pgvector to avoid.
	if collectionHasVectorField(collection) && vectorAvailable(ctx, tx) {
		for _, field := range collection.Fields {
			if field.Type != "vector" {
				continue
			}
			dims, metric := vectorOptions(field)
			spec, ok := vectorMetrics[metric]
			// HNSW cannot index past its dimension ceiling. The column still
			// works, so this skips the index rather than refusing the column.
			if !ok || dims > maxIndexableVectorDims {
				continue
			}
			name := "dbo_ix_" + collection.Name + "_" + field.Name + "_hnsw"
			desiredIdx[name] = struct{}{}
			if _, err := tx.Exec(ctx, fmt.Sprintf(`drop index if exists %s`, quoteIdent(project.SchemaName, name))); err != nil {
				return err
			}
			stmt := fmt.Sprintf(`create index %s on %s using hnsw (%s %s)`,
				quoteIdent(name), table, recordColumnSQL(collection, field.Name), spec.opClass)
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return mapConstraintError(err, field.Name+" vector index")
			}
		}
	}

	for name := range existingIdx {
		if _, keep := desiredIdx[name]; !keep {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`drop index if exists %s`, quoteIdent(project.SchemaName, name))); err != nil {
				return err
			}
		}
	}
	return nil
}

func ownedConstraintNames(ctx context.Context, tx pgx.Tx, schemaName string, tableName string) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `
		select con.conname
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where ns.nspname = $1 and cls.relname = $2 and con.contype = 'c' and con.conname like 'dbo_ck_%'`,
		schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

func ownedIndexNames(ctx context.Context, tx pgx.Tx, schemaName string, tableName string) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, `
		select indexname from pg_indexes
		where schemaname = $1 and tablename = $2 and indexname like 'dbo_ix_%'`,
		schemaName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	return out, rows.Err()
}

// mapConstraintError turns "existing rows violate this constraint" into a 422
// naming the constraint, rather than a 500. 23P01 is the exclusion case: adding
// an overlap rule to a table whose existing rows already overlap.
func mapConstraintError(err error, name string) error {
	switch pgErrCode(err) {
	case "23514", "23505", "23P01", "42P07", "42710", "42601", "42883", "42804", "0A000":
		return fmt.Errorf("%w: %q could not be applied: %s", ErrValidation, name, pgErrMessage(err))
	default:
		return err
	}
}

// trigramAvailable reports whether pg_trgm can be used, installing it if the
// database has it packaged and the role is allowed to.
//
// Unlike the exclusion constraints, which refuse to save without btree_gist
// because they are a correctness guarantee, this only makes search faster. A
// role that cannot create extensions should still be able to save a collection,
// so the attempt runs inside a savepoint and a failure just means no index.
func trigramAvailable(ctx context.Context, tx pgx.Tx) bool {
	var installed bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from pg_extension where extname = 'pg_trgm')`).Scan(&installed); err != nil {
		return false
	}
	if installed {
		return true
	}
	if _, err := tx.Exec(ctx, `savepoint dbo_trgm`); err != nil {
		return false
	}
	if _, err := tx.Exec(ctx, `create extension if not exists pg_trgm`); err != nil {
		_, _ = tx.Exec(ctx, `rollback to savepoint dbo_trgm`)
		return false
	}
	_, _ = tx.Exec(ctx, `release savepoint dbo_trgm`)
	return true
}
