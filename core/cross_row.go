package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Cross-row invariants. A CHECK constraint only ever sees the row being
// written, so two whole classes of rule cannot be expressed with one:
//
//	consistency  a payment's invoice must belong to the payment's patient —
//	             the truth lives in another table's row
//	exclusion    two appointments must not occupy one provider at one time —
//	             the truth lives in the other rows of the same table
//
// Both are enforced by PostgreSQL inside the writing transaction, so a second
// client, a direct SQL session or a batch cannot get around them.

const (
	maxCollectionConsistency = 12
	maxCollectionExclusions  = 8
)

// ConsistencyRule requires that the record referenced by Field agrees with this
// row: target[Field].Remote must equal this row's Local.
type ConsistencyRule struct {
	Name   string `json:"name"`
	Field  string `json:"field"`  // relation field on this collection
	Remote string `json:"remote"` // field on the related collection
	Local  string `json:"local"`  // field on this collection (defaults to Remote)
}

// ExclusionRule forbids two rows that agree on Equals from overlapping in time
// between From and To. Where narrows which rows participate, so a cancelled
// booking does not block the slot it released.
type ExclusionRule struct {
	Name   string   `json:"name"`
	Equals []string `json:"equals"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Where  string   `json:"where,omitempty"`
}

type crossRowOptions struct {
	Consistency []ConsistencyRule `json:"consistency"`
	Exclusions  []ExclusionRule   `json:"exclusions"`
}

func parseCrossRowOptions(raw json.RawMessage) crossRowOptions {
	var opts crossRowOptions
	if len(raw) == 0 {
		return opts
	}
	_ = json.Unmarshal(raw, &opts)
	for i := range opts.Consistency {
		r := &opts.Consistency[i]
		r.Name = NormalizeIdentifier(r.Name)
		r.Field = NormalizeIdentifier(r.Field)
		r.Remote = NormalizeIdentifier(r.Remote)
		r.Local = NormalizeIdentifier(r.Local)
		if r.Local == "" {
			r.Local = r.Remote
		}
	}
	for i := range opts.Exclusions {
		r := &opts.Exclusions[i]
		r.Name = NormalizeIdentifier(r.Name)
		r.From = NormalizeIdentifier(r.From)
		r.To = NormalizeIdentifier(r.To)
		for j := range r.Equals {
			r.Equals[j] = NormalizeIdentifier(r.Equals[j])
		}
	}
	return opts
}

func collectionCrossRow(collection *Collection) crossRowOptions {
	if collection == nil {
		return crossRowOptions{}
	}
	return parseCrossRowOptions(collection.Options)
}

// relationFieldTarget resolves a relation field to the collection it points at.
func relationFieldTarget(collection *Collection, name string) (Field, string, bool) {
	for _, field := range collection.Fields {
		if field.Name != name || field.Type != "relation" || fieldIsMultiple(field) {
			continue
		}
		target, _ := field.Options["collection"].(string)
		target = NormalizeIdentifier(target)
		if target == "" {
			return Field{}, "", false
		}
		return field, target, true
	}
	return Field{}, "", false
}

// ValidateCrossRowRules checks everything resolvable without the database.
// Remote field names are checked against the target collection at sync time,
// where the catalogue is available.
func ValidateCrossRowRules(collection *Collection) error {
	opts := collectionCrossRow(collection)
	if len(opts.Consistency) > maxCollectionConsistency {
		return fmt.Errorf("%w: at most %d consistency rules are supported", ErrValidation, maxCollectionConsistency)
	}
	if len(opts.Exclusions) > maxCollectionExclusions {
		return fmt.Errorf("%w: at most %d exclusion rules are supported", ErrValidation, maxCollectionExclusions)
	}
	seen := map[string]struct{}{}
	for _, rule := range opts.Consistency {
		if err := ValidateDataIdentifier("consistency rule name", rule.Name); err != nil {
			return err
		}
		if _, dup := seen[rule.Name]; dup {
			return fmt.Errorf("%w: duplicate consistency rule %q", ErrValidation, rule.Name)
		}
		seen[rule.Name] = struct{}{}
		if _, _, ok := relationFieldTarget(collection, rule.Field); !ok {
			return fmt.Errorf("%w: consistency rule %q needs options.field to be a single relation field", ErrValidation, rule.Name)
		}
		if !fieldExistsOnCollection(collection, rule.Local) {
			return fmt.Errorf("%w: consistency rule %q references unknown field %q", ErrValidation, rule.Name, rule.Local)
		}
		if rule.Remote == "" {
			return fmt.Errorf("%w: consistency rule %q needs options.remote", ErrValidation, rule.Name)
		}
		if err := ValidateDataIdentifier("remote field", rule.Remote); err != nil {
			return err
		}
	}
	names := map[string]struct{}{}
	for _, rule := range opts.Exclusions {
		if err := ValidateDataIdentifier("exclusion rule name", rule.Name); err != nil {
			return err
		}
		if _, dup := names[rule.Name]; dup {
			return fmt.Errorf("%w: duplicate exclusion rule %q", ErrValidation, rule.Name)
		}
		names[rule.Name] = struct{}{}
		for _, field := range []string{rule.From, rule.To} {
			if !fieldIsTimestamp(collection, field) {
				return fmt.Errorf("%w: exclusion rule %q needs options.from and options.to to be date fields", ErrValidation, rule.Name)
			}
		}
		if len(rule.Equals) == 0 {
			return fmt.Errorf("%w: exclusion rule %q needs at least one field in options.equals", ErrValidation, rule.Name)
		}
		for _, field := range rule.Equals {
			if !fieldExistsOnCollection(collection, field) {
				return fmt.Errorf("%w: exclusion rule %q references unknown field %q", ErrValidation, rule.Name, field)
			}
		}
		if strings.TrimSpace(rule.Where) != "" {
			if _, err := compileImmutableExpr(rule.Where, collection, "exclusion where"); err != nil {
				return err
			}
		}
	}
	return nil
}

func fieldExistsOnCollection(collection *Collection, name string) bool {
	if name == "" {
		return false
	}
	if name == collectionPrimaryKeyField(collection) {
		return true
	}
	for _, field := range collection.Fields {
		if field.Name == name && !field.Hidden {
			return true
		}
	}
	return false
}

func fieldIsTimestamp(collection *Collection, name string) bool {
	for _, field := range collection.Fields {
		if field.Name == name {
			return field.Type == "date" || field.Type == "autodate"
		}
	}
	return false
}

func consistencyObjectNames(collectionName, ruleName string) (constraint, function, trigger string) {
	base := collectionName + "_" + ruleName
	return "dbo_cs_" + base, "dbo_csfn_" + base, "dbo_cstg_" + base
}

// syncCrossRowRules brings triggers and exclusion constraints in line with the
// collection definition, dropping the ones Dublyobase owns that are no longer
// declared. Hand-written triggers on the same table are never touched.
func syncCrossRowRules(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection) error {
	if err := ValidateCrossRowRules(collection); err != nil {
		return err
	}
	opts := collectionCrossRow(collection)
	table := quoteIdent(project.SchemaName, collection.Name)

	existing, err := ownedTriggerNames(ctx, tx, project.SchemaName, collection.Name)
	if err != nil {
		return err
	}
	desired := map[string]struct{}{}
	for _, rule := range opts.Consistency {
		relField, targetName, ok := relationFieldTarget(collection, rule.Field)
		if !ok {
			return fmt.Errorf("%w: consistency rule %q needs options.field to be a single relation field", ErrValidation, rule.Name)
		}
		target, err := getCollectionTx(ctx, tx, project.ID, targetName)
		if err != nil {
			return fmt.Errorf("%w: consistency rule %q targets unknown collection %q", ErrValidation, rule.Name, targetName)
		}
		if !fieldExistsOnCollection(target, rule.Remote) {
			return fmt.Errorf("%w: consistency rule %q references unknown field %q on %q", ErrValidation, rule.Name, rule.Remote, targetName)
		}
		constraintName, fnName, tgName := consistencyObjectNames(collection.Name, rule.Name)
		desired[tgName] = struct{}{}

		// SECURITY DEFINER: the rule is about whether the data agrees, not about
		// what this caller may see. Without it, a row the caller cannot SELECT
		// would look absent and a perfectly valid write would be rejected.
		// search_path is pinned so the body cannot be redirected.
		body := fmt.Sprintf(`
create or replace function %s() returns trigger
language plpgsql security definer set search_path = %s, pg_catalog as $fn$
begin
  if new.%s is null or new.%s is null then
    return new;
  end if;
  if not exists (
    select 1 from %s t
    where t.%s = new.%s and t.%s is not distinct from new.%s
  ) then
    raise exception 'related record does not match this row'
      using errcode = '23514', constraint = %s;
  end if;
  return new;
end
$fn$;`,
			quoteIdent(project.SchemaName, fnName),
			quoteIdent(project.SchemaName),
			quoteIdent(rule.Field), quoteIdent(rule.Local),
			quoteIdent(project.SchemaName, targetName),
			quoteIdent(collectionPrimaryKeyField(target)), quoteIdent(rule.Field),
			quoteIdent(rule.Remote), quoteIdent(rule.Local),
			quoteLiteral(constraintName),
		)
		_ = relField
		if _, err := tx.Exec(ctx, body); err != nil {
			return mapConstraintError(err, rule.Name)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop trigger if exists %s on %s`, quoteIdent(tgName), table)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`create trigger %s before insert or update on %s for each row execute function %s()`,
			quoteIdent(tgName), table, quoteIdent(project.SchemaName, fnName),
		)); err != nil {
			return mapConstraintError(err, rule.Name)
		}
	}
	for name := range existing {
		if _, keep := desired[name]; !keep {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`drop trigger if exists %s on %s`, quoteIdent(name), table)); err != nil {
				return err
			}
		}
	}

	return syncExclusionRules(ctx, tx, project, collection, opts.Exclusions)
}

