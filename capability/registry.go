package capability

import (
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// ProviderEntry represents a plugin that implements a capability.
type ProviderEntry struct {
	// PluginName is the name of the plugin providing this capability.
	PluginName string

	// Priority determines provider selection order; higher values win.
	Priority int

	// InterfaceImpl is the reflect.Type of the concrete type implementing the capability.
	InterfaceImpl reflect.Type
}

// Registry manages capability contracts and their providers.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	contracts map[string]Contract
	providers map[string][]ProviderEntry
}

// NewRegistry creates a new empty capability registry.
func NewRegistry() *Registry {
	return &Registry{
		contracts: make(map[string]Contract),
		providers: make(map[string][]ProviderEntry),
	}
}

// RegisterContract adds a capability contract to the registry.
// Returns an error if a contract with the same name already exists
// but has a different InterfaceType.
func (r *Registry) RegisterContract(c Contract) error {
	if c.Name == "" {
		return fmt.Errorf("capability: contract name is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.contracts[c.Name]; ok {
		if existing.InterfaceType != c.InterfaceType {
			return fmt.Errorf("capability: contract %q already registered with different interface type (existing: %v, new: %v)",
				c.Name, existing.InterfaceType, c.InterfaceType)
		}
		// Same name and same type — allow re-registration (idempotent).
		return nil
	}

	r.contracts[c.Name] = c
	return nil
}

// RegisterProvider registers a plugin as a provider for a capability.
// Returns an error if the capability has not been registered.
func (r *Registry) RegisterProvider(capabilityName, pluginName string, priority int, implType reflect.Type) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.contracts[capabilityName]; !ok {
		return fmt.Errorf("capability: %q is not a registered capability", capabilityName)
	}

	r.providers[capabilityName] = append(r.providers[capabilityName], ProviderEntry{
		PluginName:    pluginName,
		Priority:      priority,
		InterfaceImpl: implType,
	})

	return nil
}

// Resolve returns the highest-priority provider for a capability.
// Returns an error if no providers are registered for the capability.
func (r *Registry) Resolve(capabilityName string) (*ProviderEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, ok := r.providers[capabilityName]
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("capability: no providers registered for %q", capabilityName)
	}

	best := &entries[0]
	for i := 1; i < len(entries); i++ {
		if entries[i].Priority > best.Priority {
			best = &entries[i]
		}
	}

	return best, nil
}

// ListCapabilities returns a sorted list of all registered capability names.
func (r *Registry) ListCapabilities() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.contracts))
	for name := range r.contracts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasProvider returns true if at least one provider is registered for the capability.
func (r *Registry) HasProvider(capabilityName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries, ok := r.providers[capabilityName]
	return ok && len(entries) > 0
}

// ListProviders returns all providers registered for a capability.
// Returns nil if no providers are registered.
func (r *Registry) ListProviders(capabilityName string) []ProviderEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := r.providers[capabilityName]
	if len(entries) == 0 {
		return nil
	}

	result := make([]ProviderEntry, len(entries))
	copy(result, entries)
	return result
}

// ContractFor returns the contract for a capability name.
// Returns false if the capability is not registered.
func (r *Registry) ContractFor(capabilityName string) (*Contract, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.contracts[capabilityName]
	if !ok {
		return nil, false
	}
	return &c, true
}
