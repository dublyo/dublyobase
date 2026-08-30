package core

import (
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgx defaults MaxConns to max(4, numCPU), which caps a server at about four
// concurrent queries because every request holds a connection for its whole
// transaction. Measured against a live instance that was a flat ceiling of
// roughly six requests a second whatever the concurrency.
func TestPoolLimitsOverrideTheLibraryDefault(t *testing.T) {
	log := slog.New(slog.DiscardHandler)

	base := "postgres://u:p@localhost:5432/db?sslmode=disable"
	cfgFor := func(url string, max, min int32) *pgxpool.Config {
		poolCfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal(err)
		}
		applyPoolLimits(poolCfg, url, &Config{DatabaseMaxConns: max, DatabaseMinConns: min}, log)
		return poolCfg
	}

	got := cfgFor(base, 25, 2)
	if got.MaxConns != 25 || got.MinConns != 2 {
		t.Errorf("defaults not applied: max=%d min=%d", got.MaxConns, got.MinConns)
	}

	// An operator who wrote the limit into the URL meant it.
	explicit := base + "&pool_max_conns=7"
	got = cfgFor(explicit, 25, 2)
	if got.MaxConns != 7 {
		t.Errorf("explicit pool_max_conns was overridden: %d", got.MaxConns)
	}

	// Min above max would make the pool unusable.
	got = cfgFor(base, 4, 50)
	if got.MinConns > got.MaxConns {
		t.Errorf("min %d exceeds max %d", got.MinConns, got.MaxConns)
	}

	// Zero means "leave the library default alone".
	poolCfg, _ := pgxpool.ParseConfig(base)
	before := poolCfg.MaxConns
	applyPoolLimits(poolCfg, base, &Config{}, log)
	if poolCfg.MaxConns != before {
		t.Errorf("zero config changed MaxConns from %d to %d", before, poolCfg.MaxConns)
	}
}
