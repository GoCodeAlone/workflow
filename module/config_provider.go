// Package module contains the config provider implementation.
package module

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// configKeyRegexp matches {{config "key"}} or {{ config "key" }} patterns.
// It handles single or double quotes and optional whitespace.
var configKeyRegexp = regexp.MustCompile(`\{\{\s*config\s+"([^"]+)"\s*\}\}`)

// SchemaEntry defines a single configuration key's metadata.
type SchemaEntry struct {
	Env       string `json:"env"`
	Required  bool   `json:"required"`
	Default   string `json:"default"`
	Sensitive bool   `json:"sensitive"`
	Desc      string `json:"desc"`
}

// ConfigRegistry is a thread-safe, immutable store of resolved configuration values.
type ConfigRegistry struct {
	mu        sync.RWMutex
	values    map[string]string
	sensitive map[string]bool
	frozen    bool
}

// globalConfigRegistry is the singleton config registry used by the engine.
var globalConfigRegistry = &ConfigRegistry{
	values:    make(map[string]string),
	sensitive: make(map[string]bool),
}

// GetConfigRegistry returns the global config registry singleton.
func GetConfigRegistry() *ConfigRegistry {
	return globalConfigRegistry
}

// NewConfigRegistry creates a fresh ConfigRegistry. Primarily used for testing.
func NewConfigRegistry() *ConfigRegistry {
	return &ConfigRegistry{
		values:    make(map[string]string),
		sensitive: make(map[string]bool),
	}
}

// Set stores a value in the registry. Returns an error if the registry is frozen.
func (r *ConfigRegistry) Set(key, value string, sensitive bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("config registry is frozen; cannot set key %q", key)
	}
	r.values[key] = value
	r.sensitive[key] = sensitive
	return nil
}

// Get retrieves a value from the registry.
func (r *ConfigRegistry) Get(key string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.values[key]
	return v, ok
}

// IsSensitive returns whether a key is marked as sensitive.
func (r *ConfigRegistry) IsSensitive(key string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sensitive[key]
}

// Freeze makes the registry immutable. After calling Freeze, Set will return an error.
func (r *ConfigRegistry) Freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frozen = true
}

// Reset clears all values and unfreezes the registry. Intended for testing.
func (r *ConfigRegistry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = make(map[string]string)
	r.sensitive = make(map[string]bool)
	r.frozen = false
}

// Keys returns all registered configuration key names.
func (r *ConfigRegistry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.values))
	for k := range r.values {
		keys = append(keys, k)
	}
	return keys
}

// RedactedValue returns the value for display purposes. Sensitive values are
// replaced with "********".
func (r *ConfigRegistry) RedactedValue(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.sensitive[key] {
		return "********"
	}
	return r.values[key]
}

// ExpandConfigTemplate replaces all {{config "key"}} references in a string
// with their resolved values from the registry. Unresolved keys are left as-is.
func (r *ConfigRegistry) ExpandConfigTemplate(s string) string {
	return configKeyRegexp.ReplaceAllStringFunc(s, func(match string) string {
		sub := configKeyRegexp.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		if v, ok := r.Get(sub[1]); ok {
			return v
		}
		return match
	})
}

// ParseSchema parses a schema definition from a config map.
func ParseSchema(raw map[string]any) (map[string]SchemaEntry, error) {
	schema := make(map[string]SchemaEntry)
	for key, val := range raw {
		entryMap, ok := val.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("schema entry %q must be a map", key)
		}
		entry := SchemaEntry{}
		if v, ok := entryMap["env"].(string); ok {
			entry.Env = v
		}
		if v, ok := entryMap["required"].(bool); ok {
			entry.Required = v
		}
		if v, ok := entryMap["default"].(string); ok {
			entry.Default = v
		}
		if v, ok := entryMap["sensitive"].(bool); ok {
			entry.Sensitive = v
		}
		if v, ok := entryMap["desc"].(string); ok {
			entry.Desc = v
		}
		schema[key] = entry
	}
	return schema, nil
}

// LoadConfigSources loads configuration values into the registry from the
// declared sources in order. Later sources override earlier ones.
// Supported source types: "defaults" (from schema defaults) and "env" (from
// environment variables, with optional prefix).
func LoadConfigSources(registry *ConfigRegistry, sources []map[string]any, schemaEntries map[string]SchemaEntry) error {
	for _, src := range sources {
		srcType, _ := src["type"].(string)
		switch srcType {
		case "defaults":
			for key, entry := range schemaEntries {
				if entry.Default != "" {
					if err := registry.Set(key, entry.Default, entry.Sensitive); err != nil {
						return err
					}
				}
			}
		case "env":
			prefix, _ := src["prefix"].(string)
			for key, entry := range schemaEntries {
				envKey := entry.Env
				if envKey == "" {
					continue
				}
				if prefix != "" {
					envKey = prefix + envKey
				}
				if val, ok := os.LookupEnv(envKey); ok {
					if err := registry.Set(key, val, entry.Sensitive); err != nil {
						return err
					}
				}
			}
		default:
			return fmt.Errorf("unsupported config source type: %q", srcType)
		}
	}
	return nil
}

// ValidateRequired checks that all required schema keys have values in the
// registry. Returns an error listing all missing keys.
func ValidateRequired(registry *ConfigRegistry, schemaEntries map[string]SchemaEntry) error {
	var missing []string
	for key, entry := range schemaEntries {
		if entry.Required {
			if _, ok := registry.Get(key); !ok {
				missing = append(missing, key)
			}
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config keys: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ExpandConfigRefsMap recursively walks a config map and expands all
// {{config "key"}} references in string values using the given registry.
func ExpandConfigRefsMap(registry *ConfigRegistry, cfg map[string]any) {
	if registry == nil || cfg == nil {
		return
	}
	for k, v := range cfg {
		switch val := v.(type) {
		case string:
			cfg[k] = registry.ExpandConfigTemplate(val)
		case map[string]any:
			ExpandConfigRefsMap(registry, val)
		case []any:
			expandConfigRefsSlice(registry, val)
		}
	}
}

// expandConfigRefsSlice recursively walks a slice and expands all
// {{config "key"}} references in string values.
func expandConfigRefsSlice(registry *ConfigRegistry, items []any) {
	for i, item := range items {
		switch v := item.(type) {
		case string:
			items[i] = registry.ExpandConfigTemplate(v)
		case map[string]any:
			ExpandConfigRefsMap(registry, v)
		case []any:
			expandConfigRefsSlice(registry, v)
		}
	}
}

// ConfigProviderModule implements modular.Module for the config.provider type.
// It acts as a no-op module at runtime since all config resolution happens at
// build time via the ConfigTransformHook. The module exists to hold the config
// registry reference for service discovery.
type ConfigProviderModule struct {
	name     string
	config   map[string]any
	registry *ConfigRegistry
}

// NewConfigProviderModule creates a new ConfigProviderModule.
func NewConfigProviderModule(name string, cfg map[string]any) *ConfigProviderModule {
	return &ConfigProviderModule{
		name:     name,
		config:   cfg,
		registry: globalConfigRegistry,
	}
}

// Name returns the module name.
func (m *ConfigProviderModule) Name() string { return m.name }

// Dependencies returns an empty slice — config.provider has no dependencies.
func (m *ConfigProviderModule) Dependencies() []string { return nil }

// Configure registers the config registry as a service in the application.
func (m *ConfigProviderModule) Init(app modular.Application) error {
	return app.RegisterService("config.registry", m.registry)
}

// Registry returns the underlying ConfigRegistry.
func (m *ConfigProviderModule) Registry() *ConfigRegistry {
	return m.registry
}
