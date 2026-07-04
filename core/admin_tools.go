package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	CollectionImportCreateMissing = "create_missing"
	CollectionImportUpsert        = "upsert"
)

type CollectionSchemaItem struct {
	Name       string          `json:"name"`
	Type       CollectionType  `json:"type"`
	System     bool            `json:"system,omitempty"`
	Fields     []Field         `json:"fields"`
	ListRule   *string         `json:"listRule"`
	ViewRule   *string         `json:"viewRule"`
	CreateRule *string         `json:"createRule"`
	UpdateRule *string         `json:"updateRule"`
	DeleteRule *string         `json:"deleteRule"`
	Options    json.RawMessage `json:"options,omitempty"`
}

type CollectionExportResult struct {
	Project    string                 `json:"project"`
	ExportedAt time.Time              `json:"exportedAt"`
	Items      []CollectionSchemaItem `json:"items"`
}

type CollectionImportInput struct {
	Items             []CollectionSchemaItem `json:"items"`
	Mode              string                 `json:"mode"`
	DryRun            bool                   `json:"dryRun"`
	DropMissingFields bool                   `json:"dropMissingFields"`
}

type CollectionImportItemResult struct {
	Name    string `json:"name"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type CollectionImportResult struct {
	Items   []CollectionImportItemResult `json:"items"`
	Created int                          `json:"created"`
	Updated int                          `json:"updated"`
	Skipped int                          `json:"skipped"`
	DryRun  bool                         `json:"dryRun"`
}

func ExportCollections(ctx context.Context, pool *pgxpool.Pool, projectSlug string) (*CollectionExportResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	collections, err := ListCollections(ctx, pool, project.Slug)
	if err != nil {
		return nil, err
	}
	out := &CollectionExportResult{
		Project:    project.Slug,
		ExportedAt: time.Now().UTC(),
		Items:      make([]CollectionSchemaItem, 0, len(collections)),
	}
	for _, collection := range collections {
		out.Items = append(out.Items, CollectionSchemaFromCollection(collection))
	}
	return out, nil
}

func CollectionSchemaFromCollection(collection Collection) CollectionSchemaItem {
	options := collection.Options
	if len(options) == 0 {
		options = []byte(`{}`)
	}
	return CollectionSchemaItem{
		Name:       collection.Name,
		Type:       collection.Type,
		System:     collection.System,
		Fields:     collection.Fields,
		ListRule:   collection.ListRule,
		ViewRule:   collection.ViewRule,
		CreateRule: collection.CreateRule,
		UpdateRule: collection.UpdateRule,
		DeleteRule: collection.DeleteRule,
		Options:    options,
	}
}

func ImportCollections(ctx context.Context, pool *pgxpool.Pool, adminID string, projectSlug string, input CollectionImportInput, ip string, userAgent string) (*CollectionImportResult, error) {
	project, err := GetProject(ctx, pool, projectSlug)
	if err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = CollectionImportCreateMissing
	}
	if mode != CollectionImportCreateMissing && mode != CollectionImportUpsert {
		return nil, fmt.Errorf("%w: import mode must be create_missing or upsert", ErrValidation)
	}
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("%w: at least one collection is required", ErrValidation)
	}
	if len(input.Items) > 200 {
		return nil, fmt.Errorf("%w: at most 200 collections can be imported at once", ErrValidation)
	}

	result := &CollectionImportResult{Items: make([]CollectionImportItemResult, 0, len(input.Items)), DryRun: input.DryRun}
	seen := map[string]struct{}{}
	for _, item := range input.Items {
		item.Name = NormalizeIdentifier(item.Name)
		if _, ok := seen[item.Name]; ok {
			return nil, fmt.Errorf("%w: duplicate collection %q", ErrValidation, item.Name)
		}
		seen[item.Name] = struct{}{}

		createInput := item.collectionInput()
		if err := ValidateCollectionInput(&createInput); err != nil {
			return nil, err
		}
		existing, err := GetCollection(ctx, pool, project.Slug, createInput.Name)
		if err != nil && !errors.Is(err, ErrCollectionNotFound) {
			return nil, err
		}
		if errors.Is(err, ErrCollectionNotFound) {
			if item.System {
				result.Skipped++
				result.Items = append(result.Items, CollectionImportItemResult{
					Name:    createInput.Name,
					Action:  "skip",
					Status:  "skipped",
					Message: "system collections are created by Dublyobase project provisioning",
				})
				continue
			}
			if input.DryRun {
				result.Created++
				result.Items = append(result.Items, CollectionImportItemResult{Name: createInput.Name, Action: "create", Status: "ready"})
				continue
			}
			if _, err := CreateCollection(ctx, pool, adminID, project.Slug, createInput, ip, userAgent); err != nil {
				return nil, err
			}
			result.Created++
			result.Items = append(result.Items, CollectionImportItemResult{Name: createInput.Name, Action: "create", Status: "applied"})
			continue
		}

		if mode == CollectionImportCreateMissing {
			result.Skipped++
			result.Items = append(result.Items, CollectionImportItemResult{
				Name:    createInput.Name,
				Action:  "skip",
				Status:  "skipped",
				Message: "collection already exists",
			})
			continue
		}

		updateInput := CollectionUpdateInput{
			Fields:            createInput.Fields,
			FieldsSet:         true,
			DropMissingFields: input.DropMissingFields,
			ListRule:          createInput.ListRule,
			ViewRule:          createInput.ViewRule,
			CreateRule:        createInput.CreateRule,
			UpdateRule:        createInput.UpdateRule,
			DeleteRule:        createInput.DeleteRule,
			Options:           createInput.Options,
		}
		if input.DryRun {
			next := *existing
			next.Fields = normalizeFields(updateInput.Fields)
			next.ListRule = updateInput.ListRule
			next.ViewRule = updateInput.ViewRule
			next.CreateRule = updateInput.CreateRule
			next.UpdateRule = updateInput.UpdateRule
			next.DeleteRule = updateInput.DeleteRule
			next.Options = updateInput.Options
			if err := ValidateFields(next.Fields); err != nil {
				return nil, err
			}
			if err := ValidateCollectionRules(&next); err != nil {
				return nil, err
			}
			result.Updated++
			result.Items = append(result.Items, CollectionImportItemResult{Name: createInput.Name, Action: "update", Status: "ready"})
			continue
		}
		if _, err := UpdateCollection(ctx, pool, adminID, project.Slug, existing.Name, updateInput, ip, userAgent); err != nil {
			return nil, err
		}
		result.Updated++
		result.Items = append(result.Items, CollectionImportItemResult{Name: createInput.Name, Action: "update", Status: "applied"})
	}

	action := "collections.import"
	if input.DryRun {
		action = "collections.import.preview"
	}
	if err := InsertAudit(ctx, pool, AuditEvent{
		AdminID:    &adminID,
		Action:     action,
		TargetType: "project",
		TargetID:   project.ID,
		IP:         ip,
		UserAgent:  userAgent,
		Data: map[string]any{
			"project":               project.Slug,
			"mode":                  mode,
			"dryRun":                input.DryRun,
			"dropMissingFields":     input.DropMissingFields,
			"collections_requested": len(input.Items),
			"created":               result.Created,
			"updated":               result.Updated,
			"skipped":               result.Skipped,
		},
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (item CollectionSchemaItem) collectionInput() CollectionInput {
	options := item.Options
	if len(options) == 0 {
		options = []byte(`{}`)
	}
	return CollectionInput{
		Name:       item.Name,
		Type:       item.Type,
		Fields:     item.Fields,
		ListRule:   item.ListRule,
		ViewRule:   item.ViewRule,
		CreateRule: item.CreateRule,
		UpdateRule: item.UpdateRule,
		DeleteRule: item.DeleteRule,
		Options:    options,
	}
}
