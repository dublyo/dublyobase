package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	var out []Collection
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
	if input.FieldsSet {
		next.Fields = normalizeFields(input.Fields)
		if err := ValidateFields(next.Fields); err != nil {
			return nil, err
		}
		if err := ValidateCollectionRules(&next); err != nil {
			return nil, err
		}
		if err := applyFieldDiff(ctx, tx, project.SchemaName, oldName, current.Fields, next.Fields, input.DropMissingFields); err != nil {
			return nil, err
		}
		next.Fields = stripFieldMigrationOptions(next.Fields)
	} else if err := ValidateCollectionRules(&next); err != nil {
		return nil, err
	}
	if next.Name != oldName {
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
	fieldsJSON, err := encodeFields(next.Fields)
	if err != nil {
		return nil, err
	}
	options := next.Options
	if len(input.Options) != 0 {
		options = input.Options
	}
	if len(options) == 0 {
		options = []byte(`{}`)
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
	if err := syncCollectionPolicies(ctx, tx, project, &next); err != nil {
		return nil, err
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
	if _, err := tx.Exec(ctx, fmt.Sprintf(`drop table %s`, quoteIdent(project.SchemaName, collection.Name))); err != nil {
		if pgErrCode(err) == "42P01" {
			return ErrSchemaDrift
		}
		return err
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
		if _, ok := out[i].Options["oldName"]; !ok {
			continue
		}
		options := make(map[string]any, len(out[i].Options))
		for key, value := range out[i].Options {
			if key == "oldName" {
				continue
			}
			options[key] = value
		}
		out[i].Options = options
	}
	return out
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
