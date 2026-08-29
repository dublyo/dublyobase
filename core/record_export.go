package core

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CSV export of records and aggregates. Both stream: rows are written as they
// are read so a large export does not have to be held in memory, and both go
// through the ordinary record path, so row-level security decides what is in
// the file exactly as it decides what is on the screen.

const (
	exportBatchSize   = 500
	maxExportRows     = 100_000
	exportRelationSep = "; "
	// long enough to stream a large collection, short enough that a runaway
	// query still releases its connection
	exportStatementTimeout = "300s"
)

// utf8BOM is written first because Excel assumes the system codepage otherwise
// and renders UTF-8 as mojibake — which for Arabic names means the file is
// useless, silently, and only for some readers.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// A column in an export is either a plain field or a dotted path that walks
// many-to-one relations: `client.home_location.name`. Walking towards ONE
// record is unambiguous, which is why only that direction is supported —
// flattening a one-to-many (a patient's appointments) has several right answers
// and belongs in an aggregate, not a column.
type exportPath struct {
	header string
	steps  []string // relation field names to walk
	leaf   string   // field to read on the final record, "" means label the record
}

const maxExportDepth = 4

func parseExportPath(collection *Collection, collections map[string]*Collection, raw string) (exportPath, error) {
	parts := strings.Split(raw, ".")
	if len(parts) > maxExportDepth {
		return exportPath{}, fmt.Errorf("%w: %q walks more than %d relations", ErrValidation, raw, maxExportDepth)
	}
	path := exportPath{header: raw}
	current := collection
	for i, part := range parts {
		name := NormalizeIdentifier(part)
		if name == "" {
			return exportPath{}, fmt.Errorf("%w: empty segment in %q", ErrValidation, raw)
		}
		field, ok := fieldOnCollection(current, name)
		last := i == len(parts)-1
		if !ok {
			if last && (name == collectionPrimaryKeyField(current) || name == "created" || name == "updated") {
				path.leaf = name
				return path, nil
			}
			return exportPath{}, fmt.Errorf("%w: %q has no field %q", ErrValidation, raw, name)
		}
		if last {
			path.leaf = name
			return path, nil
		}
		if field.Type != "relation" || fieldIsMultiple(field) {
			return exportPath{}, fmt.Errorf("%w: %q is not a single relation, so %q cannot continue through it", ErrValidation, name, raw)
		}
		targetName, _ := field.Options["collection"].(string)
		next, ok := collections[NormalizeIdentifier(targetName)]
		if !ok {
			return exportPath{}, fmt.Errorf("%w: %q points at an unavailable collection", ErrValidation, name)
		}
		path.steps = append(path.steps, name)
		current = next
	}
	return path, nil
}

type RecordExportOptions struct {
	Filter        string
	Search        string
	Sort          string
	Fields        string
	Limit         int
	RelationsAsID bool // emit the raw id instead of a human label
}

// ValidateExportOptions resolves the field paths without touching a row, so a
// bad path is rejected before any bytes are committed. Without it the response
// headers and the BOM are already sent by the time the error is discovered, and
// the caller downloads a "CSV" whose first line is a JSON error.
func ValidateExportOptions(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, opts RecordExportOptions) error {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return err
	}
	reachable, err := reachableCollections(ctx, pool, auth, collection)
	if err != nil {
		return err
	}
	if _, err := exportPaths(collection, reachable, opts.Fields); err != nil {
		return err
	}
	if _, err := orderByClause(collection, opts.Sort); err != nil {
		return err
	}
	_, err = CompileRecordListFilter(opts.Filter, opts.Search, collection)
	return err
}

