package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"windowseat/internal/contracts"
)

// DefaultStatePath is $XDG_STATE_HOME/windowseat/snapshot.json, falling back
// to the user cache directory.
func DefaultStatePath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		base = cache
	}
	return filepath.Join(base, "windowseat", "snapshot.json"), nil
}

// store keeps the latest snapshot on disk. It holds nothing else.
type store struct {
	path    string
	mu      sync.Mutex
	lastErr error
}

func newStore(path string) *store {
	return &store{path: path}
}

func (s *store) enabled() bool {
	return s.path != ""
}

// load returns the saved provider snapshots, or none if the file is
// missing or no longer valid.
func (s *store) load() map[contracts.Provider]contracts.ProviderSnapshot {
	if !s.enabled() {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.setErr(err)
		}
		return nil
	}
	var snapshot contracts.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		s.setErr(err)
		return nil
	}
	if err := snapshot.Validate(); err != nil {
		s.setErr(err)
		return nil
	}
	saved := make(map[contracts.Provider]contracts.ProviderSnapshot, len(snapshot.Providers))
	for _, p := range snapshot.Providers {
		saved[p.Provider] = p
	}
	return saved
}

func (s *store) save(snapshot contracts.Snapshot) {
	if !s.enabled() || len(snapshot.Providers) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = writeAtomic(s.path, snapshot)
}

func (s *store) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

func (s *store) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastErr = err
}

func writeAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".snapshot-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
