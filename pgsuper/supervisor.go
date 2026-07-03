package pgsuper

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
)

// Supervisor manages one Cluster per Postgres major version, lazily started.
type Supervisor struct {
	root string // parent dir holding per-version PGDATA dirs

	mu       sync.Mutex
	clusters map[Version]*Cluster
}

// New creates a Supervisor rooted at the given clusters directory.
func New(root string) *Supervisor {
	return &Supervisor{
		root:     root,
		clusters: make(map[Version]*Cluster),
	}
}

// EnsureCluster initializes (initdb) and starts the cluster for a version if
// needed, and returns it. Repeated calls reuse a running cluster.
func (s *Supervisor) EnsureCluster(v Version) (*Cluster, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if c, ok := s.clusters[v]; ok && c.Running() {
		return c, nil
	}

	binDir, err := v.BinDir()
	if err != nil {
		return nil, err
	}

	c := &Cluster{
		Version: v,
		DataDir: filepath.Join(s.root, "pg"+v.String()),
		Port:    v.Port(),
		binDir:  binDir,
	}
	if err := c.Init(); err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, err
	}
	s.clusters[v] = c
	return c, nil
}

// ProvisionProject ensures the target-version cluster is up and creates the
// project database inside it.
func (s *Supervisor) ProvisionProject(v Version, name string) (*Cluster, error) {
	c, err := s.EnsureCluster(v)
	if err != nil {
		return nil, err
	}
	if err := c.ensureDatabase(name); err != nil {
		return nil, err
	}
	return c, nil
}

// Clusters returns the tracked clusters sorted by version.
func (s *Supervisor) Clusters() []*Cluster {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Cluster, 0, len(s.clusters))
	for _, c := range s.clusters {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// StopAll performs a fast shutdown of every tracked cluster (used on exit).
func (s *Supervisor) StopAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var firstErr error
	for v, c := range s.clusters {
		if err := c.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("stop pg%s: %w", v, err)
		}
	}
	return firstErr
}
