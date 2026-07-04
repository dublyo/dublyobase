package core

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultRecordPrimaryKey      = "id"
	defaultRecordPrimaryKeyType  = "uuid"
	collectionOptionImported     = "imported"
	collectionOptionManaged      = "managed"
	collectionOptionSourceSchema = "sourceSchema"
	collectionOptionSourceTable  = "sourceTable"
)

type collectionRuntimeOptions struct {
	Imported              bool   `json:"imported,omitempty"`
	Managed               bool   `json:"managed,omitempty"`
	SourceSchema          string `json:"sourceSchema,omitempty"`
	SourceTable           string `json:"sourceTable,omitempty"`
	PrimaryKey            string `json:"primaryKey,omitempty"`
	PrimaryKeyField       string `json:"primaryKeyField,omitempty"`
	PrimaryKeyType        string `json:"primaryKeyType,omitempty"`
	StandardSystemColumns bool   `json:"standardSystemColumns,omitempty"`
}

func parseCollectionRuntimeOptions(raw json.RawMessage) collectionRuntimeOptions {
	var opts collectionRuntimeOptions
	if len(raw) == 0 || string(raw) == "null" {
		return opts
	}
	_ = json.Unmarshal(raw, &opts)
	opts.SourceSchema = strings.TrimSpace(opts.SourceSchema)
	opts.SourceTable = strings.TrimSpace(opts.SourceTable)
	opts.PrimaryKey = strings.TrimSpace(opts.PrimaryKey)
	opts.PrimaryKeyField = NormalizeIdentifier(opts.PrimaryKeyField)
	opts.PrimaryKeyType = strings.ToLower(strings.TrimSpace(opts.PrimaryKeyType))
	return opts
}

func validateCollectionOptionsJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return fmt.Errorf("%w: options must be a JSON object", ErrValidation)
	}
	if body == nil {
		return fmt.Errorf("%w: options must be a JSON object", ErrValidation)
	}
	return nil
}

func collectionOptionsImported(raw json.RawMessage) bool {
	opts := parseCollectionRuntimeOptions(raw)
	return opts.Imported
}

func collectionIsImported(collection *Collection) bool {
	if collection == nil {
		return false
	}
	return collectionOptionsImported(collection.Options)
}

func collectionIsManaged(collection *Collection) bool {
	if collection == nil {
		return true
	}
	opts := parseCollectionRuntimeOptions(collection.Options)
	if !opts.Imported {
		return true
	}
	return opts.Managed
}

func collectionCanAlterSchema(collection *Collection) bool {
	return !collectionIsImported(collection) || collectionIsManaged(collection)
}

func collectionStandardSystemColumns(collection *Collection) bool {
	if collection == nil || !collectionIsImported(collection) {
		return true
	}
	opts := parseCollectionRuntimeOptions(collection.Options)
	return opts.StandardSystemColumns
}

func collectionPhysicalTable(project *Project, collection *Collection) (string, string, error) {
	if project == nil || collection == nil {
		return "", "", fmt.Errorf("%w: collection table is not available", ErrValidation)
	}
	opts := parseCollectionRuntimeOptions(collection.Options)
	if opts.Imported {
		if opts.SourceSchema == "" || opts.SourceTable == "" {
			return "", "", fmt.Errorf("%w: imported collection is missing source table metadata", ErrSchemaDrift)
		}
		return opts.SourceSchema, opts.SourceTable, nil
	}
	return project.SchemaName, collection.Name, nil
}

func collectionPrimaryKeySource(collection *Collection) string {
	opts := parseCollectionRuntimeOptions(collection.Options)
	if opts.PrimaryKey != "" {
		return opts.PrimaryKey
	}
	return defaultRecordPrimaryKey
}

func collectionPrimaryKeyField(collection *Collection) string {
	opts := parseCollectionRuntimeOptions(collection.Options)
	if opts.PrimaryKeyField != "" {
		return opts.PrimaryKeyField
	}
	if opts.PrimaryKey != "" {
		return normalizeColumnAPIName(opts.PrimaryKey)
	}
	return defaultRecordPrimaryKey
}

func collectionPrimaryKeyType(collection *Collection) string {
	opts := parseCollectionRuntimeOptions(collection.Options)
	if opts.PrimaryKeyType != "" {
		return opts.PrimaryKeyType
	}
	return defaultRecordPrimaryKeyType
}

func collectionOptionsWithRuntime(base json.RawMessage, runtime collectionRuntimeOptions) (json.RawMessage, error) {
	body := map[string]any{}
	if len(base) > 0 && string(base) != "null" {
		if err := json.Unmarshal(base, &body); err != nil {
			return nil, fmt.Errorf("%w: options must be a JSON object", ErrValidation)
		}
	}
	body[collectionOptionImported] = runtime.Imported
	body[collectionOptionManaged] = runtime.Managed
	body[collectionOptionSourceSchema] = runtime.SourceSchema
	body[collectionOptionSourceTable] = runtime.SourceTable
	body["primaryKey"] = runtime.PrimaryKey
	body["primaryKeyField"] = runtime.PrimaryKeyField
	body["primaryKeyType"] = runtime.PrimaryKeyType
	body["standardSystemColumns"] = runtime.StandardSystemColumns
	return json.Marshal(body)
}

func normalizeColumnAPIName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if valid {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" || out[0] < 'a' || out[0] > 'z' {
		out = "field_" + out
	}
	if _, reserved := reservedDataIdentifiers[out]; reserved || strings.HasPrefix(out, "pg_") || strings.HasPrefix(out, "_dbo") {
		out = "field_" + out
	}
	if len(out) > 59 {
		out = out[:59]
	}
	out = strings.TrimRight(out, "_")
	if out == "" || out[0] < 'a' || out[0] > 'z' {
		out = "field"
	}
	return out
}