// ExportRows streams the collection to fn one row at a time, honouring the same
// filter, search, sort and field selection as the record list. It is one query
// rather than a page loop: OFFSET paging re-scans everything it has already
// skipped, so the cost grows with the square of the row count, and a large
// export slows to a crawl exactly when it matters.
func ExportRows(
	ctx context.Context,
	pool *pgxpool.Pool,
	auth *RecordAuth,
	collectionName string,
	opts RecordExportOptions,
	header func([]string) error,
	row func([]string) error,
) (int, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return 0, err
	}
	// Every collection a path can reach, resolved once.
	reachable, err := reachableCollections(ctx, pool, auth, collection)
	if err != nil {
		return 0, err
	}
	paths, err := exportPaths(collection, reachable, opts.Fields)
	if err != nil {
		return 0, err
	}
	baseColumns := allRecordColumns(collection)
	table, err := recordTable(auth, collection)
	if err != nil {
		return 0, err
	}
	orderBy, err := orderByClause(collection, opts.Sort)
	if err != nil {
		return 0, err
	}
	filter, err := CompileRecordListFilter(opts.Filter, opts.Search, collection)
	if err != nil {
		return 0, err
	}
	where := ""
	if filter.SQL != "" {
		where = " where " + filter.SQL
	}
	limit := opts.Limit
	if limit <= 0 || limit > maxExportRows {
		limit = maxExportRows
	}
	query := fmt.Sprintf(`select %s from %s%s order by %s limit %d`,
		recordSelectList(collection, baseColumns), table, where, orderBy, limit)

	headers := make([]string, len(paths))
	for i, p := range paths {
		headers[i] = p.header
	}
	if err := header(headers); err != nil {
		return 0, err
	}

	// Rows are buffered in chunks so related records can be fetched in batches
	// rather than one lookup per cell.
	written := 0
	buffer := make([]Record, 0, exportBatchSize)
	flush := func() error {
		if len(buffer) == 0 {
			return nil
		}
		if err := resolveExportPaths(ctx, pool, auth, collection, reachable, paths, buffer, opts.RelationsAsID); err != nil {
			return err
		}
		for _, record := range buffer {
			cells := make([]string, len(paths))
			for i, p := range paths {
				cells[i] = exportPathValue(record, p, collection, reachable, opts.RelationsAsID)
			}
			if err := row(cells); err != nil {
				return err
			}
			written++
		}
		buffer = buffer[:0]
		return nil
	}

	err = withRecordTxTimeout(ctx, pool, auth, "list", exportStatementTimeout, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, filter.Args...)
		if err != nil {
			return mapRecordDBError(err)
		}
		defer rows.Close()
		for rows.Next() {
			record, err := scanRecordValues(rows, baseColumns, columnFormats(collection))
			if err != nil {
				return err
			}
			buffer = append(buffer, record)
			if len(buffer) >= exportBatchSize {
				// The related-record lookups must not run on this connection
				// while its rows are still being read, so the buffer is drained
				// after the cursor is exhausted for this chunk.
				if err := rows.Err(); err != nil {
					return mapRecordDBError(err)
				}
			}
		}
		return mapRecordDBError(rows.Err())
	})
	if err != nil {
		return written, err
	}
	// Drain in batches now that the reading transaction is closed.
	all := buffer
	buffer = make([]Record, 0, exportBatchSize)
	for start := 0; start < len(all); start += exportBatchSize {
		stop := start + exportBatchSize
		if stop > len(all) {
			stop = len(all)
		}
		buffer = append(buffer[:0], all[start:stop]...)
		if err := flush(); err != nil {
			return written, err
		}
	}
	return written, nil
}

// ExportRecordsCSV writes the collection to w as CSV.
func ExportRecordsCSV(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, opts RecordExportOptions, w io.Writer) (int, error) {
	if _, err := w.Write(utf8BOM); err != nil {
		return 0, err
	}
	writer := csv.NewWriter(w)
	n := 0
	count, err := ExportRows(ctx, pool, auth, collectionName, opts,
		func(h []string) error { return writer.Write(h) },
		func(r []string) error {
			n++
			if n%exportBatchSize == 0 {
				writer.Flush()
			}
			return writer.Write(r)
		})
	writer.Flush()
	if err != nil {
		return count, err
	}
	return count, writer.Error()
}

