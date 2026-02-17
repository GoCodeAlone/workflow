package plugin

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/schema"
)

// EnginePluginManager wraps the PluginLoader with lifecycle management,
// allowing plugins to be registered, enabled, and disabled independently.
type EnginePluginManager struct {
	loader        *PluginLoader
	capabilityReg *capability.Registry
	schemaReg     *schema.ModuleSchemaRegistry
	plugins       map[string]EnginePlugin // keyed by name
	enabled       map[string]bool
}

// NewEnginePluginManager creates a new manager backed by the given capability and schema registries.
func NewEnginePluginManager(capReg *capability.Registry, schemaReg *schema.ModuleSchemaRegistry) *EnginePluginManager {
	return &EnginePluginManager{
		loader:        NewPluginLoader(capReg, schemaReg),
		capabilityReg: capReg,
		schemaReg:     schemaReg,
		plugins:       make(map[string]EnginePlugin),
		enabled:       make(map[string]bool),
	}
}

// Register adds a plugin to the manager without enabling it.
// Returns an error if a plugin with the same name is already registered.
func (m *EnginePluginManager) Register(p EnginePlugin) error {
	name := p.EngineManifest().Name
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("engine plugin manager: plugin %q already registered", name)
	}
	m.plugins[name] = p
	return nil
}

// Enable activates a registered plugin, loading it into the PluginLoader.
// Returns an error if the plugin is not registered or is already enabled.
func (m *EnginePluginManager) Enable(name string) error {
	p, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("engine plugin manager: plugin %q not registered", name)
	}
	if m.enabled[name] {
		return fmt.Errorf("engine plugin manager: plugin %q already enabled", name)
	}
	if err := m.loader.LoadPlugin(p); err != nil {
		return fmt.Errorf("engine plugin manager: enable %q: %w", name, err)
	}
	m.enabled[name] = true
	return nil
}

// Disable deactivates a plugin. The plugin remains registered but its factories
// are no longer active. Note: a full rebuild of the loader is performed to
// remove the plugin's contributions.
func (m *EnginePluginManager) Disable(name string) error {
	if _, exists := m.plugins[name]; !exists {
		return fmt.Errorf("engine plugin manager: plugin %q not registered", name)
	}
	if !m.enabled[name] {
		return fmt.Errorf("engine plugin manager: plugin %q not enabled", name)
	}

	m.enabled[name] = false

	// Rebuild the loader from scratch with only the remaining enabled plugins.
	newLoader := NewPluginLoader(m.capabilityReg, m.schemaReg)
	for pName, p := range m.plugins {
		if m.enabled[pName] {
			if err := newLoader.LoadPlugin(p); err != nil {
				return fmt.Errorf("engine plugin manager: rebuild after disable %q: %w", name, err)
			}
		}
	}
	m.loader = newLoader
	return nil
}

// IsEnabled returns true if the named plugin is currently enabled.
func (m *EnginePluginManager) IsEnabled(name string) bool {
	return m.enabled[name]
}

// Get returns the plugin with the given name and a boolean indicating whether it exists.
func (m *EnginePluginManager) Get(name string) (EnginePlugin, bool) {
	p, ok := m.plugins[name]
	return p, ok
}

// List returns all registered plugins (enabled and disabled).
func (m *EnginePluginManager) List() []EnginePlugin {
	out := make([]EnginePlugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		out = append(out, p)
	}
	return out
}

// Loader returns the underlying PluginLoader for accessing aggregated factories and hooks.
func (m *EnginePluginManager) Loader() *PluginLoader {
	return m.loader
}
