package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Field struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Required bool           `json:"required,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

var supportedFieldTypes = map[string]struct{}{
	"bool":     {},
	"date":     {},
	"email":    {},
	"file":     {},
	"json":     {},
	"number":   {},
	"relation": {},
	"select":   {},
	"text":     {},
	"url":      {},
}

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
	switch field.Type {
	case "select":
		values, ok := field.Options["values"]
		if field.Options == nil || !ok {
			return fmt.Errorf("%w: select field %q requires options.values", ErrValidation, field.Name)
		}
		if len(stringSlice(values)) == 0 {
			return fmt.Errorf("%w: select field %q requires at least one value", ErrValidation, field.Name)
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
	case "file":
		if _, ok := field.Options["multiple"]; ok {
			if _, ok := field.Options["multiple"].(bool); !ok {
				return fmt.Errorf("%w: file field %q options.multiple must be a boolean", ErrValidation, field.Name)
			}
		}
	case "text", "email", "url", "number", "bool", "date", "json":
	}
	return nil
}

func ColumnDDL(field Field) (string, error) {
	var columnType string
	switch field.Type {
	case "text", "email", "url":
		columnType = "text"
	case "number":
		columnType = "double precision"
	case "bool":
		columnType = "boolean"
	case "date":
		columnType = "timestamptz"
	case "json", "file":
		columnType = "jsonb"
	case "select":
		if boolOption(field.Options, "multi") {
			columnType = "text[]"
		} else {
			columnType = "text"
		}
	case "relation":
		if boolOption(field.Options, "multi") {
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
		if out[i].Options == nil {
			out[i].Options = map[string]any{}
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