func syncExclusionRules(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection, rules []ExclusionRule) error {
	table := quoteIdent(project.SchemaName, collection.Name)
	existing, err := ownedExclusionNames(ctx, tx, project.SchemaName, collection.Name)
	if err != nil {
		return err
	}
	desired := map[string]struct{}{}
	if len(rules) > 0 {
		// Comparing a scalar with = inside a gist index needs btree_gist.
		if _, err := tx.Exec(ctx, `create extension if not exists btree_gist`); err != nil {
			return fmt.Errorf("%w: exclusion rules require the btree_gist extension, which this database role cannot create: %s",
				ErrValidation, pgErrMessage(err))
		}
	}
	for _, rule := range rules {
		name := "dbo_ex_" + collection.Name + "_" + rule.Name
		desired[name] = struct{}{}
		parts := make([]string, 0, len(rule.Equals)+1)
		for _, field := range rule.Equals {
			parts = append(parts, fmt.Sprintf("%s with =", recordColumnSQL(collection, field)))
		}
		parts = append(parts, fmt.Sprintf("tstzrange(%s, %s) with &&",
			recordColumnSQL(collection, rule.From), recordColumnSQL(collection, rule.To)))
		stmt := fmt.Sprintf(`alter table %s add constraint %s exclude using gist (%s)`,
			table, quoteIdent(name), strings.Join(parts, ", "))
		if strings.TrimSpace(rule.Where) != "" {
			where, err := compileImmutableExpr(rule.Where, collection, "exclusion where")
			if err != nil {
				return err
			}
			stmt += " where (" + where + ")"
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, table, quoteIdent(name))); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return mapConstraintError(err, rule.Name)
		}
	}
	for name := range existing {
		if _, keep := desired[name]; !keep {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, table, quoteIdent(name))); err != nil {
				return err
			}
		}
	}
	return nil
}

func ownedTriggerNames(ctx context.Context, tx pgx.Tx, schemaName, tableName string) (map[string]struct{}, error) {
	return scanNameSet(ctx, tx, `
		select tg.tgname from pg_trigger tg
		join pg_class cls on cls.oid = tg.tgrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where ns.nspname = $1 and cls.relname = $2 and not tg.tgisinternal and tg.tgname like 'dbo_cstg_%'`,
		schemaName, tableName)
}

func ownedExclusionNames(ctx context.Context, tx pgx.Tx, schemaName, tableName string) (map[string]struct{}, error) {
	return scanNameSet(ctx, tx, `
		select con.conname from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where ns.nspname = $1 and cls.relname = $2 and con.contype = 'x' and con.conname like 'dbo_ex_%'`,
		schemaName, tableName)
}

func scanNameSet(ctx context.Context, tx pgx.Tx, query string, args ...any) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx, query, args...)
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