// exportAggregateTo emits a grouped aggregate through the given header/row
// writers, so CSV and XLSX share one implementation.
func exportAggregateTo(
	ctx context.Context,
	pool *pgxpool.Pool,
	auth *RecordAuth,
	collectionName string,
	input AggregateInput,
	header func([]string) error,
	row func([]string) error,
) (int, error) {
	result, err := AggregateRecords(ctx, pool, auth, collectionName, input)
	if err != nil {
		return 0, err
	}
	groupCols := make([]string, 0, len(input.GroupBy))
	for _, raw := range input.GroupBy {
		groupCols = append(groupCols, NormalizeIdentifier(raw))
	}
	valueSet := map[string]struct{}{}
	for _, item := range result.Items {
		for key := range item.Values {
			valueSet[key] = struct{}{}
		}
	}
	values := make([]string, 0, len(valueSet))
	for key := range valueSet {
		values = append(values, key)
	}
	sort.Strings(values)
	if err := header(append(append([]string{}, groupCols...), values...)); err != nil {
		return 0, err
	}
	for _, item := range result.Items {
		cells := make([]string, 0, len(groupCols)+len(values))
		for _, col := range groupCols {
			cells = append(cells, formatExportValue(item.Group[col]))
		}
		for _, col := range values {
			cells = append(cells, formatExportValue(item.Values[col]))
		}
		if err := row(cells); err != nil {
			return len(result.Items), err
		}
	}
	return len(result.Items), nil
}

// ExportAggregateCSV writes a grouped aggregate as CSV. Totals are computed in
// Postgres, so a report over two one-to-many relations cannot be inflated by
// the fan-out a flat join would produce.
func ExportAggregateCSV(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, input AggregateInput, w io.Writer) (int, error) {
	if _, err := w.Write(utf8BOM); err != nil {
		return 0, err
	}
	writer := csv.NewWriter(w)
	count, err := exportAggregateTo(ctx, pool, auth, collectionName, input, writer.Write, writer.Write)
	writer.Flush()
	if err != nil {
		return count, err
	}
	return count, writer.Error()
}

func exportColumns(collection *Collection, fields string) ([]string, error) {
	all := allRecordColumns(collection)
	if strings.TrimSpace(fields) == "" {
		return all, nil
	}
	allowed := map[string]struct{}{}
	for _, name := range all {
		allowed[name] = struct{}{}
	}
	out := []string{}
	for _, part := range strings.Split(fields, ",") {
		name := NormalizeIdentifier(part)
		if name == "" {
			continue
		}
		if _, ok := allowed[name]; !ok {
			return nil, fmt.Errorf("%w: unknown field %q", ErrValidation, name)
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return all, nil
	}
	return out, nil
}

func fieldOnCollection(collection *Collection, name string) (Field, bool) {
	for _, field := range collection.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return Field{}, false
}

func exportCell(record Record, column string, collection *Collection, targets map[string]*Collection, rawIDs bool) string {
	field, isField := fieldOnCollection(collection, column)
	if isField && field.Type == "relation" && !rawIDs {
		targetName, _ := field.Options["collection"].(string)
		target := targets[NormalizeIdentifier(targetName)]
		displayField, _ := field.Options["displayField"].(string)
		expanded, _ := record["expand"].(map[string]any)
		switch value := expanded[column].(type) {
		case map[string]any:
			return RelationLabel(Record(value), target, displayField)
		case Record:
			return RelationLabel(value, target, displayField)
		case []Record:
			parts := make([]string, 0, len(value))
			for _, item := range value {
				parts = append(parts, RelationLabel(item, target, displayField))
			}
			return strings.Join(parts, exportRelationSep)
		case []any:
			parts := make([]string, 0, len(value))
			for _, item := range value {
				if m, ok := item.(map[string]any); ok {
					parts = append(parts, RelationLabel(Record(m), target, displayField))
				}
			}
			return strings.Join(parts, exportRelationSep)
		}
	}
	return formatExportValue(record[column])
}

// RelationLabel renders a related record the way a person would recognise it:
// the configured display field, then any field marked presentable, then a
// name-like column, then an identifier-like one. This mirrors the admin panel
// so an export reads the same as the screen it came from.
func RelationLabel(record Record, target *Collection, displayField string) string {
	if record == nil {
		return ""
	}
	read := func(name string) string {
		if name == "" {
			return ""
		}
		if v, ok := record[name]; ok {
			if s := formatExportValue(v); s != "" {
				return s
			}
		}
		return ""
	}
	if label := read(NormalizeIdentifier(displayField)); label != "" {
		return label
	}
	if target != nil {
		for _, field := range target.Fields {
			if field.Presentable && !field.Hidden {
				if label := read(field.Name); label != "" {
					return label
				}
			}
		}
		for _, pattern := range []func(string) bool{
			func(n string) bool {
				return n == "full_name" || n == "name" || n == "display_name" || n == "title" || n == "label" || n == "subject"
			},
			func(n string) bool {
				return strings.Contains(n, "name") || strings.Contains(n, "title") || strings.Contains(n, "label")
			},
			func(n string) bool {
				return strings.HasSuffix(n, "_no") || strings.Contains(n, "number") || strings.Contains(n, "code") ||
					strings.Contains(n, "mrn") || strings.Contains(n, "reference") || strings.Contains(n, "sku")
			},
		} {
			for _, field := range target.Fields {
				if field.Hidden || (field.Type != "text" && field.Type != "email") {
					continue
				}
				if pattern(field.Name) {
					if label := read(field.Name); label != "" {
						return label
					}
				}
			}
		}
	}
	if id, ok := record["id"].(string); ok {
		return id
	}
	return ""
}

func formatExportValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, formatExportValue(item))
		}
		return strings.Join(parts, exportRelationSep)
	default:
		return fmt.Sprint(v)
	}
}

