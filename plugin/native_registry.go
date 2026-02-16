package plugin

import (
	"sort"
	"sync"
)

// NativeRegistry manages compiled-in native plugins.
type NativeRegistry struct {
	mu      sync.RWMutex
	plugins map[string]NativePlugin
}

// NewNativeRegistry creates a new empty native plugin registry.
func NewNativeRegistry() *NativeRegistry {
	return &NativeRegistry{
		plugins: make(map[string]NativePlugin),
	}
}

// Register adds a native plugin to the registry, keyed by its Name().
func (r *NativeRegistry) Register(p NativePlugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

// Get retrieves a native plugin by name.
func (r *NativeRegistry) Get(name string) (NativePlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

// List returns all registered native plugins sorted by name.
func (r *NativeRegistry) List() []NativePlugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugins := make([]NativePlugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		plugins = append(plugins, p)
	}
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Name() < plugins[j].Name()
	})
	return plugins
}

// UIPages returns all UI page definitions from all registered plugins, sorted by ID.
func (r *NativeRegistry) UIPages() []UIPageDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var pages []UIPageDef
	for _, p := range r.plugins {
		pages = append(pages, p.UIPages()...)
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].ID < pages[j].ID
	})
	return pages
}
