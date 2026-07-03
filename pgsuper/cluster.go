package pgsuper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Cluster is a single supervised Postgres data directory (one major version).
type Cluster struct {
	Version Version
	DataDir string // PGDATA
	Port    int

	binDir string
}

func (c *Cluster) bin(name string) string { return filepath.Join(c.binDir, name) }

// Initialized reports whether initdb has already run for this data dir.
func (c *Cluster) Initialized() bool {
	_, err := os.Stat(filepath.Join(c.DataDir, "PG_VERSION"))
	return err == nil
}

// Init runs initdb into DataDir if not already initialized.
func (c *Cluster) Init() error {
	if c.Initialized() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.DataDir), 0o750); err != nil {
		return err
	}
	cmd := exec.Command(c.bin("initdb"),
		"-D", c.DataDir,
		"-U", "postgres",
		"--auth-local=trust",
		"--auth-host=scram-sha-256",
		"-E", "UTF8",
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("initdb pg%s: %w", c.Version, err)
	}
	return nil
}

// Start launches the postgres server (via pg_ctl) and waits for readiness.
// The unix socket lives inside DataDir to keep the cluster self-contained.
func (c *Cluster) Start() error {
	if c.Running() {
		return nil
	}
	logfile := filepath.Join(c.DataDir, "postgres.log")
	cmd := exec.Command(c.bin("pg_ctl"),
		"-D", c.DataDir,
		"-l", logfile,
		"-o", fmt.Sprintf("-p %d -k %s -c listen_addresses=127.0.0.1", c.Port, c.DataDir),
		"-w", "start",
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start pg%s: %w", c.Version, err)
	}
	return nil
}

// Stop performs a fast shutdown of the cluster.
func (c *Cluster) Stop() error {
	if !c.Initialized() {
		return nil
	}
	cmd := exec.Command(c.bin("pg_ctl"), "-D", c.DataDir, "-m", "fast", "-w", "stop")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// Running reports whether the server accepts connections (via pg_isready).
func (c *Cluster) Running() bool {
	cmd := exec.Command(c.bin("pg_isready"),
		"-p", strconv.Itoa(c.Port),
		"-h", c.DataDir, // socket dir
	)
	return cmd.Run() == nil
}

// ConnString returns a libpq connection string for the given database.
func (c *Cluster) ConnString(db string) string {
	if db == "" {
		db = "postgres"
	}
	return fmt.Sprintf("host=%s port=%d user=postgres dbname=%s sslmode=disable",
		c.DataDir, c.Port, db)
}

// ensureDatabase creates a project database if it doesn't already exist.
// (Roles/schema/RLS grants land in M2; this proves the provisioning path.)
func (c *Cluster) ensureDatabase(name string) error {
	cmd := exec.Command(c.bin("createdb"),
		"-p", strconv.Itoa(c.Port),
		"-h", c.DataDir,
		"-U", "postgres",
		name,
	)
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(out), "already exists") {
		return fmt.Errorf("createdb %q: %s", name, strings.TrimSpace(string(out)))
	}
	return nil
}
