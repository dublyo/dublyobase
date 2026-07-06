package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaDiscoveryInput struct {
	Schema string `json:"schema,omitempty"`
	Table  string `json:"table,omitempty"`
}

type SchemaDiscoveryResult struct {
	Items []DiscoveredTable `json:"items"`
}

type DiscoveredTable struct {
	Schema                string                 `json:"schema"`
	Table                 string                 `json:"table"`
	SuggestedName         string                 `json:"suggestedName"`
	ExistingCollection    string                 `json:"existingCollection,omitempty"`
	Imported              bool                   `json:"imported"`
	CanImport             bool                   `json:"canImport"`
	CanManage             bool                   `json:"canManage"`
	Reason                string                 `json:"reason,omitempty"`
	PrimaryKey            *DiscoveredPrimaryKey  `json:"primaryKey,omitempty"`
	StandardSystemColumns bool                   `json:"standardSystemColumns"`
	Columns               []DiscoveredColumn     `json:"columns"`
	Fields                []Field                `json:"fields"`
	ForeignKeys           []DiscoveredForeignKey `json:"foreignKeys"`
}

type DiscoveredPrimaryKey struct {
	Column     string `json:"column"`
	Field      string `json:"field"`
	Type       string `json:"type"`
	HasDefault bool   `json:"hasDefault"`
}

type DiscoveredColumn struct {
	Name       string `json:"name"`
	FieldName  string `json:"fieldName,omitempty"`
	DataType   string `json:"dataType"`
	UDTName    string `json:"udtName"`
	Nullable   bool   `json:"nullable"`
	HasDefault bool   `json:"hasDefault"`
	PrimaryKey bool   `json:"primaryKey"`
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason,omitempty"`
}

type DiscoveredForeignKey struct {
	Column       string `json:"column"`
	TargetSchema string `json:"targetSchema"`
	TargetTable  string `json:"targetTable"`
	TargetColumn string `json:"targetColumn"`
	OnDelete     string `json:"onDelete,omitempty"`
}

type SchemaImportInput struct {
	Items  []SchemaImportItem `json:"items"`
	DryRun bool               `json:"dryRun"`
}

type SchemaImportItem struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Name   string `json:"name,omitempty"`
}

type discoveredColumnInternal struct {
	DiscoveredColumn
	Ordinal int
}