// reachableCollections loads every collection an export path could walk into,
// so path parsing and label rendering never hit the database per row.
func reachableCollections(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, root *Collection) (map[string]*Collection, error) {
	out := map[string]*Collection{root.Name: root}
	frontier := []*Collection{root}
	for depth := 0; depth < maxExportDepth && len(frontier) > 0; depth++ {
		next := []*Collection{}
		for _, collection := range frontier {
			for _, field := range collection.Fields {
				if field.Type != "relation" || field.Hidden {
					continue
				}
				name, _ := field.Options["collection"].(string)
				name = NormalizeIdentifier(name)
				if name == "" {
					continue
				}
				if _, seen := out[name]; seen {
					continue
				}
				target, err := recordCollection(ctx, pool, auth.Project.Slug, name)
				if err != nil {
					continue // a relation to something unreadable simply cannot be walked
				}
				out[name] = target
				next = append(next, target)
			}
		}
		frontier = next
	}
	return out, nil
}

func exportPaths(collection *Collection, reachable map[string]*Collection, fields string) ([]exportPath, error) {
	if strings.TrimSpace(fields) == "" {
		out := []exportPath{}
		for _, name := range allRecordColumns(collection) {
			out = append(out, exportPath{header: name, leaf: name})
		}
		return out, nil
	}
	out := []exportPath{}
	for _, part := range strings.Split(fields, ",") {
		raw := strings.TrimSpace(part)
		if raw == "" {
			continue
		}
		path, err := parseExportPath(collection, reachable, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	if len(out) == 0 {
		return exportPaths(collection, reachable, "")
	}
	return out, nil
}

// resolveExportPaths walks every path one hop at a time for the whole chunk, so
// a batch of 500 rows costs one query per relation step rather than one per
// cell. Resolved records are cached on each row under a private key.
func resolveExportPaths(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collection *Collection, reachable map[string]*Collection, paths []exportPath, records []Record, rawIDs bool) error {
	// Every hop is resolved for the whole chunk at once, and the result is
	// stored on the ROOT row keyed by the path prefix. Storing it on the
	// intermediate record instead would work for one hop and silently produce
	// blanks for two, because the next hop reads from the root again.
	done := map[string]struct{}{}
	for _, path := range paths {
		steps := path.steps
		if !rawIDs {
			if leafOwner := walkCollection(collection, reachable, path.steps); leafOwner != nil {
				if leaf, ok := fieldOnCollection(leafOwner, path.leaf); ok && leaf.Type == "relation" && !fieldIsMultiple(leaf) {
					steps = append(append([]string{}, path.steps...), path.leaf)
				}
			}
		}
		current := collection
		prefix := ""
		for _, step := range steps {
			field, ok := fieldOnCollection(current, step)
			if !ok {
				break
			}
			targetName := NormalizeIdentifier(fmt.Sprint(field.Options["collection"]))
			target, ok := reachable[targetName]
			if !ok {
				break
			}
			nextPrefix := prefix + "/" + step
			if _, already := done[nextPrefix]; !already {
				ids := make([]string, 0, len(records))
				seen := map[string]struct{}{}
				for _, root := range records {
					id, _ := exportStepValue(root, prefix, step).(string)
					if id == "" {
						continue
					}
					if _, dup := seen[id]; dup {
						continue
					}
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
				if len(ids) > 0 {
					byID, err := getRecordsByIDs(ctx, pool, auth, targetName, ids)
					if err != nil {
						return err
					}
					for _, root := range records {
						id, _ := exportStepValue(root, prefix, step).(string)
						if rec, ok := byID[id]; ok {
							setExportResolved(root, nextPrefix, rec)
						}
					}
				}
				done[nextPrefix] = struct{}{}
			}
			prefix = nextPrefix
			current = target
		}
	}
	return nil
}

// walkCollection follows a chain of relation fields and returns the collection
// the last one lands on, or nil if the chain breaks.
func walkCollection(from *Collection, reachable map[string]*Collection, steps []string) *Collection {
	current := from
	for _, step := range steps {
		field, ok := fieldOnCollection(current, step)
		if !ok {
			return nil
		}
		current = reachable[NormalizeIdentifier(fmt.Sprint(field.Options["collection"]))]
		if current == nil {
			return nil
		}
	}
	return current
}

const exportResolvedKey = "__dbo_export_resolved"

func setExportResolved(row Record, prefix string, value Record) {
	bag, _ := row[exportResolvedKey].(map[string]Record)
	if bag == nil {
		bag = map[string]Record{}
		row[exportResolvedKey] = bag
	}
	bag[prefix] = value
}

func getExportResolved(row Record, prefix string) Record {
	bag, _ := row[exportResolvedKey].(map[string]Record)
	if bag == nil {
		return nil
	}
	return bag[prefix]
}

// exportStepValue reads the relation id for one hop: from the row itself at the
// root, or from whatever that row resolved to at deeper levels.
func exportStepValue(row Record, prefix string, step string) any {
	if prefix == "" {
		return row[step]
	}
	if rec := getExportResolved(row, prefix); rec != nil {
		return rec[step]
	}
	return nil
}

func exportPathValue(row Record, path exportPath, collection *Collection, reachable map[string]*Collection, rawIDs bool) string {
	current := collection
	prefix := ""
	source := row
	for _, step := range path.steps {
		field, ok := fieldOnCollection(current, step)
		if !ok {
			return ""
		}
		targetName := NormalizeIdentifier(fmt.Sprint(field.Options["collection"]))
		prefix += "/" + step
		resolved := getExportResolved(row, prefix)
		if resolved == nil {
			return ""
		}
		source = resolved
		current = reachable[targetName]
		if current == nil {
			return ""
		}
	}
	// The leaf may itself be a relation, in which case it is labelled rather
	// than printed as a uuid — unless raw ids were asked for.
	if field, ok := fieldOnCollection(current, path.leaf); ok && field.Type == "relation" && !rawIDs {
		targetName := NormalizeIdentifier(fmt.Sprint(field.Options["collection"]))
		target := reachable[targetName]
		displayField, _ := field.Options["displayField"].(string)
		if !fieldIsMultiple(field) {
			id, _ := source[path.leaf].(string)
			if id == "" {
				return ""
			}
			leafPrefix := prefix + "/" + path.leaf
			if rec := getExportResolved(row, leafPrefix); rec != nil {
				return RelationLabel(rec, target, displayField)
			}
			return id
		}
	}
	return formatExportValue(source[path.leaf])
}
