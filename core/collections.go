package core

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const collectionAdvisoryLockID int64 = 326_326_004

var (
	ErrCollectionExists   = errors.New("collection exists")
	ErrCollectionNotFound = errors.New("collection not found")
	ErrDestructiveChange  = errors.New("destructive change")
	ErrSchemaDrift        = errors.New("schema drift")
	ErrNotImplemented     = errors.New("not implemented")
)

type CollectionInput struct {
	Name       string          `json:"name"`
	Type       CollectionType  `json:"type"`
	Fields     []Field         `json:"fields"`
	ListRule   *string         `json:"listRule"`
	ViewRule   *string         `json:"viewRule"`
	CreateRule *string         `json:"createRule"`
	UpdateRule *string         `json:"updateRule"`
	DeleteRule *string         `json:"deleteRule"`
	Options    json.RawMessage `json:"options,omitempty"`
}

type CollectionUpdateInput struct {
	Name              *string         `json:"name"`
	Fields            []Field         `json:"fields"`
	FieldsSet         bool            `json:"-"`
	DropMissingFields bool            `json:"dropMissingFields"`
	ListRule          *string         `json:"listRule"`
	ViewRule          *string         `json:"viewRule"`
	CreateRule        *string         `json:"createRule"`
	UpdateRule        *string         `json:"updateRule"`
	DeleteRule        *string         `json:"deleteRule"`
	Options           json.RawMessage `json:"options,omitempty"`
}

func ValidateCollectionInput(input *CollectionInput) error {
	input.Name = NormalizeIdentifier(input.Name)
	if err := ValidateDataIdentifier("collection name", input.Name); err != nil {
		return err
	}
	if input.Type == "" {
		input.Type = CollectionBase
	}
	if input.Type == CollectionView {
		return ErrNotImplemented
	}
	if input.Type != CollectionBase && input.Type != CollectionAuth {
		return fmt.Errorf("%w: unsupported collection type %q", ErrValidation, input.Type)
	}
	input.Fields = normalizeFields(input.Fields)
	return ValidateFields(input.Fields)
}

