package core

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

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
)

// utf8BOM is written first because Excel assumes the system codepage otherwise
// and renders UTF-8 as mojibake — which for Arabic names means the file is
// useless, silently, and only for some readers.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

type RecordExportOptions struct {
	Filter        string
	Search        string
	Sort          string
	Fields        string
	Limit         int
	RelationsAsID bool // emit the raw id instead of a human label
}

// ExportRecordsCSV writes the collection to w, honouring the same filter,
// search, sort and field projection as the record list — so what is exported is
// what the caller was looking at.
func ExportRecordsCSV(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, opts RecordExportOptions, w io.Writer) (int, error) {
	collection, err := recordCollection(ctx, pool, auth.Project.Slug, collectionName)
	if err != nil {
		return 0, err
	}
	columns, err := exportColumns(collection, opts.Fields)
	if err != nil {
		return 0, err
	}
	// Relation columns are resolved to labels, which needs the target
	// definitions and the expand data.
	targets := map[string]*Collection{}
	expand := []string{}
	if !opts.RelationsAsID {
		for _, name := range columns {
			field, ok := fieldOnCollection(collection, name)
			if !ok || field.Type != "relation" {
				continue
			}
			targetName, _ := field.Options["collection"].(string)
			targetName = NormalizeIdentifier(targetName)
			if targetName == "" {
				continue
			}
			if _, seen := targets[targetName]; !seen {
				target, err := recordCollection(ctx, pool, auth.Project.Slug, targetName)
				if err == nil {
					targets[targetName] = target
				}
			}
			expand = append(expand, name)
		}
	}

	if _, err := w.Write(utf8BOM); err != nil {
		return 0, err
	}
	writer := csv.NewWriter(w)
	if err := writer.Write(columns); err != nil {
		return 0, err
	}

	limit := opts.Limit
	if limit <= 0 || limit > maxExportRows {
		limit = maxExportRows
	}
	written := 0
	for offset := 0; written < limit; offset += exportBatchSize {
		perPage := exportBatchSize
		if remaining := limit - written; remaining < perPage {
			perPage = remaining
		}
		page, err := ListRecords(ctx, pool, auth, collectionName, RecordListOptions{
			Page:      1,
			PerPage:   perPage,
			Offset:    offset,
			Sort:      opts.Sort,
			Filter:    opts.Filter,
			Search:    opts.Search,
			Expand:    strings.Join(expand, ","),
			SkipTotal: true,
		})
		if err != nil {
			return written, err
		}
		if len(page.Items) == 0 {
			break
		}
		for _, record := range page.Items {
			row := make([]string, len(columns))
			for i, column := range columns {
				row[i] = exportCell(record, column, collection, targets, opts.RelationsAsID)
			}
			if err := writer.Write(row); err != nil {
				return written, err
			}
			written++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return written, err
		}
		if len(page.Items) < perPage {
			break
		}
	}
	writer.Flush()
	return written, writer.Error()
}

// ExportAggregateCSV writes a grouped aggregate. Totals are computed in
// Postgres, so a report over two one-to-many branches cannot be inflated by the
// fan-out a flat join would produce.
func ExportAggregateCSV(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, collectionName string, input AggregateInput, w io.Writer) (int, error) {
	result, err := AggregateRecords(ctx, pool, auth, collectionName, input)
	if err != nil {
		return 0, err
	}
	groupCols := make([]string, 0, len(input.GroupBy))
	for _, raw := range input.GroupBy {
		groupCols = append(groupCols, NormalizeIdentifier(raw))
	}
	valueCols := map[string]struct{}{}
	for _, item := range result.Items {
		for key := range item.Values {
			valueCols[key] = struct{}{}
		}
	}
	values := make([]string, 0, len(valueCols))
	for key := range valueCols {
		values = append(values, key)
	}
	sort.Strings(values)

	if _, err := w.Write(utf8BOM); err != nil {
		return 0, err
	}
	writer := csv.NewWriter(w)
	if err := writer.Write(append(append([]string{}, groupCols...), values...)); err != nil {
		return 0, err
	}
	for _, item := range result.Items {
		row := make([]string, 0, len(groupCols)+len(values))
		for _, col := range groupCols {
			row = append(row, formatExportValue(item.Group[col]))
		}
		for _, col := range values {
			row = append(row, formatExportValue(item.Values[col]))
		}
		if err := writer.Write(row); err != nil {
			return len(result.Items), err
		}
	}
	writer.Flush()
	return len(result.Items), writer.Error()
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
