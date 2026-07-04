package core

import (
	"context"
	"testing"
	"time"
)

func TestRunDueBackupJobsAllowsProjectJoinWhenIdle(t *testing.T) {
	pool := testPool(t)
	dropDbo(t, pool)
	ctx := context.Background()

	if err := Migrate(ctx, pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	if err := RunDueBackupJobs(ctx, pool, &Config{StorageType: "local", StorageLocalPath: t.TempDir()}, time.Now().UTC()); err != nil {
		t.Fatalf("idle backup worker query failed: %v", err)
	}
}