func CreateCollection(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input CollectionInput, ip string, userAgent string) (*Collection, error) {
	if err := ValidateCollectionInput(&input); err != nil {
		return nil, err
	}
	for _, field := range input.Fields {
		if field.AdoptExistingColumn {
			return nil, fmt.Errorf("%w: field %q can adopt an existing column only when updating a collection", ErrValidation, field.Name)
		}
	}
	if err := validateCollectionOptionsJSON(input.Options); err != nil {
		return nil, err
	}
	if collectionOptionsImported(input.Options) {
		return nil, fmt.Errorf("%w: use schema import for existing Postgres tables", ErrValidation)
	}
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	fieldsJSON, err := encodeFields(input.Fields)
	if err != nil {
		return nil, err
	}
	options := input.Options
	if len(options) == 0 {
		options = []byte(`{}`)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
		return nil, err
	}

	collection := &Collection{
		ProjectID:  project.ID,
		Name:       input.Name,
		Type:       input.Type,
		Fields:     input.Fields,
		ListRule:   input.ListRule,
		ViewRule:   input.ViewRule,
		CreateRule: input.CreateRule,
		UpdateRule: input.UpdateRule,
		DeleteRule: input.DeleteRule,
		Options:    options,
	}
	if err := ValidateCollectionRules(collection); err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		insert into _dbo.collections
			(project_id, name, type, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options)
		values ($1, $2, $3, $4::jsonb, $5, $6, $7, $8, $9, $10::jsonb)
		returning id`,
		project.ID,
		collection.Name,
		collection.Type,
		fieldsJSON,
		input.ListRule,
		input.ViewRule,
		input.CreateRule,
		input.UpdateRule,
		input.DeleteRule,
		options,
	).Scan(&collection.ID); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, ErrCollectionExists
		}
		return nil, err
	}

	if err := createCollectionTable(ctx, tx, project.SchemaName, collection.Name, collection.Fields); err != nil {
		if code := pgErrCode(err); code == "42P07" {
			return nil, ErrProvisioningConflict
		}
		return nil, err
	}
	if err := syncRelationUniqueIndexes(ctx, tx, project.SchemaName, "", collection.Name, nil, collection.Fields); err != nil {
		return nil, err
	}
	if err := syncRelationForeignKeys(ctx, tx, project, project.SchemaName, "", collection.Name, nil, collection.Fields); err != nil {
		return nil, err
	}
	if err := syncCollectionPolicies(ctx, tx, project, collection); err != nil {
		return nil, err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "collection.create",
		TargetType: "collection",
		TargetID:   collection.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "collection": collection.Name},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return collection, nil
}

func createCollectionTable(ctx context.Context, tx pgx.Tx, schemaName string, tableName string, fields []Field) error {
	table := quoteIdent(schemaName, tableName)
	columns := []string{
		`id uuid primary key default gen_random_uuid()`,
		`created timestamptz not null default now()`,
		`updated timestamptz not null default now()`,
	}
	for _, field := range fields {
		ddl, err := ColumnDDL(field)
		if err != nil {
			return err
		}
		columns = append(columns, fmt.Sprintf(`%s %s`, quoteIdent(field.Name), ddl))
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`create table %s (%s)`, table, strings.Join(columns, ", "))); err != nil {
		return err
	}
	return enableDefaultDenyRLS(ctx, tx, schemaName, tableName)
}

func syncRelationUniqueIndexes(ctx context.Context, tx pgx.Tx, schemaName string, currentTableName string, nextTableName string, currentFields []Field, nextFields []Field) error {
	stale := map[string]struct{}{}
	if currentTableName != "" {
		for _, field := range currentFields {
			if relationNeedsUniqueIndex(field) {
				stale[relationUniqueIndexName(currentTableName, field.Name)] = struct{}{}
			}
		}
	}
	desired := map[string]Field{}
	for _, field := range nextFields {
		if relationNeedsUniqueIndex(field) {
			desired[relationUniqueIndexName(nextTableName, field.Name)] = field
		}
	}
	for name := range stale {
		if _, keep := desired[name]; keep {
			continue
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop index if exists %s`, quoteIdent(schemaName, name))); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	table := quoteIdent(schemaName, nextTableName)
	for name, field := range desired {
		column := quoteIdent(field.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf(`create unique index if not exists %s on %s (%s) where %s is not null`, quoteIdent(name), table, column, column)); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	return nil
}

func relationNeedsUniqueIndex(field Field) bool {
	return field.Type == "relation" && boolOption(field.Options, "unique") && !fieldIsMultiple(field)
}

func relationUniqueIndexName(tableName string, fieldName string) string {
	raw := "dbo_reluniq_" + tableName + "_" + fieldName
	if len(raw) <= 55 {
		return raw
	}
	sum := sha1.Sum([]byte(raw))
	prefix := raw
	if len(prefix) > 42 {
		prefix = prefix[:42]
	}
	return fmt.Sprintf("%s_%x", prefix, sum[:6])
}

func syncRelationForeignKeys(ctx context.Context, tx pgx.Tx, project *Project, sourceSchemaName string, currentTableName string, nextTableName string, currentFields []Field, nextFields []Field) error {
	if project == nil {
		return fmt.Errorf("%w: project is required", ErrValidation)
	}
	sourceTable := quoteIdent(sourceSchemaName, nextTableName)
	for _, field := range currentFields {
		if field.Type != "relation" || fieldIsMultiple(field) {
			continue
		}
		name := relationForeignKeyName(nonEmptyString(currentTableName, nextTableName), field.Name)
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, sourceTable, quoteIdent(name))); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	for _, field := range nextFields {
		if field.Type != "relation" || fieldIsMultiple(field) {
			continue
		}
		spec, err := relationForeignKeySpec(ctx, tx, project, nextTableName, field)
		if err != nil {
			return err
		}
		if spec == nil {
			continue
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(`alter table %s drop constraint if exists %s`, sourceTable, quoteIdent(spec.Name))); err != nil {
			return mapSchemaSyncError(err)
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf(
			`alter table %s add constraint %s foreign key (%s) references %s (%s) on delete %s not valid`,
			sourceTable,
			quoteIdent(spec.Name),
			quoteIdent(spec.SourceColumn),
			quoteIdent(spec.TargetSchema, spec.TargetTable),
			quoteIdent(spec.TargetColumn),
			spec.OnDelete,
		)); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	return nil
}

