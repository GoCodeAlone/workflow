package dynamic

import (
	"fmt"
	"sync"
)

// ComponentRegistry tracks all dynamically loaded components.
// It is safe for concurrent access.
type ComponentRegistry struct {
	mu         sync.RWMutex
	components map[string]*DynamicComponent
}

// NewComponentRegistry creates an empty registry.
func NewComponentRegistry() *ComponentRegistry {
	return &ComponentRegistry{
		components: make(map[string]*DynamicComponent),
	}
}

// Register adds or replaces a component in the registry.
func (r *ComponentRegistry) Register(id string, component *DynamicComponent) error {
	if id == "" {
		return fmt.Errorf("component id must not be empty")
	}
	if component == nil {
		return fmt.Errorf("component must not be nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.components[id] = component
	return nil
}

// Unregister removes a component from the registry.
func (r *ComponentRegistry) Unregister(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.components[id]; !exists {
		return fmt.Errorf("component %q not found", id)
	}
	delete(r.components, id)
	return nil
}

// Get retrieves a component by ID.
func (r *ComponentRegistry) Get(id string) (*DynamicComponent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.components[id]
	return c, ok
}

// List returns info for all registered components.
func (r *ComponentRegistry) List() []ComponentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]ComponentInfo, 0, len(r.components))
	for _, c := range r.components {
		infos = append(infos, c.Info())
	}
	return infos
}

// Count returns the number of registered components.
func (r *ComponentRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.components)
}
