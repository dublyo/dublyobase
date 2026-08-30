package apis

import (
	"context"
	"errors"
	"testing"

	"github.com/dublyo/dublyobase/core"
	"github.com/jackc/pgx/v5/pgconn"
)

// Every record request runs with a five second statement timeout. Under load
// reads exceed it and PostgreSQL cancels them with 57014, which nothing mapped
// — so a request that was merely slow reported as "internal server error". A
// caller seeing 500 has no reason to retry; one seeing a timeout does.
func TestQueryCancellationIsATimeoutNotAnInternalError(t *testing.T) {
	for _, tc := range []struct {
		name string
		code string
	}{
		{"statement timeout", "57014"},
		{"too many connections", "53300"},
		{"out of memory", "53200"},
	} {
		err := core.MapRecordDBErrorForTest(&pgconn.PgError{Code: tc.code, Message: "canceling statement due to statement timeout"})
		if !errors.Is(err, core.ErrTimeout) {
			t.Errorf("%s (%s) mapped to %v, want a timeout", tc.name, tc.code, err)
		}
	}

	// An ordinary failure is untouched.
	err := core.MapRecordDBErrorForTest(&pgconn.PgError{Code: "23505", Message: "duplicate key"})
	if errors.Is(err, core.ErrTimeout) {
		t.Errorf("a unique violation was mapped to a timeout: %v", err)
	}
	if err := core.MapRecordDBErrorForTest(nil); err != nil {
		t.Errorf("nil mapped to %v", err)
	}
	_ = context.Background()
}