type relationFKSpec struct {
	Name         string
	SourceColumn string
	TargetSchema string
	TargetTable  string
	TargetColumn string
	OnDelete     string
}

func relationForeignKeySpec(ctx context.Context, tx pgx.Tx, project *Project, sourceTableName string, field Field) (*relationFKSpec, error) {
	targetName, _ := field.Options["collection"].(string)
	targetName = NormalizeIdentifier(targetName)
	if targetName == "" {
		return nil, fmt.Errorf("%w: relation field %q requires options.collection", ErrValidation, field.Name)
	}
	target, err := getCollectionTx(ctx, tx, project.ID, targetName)
	if err != nil {
		return nil, err
	}
	if collectionPrimaryKeyType(target) != defaultRecordPrimaryKeyType {
		return nil, nil
	}
	targetSchema, targetTable, err := collectionPhysicalTable(project, target)
	if err != nil {
		return nil, err
	}
	onDelete, err := relationOnDeleteClause(field)
	if err != nil {
		return nil, err
	}
	return &relationFKSpec{
		Name:         relationForeignKeyName(sourceTableName, field.Name),
		SourceColumn: field.Name,
		TargetSchema: targetSchema,
		TargetTable:  targetTable,
		TargetColumn: collectionPrimaryKeySource(target),
		OnDelete:     onDelete,
	}, nil
}

func relationOnDeleteClause(field Field) (string, error) {
	if boolOption(field.Options, "cascadeDelete") {
		return "cascade", nil
	}
	onDelete, _ := field.Options["onDelete"].(string)
	switch NormalizeRelationOnDeleteOption(onDelete) {
	case "restrict":
		return "restrict", nil
	case "cascade":
		return "cascade", nil
	case "set_null":
		if field.Required {
			return "", fmt.Errorf("%w: relation field %q cannot use set_null while required", ErrValidation, field.Name)
		}
		return "set null", nil
	default:
		return "", fmt.Errorf("%w: relation field %q options.onDelete must be restrict, cascade or set_null", ErrValidation, field.Name)
	}
}

func relationForeignKeyName(tableName string, fieldName string) string {
	raw := "dbo_relfk_" + tableName + "_" + fieldName
	if len(raw) <= 55 {
		return raw
	}
	sum := sha1.Sum([]byte(raw))
	prefix := raw
	if len(prefix) > 42 {
		prefix = prefix[:42]
	}
	return fmt.Sprintf("%s_%x", prefix, sum[:6])
}

func nonEmptyString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func ListCollections(ctx context.Context, pool *pgxpool.Pool, projectSlug string) ([]Collection, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `
		select id, project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options
		from _dbo.collections
		where project_id = $1
		order by created_at desc`,
		project.ID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Collection, 0)
	for rows.Next() {
		collection, err := scanCollection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *collection)
	}
	return out, rows.Err()
}

func GetCollection(ctx context.Context, pool *pgxpool.Pool, projectSlug string, name string) (*Collection, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	name = NormalizeIdentifier(name)
	if err := ValidateDataIdentifier("collection name", name); err != nil {
		return nil, err
	}
	row := pool.QueryRow(ctx, `
		select id, project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options
		from _dbo.collections
		where project_id = $1 and name = $2`,
		project.ID,
		name,
	)
	return scanCollection(row)
}

