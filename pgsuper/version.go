// Package pgsuper supervises one or more Postgres server clusters (majors
// 16/17/18) as child processes: binary discovery, initdb, start/stop/health,
// and project provisioning. This is the component neither PocketBase (embedded
// SQLite) nor postbase (hardcoded single version) provides.
package pgsuper

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version is a Postgres major version.
type Version int

const (
	PG16 Version = 16
	PG17 Version = 17
	PG18 Version = 18
)

// SupportedVersions are the majors dublyobase bundles and can supervise.
var SupportedVersions = []Version{PG16, PG17, PG18}

func (v Version) String() string { return strconv.Itoa(int(v)) }

// ParseVersion validates and parses a "16"/"17"/"18" string.
func ParseVersion(s string) (Version, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid postgres version %q", s)
	}
	v := Version(n)
	for _, sv := range SupportedVersions {
		if sv == v {
			return v, nil
		}
	}
	return 0, fmt.Errorf("unsupported postgres version %d (supported: 16, 17, 18)", n)
}

// Port returns the deterministic port for a version's cluster (5416/5417/5418).
func (v Version) Port() int { return 5400 + int(v) }

// BinDir locates the bin directory containing initdb/pg_ctl/postgres for this
// major. Order: explicit env override, then common PGDG/Homebrew locations.
func (v Version) BinDir() (string, error) {
	if env := os.Getenv(fmt.Sprintf("PGSUPER_PG%d_BINDIR", int(v))); env != "" {
		return env, nil
	}
	candidates := []string{
		fmt.Sprintf("/usr/lib/postgresql/%d/bin", int(v)),          // Debian/Ubuntu PGDG
		fmt.Sprintf("/usr/pgsql-%d/bin", int(v)),                   // RHEL/Rocky PGDG
		fmt.Sprintf("/opt/homebrew/opt/postgresql@%d/bin", int(v)), // Homebrew (arm64)
		fmt.Sprintf("/usr/local/opt/postgresql@%d/bin", int(v)),    // Homebrew (intel)
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(filepath.Join(dir, "initdb")); err == nil && !fi.IsDir() {
			return dir, nil
		}
	}
	return "", fmt.Errorf(
		"postgres %d binaries not found; install them or set PGSUPER_PG%d_BINDIR",
		int(v), int(v),
	)
}
