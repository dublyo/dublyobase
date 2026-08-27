package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BatchOp is one record operation inside a batch.
//
// KNOWN LIMIT: operations cannot reference each other's results. Managed
// collections reject caller-assigned ids, so a batch cannot create an invoice
// and then create its lines against the generated id — the parent must be
// created first, and only the lines batched. Closing this needs either
// caller-assigned ids on managed collections or a result-reference syntax
// (e.g. "$0.id"). Not yet implemented.
//
// Also absent, and required before calling this an ERP-grade transaction:
// idempotency keys, optimistic concurrency (If-Match / row version),
// conditional updates for stock decrement, and a transactional audit/outbox
// row written inside this same transaction.
type BatchOp struct {
	Method     string
	Collection string
	ID         string
	Body       map[string]json.RawMessage
}

// BatchOpResult carries what an operation produced, so callers can emit
// realtime and webhook events only after the whole batch has committed.
type BatchOpResult struct {
	Status     int
	Collection string
	Action     string
	ID         string
	Record     Record
}

// RunAtomicBatch executes every operation inside ONE transaction. Either all of
// them commit or none do — the guarantee an order + its lines + a stock
// movement need, and the reason the non-atomic path could leave half-written
// business documents behind when operation N failed after N-1 had committed.
//
// request.operation is reset before each statement so per-operation RLS
// policies still apply exactly as they do on the single-record endpoints.
func RunAtomicBatch(ctx context.Context, pool *pgxpool.Pool, auth *RecordAuth, ops []BatchOp) ([]BatchOpResult, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: batch requires at least one request", ErrValidation)
	}
	collections := make([]*Collection, len(ops))
	for i, op := range ops {
		name := NormalizeIdentifier(op.Collection)
		if name == "" {
			return nil, fmt.Errorf("%w: request %d is missing a collection", ErrValidation, i)
		}
		collection, err := recordCollection(ctx, pool, auth.Project.Slug, name)
		if err != nil {
			return nil, err
		}
		collections[i] = collection
	}

	// SET LOCAL ROLE applies to the whole transaction, so every operation has
	// to agree on it. Mixing managed and imported collections under the service
	// role would otherwise silently run half the batch with RLS bypassed.
	bypassRole := auth.Role == RecordRoleService && collectionIsImported(collections[0])
	for i, collection := range collections {
		if want := auth.Role == RecordRoleService && collectionIsImported(collection); want != bypassRole {
			return nil, fmt.Errorf("%w: an atomic batch cannot mix imported and managed collections (request %d)", ErrValidation, i)
		}
	}

	results := make([]BatchOpResult, 0, len(ops))
	err := withRecordTxOptions(ctx, pool, auth, "create", bypassRole, func(tx pgx.Tx) error {
		for i, op := range ops {
			result, err := runBatchOpInTx(ctx, tx, auth, collections[i], op)
			if err != nil {
				return fmt.Errorf("request %d: %w", i, err)
			}
			results = append(results, result)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func runBatchOpInTx(ctx context.Context, tx pgx.Tx, auth *RecordAuth, collection *Collection, op BatchOp) (BatchOpResult, error) {
	method := strings.ToUpper(strings.TrimSpace(op.Method))
	action := batchActionForMethod(method)
	if action == "" {
		return BatchOpResult{}, fmt.Errorf("%w: unsupported batch method %q", ErrValidation, op.Method)
	}
	if _, err := tx.Exec(ctx, `select set_config('request.operation', $1, true)`, action); err != nil {
		return BatchOpResult{}, err
	}

	res := BatchOpResult{Collection: collection.Name, Action: action, ID: strings.TrimSpace(op.ID)}
	switch method {
	case http.MethodGet:
		if res.ID == "" {
			return BatchOpResult{}, fmt.Errorf("%w: id is required for GET", ErrValidation)
		}
		record, err := getRecordInTx(ctx, tx, auth, collection, res.ID)
		if err != nil {
			return BatchOpResult{}, err
		}
		res.Status, res.Record = http.StatusOK, record
	case http.MethodPost:
		record, err := createRecordInTx(ctx, tx, auth, collection, op.Body)
		if err != nil {
			return BatchOpResult{}, err
		}
		res.Status, res.Record = http.StatusCreated, record
		res.ID, _ = record[RecordPrimaryKeyField(collection)].(string)
	case http.MethodPatch, http.MethodPut:
		if res.ID == "" {
			return BatchOpResult{}, fmt.Errorf("%w: id is required for update", ErrValidation)
		}
		record, err := updateRecordInTx(ctx, tx, auth, collection, res.ID, op.Body)
		if err != nil {
			return BatchOpResult{}, err
		}
		res.Status, res.Record = http.StatusOK, record
	case http.MethodDelete:
		if res.ID == "" {
			return BatchOpResult{}, fmt.Errorf("%w: id is required for DELETE", ErrValidation)
		}
		record, err := deleteRecordInTx(ctx, tx, auth, collection, res.ID)
		if err != nil {
			return BatchOpResult{}, err
		}
		res.Status, res.Record = http.StatusNoContent, record
	}
	return res, nil
}

func batchActionForMethod(method string) string {
	switch method {
	case http.MethodGet:
		return "view"
	case http.MethodPost:
		return "create"
	case http.MethodPatch, http.MethodPut:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return ""
	}
}