func UpdateCollection(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, name string, input CollectionUpdateInput, ip string, userAgent string) (*Collection, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	name = NormalizeIdentifier(name)
	if err := ValidateDataIdentifier("collection name", name); err != nil {
		return nil, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
		return nil, err
	}

	current, err := getCollectionTx(ctx, tx, project.ID, name)
	if err != nil {
		return nil, err
	}
	next := *current
	oldName := current.Name
	if input.Name != nil {
		next.Name = NormalizeIdentifier(*input.Name)
		if err := ValidateDataIdentifier("collection name", next.Name); err != nil {
			return nil, err
		}
	}
	if input.ListRule != nil {
		next.ListRule = input.ListRule
	}
	if input.ViewRule != nil {
		next.ViewRule = input.ViewRule
	}
	if input.CreateRule != nil {
		next.CreateRule = input.CreateRule
	}
	if input.UpdateRule != nil {
		next.UpdateRule = input.UpdateRule
	}
	if input.DeleteRule != nil {
		next.DeleteRule = input.DeleteRule
	}
	options := next.Options
	if len(input.Options) != 0 {
		if err := validateCollectionOptionsJSON(input.Options); err != nil {
			return nil, err
		}
		options = input.Options
	}
	if len(options) == 0 {
		options = []byte(`{}`)
	}
	if collectionIsImported(current) && !collectionOptionsImported(options) {
		return nil, fmt.Errorf("%w: imported collection metadata cannot be removed", ErrValidation)
	}
	if !collectionIsImported(current) && collectionOptionsImported(options) {
		return nil, fmt.Errorf("%w: use schema import for existing Postgres tables", ErrValidation)
	}
	next.Options = options
	if collectionIsImported(current) {
		if collectionIsManaged(&next) && !collectionStandardSystemColumns(current) {
			return nil, fmt.Errorf("%w: imported table needs id uuid, created and updated before Dublyobase can manage fields", ErrValidation)
		}
		if input.FieldsSet && !collectionCanAlterSchema(&next) {
			return nil, fmt.Errorf("%w: imported table is read-only until managed by Dublyobase", ErrDestructiveChange)
		}
	}
	if input.FieldsSet {
		next.Fields = normalizeFields(input.Fields)
		if err := ValidateFields(next.Fields); err != nil {
			return nil, err
		}
		if err := ValidateCollectionRules(&next); err != nil {
			return nil, err
		}
		schemaName, tableName, err := collectionPhysicalTable(project, current)
		if err != nil {
			return nil, err
		}
		if err := applyFieldDiff(ctx, tx, schemaName, tableName, current.Fields, next.Fields, input.DropMissingFields); err != nil {
			return nil, err
		}
		next.Fields = stripFieldMigrationOptions(next.Fields)
	} else if err := ValidateCollectionRules(&next); err != nil {
		return nil, err
	}
	if next.Name != oldName && !collectionIsImported(current) {
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`alter table %s rename to %s`, quoteIdent(project.SchemaName, oldName), quoteIdent(next.Name)),
		); err != nil {
			switch pgErrCode(err) {
			case "42P01":
				return nil, ErrSchemaDrift
			case "42P07":
				return nil, ErrCollectionExists
			}
			return nil, err
		}
	}
	if input.FieldsSet {
		currentSchemaName, currentTableName, err := collectionPhysicalTable(project, current)
		if err != nil {
			return nil, err
		}
		nextSchemaName, nextTableName, err := collectionPhysicalTable(project, &next)
		if err != nil {
			return nil, err
		}
		if currentSchemaName != nextSchemaName {
			return nil, fmt.Errorf("%w: relation unique indexes cannot move schemas", ErrValidation)
		}
		if err := syncRelationUniqueIndexes(ctx, tx, nextSchemaName, currentTableName, nextTableName, current.Fields, next.Fields); err != nil {
			return nil, err
		}
		if err := syncRelationForeignKeys(ctx, tx, project, nextSchemaName, currentTableName, nextTableName, current.Fields, next.Fields); err != nil {
			return nil, err
		}
	}
	fieldsJSON, err := encodeFields(next.Fields)
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow(ctx, `
		update _dbo.collections
		set name = $1,
			fields = $2::jsonb,
			options = $3::jsonb,
			list_rule = $4,
			view_rule = $5,
			create_rule = $6,
			update_rule = $7,
			delete_rule = $8,
			updated_at = now()
		where id = $9
		returning id, project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options`,
		next.Name,
		fieldsJSON,
		options,
		next.ListRule,
		next.ViewRule,
		next.CreateRule,
		next.UpdateRule,
		next.DeleteRule,
		next.ID,
	).Scan(&next.ID, &next.ProjectID, &next.Name, &next.Type, &next.System, &fieldsJSON, &next.ListRule, &next.ViewRule, &next.CreateRule, &next.UpdateRule, &next.DeleteRule, &next.Options); err != nil {
		if pgErrCode(err) == "23505" {
			return nil, ErrCollectionExists
		}
		return nil, err
	}
	next.Fields, err = ParseFields(fieldsJSON)
	if err != nil {
		return nil, err
	}
	if !collectionIsImported(&next) {
		if err := syncCollectionPolicies(ctx, tx, project, &next); err != nil {
			return nil, err
		}
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "collection.update",
		TargetType: "collection",
		TargetID:   next.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "collection": next.Name},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &next, nil
}

