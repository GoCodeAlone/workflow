package module

import (
	"fmt"
	"sync"
)

// MemoryIaCStateStore is an in-memory implementation of IaCStateStore.
// Suitable for testing and development; state is lost on restart.
type MemoryIaCStateStore struct {
	mu     sync.RWMutex
	states map[string]*IaCState
	locks  map[string]bool
}

// NewMemoryIaCStateStore creates a new empty in-memory state store.
func NewMemoryIaCStateStore() *MemoryIaCStateStore {
	return &MemoryIaCStateStore{
		states: make(map[string]*IaCState),
		locks:  make(map[string]bool),
	}
}

// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
func (s *MemoryIaCStateStore) GetState(resourceID string) (*IaCState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[resourceID]
	if !ok {
		return nil, nil
	}
	// Return a shallow copy to prevent external mutation.
	cp := *st
	return &cp, nil
}

// SaveState inserts or replaces a state record.
func (s *MemoryIaCStateStore) SaveState(state *IaCState) error {
	if state == nil {
		return fmt.Errorf("iac state store: SaveState: state must not be nil")
	}
	if state.ResourceID == "" {
		return fmt.Errorf("iac state store: SaveState: resource_id must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *state
	s.states[state.ResourceID] = &cp
	return nil
}

// ListStates returns all state records matching the provided key=value filter.
// Supported filter keys: "resource_type", "provider", "status".
func (s *MemoryIaCStateStore) ListStates(filter map[string]string) ([]*IaCState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var results []*IaCState
	for _, st := range s.states {
		if matchesFilter(st, filter) {
			cp := *st
			results = append(results, &cp)
		}
	}
	return results, nil
}

// DeleteState removes a state record by resource ID.
func (s *MemoryIaCStateStore) DeleteState(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.states[resourceID]; !ok {
		return fmt.Errorf("iac state store: DeleteState: resource %q not found", resourceID)
	}
	delete(s.states, resourceID)
	return nil
}

// Lock acquires an exclusive advisory lock for the given resource ID.
func (s *MemoryIaCStateStore) Lock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locks[resourceID] {
		return fmt.Errorf("iac state store: Lock: resource %q is already locked", resourceID)
	}
	s.locks[resourceID] = true
	return nil
}

// Unlock releases the advisory lock for the given resource ID.
func (s *MemoryIaCStateStore) Unlock(resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locks[resourceID] {
		return fmt.Errorf("iac state store: Unlock: resource %q is not locked", resourceID)
	}
	delete(s.locks, resourceID)
	return nil
}

// matchesFilter returns true if state satisfies all entries in the filter map.
func matchesFilter(st *IaCState, filter map[string]string) bool {
	for k, v := range filter {
		switch k {
		case "resource_type":
			if st.ResourceType != v {
				return false
			}
		case "provider":
			if st.Provider != v {
				return false
			}
		case "status":
			if st.Status != v {
				return false
			}
		}
	}
	return true
}
