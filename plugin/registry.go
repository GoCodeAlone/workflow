package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// PluginEntry holds the manifest and component for a registered plugin.
type PluginEntry struct {
	Manifest  *PluginManifest           `json:"manifest"`
	Component *dynamic.DynamicComponent `json:"-"`
	SourceDir string                    `json:"source_dir,omitempty"`
}

// PluginRegistry manages plugin registration and lookup.
type PluginRegistry interface {
	Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error
	Unregister(name string) error
	Get(name string) (*PluginEntry, bool)
	List() []*PluginEntry
	CheckDependencies(manifest *PluginManifest) error
}

// LocalRegistry implements PluginRegistry by scanning local directories.
type LocalRegistry struct {
	mu      sync.RWMutex
	plugins map[string]*PluginEntry
}

// NewLocalRegistry creates a new empty local registry.
func NewLocalRegistry() *LocalRegistry {
	return &LocalRegistry{
		plugins: make(map[string]*PluginEntry),
	}
}

// Register adds a plugin to the registry after validating its manifest
// and checking version compatibility of declared dependencies.
func (r *LocalRegistry) Register(manifest *PluginManifest, component *dynamic.DynamicComponent, sourceDir string) error {
	if manifest == nil {
		return fmt.Errorf("manifest is required")
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	if err := r.CheckDependencies(manifest); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for version conflicts: if plugin already registered,
	// the new version must be >= the existing version.
	if existing, ok := r.plugins[manifest.Name]; ok {
		existingV, _ := ParseSemver(existing.Manifest.Version)
		newV, _ := ParseSemver(manifest.Version)
		if newV.Compare(existingV) < 0 {
			return fmt.Errorf("cannot downgrade plugin %q from %s to %s", manifest.Name, existing.Manifest.Version, manifest.Version)
		}
	}

	r.plugins[manifest.Name] = &PluginEntry{
		Manifest:  manifest,
		Component: component,
		SourceDir: sourceDir,
	}
	return nil
}

// Unregister removes a plugin from the registry.
func (r *LocalRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[name]; !ok {
		return fmt.Errorf("plugin %q not found", name)
	}
	delete(r.plugins, name)
	return nil
}

// Get retrieves a plugin entry by name.
func (r *LocalRegistry) Get(name string) (*PluginEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.plugins[name]
	return entry, ok
}

// List returns all registered plugin entries.
func (r *LocalRegistry) List() []*PluginEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]*PluginEntry, 0, len(r.plugins))
	for _, e := range r.plugins {
		entries = append(entries, e)
	}
	return entries
}

// CheckDependencies verifies that all dependencies declared in the manifest
// are satisfied by currently registered plugins.
func (r *LocalRegistry) CheckDependencies(manifest *PluginManifest) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, dep := range manifest.Dependencies {
		entry, ok := r.plugins[dep.Name]
		if !ok {
			return fmt.Errorf("unsatisfied dependency: plugin %q requires %q which is not registered", manifest.Name, dep.Name)
		}
		ok, err := CheckVersion(entry.Manifest.Version, dep.Constraint)
		if err != nil {
			return fmt.Errorf("dependency check error for %q: %w", dep.Name, err)
		}
		if !ok {
			return fmt.Errorf("dependency %q version %s does not satisfy constraint %q", dep.Name, entry.Manifest.Version, dep.Constraint)
		}
	}
	return nil
}

// ScanDirectory scans a directory for plugin subdirectories.
// Each subdirectory should contain a plugin.json manifest and a .go source file.
func (r *LocalRegistry) ScanDirectory(dir string, loader *dynamic.Loader) ([]*PluginEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("scan directory %s: %w", dir, err)
	}

	var loaded []*PluginEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pluginDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(pluginDir, "plugin.json")

		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue // Not a plugin directory
		}

		manifest, err := LoadManifest(manifestPath)
		if err != nil {
			return loaded, fmt.Errorf("load manifest from %s: %w", pluginDir, err)
		}

		// Load the component source from the plugin directory
		var comp *dynamic.DynamicComponent
		if loader != nil {
			sourceFiles, _ := filepath.Glob(filepath.Join(pluginDir, "*.go"))
			for _, sf := range sourceFiles {
				base := filepath.Base(sf)
				if base == "plugin_test.go" || filepath.Ext(base) != ".go" {
					continue
				}
				c, loadErr := loader.LoadFromFile(manifest.Name, sf)
				if loadErr != nil {
					return loaded, fmt.Errorf("load component from %s: %w", sf, loadErr)
				}
				comp = c
				break // Load the first .go file as the component
			}
		}

		if err := r.Register(manifest, comp, pluginDir); err != nil {
			return loaded, fmt.Errorf("register plugin %q: %w", manifest.Name, err)
		}
		loaded = append(loaded, &PluginEntry{Manifest: manifest, Component: comp, SourceDir: pluginDir})
	}
	return loaded, nil
}

// SaveManifest writes a manifest to a JSON file.
func SaveManifest(path string, manifest *PluginManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