func applyFieldDiff(ctx context.Context, tx pgx.Tx, schemaName string, tableName string, currentFields []Field, nextFields []Field, dropMissing bool) error {
	current := fieldByName(currentFields)
	usedOldNames := map[string]struct{}{}
	for _, field := range nextFields {
		oldName, _ := field.Options["oldName"].(string)
		oldName = NormalizeIdentifier(oldName)
		if oldName != "" && oldName != field.Name {
			if err := ValidateDataIdentifier("old field name", oldName); err != nil {
				return err
			}
			if _, ok := current[oldName]; !ok {
				return fmt.Errorf("%w: oldName %q does not exist", ErrValidation, oldName)
			}
			if _, collision := current[field.Name]; collision {
				return fmt.Errorf("%w: field %q already exists", ErrValidation, field.Name)
			}
			if _, used := usedOldNames[oldName]; used {
				return fmt.Errorf("%w: oldName %q reused", ErrValidation, oldName)
			}
			usedOldNames[oldName] = struct{}{}
			if _, err := tx.Exec(ctx,
				fmt.Sprintf(`alter table %s rename column %s to %s`, quoteIdent(schemaName, tableName), quoteIdent(oldName), quoteIdent(field.Name)),
			); err != nil {
				return mapSchemaSyncError(err)
			}
			continue
		}
		if existing, ok := current[field.Name]; ok {
			currentDDL, err := ColumnDDL(existing)
			if err != nil {
				return err
			}
			nextDDL, err := ColumnDDL(field)
			if err != nil {
				return err
			}
			if currentDDL != nextDDL {
				return fmt.Errorf("%w: field %q requires a manual migration", ErrDestructiveChange, field.Name)
			}
			continue
		}
		if field.AdoptExistingColumn {
			// Application-owned migrations may add a column before Dublyobase metadata
			// is updated. Adoption is explicit and catalog-verified so an upsert can
			// never turn a typo or incompatible column into trusted metadata.
			if err := validateAdoptExistingColumn(ctx, tx, schemaName, tableName, field); err != nil {
				return err
			}
			continue
		}
		ddl, err := ColumnDDL(field)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`alter table %s add column %s %s`, quoteIdent(schemaName, tableName), quoteIdent(field.Name), ddl),
		); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	nextNames := fieldByName(nextFields)
	for _, field := range currentFields {
		if _, ok := nextNames[field.Name]; ok {
			continue
		}
		if _, renamed := usedOldNames[field.Name]; renamed {
			continue
		}
		if !dropMissing {
			return fmt.Errorf("%w: field %q would be dropped", ErrDestructiveChange, field.Name)
		}
		if _, err := tx.Exec(ctx,
			fmt.Sprintf(`alter table %s drop column %s`, quoteIdent(schemaName, tableName), quoteIdent(field.Name)),
		); err != nil {
			return mapSchemaSyncError(err)
		}
	}
	return nil
}

