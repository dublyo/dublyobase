package core

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

type Field struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Required    bool           `json:"required,omitempty"`
	Hidden      bool           `json:"hidden,omitempty"`
	Presentable bool           `json:"presentable,omitempty"`
	Help        string         `json:"help,omitempty"`
	Options     map[string]any `json:"options,omitempty"`
}

var supportedFieldTypes = map[string]struct{}{
	"autodate": {},
	"bool":     {},
	"date":     {},
	"email":    {},
	"editor":   {},
	"file":     {},
	"json":     {},
	"number":   {},
	"password": {},
	"relation": {},
	"select":   {},
	"text":     {},
	"url":      {},
}

const maxSafeJSONInt int64 = 1<<53 - 1

func ParseFields(raw json.RawMessage) ([]Field, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var fields []Field
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("%w: invalid fields JSON", ErrValidation)
	}
	if err := ValidateFields(fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func ValidateFields(fields []Field) error {
	seen := map[string]struct{}{}
	for i := range fields {
		fields[i].Name = NormalizeIdentifier(fields[i].Name)
		fields[i].Type = strings.ToLower(strings.TrimSpace(fields[i].Type))
		if err := ValidateDataIdentifier("field name", fields[i].Name); err != nil {
			return err
		}
		if _, ok := seen[fields[i].Name]; ok {
			return fmt.Errorf("%w: duplicate field %q", ErrValidation, fields[i].Name)
		}
		seen[fields[i].Name] = struct{}{}
		if _, ok := supportedFieldTypes[fields[i].Type]; !ok {
			return fmt.Errorf("%w: unsupported field type %q", ErrValidation, fields[i].Type)
		}
		if err := validateFieldOptions(fields[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldOptions(field Field) error {
	if len(field.Help) > 500 {
		return fmt.Errorf("%w: field %q help text is too long", ErrValidation, field.Name)
	}
	if field.Hidden && field.Presentable {
		return fmt.Errorf("%w: hidden field %q cannot be presentable", ErrValidation, field.Name)
	}
	switch field.Type {
	case "autodate":
		if !boolOption(field.Options, "onCreate") && !boolOption(field.Options, "onUpdate") {
			return fmt.Errorf("%w: autodate field %q requires options.onCreate or options.onUpdate", ErrValidation, field.Name)
		}
	case "number":
		min, hasMin := floatOption(field.Options, "min")
		max, hasMax := floatOption(field.Options, "max")
		if hasMin && boolOption(field.Options, "onlyInt") && min != math.Trunc(min) {
			return fmt.Errorf("%w: number field %q options.min must be an integer when options.onlyInt is enabled", ErrValidation, field.Name)
		}
		if hasMax && boolOption(field.Options, "onlyInt") && max != math.Trunc(max) {
			return fmt.Errorf("%w: number field %q options.max must be an integer when options.onlyInt is enabled", ErrValidation, field.Name)
		}
		if hasMin && hasMax && max < min {
			return fmt.Errorf("%w: number field %q options.max must be greater than or equal to options.min", ErrValidation, field.Name)
		}
	case "text", "url", "password":
		min, hasMin := intOption(field.Options, "min")
		max, hasMax := intOption(field.Options, "max")
		if hasMin && min < 0 {
			return fmt.Errorf("%w: field %q options.min must be non-negative", ErrValidation, field.Name)
		}
		if hasMax && max < 0 {
			return fmt.Errorf("%w: field %q options.max must be non-negative", ErrValidation, field.Name)
		}
		if field.Type == "password" {
			if hasMin && min > 71 {
				return fmt.Errorf("%w: password field %q options.min must not exceed 71", ErrValidation, field.Name)
			}
			if hasMax && max > 71 {
				return fmt.Errorf("%w: password field %q options.max must not exceed 71", ErrValidation, field.Name)
			}
			cost, hasCost := intOption(field.Options, "cost")
			if hasCost && (cost < 4 || cost > 31) {
				return fmt.Errorf("%w: password field %q options.cost must be between 4 and 31", ErrValidation, field.Name)
			}
		}
		if hasMin && hasMax && max > 0 && max < min {
			return fmt.Errorf("%w: field %q options.max must be greater than or equal to options.min", ErrValidation, field.Name)
		}
		if pattern, _ := field.Options["pattern"].(string); pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("%w: field %q options.pattern is invalid", ErrValidation, field.Name)
			}
		}
	case "email":
		if len(stringSlice(field.Options["onlyDomains"])) > 0 && len(stringSlice(field.Options["exceptDomains"])) > 0 {
			return fmt.Errorf("%w: email field %q cannot use both onlyDomains and exceptDomains", ErrValidation, field.Name)
		}
	case "editor", "json":
		if maxSize, ok := int64Option(field.Options, "maxSize"); ok && (maxSize < 0 || maxSize > maxSafeJSONInt) {
			return fmt.Errorf("%w: field %q options.maxSize must be between 0 and %d", ErrValidation, field.Name, maxSafeJSONInt)
		}
	case "select":
		values, ok := field.Options["values"]
		if field.Options == nil || !ok {
			return fmt.Errorf("%w: select field %q requires options.values", ErrValidation, field.Name)
		}
		selectValues := stringSlice(values)
		if len(selectValues) == 0 {
			return fmt.Errorf("%w: select field %q requires at least one value", ErrValidation, field.Name)
		}
		if hasDuplicates(selectValues) {
			return fmt.Errorf("%w: select field %q options.values contains duplicates", ErrValidation, field.Name)
		}
		if err := validateMinMaxSelectOptions(field, len(selectValues)); err != nil {
			return err
		}
	case "relation":
		collection, _ := field.Options["collection"].(string)
		collection = NormalizeIdentifier(collection)
		if collection == "" {
			return fmt.Errorf("%w: relation field %q requires options.collection", ErrValidation, field.Name)
		}
		if err := ValidateDataIdentifier("relation collection", collection); err != nil {
			return err
		}
		if err := validateMinMaxSelectOptions(field, 0); err != nil {
			return err
		}
	case "file":
		if _, ok := field.Options["multiple"]; ok {
			if _, ok := field.Options["multiple"].(bool); !ok {
				return fmt.Errorf("%w: file field %q options.multiple must be a boolean", ErrValidation, field.Name)
			}
		}
		if err := validateMinMaxSelectOptions(field, 0); err != nil {
			return err
		}
		if maxSize, ok := int64Option(field.Options, "maxSize"); ok && (maxSize < 0 || maxSize > maxSafeJSONInt) {
			return fmt.Errorf("%w: file field %q options.maxSize must be between 0 and %d", ErrValidation, field.Name, maxSafeJSONInt)
		}
		for _, mime := range stringSlice(field.Options["mimeTypes"]) {
			if !strings.Contains(mime, "/") {
				return fmt.Errorf("%w: file field %q options.mimeTypes contains invalid MIME type", ErrValidation, field.Name)
			}
		}
	case "bool", "date":
	}
	return nil
}

func ColumnDDL(field Field) (string, error) {
	var columnType string
	switch field.Type {
	case "text", "email", "url", "editor", "password":
		columnType = "text"
	case "number":
		columnType = "double precision"
	case "bool":
		columnType = "boolean"
	case "date", "autodate":
		columnType = "timestamptz"
	case "json", "file":
		columnType = "jsonb"
	case "select":
		if fieldIsMultiple(field) {
			columnType = "text[]"
		} else {
			columnType = "text"
		}
	case "relation":
		if fieldIsMultiple(field) {
			columnType = "uuid[]"
		} else {
			columnType = "uuid"
		}
	default:
		return "", fmt.Errorf("%w: unsupported field type %q", ErrValidation, field.Type)
	}
	switch field.Type {
	case "bool":
		if field.Required {
			return "boolean not null", nil
		}
		return "boolean not null default false", nil
	case "json":
		return "jsonb not null default '{}'::jsonb", nil
	case "file":
		return "jsonb", nil
	}
	ddl := columnType
	if field.Required {
		ddl += " not null"
	}
	return ddl, nil
}

func normalizeFields(fields []Field) []Field {
	out := make([]Field, len(fields))
	copy(out, fields)
	for i := range out {
		out[i].Name = NormalizeIdentifier(out[i].Name)
		out[i].Type = strings.ToLower(strings.TrimSpace(out[i].Type))
		out[i].Help = strings.TrimSpace(out[i].Help)
		if out[i].Hidden {
			out[i].Presentable = false
		}
		if out[i].Options == nil {
			out[i].Options = map[string]any{}
		}
		if out[i].Type == "relation" {
			if collection, _ := out[i].Options["collection"].(string); collection != "" {
				out[i].Options["collection"] = NormalizeIdentifier(collection)
			}
		}
	}
	return out
}

func encodeFields(fields []Field) ([]byte, error) {
	fields = normalizeFields(fields)
	if fields == nil {
		fields = []Field{}
	}
	return json.Marshal(fields)
}

func fieldByName(fields []Field) map[string]Field {
	out := make(map[string]Field, len(fields))
	for _, field := range fields {
		out[field.Name] = field
	}
	return out
}

func boolOption(options map[string]any, key string) bool {
	v, _ := options[key].(bool)
	return v
}

func intOption(options map[string]any, key string) (int, bool) {
	v, ok := numericOption(options, key)
	maxInt := float64(int(^uint(0) >> 1))
	minInt := -maxInt - 1
	if !ok || v != math.Trunc(v) || v < minInt || v > maxInt {
		return 0, false
	}
	return int(v), true
}

func int64Option(options map[string]any, key string) (int64, bool) {
	v, ok := numericOption(options, key)
	if !ok || v != math.Trunc(v) || v < -float64(maxSafeJSONInt) || v > float64(maxSafeJSONInt) {
		return 0, false
	}
	return int64(v), true
}

func floatOption(options map[string]any, key string) (float64, bool) {
	return numericOption(options, key)
}

func numericOption(options map[string]any, key string) (float64, bool) {
	if options == nil {
		return 0, false
	}
	switch v := options[key].(type) {
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return 0, false
		}
		return v, true
	case float32:
		out := float64(v)
		if math.IsInf(out, 0) || math.IsNaN(out) {
			return 0, false
		}
		return out, true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		out, err := v.Float64()
		return out, err == nil
	default:
		return 0, false
	}
}

func validateMinMaxSelectOptions(field Field, maxAllowed int) error {
	minSelect, hasMin := intOption(field.Options, "minSelect")
	maxSelect, hasMax := intOption(field.Options, "maxSelect")
	if hasMin && minSelect < 0 {
		return fmt.Errorf("%w: field %q options.minSelect must be non-negative", ErrValidation, field.Name)
	}
	if hasMax && maxSelect < 1 {
		return fmt.Errorf("%w: field %q options.maxSelect must be greater than zero", ErrValidation, field.Name)
	}
	if hasMin && hasMax && maxSelect < minSelect {
		return fmt.Errorf("%w: field %q options.maxSelect must be greater than or equal to options.minSelect", ErrValidation, field.Name)
	}
	if maxAllowed > 0 && hasMax && maxSelect > maxAllowed {
		return fmt.Errorf("%w: field %q options.maxSelect cannot exceed available values", ErrValidation, field.Name)
	}
	return nil
}

func fieldIsMultiple(field Field) bool {
	if maxSelect, ok := intOption(field.Options, "maxSelect"); ok && maxSelect > 1 {
		return true
	}
	return boolOption(field.Options, "multi") || boolOption(field.Options, "multiple")
}

func hasDuplicates(values []string) bool {
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func stringSlice(v any) []string {
	switch raw := v.(type) {
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			s, ok := item.(string)
			if !ok {
				return nil
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		return nil
	}
}
