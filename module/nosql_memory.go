package module

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// MemoryNoSQLConfig holds configuration for the nosql.memory module.
type MemoryNoSQLConfig struct {
	Collection string `json:"collection" yaml:"collection"`
}

// MemoryNoSQL is a thread-safe in-memory NoSQL store.
// type: nosql.memory — useful for testing and local scenarios.
type MemoryNoSQL struct {
	name  string
	cfg   MemoryNoSQLConfig
	mu    sync.RWMutex
	items map[string]map[string]any
}

// NewMemoryNoSQL creates a new MemoryNoSQL module.
func NewMemoryNoSQL(name string, cfg MemoryNoSQLConfig) *MemoryNoSQL {
	return &MemoryNoSQL{
		name:  name,
		cfg:   cfg,
		items: make(map[string]map[string]any),
	}
}

func (m *MemoryNoSQL) Name() string { return m.name }

func (m *MemoryNoSQL) Init(_ modular.Application) error { return nil }

func (m *MemoryNoSQL) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "In-memory NoSQL store: " + m.name, Instance: m},
	}
}

func (m *MemoryNoSQL) RequiresServices() []modular.ServiceDependency { return nil }

// Get retrieves an item by key. Returns nil, nil when the key does not exist.
func (m *MemoryNoSQL) Get(_ context.Context, key string) (map[string]any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.items[key]
	if !ok {
		return nil, nil
	}
	// Return a shallow copy to prevent external mutation
	out := make(map[string]any, len(item))
	for k, v := range item {
		out[k] = v
	}
	return out, nil
}

// Put inserts or replaces an item.
func (m *MemoryNoSQL) Put(_ context.Context, key string, item map[string]any) error {
	if key == "" {
		return fmt.Errorf("nosql.memory %q: key must not be empty", m.name)
	}
	stored := make(map[string]any, len(item))
	for k, v := range item {
		stored[k] = v
	}
	m.mu.Lock()
	m.items[key] = stored
	m.mu.Unlock()
	return nil
}

// Delete removes an item by key. Does not error if key does not exist.
func (m *MemoryNoSQL) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	delete(m.items, key)
	m.mu.Unlock()
	return nil
}

// Query returns items matching the params filter.
// Supported params: "prefix" (string key prefix filter).
func (m *MemoryNoSQL) Query(_ context.Context, params map[string]any) ([]map[string]any, error) {
	prefix, _ := params["prefix"].(string)

	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []map[string]any
	for key, item := range m.items {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		out := make(map[string]any, len(item)+1)
		for k, v := range item {
			out[k] = v
		}
		out["_key"] = key
		results = append(results, out)
	}
	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}
