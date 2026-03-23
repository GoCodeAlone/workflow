package wftest

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/GoCodeAlone/modular"
	"gopkg.in/yaml.v3"
)

// StateStore provides in-memory state for test pipelines.
// It is safe for concurrent use.
type StateStore struct {
	mu     sync.RWMutex
	stores map[string]map[string]any // store_name → key → value
}

// NewStateStore creates an empty StateStore.
func NewStateStore() *StateStore {
	return &StateStore{stores: make(map[string]map[string]any)}
}

// Get retrieves a value from a named store.
func (s *StateStore) Get(store, key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.stores[store]
	if !ok {
		return nil, false
	}
	v, ok := m[key]
	return v, ok
}

// Set writes a value to a named store.
func (s *StateStore) Set(store, key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores[store] == nil {
		s.stores[store] = make(map[string]any)
	}
	s.stores[store][key] = value
}

// GetAll returns a shallow copy of all entries in a named store.
// Returns nil if the store does not exist.
func (s *StateStore) GetAll(store string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.stores[store]
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

// Seed loads initial state into a named store.
// Existing keys are overwritten; other keys are preserved.
func (s *StateStore) Seed(store string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stores[store] == nil {
		s.stores[store] = make(map[string]any)
	}
	maps.Copy(s.stores[store], data)
}

// LoadFixture loads state from a JSON or YAML file into a named store.
// The file must unmarshal to map[string]any.
func (s *StateStore) LoadFixture(path string, store string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("LoadFixture: read %s: %w", path, err)
	}
	var m map[string]any
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("LoadFixture: parse %s: %w", path, err)
		}
	default: // JSON
		if err := json.Unmarshal(data, &m); err != nil {
			return fmt.Errorf("LoadFixture: parse %s: %w", path, err)
		}
	}
	s.Seed(store, m)
	return nil
}

// Assert checks that each key/value pair in expected matches the actual state
// in the named store. Comparison is done via JSON marshaling so numeric types
// are normalised. Returns nil on full match, or an error describing the first
// mismatch.
func (s *StateStore) Assert(store string, expected map[string]any) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	actual := s.stores[store]
	for key, want := range expected {
		got, ok := actual[key]
		if !ok {
			return fmt.Errorf("state[%s][%s]: key not found", store, key)
		}
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		if string(wantJSON) != string(gotJSON) {
			return fmt.Errorf("state[%s][%s]: want %s, got %s", store, key, wantJSON, gotJSON)
		}
	}
	return nil
}

// stateStoreModule adapts StateStore to modular.Module so it can be registered
// in the service registry under "wftest.state_store".
type stateStoreModule struct {
	store *StateStore
}

func (m *stateStoreModule) Name() string { return "wftest.state_store" }

func (m *stateStoreModule) Init(app modular.Application) error {
	return app.RegisterService("wftest.state_store", m.store)
}

func (m *stateStoreModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: "wftest.state_store", Description: "in-memory test state store", Instance: m.store},
	}
}

func (m *stateStoreModule) RequiresServices() []modular.ServiceDependency { return nil }
