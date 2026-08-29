package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	// pgvector stores up to 16000 dimensions, but only indexes up to 2000. A
	// column past that limit still works and simply cannot be indexed, so it is
	// allowed and the index is skipped rather than refused.
	maxVectorDimensions      = 16000
	maxIndexableVectorDims   = 2000
	defaultVectorMetric      = "cosine"
	vectorEmbeddingSizeLimit = 1 << 20
)

// vectorMetrics maps the distance a collection asks for onto the operator class
// its index needs and the operator a search orders by.
var vectorMetrics = map[string]struct{ opClass, operator string }{
	// The operator is written OPERATOR(public.<=>) for the same reason the type
	// is qualified: operators resolve through search_path too, and the record
	// search_path deliberately excludes public.
	"cosine":        {"public.vector_cosine_ops", "OPERATOR(public.<=>)"},
	"l2":            {"public.vector_l2_ops", "OPERATOR(public.<->)"},
	"inner_product": {"public.vector_ip_ops", "OPERATOR(public.<#>)"},
}

func vectorOptions(field Field) (dims int, metric string) {
	metric = defaultVectorMetric
	if raw, ok := field.Options["metric"].(string); ok && strings.TrimSpace(raw) != "" {
		metric = strings.ToLower(strings.TrimSpace(raw))
	}
	if n, ok := intOption(field.Options, "dimensions"); ok {
		dims = n
	}
	return dims, metric
}

func validateVectorOptions(field Field) error {
	dims, metric := vectorOptions(field)
	if dims <= 0 {
		return fmt.Errorf("%w: vector field %q requires options.dimensions", ErrValidation, field.Name)
	}
	if dims > maxVectorDimensions {
		return fmt.Errorf("%w: vector field %q dimensions must be %d or fewer", ErrValidation, field.Name, maxVectorDimensions)
	}
	if _, ok := vectorMetrics[metric]; !ok {
		return fmt.Errorf("%w: vector field %q metric must be cosine, l2 or inner_product", ErrValidation, field.Name)
	}
	if fieldIsMultiple(field) {
		return fmt.Errorf("%w: vector field %q cannot be multiple", ErrValidation, field.Name)
	}
	return nil
}

// normalizeVectorInput accepts the JSON array a client sends and returns the
// literal pgvector parses. The dimension count is checked here rather than left
// to the database so the error names the field and both counts.
func normalizeVectorInput(field Field, raw json.RawMessage) (any, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if len(trimmed) > vectorEmbeddingSizeLimit {
		return nil, fmt.Errorf("%w: %s embedding is too large", ErrValidation, field.Name)
	}
	var values []float64
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%w: %s must be an array of numbers", ErrValidation, field.Name)
	}
	return vectorLiteral(field, values)
}

func vectorLiteral(field Field, values []float64) (string, error) {
	dims, _ := vectorOptions(field)
	if len(values) != dims {
		return "", fmt.Errorf("%w: %s expects %d dimensions, got %d", ErrValidation, field.Name, dims, len(values))
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "", fmt.Errorf("%w: %s cannot contain NaN or Infinity", ErrValidation, field.Name)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
	}
	b.WriteByte(']')
	return b.String(), nil
}

// formatVectorValue turns pgvector's text form back into a JSON array, so a
// client reads the same shape it wrote.
func formatVectorValue(raw string) any {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	if trimmed == "" {
		return []any{}
	}
	parts := strings.Split(trimmed, ",")
	out := make([]any, 0, len(parts))
	for _, part := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return raw
		}
		out = append(out, f)
	}
	return out
}

// vectorAvailable reports whether pgvector can be used, installing it when the
// database ships it and the role may create it. Like the trigram index, the
// attempt runs in a savepoint: a role without permission should still be able to
// save collections that do not use vectors.
func vectorAvailable(ctx context.Context, tx pgx.Tx) bool {
	var installed bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from pg_extension where extname = 'vector')`).Scan(&installed); err != nil {
		return false
	}
	if installed {
		return true
	}
	if _, err := tx.Exec(ctx, `savepoint dbo_vector`); err != nil {
		return false
	}
	if _, err := tx.Exec(ctx, `create extension if not exists vector schema public`); err != nil {
		_, _ = tx.Exec(ctx, `rollback to savepoint dbo_vector`)
		return false
	}
	_, _ = tx.Exec(ctx, `release savepoint dbo_vector`)
	return true
}

func collectionHasVectorField(collection *Collection) bool {
	for _, field := range collection.Fields {
		if field.Type == "vector" {
			return true
		}
	}
	return false
}

// compileVectorOrder builds the ORDER BY for a nearest-neighbour search and
// returns the literal to bind at the given placeholder position.
//
// The operator is chosen by the field's own metric so the ordering matches the
// index that was built for it; ordering by a different operator would silently
// fall back to a sequential scan.
func compileVectorOrder(collection *Collection, fieldName string, values []float64, argPos int) (Field, string, string, error) {
	name := NormalizeIdentifier(fieldName)
	var field Field
	found := false
	for _, candidate := range collection.Fields {
		if candidate.Name == name {
			field, found = candidate, true
			break
		}
	}
	if !found {
		return field, "", "", fmt.Errorf("%w: unknown field %q", ErrValidation, fieldName)
	}
	if field.Type != "vector" {
		return field, "", "", fmt.Errorf("%w: field %q is not a vector", ErrValidation, fieldName)
	}
	literal, err := vectorLiteral(field, values)
	if err != nil {
		return field, "", "", err
	}
	_, metric := vectorOptions(field)
	spec, ok := vectorMetrics[metric]
	if !ok {
		return field, "", "", fmt.Errorf("%w: field %q has an unknown metric", ErrValidation, fieldName)
	}
	expr := fmt.Sprintf("%s %s $%d::public.vector", recordColumnSQL(collection, field.Name), spec.operator, argPos)
	return field, literal, expr, nil
}
