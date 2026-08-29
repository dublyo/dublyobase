package core

import (
	"fmt"
	"strings"
)

// maxFilterRelationDepth caps how many relations one filter key may walk. Each
// hop is another nested subquery, so an uncapped path is a way to make the
// database do arbitrary work from a query string.
const maxFilterRelationDepth = 4

// filterRootAlias is the alias the outer query must give its table when
// relation filters are enabled. The subqueries qualify the outer columns with
// it, because an unqualified name would bind to the inner table whenever both
// happen to have a column of that name.
const filterRootAlias = "t"

// FilterRelations lets a filter reach fields on related collections. It is
// optional: without it a dotted key is rejected, which is what the aggregate
// and export paths still do.
type FilterRelations struct {
	// Collections is every collection reachable from the root by relation.
	Collections map[string]*Collection
	// Table returns the schema-qualified table a collection reads from.
	Table func(*Collection) (string, error)
}

// relationHop is one step along a dotted filter key.
type relationHop struct {
	field      Field       // the relation field on the previous collection
	target     *Collection // what it points at
	targetName string      // quoted, schema-qualified table
}

// resolveFilterPath walks a dotted key and returns the hops plus the field the
// last segment names. Every hop must be a declared relation, so a filter can
// only reach tables the schema already links to — never an arbitrary one.
func (b *recordFilterBuilder) resolveFilterPath(raw string) ([]relationHop, *Collection, Field, error) {
	parts := strings.Split(raw, ".")
	if len(parts) > maxFilterRelationDepth {
		return nil, nil, Field{}, fmt.Errorf("%w: %q walks more than %d relations", ErrInvalidFilter, raw, maxFilterRelationDepth)
	}
	if b.relations == nil || b.relations.Collections == nil || b.relations.Table == nil {
		return nil, nil, Field{}, fmt.Errorf("%w: unknown filter field %q", ErrInvalidFilter, raw)
	}

	current := b.collection
	hops := make([]relationHop, 0, len(parts)-1)
	for i, part := range parts {
		name := NormalizeIdentifier(part)
		if name == "" {
			return nil, nil, Field{}, fmt.Errorf("%w: empty segment in %q", ErrInvalidFilter, raw)
		}
		last := i == len(parts)-1
		if last {
			field, ok := filterableRecordField(current, name)
			if !ok {
				return nil, nil, Field{}, fmt.Errorf("%w: unknown filter field %q", ErrInvalidFilter, raw)
			}
			return hops, current, field, nil
		}
		field, ok := fieldOnCollection(current, name)
		if !ok || field.Type != "relation" {
			return nil, nil, Field{}, fmt.Errorf("%w: %q is not a relation, so %q cannot continue through it", ErrInvalidFilter, name, raw)
		}
		if field.Hidden {
			return nil, nil, Field{}, fmt.Errorf("%w: unknown filter field %q", ErrInvalidFilter, raw)
		}
		targetName, _ := field.Options["collection"].(string)
		target, ok := b.relations.Collections[NormalizeIdentifier(targetName)]
		if !ok {
			return nil, nil, Field{}, fmt.Errorf("%w: %q points at an unavailable collection", ErrInvalidFilter, name)
		}
		table, err := b.relations.Table(target)
		if err != nil {
			return nil, nil, Field{}, fmt.Errorf("%w: %q points at an unavailable collection", ErrInvalidFilter, name)
		}
		hops = append(hops, relationHop{field: field, target: target, targetName: table})
		current = target
	}
	return nil, nil, Field{}, fmt.Errorf("%w: unknown filter field %q", ErrInvalidFilter, raw)
}

// compileRelationPath turns a dotted key into nested EXISTS subqueries.
//
// The subqueries run as the same database role as the outer query, so
// row-level security applies to the related tables too: filtering on a row the
// caller cannot read simply does not match, rather than revealing it.
func (b *recordFilterBuilder) compileRelationPath(raw string, rawValue any, depth int) (string, error) {
	hops, leafCollection, leafField, err := b.resolveFilterPath(raw)
	if err != nil {
		return "", err
	}

	// Build the innermost predicate first, against the last collection's alias.
	alias := fmt.Sprintf("dbo_rel%d", len(hops))
	inner, err := b.compilePredicateOn(alias, leafCollection, leafField, rawValue, depth)
	if err != nil {
		return "", err
	}

	// Then wrap outwards, each hop joining its parent's relation column.
	for i := len(hops) - 1; i >= 0; i-- {
		hop := hops[i]
		childAlias := fmt.Sprintf("dbo_rel%d", i+1)
		parent := filterRootAlias
		if i > 0 {
			parent = fmt.Sprintf("dbo_rel%d", i)
		}
		parentCollection := b.collection
		if i > 0 {
			parentCollection = hops[i-1].target
		}
		parentColumn := aliasColumnSQL(parent, parentCollection, hop.field.Name)
		childKey := aliasColumnSQL(childAlias, hop.target, collectionPrimaryKeyField(hop.target))

		join := fmt.Sprintf("%s = %s", childKey, parentColumn)
		if fieldIsMultiple(hop.field) {
			// A multi-relation stores an array of ids; match any of them.
			join = fmt.Sprintf("%s = any(%s)", childKey, parentColumn)
		}
		inner = fmt.Sprintf("exists (select 1 from %s as %s where %s and (%s))",
			hop.targetName, quoteIdent(childAlias), join, inner)
	}
	return inner, nil
}

// aliasColumnSQL qualifies a collection's column with a table alias.
func aliasColumnSQL(alias string, collection *Collection, name string) string {
	return quoteIdent(alias) + "." + recordColumnSQL(collection, name)
}

// compilePredicateOn applies the operator map for the last segment of a dotted
// key against the related table's alias.
func (b *recordFilterBuilder) compilePredicateOn(alias string, collection *Collection, field Field, rawValue any, depth int) (string, error) {
	column := aliasColumnSQL(alias, collection, field.Name)
	ops, ok := rawValue.(map[string]any)
	if !ok || len(ops) == 0 {
		return b.compilePredicateColumn(column, field, "_eq", rawValue)
	}
	if depth > maxRecordFilterDepth {
		return "", fmt.Errorf("%w: JSON filter nesting is too deep", ErrInvalidFilter)
	}
	keys := sortedMapKeys(ops)
	parts := make([]string, 0, len(keys))
	for _, op := range keys {
		sql, err := b.compilePredicateColumn(column, field, op, ops[op])
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
	return "(" + strings.Join(parts, " and ") + ")", nil
}