func DeleteCollection(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, name string, confirm string, ip string, userAgent string) error {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return err
	}
	name = NormalizeIdentifier(name)
	if err := ValidateDataIdentifier("collection name", name); err != nil {
		return err
	}
	if confirm != name {
		return fmt.Errorf("%w: delete confirmation must match collection name", ErrValidation)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, collectionAdvisoryLockID); err != nil {
		return err
	}
	collection, err := getCollectionTx(ctx, tx, project.ID, name)
	if err != nil {
		return err
	}
	if collection.System {
		return fmt.Errorf("%w: system collections cannot be deleted", ErrValidation)
	}
	if !collectionIsImported(collection) {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`drop table %s`, quoteIdent(project.SchemaName, collection.Name))); err != nil {
			if pgErrCode(err) == "42P01" {
				return ErrSchemaDrift
			}
			return err
		}
	}
	if _, err := tx.Exec(ctx, `delete from _dbo.collections where id = $1`, collection.ID); err != nil {
		return err
	}
	if err := InsertAudit(ctx, tx, AuditEvent{
		AdminID:    &adminID,
		Action:     "collection.delete",
		TargetType: "collection",
		TargetID:   collection.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data:       map[string]any{"project": project.Slug, "collection": collection.Name},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func stripFieldMigrationOptions(fields []Field) []Field {
	out := make([]Field, len(fields))
	copy(out, fields)
	for i := range out {
		if _, ok := out[i].Options["oldName"]; ok {
			options := make(map[string]any, len(out[i].Options))
			for key, value := range out[i].Options {
				if key == "oldName" {
					continue
				}
				options[key] = value
			}
			out[i].Options = options
		}
		out[i].AdoptExistingColumn = false
		out[i].ExistingSQLType = ""
		out[i].DatabaseDefault = nil
	}
	return out
}

func validateAdoptExistingFields(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, schemaName string, tableName string, currentFields []Field, nextFields []Field) error {
	current := fieldByName(currentFields)
	for _, field := range nextFields {
		if !field.AdoptExistingColumn {
			continue
		}
		if _, exists := current[field.Name]; exists {
			continue
		}
		if err := validateAdoptExistingColumn(ctx, q, schemaName, tableName, field); err != nil {
			return err
		}
	}
	return nil
}

type adoptedPhysicalColumn struct {
	FormattedType string
	UDTName       string
	Nullable      bool
	Default       *string
}

func validateAdoptExistingColumn(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, schemaName string, tableName string, field Field) error {
	var column adoptedPhysicalColumn
	err := q.QueryRow(ctx, `
		select pg_catalog.format_type(a.atttypid, a.atttypmod),
		       t.typname,
		       not a.attnotnull,
		       pg_catalog.pg_get_expr(d.adbin, d.adrelid)
		from pg_catalog.pg_attribute a
		join pg_catalog.pg_class c on c.oid = a.attrelid
		join pg_catalog.pg_namespace n on n.oid = c.relnamespace
		join pg_catalog.pg_type t on t.oid = a.atttypid
		left join pg_catalog.pg_attrdef d on d.adrelid = a.attrelid and d.adnum = a.attnum
		where n.nspname = $1
		  and c.relname = $2
		  and a.attname = $3
		  and a.attnum > 0
		  and not a.attisdropped`,
		schemaName,
		tableName,
		field.Name,
	).Scan(&column.FormattedType, &column.UDTName, &column.Nullable, &column.Default)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: field %q requested adoption but column %s.%s.%s does not exist", ErrSchemaDrift, field.Name, schemaName, tableName, field.Name)
	}
	if err != nil {
		return err
	}
	if !fieldTypeCompatibleWithExistingColumn(field, column.UDTName) {
		return fmt.Errorf("%w: field %q type %q is incompatible with existing SQL type %q", ErrSchemaDrift, field.Name, field.Type, column.FormattedType)
	}
	if field.ExistingSQLType != "" && normalizePostgresType(field.ExistingSQLType) != normalizePostgresType(column.FormattedType) {
		return fmt.Errorf("%w: field %q expected SQL type %q but found %q", ErrSchemaDrift, field.Name, field.ExistingSQLType, column.FormattedType)
	}
	if field.Required && column.Nullable {
		return fmt.Errorf("%w: required field %q cannot adopt a nullable column", ErrSchemaDrift, field.Name)
	}
	if !field.Required && !column.Nullable && column.Default == nil {
		return fmt.Errorf("%w: optional field %q cannot adopt a not-null column without a database default", ErrSchemaDrift, field.Name)
	}
	if field.DatabaseDefault != nil {
		if column.Default == nil || !databaseDefaultMatches(*column.Default, field.DatabaseDefault) {
			return fmt.Errorf("%w: field %q database default does not match the existing column", ErrSchemaDrift, field.Name)
		}
	}
	return nil
}

func fieldTypeCompatibleWithExistingColumn(field Field, udtName string) bool {
	udtName = strings.ToLower(strings.TrimSpace(udtName))
	switch field.Type {
	case "text", "email", "url", "editor", "password":
		return udtName == "text" || udtName == "varchar" || udtName == "bpchar" || udtName == "citext"
	case "number":
		switch udtName {
		case "int2", "int4", "int8", "numeric", "float4", "float8":
			return true
		default:
			return false
		}
	case "bool":
		return udtName == "bool"
	case "date", "autodate":
		return udtName == "timestamptz"
	case "json", "file":
		return udtName == "jsonb"
	case "select":
		if fieldIsMultiple(field) {
			return udtName == "_text"
		}
		return udtName == "text"
	case "relation":
		if fieldIsMultiple(field) {
			return udtName == "_uuid"
		}
		return udtName == "uuid"
	default:
		return false
	}
}

func normalizePostgresType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	value = strings.ReplaceAll(value, " ,", ",")
	value = strings.ReplaceAll(value, ", ", ",")
	switch {
	case value == "int2":
		return "smallint"
	case value == "int4" || value == "int":
		return "integer"
	case value == "int8":
		return "bigint"
	case value == "float4":
		return "real"
	case value == "float8":
		return "double precision"
	case value == "bool":
		return "boolean"
	case value == "timestamptz":
		return "timestamp with time zone"
	case value == "timestamp":
		return "timestamp without time zone"
	case value == "varchar":
		return "character varying"
	case strings.HasPrefix(value, "varchar("):
		return "character varying" + strings.TrimPrefix(value, "varchar")
	case value == "decimal":
		return "numeric"
	case strings.HasPrefix(value, "decimal("):
		return "numeric" + strings.TrimPrefix(value, "decimal")
	default:
		return value
	}
}

