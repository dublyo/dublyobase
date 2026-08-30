package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// RollupSpec makes a field the running aggregate of a related collection's
// rows: an order's line subtotal, a product's review count, a customer's
// lifetime spend.
//
// It is maintained rather than checked. A constraint that merely rejected a
// disagreeing total would leave every caller to compute the total correctly
// first, which is the work they wanted the database to do. Here the value is
// written by a trigger on the child table, so it cannot drift and no client has
// to remember to update it.
type RollupSpec struct {
	Collection string `json:"collection"` // the child collection
	Field      string `json:"field"`      // the relation on the child pointing back here
	Aggregate  string `json:"aggregate"`  // sum, count, avg, min, max
	Source     string `json:"source"`     // the child field to aggregate; unused for count
	Where      string `json:"where"`      // optional filter on the child row
}

var rollupAggregates = map[string]struct{}{
	"sum": {}, "count": {}, "avg": {}, "min": {}, "max": {},
}

func fieldRollup(field Field) (RollupSpec, bool) {
	raw, ok := field.Options["rollup"].(map[string]any)
	if !ok {
		return RollupSpec{}, false
	}
	spec := RollupSpec{
		Collection: stringFromAny(raw["collection"]),
		Field:      stringFromAny(raw["field"]),
		Aggregate:  strings.ToLower(strings.TrimSpace(stringFromAny(raw["aggregate"]))),
		Source:     stringFromAny(raw["source"]),
		Where:      stringFromAny(raw["where"]),
	}
	if spec.Aggregate == "" {
		spec.Aggregate = "sum"
	}
	return spec, spec.Collection != "" && spec.Field != ""
}

func stringFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// FieldIsRollup reports whether the database owns this field's value.
func FieldIsRollup(field Field) bool {
	_, ok := fieldRollup(field)
	return ok
}

func validateRollupField(field Field) error {
	spec, ok := fieldRollup(field)
	if !ok {
		return nil
	}
	if _, valid := rollupAggregates[spec.Aggregate]; !valid {
		return fmt.Errorf("%w: rollup field %q aggregate must be sum, count, avg, min or max", ErrValidation, field.Name)
	}
	if spec.Aggregate != "count" && spec.Source == "" {
		return fmt.Errorf("%w: rollup field %q needs options.rollup.source for a %s", ErrValidation, field.Name, spec.Aggregate)
	}
	switch field.Type {
	case "number", "decimal":
	default:
		return fmt.Errorf("%w: rollup is not supported on %q fields", ErrValidation, field.Type)
	}
	if field.Required {
		return fmt.Errorf("%w: rollup field %q cannot also be required — the database supplies its value", ErrValidation, field.Name)
	}
	if FieldIsComputed(field) {
		return fmt.Errorf("%w: field %q cannot be both computed and a rollup", ErrValidation, field.Name)
	}
	return nil
}

func collectionRollupFields(collection *Collection) []Field {
	var out []Field
	for _, field := range collection.Fields {
		if FieldIsRollup(field) {
			out = append(out, field)
		}
	}
	return out
}

func rollupObjectNames(parent, fieldName string) (fnName, tgName string) {
	return "dbo_rollup_fn_" + parent + "_" + fieldName, "dbo_rollup_" + parent + "_" + fieldName
}