func DiscoverSchemaTables(ctx context.Context, pool *pgxpool.Pool, projectSlug string, input SchemaDiscoveryInput) (*SchemaDiscoveryResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	collections, err := ListCollections(ctx, pool, project.Slug)
	if err != nil {
		return nil, err
	}
	existingByTable := collectionNamesByPhysicalTable(project, collections)
	existingNames := collectionNameSet(collections)

	schemaFilter := strings.TrimSpace(input.Schema)
	tableFilter := strings.TrimSpace(input.Table)
	if schemaFilter != "" && isSystemSchema(schemaFilter) {
		return &SchemaDiscoveryResult{Items: []DiscoveredTable{}}, nil
	}

	rows, err := pool.Query(ctx, `
		select table_schema, table_name
		from information_schema.tables
		where table_type = 'BASE TABLE'
			and table_schema <> 'information_schema'
			and table_schema <> '_dbo'
			and table_schema not like 'pg\_%' escape '\'
			and ($1 = '' or table_schema = $1)
			and ($2 = '' or table_name ilike '%' || $2 || '%')
		order by table_schema, table_name
		limit 500`,
		schemaFilter,
		tableFilter,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []DiscoveredTable{}
	seenSuggestions := map[string]int{}
	for rows.Next() {
		var schemaName, tableName string
		if err := rows.Scan(&schemaName, &tableName); err != nil {
			return nil, err
		}
		table, err := discoverOneTable(ctx, pool, project, existingByTable, existingNames, schemaName, tableName)
		if err != nil {
			return nil, err
		}
		table.SuggestedName = uniqueDiscoveredCollectionName(table.SuggestedName, table.Schema, seenSuggestions)
		items = append(items, table)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &SchemaDiscoveryResult{Items: items}, nil
}

func ImportSchemaTables(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input SchemaImportInput, ip string, userAgent string) (*CollectionImportResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one table is required", ErrValidation)
	}
	if len(input.Items) > 100 {
		return nil, fmt.Errorf("%w: at most 100 tables can be imported at once", ErrValidation)
	}
	collections, err := ListCollections(ctx, pool, project.Slug)
	if err != nil {
		return nil, err
	}
	existingByTable := collectionNamesByPhysicalTable(project, collections)
	existingNames := collectionNameSet(collections)

	result := &CollectionImportResult{Items: make([]CollectionImportItemResult, 0, len(input.Items)), DryRun: input.DryRun}
	seenSources := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	type readyImport struct {
		table DiscoveredTable
		name  string
	}
	ready := []readyImport{}

	for _, item := range input.Items {
		item.Schema = strings.TrimSpace(item.Schema)
		item.Table = strings.TrimSpace(item.Table)
		if item.Schema == "" || item.Table == "" {
			return nil, fmt.Errorf("%w: schema and table are required", ErrValidation)
		}
		if isSystemSchema(item.Schema) {
			return nil, fmt.Errorf("%w: system schemas cannot be imported", ErrValidation)
		}
		sourceKey := item.Schema + "\x00" + item.Table
		if _, ok := seenSources[sourceKey]; ok {
			return nil, fmt.Errorf("%w: duplicate table %s.%s", ErrValidation, item.Schema, item.Table)
		}
		seenSources[sourceKey] = struct{}{}

		table, err := discoverOneTable(ctx, pool, project, existingByTable, existingNames, item.Schema, item.Table)
		if err != nil {
			return nil, err
		}
		name := NormalizeIdentifier(item.Name)
		if name == "" {
			name = table.SuggestedName
		}
		if err := ValidateDataIdentifier("collection name", name); err != nil {
			return nil, err
		}
		if _, duplicate := seenNames[name]; duplicate {
			return nil, fmt.Errorf("%w: duplicate collection %q", ErrValidation, name)
		}
		seenNames[name] = struct{}{}
		if table.Imported || table.ExistingCollection != "" {
			result.Skipped++
			result.Items = append(result.Items, CollectionImportItemResult{Name: name, Action: "skip", Status: "skipped", Message: "table already has collection metadata"})
			continue
		}
		if _, exists := existingNames[name]; exists {
			result.Skipped++
			result.Items = append(result.Items, CollectionImportItemResult{Name: name, Action: "skip", Status: "skipped", Message: "collection name already exists"})
			continue
		}
		if !table.CanImport {
			result.Skipped++
			result.Items = append(result.Items, CollectionImportItemResult{Name: name, Action: "skip", Status: "skipped", Message: table.Reason})
			continue
		}
		if len(table.Fields) == 0 {
			result.Skipped++
			result.Items = append(result.Items, CollectionImportItemResult{Name: name, Action: "skip", Status: "skipped", Message: "no supported editable columns were found"})
			continue
		}
		if input.DryRun {
			result.Created++
			result.Items = append(result.Items, CollectionImportItemResult{Name: name, Action: "import", Status: "ready"})
			continue
		}
		ready = append(ready, readyImport{table: table, name: name})
	}

	if !input.DryRun && len(ready) > 0 {
		batchRelationTargets := map[string]string{}
		for _, item := range ready {
			batchRelationTargets[item.table.Schema+"\x00"+item.table.Table] = item.name
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
			return nil, err
		}
		for _, item := range ready {
			rewriteBatchRelationTargets(&item.table, batchRelationTargets)
			if err := insertImportedCollection(ctx, tx, project, item.name, item.table); err != nil {
				if errors.Is(err, ErrCollectionExists) {
					result.Skipped++
					result.Items = append(result.Items, CollectionImportItemResult{Name: item.name, Action: "skip", Status: "skipped", Message: "collection name already exists"})
					continue
				}
				return nil, err
			}
			result.Created++
			result.Items = append(result.Items, CollectionImportItemResult{Name: item.name, Action: "import", Status: "applied"})
		}
		if err := InsertAudit(ctx, tx, AuditEvent{
			AdminID:    &adminID,
			Action:     "schema.import",
			TargetType: "project",
			TargetID:   project.ID,
			IP:         ip,
			UserAgent:  userAgent,
			Data: map[string]any{
				"project": project.Slug,
				"dryRun":  input.DryRun,
				"created": result.Created,
				"skipped": result.Skipped,
			},
		}); err != nil {
			return nil, err
		}
		return result, tx.Commit(ctx)
	}

	action := "schema.import.preview"
	if !input.DryRun {
		action = "schema.import"
	}
	if err := InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &adminID,
		Action:     action,
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"project": project.Slug,
			"dryRun":  input.DryRun,
			"created": result.Created,
			"skipped": result.Skipped,
		},
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func rewriteBatchRelationTargets(table *DiscoveredTable, batchRelationTargets map[string]string) {
	if len(batchRelationTargets) == 0 {
		return
	}
	for i := range table.Fields {
		field := &table.Fields[i]
		if field.Type != "relation" || field.Options == nil {
			continue
		}
		targetSchema, _ := field.Options["targetSchema"].(string)
		targetTable, _ := field.Options["targetTable"].(string)
		if targetSchema == "" || targetTable == "" {
			continue
		}
		if collectionName := batchRelationTargets[targetSchema+"\x00"+targetTable]; collectionName != "" {
			field.Options["collection"] = collectionName
		}
	}
}

func discoverOneTable(ctx context.Context, pool *pgxpool.Pool, project *Project, existingByTable map[string]string, existingNames map[string]struct{}, schemaName string, tableName string) (DiscoveredTable, error) {
	if isSystemSchema(schemaName) {
		return DiscoveredTable{}, fmt.Errorf("%w: system schemas cannot be discovered", ErrValidation)
	}
	columns, err := discoverColumns(ctx, pool, schemaName, tableName)
	if err != nil {
		return DiscoveredTable{}, err
	}
	if len(columns) == 0 {
		return DiscoveredTable{}, ErrCollectionNotFound
	}
	pkCols, err := discoverPrimaryKey(ctx, pool, schemaName, tableName)
	if err != nil {
		return DiscoveredTable{}, err
	}
	fks, err := discoverForeignKeys(ctx, pool, schemaName, tableName)
	if err != nil {
		return DiscoveredTable{}, err
	}

	pkSet := map[string]struct{}{}
	for _, pk := range pkCols {
		pkSet[pk] = struct{}{}
	}
	for i := range columns {
		_, columns[i].PrimaryKey = pkSet[columns[i].Name]
	}
	columnNames := apiNamesForColumns(columns)
	standard := tableHasStandardSystemColumns(columns, pkCols)
	if standard {
		columnNames["id"] = "id"
		columnNames["created"] = "created"
		columnNames["updated"] = "updated"
	}
	if len(pkCols) == 1 {
		// Imported tables still need the same REST surface as native
		// collections. Keep the physical source column in collection options,
		// but expose the single record key as "id" for projections, filters,
		// record URLs, and client SDKs.
		columnNames[pkCols[0]] = defaultRecordPrimaryKey
	}
	fields, discoveredColumns := fieldsForDiscoveredColumns(columns, columnNames, fks, existingByTable, existingNames, standard)

	table := DiscoveredTable{
		Schema:                schemaName,
		Table:                 tableName,
		SuggestedName:         suggestedCollectionName(schemaName, tableName, project.SchemaName),
		ExistingCollection:    existingByTable[schemaName+"\x00"+tableName],
		Imported:              existingByTable[schemaName+"\x00"+tableName] != "",
		CanManage:             standard,
		StandardSystemColumns: standard,
		Columns:               discoveredColumns,
		Fields:                fields,
		ForeignKeys:           fks,
	}
	if len(pkCols) != 1 {
		table.CanImport = false
		if len(pkCols) == 0 {
			table.Reason = "table has no primary key"
		} else {
			table.Reason = "composite primary keys are not supported yet"
		}
		return table, nil
	}
	pkColumn := columnsByName(columns)[pkCols[0]]
	pkField := columnNames[pkColumn.Name]
	table.PrimaryKey = &DiscoveredPrimaryKey{Column: pkColumn.Name, Field: pkField, Type: pkColumn.UDTName, HasDefault: pkColumn.HasDefault}
	if !primaryKeyTypeUsable(pkColumn.UDTName) {
		table.CanImport = false
		table.Reason = "primary key type is not supported for REST record routes"
		return table, nil
	}
	table.CanImport = true
	if table.ExistingCollection != "" {
		table.CanImport = false
		table.Reason = "table already has collection metadata"
	}
	return table, nil
}

func discoverColumns(ctx context.Context, pool *pgxpool.Pool, schemaName string, tableName string) ([]discoveredColumnInternal, error) {
	rows, err := pool.Query(ctx, `
		select column_name, data_type, udt_name, is_nullable = 'YES', column_default is not null, ordinal_position
		from information_schema.columns
		where table_schema = $1 and table_name = $2
		order by ordinal_position`,
		schemaName,
		tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := []discoveredColumnInternal{}
	for rows.Next() {
		var col discoveredColumnInternal
		if err := rows.Scan(&col.Name, &col.DataType, &col.UDTName, &col.Nullable, &col.HasDefault, &col.Ordinal); err != nil {
			return nil, err
		}
		col.UDTName = strings.ToLower(strings.TrimSpace(col.UDTName))
		columns = append(columns, col)
	}
	return columns, rows.Err()
}

func discoverPrimaryKey(ctx context.Context, pool *pgxpool.Pool, schemaName string, tableName string) ([]string, error) {
	rows, err := pool.Query(ctx, `
		select kcu.column_name
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
			on kcu.constraint_schema = tc.constraint_schema
			and kcu.constraint_name = tc.constraint_name
			and kcu.table_schema = tc.table_schema
			and kcu.table_name = tc.table_name
		where tc.constraint_type = 'PRIMARY KEY'
			and tc.table_schema = $1
			and tc.table_name = $2
		order by kcu.ordinal_position`,
		schemaName,
		tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func discoverForeignKeys(ctx context.Context, pool *pgxpool.Pool, schemaName string, tableName string) ([]DiscoveredForeignKey, error) {
	rows, err := pool.Query(ctx, `
		select kcu.column_name, ccu.table_schema, ccu.table_name, ccu.column_name, rc.delete_rule
		from information_schema.table_constraints tc
		join information_schema.key_column_usage kcu
			on kcu.constraint_schema = tc.constraint_schema
			and kcu.constraint_name = tc.constraint_name
			and kcu.table_schema = tc.table_schema
			and kcu.table_name = tc.table_name
		join information_schema.constraint_column_usage ccu
			on ccu.constraint_schema = tc.constraint_schema
			and ccu.constraint_name = tc.constraint_name
		left join information_schema.referential_constraints rc
			on rc.constraint_schema = tc.constraint_schema
			and rc.constraint_name = tc.constraint_name
		where tc.constraint_type = 'FOREIGN KEY'
			and tc.table_schema = $1
			and tc.table_name = $2
		order by kcu.ordinal_position`,
		schemaName,
		tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DiscoveredForeignKey{}
	for rows.Next() {
		var fk DiscoveredForeignKey
		if err := rows.Scan(&fk.Column, &fk.TargetSchema, &fk.TargetTable, &fk.TargetColumn, &fk.OnDelete); err != nil {
			return nil, err
		}
		out = append(out, fk)
	}
	return out, rows.Err()
}

func insertImportedCollection(ctx context.Context, tx pgx.Tx, project *Project, name string, table DiscoveredTable) error {
	fieldsJSON, err := encodeFields(table.Fields)
	if err != nil {
		return err
	}
	if table.PrimaryKey == nil {
		return fmt.Errorf("%w: primary key is required", ErrValidation)
	}
	options, err := collectionOptionsWithRuntime([]byte(`{}`), collectionRuntimeOptions{
		Imported:              true,
		Managed:               false,
		SourceSchema:          table.Schema,
		SourceTable:           table.Table,
		PrimaryKey:            table.PrimaryKey.Column,
		PrimaryKeyField:       table.PrimaryKey.Field,
		PrimaryKeyType:        table.PrimaryKey.Type,
		PrimaryKeyHasDefault:  table.PrimaryKey.HasDefault,
		StandardSystemColumns: table.StandardSystemColumns,
	})
	if err != nil {
		return err
	}
	var id string
	if err := tx.QueryRow(ctx, `
		insert into _dbo.collections
			(project_id, name, type, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options)
		values ($1, $2, $3, $4::jsonb, null, null, null, null, null, $5::jsonb)
		returning id`,
		project.ID,
		name,
		CollectionBase,
		fieldsJSON,
		options,
	).Scan(&id); err != nil {
		if pgErrCode(err) == "23505" {
			return ErrCollectionExists
		}
		return err
	}
	return nil
}

func fieldsForDiscoveredColumns(columns []discoveredColumnInternal, names map[string]string, fks []DiscoveredForeignKey, existingByTable map[string]string, existingNames map[string]struct{}, standard bool) ([]Field, []DiscoveredColumn) {
	fkByColumn := map[string]DiscoveredForeignKey{}
	for _, fk := range fks {
		fkByColumn[fk.Column] = fk
	}
	fields := []Field{}
	outColumns := make([]DiscoveredColumn, 0, len(columns))
	for _, col := range columns {
		fieldName := names[col.Name]
		col.FieldName = fieldName
		if col.PrimaryKey || (standard && (col.Name == "created" || col.Name == "updated")) {
			col.Supported = true
			outColumns = append(outColumns, col.DiscoveredColumn)
			continue
		}
		fieldType, options, supported, reason := postgresFieldType(col)
		if fk, ok := fkByColumn[col.Name]; ok && relationSourceTypeSupported(col.UDTName) {
			fieldType = "relation"
			supported = true
			reason = ""
			options["collection"] = relatedCollectionName(fk.TargetSchema, fk.TargetTable, existingByTable, existingNames)
			options["targetSchema"] = fk.TargetSchema
			options["targetTable"] = fk.TargetTable
			options["targetColumn"] = fk.TargetColumn
			options["onDelete"] = NormalizeRelationOnDeleteOption(fk.OnDelete)
		}
		col.Supported = supported
		col.Reason = reason
		outColumns = append(outColumns, col.DiscoveredColumn)
		if !supported {
			continue
		}
		options["sourceColumn"] = col.Name
		options["sourceType"] = col.UDTName
		field := Field{
			Name:     fieldName,
			Type:     fieldType,
			Required: !col.Nullable && !col.HasDefault,
			Options:  options,
		}
		if fieldCanSearch(field) && (fieldType == "text" || fieldType == "email" || fieldType == "url") {
			field.Searchable = true
		}
		fields = append(fields, field)
	}
	return fields, outColumns
}

func relationSourceTypeSupported(udtName string) bool {
	switch strings.ToLower(strings.TrimSpace(udtName)) {
	case "uuid", "text", "varchar", "bpchar", "citext":
		return true
	default:
		return false
	}
}

func postgresFieldType(col discoveredColumnInternal) (string, map[string]any, bool, string) {
	options := map[string]any{}
	switch col.UDTName {
	case "bool":
		return "bool", options, true, ""
	case "int2", "int4", "int8":
		options["onlyInt"] = true
		return "number", options, true, ""
	case "float4", "float8", "numeric":
		return "number", options, true, ""
	case "timestamptz", "timestamp", "date":
		return "date", options, true, ""
	case "json", "jsonb":
		return "json", options, true, ""
	case "text", "varchar", "bpchar", "citext", "uuid":
		return "text", options, true, ""
	default:
		return "", options, false, "column type is not supported yet"
	}
}

func apiNamesForColumns(columns []discoveredColumnInternal) map[string]string {
	out := map[string]string{}
	used := map[string]int{}
	for _, col := range columns {
		base := normalizeColumnAPIName(col.Name)
		name := base
		if count := used[base]; count > 0 {
			name = fmt.Sprintf("%s_%d", base, count+1)
		}
		for {
			if _, taken := used[name]; !taken {
				break
			}
			used[base]++
			name = fmt.Sprintf("%s_%d", base, used[base]+1)
		}
		used[name]++
		out[col.Name] = name
	}
	return out
}

func tableHasStandardSystemColumns(columns []discoveredColumnInternal, pkCols []string) bool {
	if len(pkCols) != 1 || pkCols[0] != defaultRecordPrimaryKey {
		return false
	}
	byName := columnsByName(columns)
	id, ok := byName[defaultRecordPrimaryKey]
	if !ok || id.UDTName != "uuid" {
		return false
	}
	created, hasCreated := byName["created"]
	updated, hasUpdated := byName["updated"]
	return hasCreated && hasUpdated && timestampLikeType(created.UDTName) && timestampLikeType(updated.UDTName)
}

func columnsByName(columns []discoveredColumnInternal) map[string]discoveredColumnInternal {
	out := make(map[string]discoveredColumnInternal, len(columns))
	for _, col := range columns {
		out[col.Name] = col
	}
	return out
}

func collectionNamesByPhysicalTable(project *Project, collections []Collection) map[string]string {
	out := map[string]string{}
	for i := range collections {
		collection := &collections[i]
		schemaName, tableName, err := collectionPhysicalTable(project, collection)
		if err != nil {
			continue
		}
		out[schemaName+"\x00"+tableName] = collection.Name
	}
	return out
}

func collectionNameSet(collections []Collection) map[string]struct{} {
	out := make(map[string]struct{}, len(collections))
	for _, collection := range collections {
		out[collection.Name] = struct{}{}
	}
	return out
}

func suggestedCollectionName(schemaName string, tableName string, projectSchema string) string {
	name := normalizeColumnAPIName(tableName)
	if schemaName != projectSchema && schemaName != "public" {
		name = normalizeColumnAPIName(schemaName + "_" + tableName)
	}
	if err := ValidateDataIdentifier("collection name", name); err == nil {
		return name
	}
	return "table_" + normalizeColumnAPIName(tableName)
}

func uniqueDiscoveredCollectionName(name string, schemaName string, seen map[string]int) string {
	if count := seen[name]; count == 0 {
		seen[name] = 1
		return name
	}
	seen[name]++
	next := normalizeColumnAPIName(schemaName + "_" + name)
	if _, exists := seen[next]; !exists {
		seen[next] = 1
		return next
	}
	for {
		seen[name]++
		candidate := fmt.Sprintf("%s_%d", name, seen[name])
		if _, exists := seen[candidate]; !exists {
			seen[candidate] = 1
			return candidate
		}
	}
}

func relatedCollectionName(schemaName string, tableName string, existingByTable map[string]string, existingNames map[string]struct{}) string {
	if name := existingByTable[schemaName+"\x00"+tableName]; name != "" {
		return name
	}
	name := normalizeColumnAPIName(tableName)
	if _, exists := existingNames[name]; exists {
		return name
	}
	return name
}

func primaryKeyTypeUsable(udtName string) bool {
	switch udtName {
	case "uuid", "text", "varchar", "bpchar", "citext", "int2", "int4", "int8":
		return true
	default:
		return false
	}
}

func timestampLikeType(udtName string) bool {
	return udtName == "timestamptz" || udtName == "timestamp"
}

func isSystemSchema(schemaName string) bool {
	return schemaName == "" || schemaName == "_dbo" || schemaName == "information_schema" || strings.HasPrefix(schemaName, "pg_")
}
