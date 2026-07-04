package core

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool connects to TEST_DATABASE_URL or skips (integration tests need a
// real Postgres; CI provides one as a service container).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func dropDbo(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `drop schema if exists _dbo cascade`); err != nil {
		t.Fatal(err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestMigrateIdempotent(t *testing.T) {
	pool := testPool(t)
	dropDbo(t, pool)
	ctx := context.Background()
	log := testLogger()

	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatalf("second migrate must be a no-op, got: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`select count(*) from _dbo.schema_migrations where version = '0001_init.sql'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("0001_init.sql recorded %d times, want exactly 1", n)
	}
}

// TestMigrateAndSeedConcurrent simulates two replicas booting simultaneously:
// both must return nil (advisory lock serializes DDL; seed tolerates the race).
func TestMigrateAndSeedConcurrent(t *testing.T) {
	pool := testPool(t)
	dropDbo(t, pool)
	dsn := os.Getenv("TEST_DATABASE_URL")
	log := testLogger()

	cfg := &Config{AdminEmail: "race@example.com", AdminPassword: "password-123"}

	const replicas = 2
	errs := make([]error, replicas)
	var wg sync.WaitGroup
	for i := 0; i < replicas; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			p, err := pgxpool.New(ctx, dsn) // separate pool per "replica"
			if err != nil {
				errs[i] = err
				return
			}
			defer p.Close()
			if err := Migrate(ctx, p, log); err != nil {
				errs[i] = err
				return
			}
			errs[i] = SeedAdmin(ctx, p, cfg, log)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("replica %d failed the concurrent boot: %v", i, err)
		}
	}

	var admins int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from _dbo.admins where email = 'race@example.com'`,
	).Scan(&admins); err != nil {
		t.Fatal(err)
	}
	if admins != 1 {
		t.Fatalf("admin seeded %d times, want exactly 1", admins)
	}
}

func TestSeedAdminNeverOverwrites(t *testing.T) {
	pool := testPool(t)
	dropDbo(t, pool)
	ctx := context.Background()
	log := testLogger()

	if err := Migrate(ctx, pool, log); err != nil {
		t.Fatal(err)
	}
	first := &Config{AdminEmail: "a@example.com", AdminPassword: "password-123"}
	if err := SeedAdmin(ctx, pool, first, log); err != nil {
		t.Fatal(err)
	}
	// Admins exist now; a different env must not add or replace anything.
	second := &Config{AdminEmail: "b@example.com", AdminPassword: "password-456"}
	if err := SeedAdmin(ctx, pool, second, log); err != nil {
		t.Fatal(err)
	}
	var total int
	if err := pool.QueryRow(ctx, `select count(*) from _dbo.admins`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("seed must never add a second admin, got %d", total)
	}

	var hash string
	if err := pool.QueryRow(ctx, `select password_hash from _dbo.admins`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("password must be bcrypt-hashed, got %q", hash[:4])
	}
}

func TestSeedAdminDefaultsToBootstrapCredential(t *testing.T) {
	pool := testPool(t)
	dropDbo(t, pool)
	ctx := context.Background()

	if err := Migrate(ctx, pool, testLogger()); err != nil {
		t.Fatal(err)
	}
	if err := SeedAdmin(ctx, pool, &Config{}, testLogger()); err != nil {
		t.Fatal(err)
	}

	var email string
	var mustChange bool
	if err := pool.QueryRow(ctx, `select email, must_change_password from _dbo.admins`).Scan(&email, &mustChange); err != nil {
		t.Fatal(err)
	}
	if email != BootstrapAdminEmail || !mustChange {
		t.Fatalf("seeded admin = %q mustChange=%v, want bootstrap forced-change admin", email, mustChange)
	}
}