// syncCollectionRollups installs the triggers that maintain this collection's
// rollup fields. The triggers live on the child tables, because that is where
// the writes that change the answer happen.
func syncCollectionRollups(ctx context.Context, tx pgx.Tx, project *Project, collection *Collection) error {
	for _, field := range collection.Fields {
		if err := validateRollupField(field); err != nil {
			return err
		}
	}
	parentTable := quoteIdent(project.SchemaName, collection.Name)
	parentPK := quoteIdent(collectionPrimaryKeyField(collection))

	for _, field := range collectionRollupFields(collection) {
		spec, _ := fieldRollup(field)
		child, err := getCollectionTx(ctx, tx, project.ID, NormalizeIdentifier(spec.Collection))
		if err != nil {
			return fmt.Errorf("%w: rollup field %q targets unknown collection %q", ErrValidation, field.Name, spec.Collection)
		}
		relField, ok := fieldOnCollection(child, NormalizeIdentifier(spec.Field))
		if !ok || relField.Type != "relation" {
			return fmt.Errorf("%w: rollup field %q needs %q.%s to be a relation", ErrValidation, field.Name, spec.Collection, spec.Field)
		}
		if fieldIsMultiple(relField) {
			return fmt.Errorf("%w: rollup field %q cannot aggregate through a multi-value relation", ErrValidation, field.Name)
		}
		if target, _ := relField.Options["collection"].(string); NormalizeIdentifier(target) != collection.Name {
			return fmt.Errorf("%w: rollup field %q expects %q.%s to point at %q", ErrValidation, field.Name, spec.Collection, spec.Field, collection.Name)
		}
		if spec.Aggregate != "count" && !fieldExistsOnCollection(child, NormalizeIdentifier(spec.Source)) {
			return fmt.Errorf("%w: rollup field %q references unknown field %q on %q", ErrValidation, field.Name, spec.Source, spec.Collection)
		}

		expr := "count(*)"
		if spec.Aggregate != "count" {
			expr = fmt.Sprintf("%s(c.%s)", spec.Aggregate, quoteIdent(fieldSourceColumn(mustField(child, spec.Source))))
		}
		where := fmt.Sprintf("c.%s = p.%s", quoteIdent(fieldSourceColumn(relField)), parentPK)
		if strings.TrimSpace(spec.Where) != "" {
			filter, err := compileImmutableExpr(spec.Where, child, "rollup where")
			if err != nil {
				return err
			}
			where += " and (" + strings.ReplaceAll(filter, `"`, `"`) + ")"
		}
		// sum and count of nothing is zero; the others are genuinely unknown.
		zero := ""
		if spec.Aggregate == "sum" || spec.Aggregate == "count" {
			zero = ", 0"
		}
		recompute := fmt.Sprintf(
			`update %s p set %s = coalesce((select %s from %s c where %s)%s) where p.%s = $1`,
			parentTable, quoteIdent(fieldSourceColumn(field)), expr,
			quoteIdent(project.SchemaName, child.Name), where, zero, parentPK)

		fnName, tgName := rollupObjectNames(collection.Name, field.Name)
		childTable := quoteIdent(project.SchemaName, child.Name)
		childRel := quoteIdent(fieldSourceColumn(relField))

		// SECURITY DEFINER for the same reason the consistency rules use it: the
		// aggregate is a fact about the data, not about what this caller may
		// read. Both the old and the new parent are refreshed, because moving a
		// child from one parent to another changes two answers.
		body := fmt.Sprintf(`
create or replace function %s() returns trigger
language plpgsql security definer set search_path = %s, pg_catalog as $fn$
begin
  if tg_op in ('UPDATE','DELETE') and old.%s is not null then
    %s;
  end if;
  if tg_op in ('INSERT','UPDATE') and new.%s is not null
     and (tg_op = 'INSERT' or new.%s is distinct from old.%s) then
    %s;
  end if;
  return null;
end
$fn$;`,
			quoteIdent(project.SchemaName, fnName), quoteIdent(project.SchemaName),
			childRel, strings.Replace(recompute, "$1", "old."+childRel, 1),
			childRel, childRel, childRel,
			strings.Replace(recompute, "$1", "new."+childRel, 1),
		)
		if _, err := tx.Exec(ctx, body); err != nil {
			return mapConstraintError(err, field.Name)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop trigger if exists %s on %s`, quoteIdent(tgName), childTable)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`create trigger %s after insert or update or delete on %s for each row execute function %s()`,
			quoteIdent(tgName), childTable, quoteIdent(project.SchemaName, fnName))); err != nil {
			return mapConstraintError(err, field.Name)
		}
		// A parent with no children never fires the child trigger, so without a
		// default a freshly created row reads null where it should read zero.
		// min, max and avg of nothing are genuinely unknown, so they stay null.
		if zero != "" {
			if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s alter column %s set default 0`,
				parentTable, quoteIdent(fieldSourceColumn(field)))); err != nil {
				return mapConstraintError(err, field.Name)
			}
		}
		// A rollup added to a table that already has rows would otherwise read
		// zero until each child happened to be touched.
		backfill := fmt.Sprintf(
			`update %s p set %s = coalesce((select %s from %s c where %s)%s)`,
			parentTable, quoteIdent(fieldSourceColumn(field)), expr,
			quoteIdent(project.SchemaName, child.Name), where, zero)
		if _, err := tx.Exec(ctx, backfill); err != nil {
			return mapConstraintError(err, field.Name)
		}
	}
	return nil
}

func mustField(collection *Collection, name string) Field {
	f, _ := fieldOnCollection(collection, NormalizeIdentifier(name))
	return f
}
