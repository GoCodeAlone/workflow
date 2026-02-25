package module

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FSIaCStateStore persists IaC state as JSON files under a configured directory.
// Lock files (resourceID + ".lock") are used for concurrent safety.
type FSIaCStateStore struct {
	dir string
	mu  sync.Mutex // protects in-process lock map
}

// NewFSIaCStateStore creates a filesystem-backed state store rooted at dir.
// The directory is created on first use if it does not exist.
func NewFSIaCStateStore(dir string) *FSIaCStateStore {
	return &FSIaCStateStore{dir: dir}
}

// statePath returns the JSON file path for a resource ID.
func (s *FSIaCStateStore) statePath(resourceID string) string {
	return filepath.Join(s.dir, sanitizeID(resourceID)+".json")
}

// lockPath returns the lock file path for a resource ID.
func (s *FSIaCStateStore) lockPath(resourceID string) string {
	return filepath.Join(s.dir, sanitizeID(resourceID)+".lock")
}

// sanitizeID replaces path-unsafe characters so resource IDs can be used as filenames.
func sanitizeID(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, "\\", "_")
	return id
}

// ensureDir creates the storage directory if it does not exist.
func (s *FSIaCStateStore) ensureDir() error {
	return os.MkdirAll(s.dir, 0o750)
}

// GetState reads the JSON state file for resourceID. Returns nil, nil when not found.
func (s *FSIaCStateStore) GetState(resourceID string) (*IaCState, error) {
	if err := s.ensureDir(); err != nil {
		return nil, fmt.Errorf("iac fs state: GetState %q: %w", resourceID, err)
	}
	data, err := os.ReadFile(s.statePath(resourceID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("iac fs state: GetState %q: %w", resourceID, err)
	}
	var st IaCState
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("iac fs state: GetState %q: unmarshal: %w", resourceID, err)
	}
	return &st, nil
}

// SaveState writes the state record as a JSON file, creating the directory as needed.
func (s *FSIaCStateStore) SaveState(state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac fs state: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac fs state: SaveState: resource_id must not be empty")
	}
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("iac fs state: SaveState %q: %w", state.ResourceID, err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("iac fs state: SaveState %q: marshal: %w", state.ResourceID, err)
	}
	if err := os.WriteFile(s.statePath(state.ResourceID), data, 0o640); err != nil {
		return fmt.Errorf("iac fs state: SaveState %q: write: %w", state.ResourceID, err)
	}
	return nil
}

// ListStates reads all JSON files from the directory and returns those matching filter.
// Supported filter keys: "resource_type", "provider", "status".
func (s *FSIaCStateStore) ListStates(filter map[string]string) ([]*IaCState, error) {
	if err := s.ensureDir(); err != nil {
		return nil, fmt.Errorf("iac fs state: ListStates: %w", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("iac fs state: ListStates: read dir: %w", err)
	}
	var results []*IaCState
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			continue // skip unreadable files
		}
		var st IaCState
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		if matchesFilter(&st, filter) {
			results = append(results, &st)
		}
	}
	return results, nil
}

// DeleteState removes the JSON state file for resourceID.
func (s *FSIaCStateStore) DeleteState(resourceID string) error {
	path := s.statePath(resourceID)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("iac fs state: DeleteState %q: not found", resourceID)
		}
		return fmt.Errorf("iac fs state: DeleteState %q: %w", resourceID, err)
	}
	return nil
}

// Lock creates a lock file for resourceID. Fails if the lock file already exists.
func (s *FSIaCStateStore) Lock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureDir(); err != nil {
		return fmt.Errorf("iac fs state: Lock %q: %w", resourceID, err)
	}
	lp := s.lockPath(resourceID)
	// O_CREATE|O_EXCL atomically creates the file only if it does not exist.
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("iac fs state: Lock %q: resource is already locked", resourceID)
		}
		return fmt.Errorf("iac fs state: Lock %q: %w", resourceID, err)
	}
	// Write a timestamp into the lock file for diagnostics.
	_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339))
	_ = f.Close()
	return nil
}

// Unlock removes the lock file for resourceID.
func (s *FSIaCStateStore) Unlock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lp := s.lockPath(resourceID)
	if err := os.Remove(lp); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("iac fs state: Unlock %q: not locked", resourceID)
		}
		return fmt.Errorf("iac fs state: Unlock %q: %w", resourceID, err)
	}
	return nil
}