func databaseDefaultMatches(actual string, expected any) bool {
	switch value := expected.(type) {
	case string:
		actualValue, ok := postgresStringDefault(actual)
		return ok && actualValue == value
	case bool:
		actualValue := strings.ToLower(stripPostgresDefaultCast(actual))
		return (value && actualValue == "true") || (!value && actualValue == "false")
	case float64:
		return numericDefaultMatches(actual, strconv.FormatFloat(value, 'g', -1, 64))
	case json.Number:
		return numericDefaultMatches(actual, value.String())
	default:
		return false
	}
}

func postgresStringDefault(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	if len(value) < 2 || value[0] != '\'' {
		return "", false
	}
	var out strings.Builder
	for i := 1; i < len(value); i++ {
		if value[i] != '\'' {
			out.WriteByte(value[i])
			continue
		}
		if i+1 < len(value) && value[i+1] == '\'' {
			out.WriteByte('\'')
			i++
			continue
		}
		tail := strings.TrimSpace(value[i+1:])
		if tail == "" || strings.HasPrefix(tail, "::") {
			return out.String(), true
		}
		return "", false
	}
	return "", false
}

func numericDefaultMatches(actual string, expected string) bool {
	actualNumber, ok := new(big.Rat).SetString(stripPostgresDefaultCast(actual))
	if !ok {
		return false
	}
	expectedNumber, ok := new(big.Rat).SetString(expected)
	return ok && actualNumber.Cmp(expectedNumber) == 0
}

func stripPostgresDefaultCast(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.Index(value, "::"); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	for strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func mapSchemaSyncError(err error) error {
	switch pgErrCode(err) {
	case "42P01":
		return ErrSchemaDrift
	case "42701", "42703":
		return fmt.Errorf("%w: schema diff no longer matches the table", ErrSchemaDrift)
	default:
		return err
	}
}

type collectionScanner interface {
	Scan(dest ...any) error
}

func scanCollection(row collectionScanner) (*Collection, error) {
	var collection Collection
	var rawFields json.RawMessage
	if err := row.Scan(
		&collection.ID,
		&collection.ProjectID,
		&collection.Name,
		&collection.Type,
		&collection.System,
		&rawFields,
		&collection.ListRule,
		&collection.ViewRule,
		&collection.CreateRule,
		&collection.UpdateRule,
		&collection.DeleteRule,
		&collection.Options,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCollectionNotFound
		}
		return nil, err
	}
	fields, err := ParseFields(rawFields)
	if err != nil {
		return nil, err
	}
	collection.Fields = fields
	return &collection, nil
}

func getCollectionTx(ctx context.Context, tx pgx.Tx, projectID string, name string) (*Collection, error) {
	row := tx.QueryRow(ctx, `
		select id, project_id, name, type, system, fields, list_rule, view_rule, create_rule, update_rule, delete_rule, options
		from _dbo.collections
		where project_id = $1 and name = $2`,
		projectID,
		name,
	)
	return scanCollection(row)
}
