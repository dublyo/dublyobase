package core

import (
	"context"
	"slices"
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

func TestPgDumpBaseArgsUseConfiguredDatabaseURL(t *testing.T) {
	args := pgDumpBaseArgs("postgresql://user:pass@example.com:5432/app?sslmode=require", "/tmp/out.dump")
	if !slices.Contains(args, "--dbname") {
		t.Fatalf("pg_dump args must pass --dbname: %v", args)
	}
	for i, arg := range args {
		if arg == "--dbname" && (i+1 >= len(args) || args[i+1] != "postgresql://user:pass@example.com:5432/app?sslmode=require") {
			t.Fatalf("--dbname does not point at configured URL: %v", args)
		}
	}
	if !slices.Contains(args, "--file") {
		t.Fatalf("pg_dump args must pass --file: %v", args)
	}
}
