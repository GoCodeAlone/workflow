package external

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/GoCodeAlone/workflow/plugin"
)

// UIPluginEntry holds the runtime state of a loaded UI plugin.
type UIPluginEntry struct {
	// Manifest is the parsed ui.json for this plugin.
	Manifest UIManifest
	// AssetsDir is the absolute path to the plugin's static assets directory.
	AssetsDir string
}

// UIPluginInfo is the JSON representation of a UI plugin for API responses.
type UIPluginInfo struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description,omitempty"`
	NavItems    []UINavItem `json:"navItems,omitempty"`
	Loaded      bool        `json:"loaded"`
}

// UIPluginManager discovers and manages UI plugins under a shared plugins
// directory.  Each UI plugin is a subdirectory that contains a "ui.json"
// manifest and an optional "assets" subdirectory with static files.
//
// # Hot-reload
//
// Calling ReloadPlugin re-reads the manifest and the assets directory from
// disk without restarting the workflow engine. The static file server for the
// plugin is updated atomically so in-flight requests are not interrupted.
//
// # Integration with PluginManager navigation
//
// Call UIPages to get UIPageDef entries for a loaded UI plugin, then register
// a UIPluginNativePlugin wrapper with a PluginManager to surface those entries
// through the standard navigation API.
type UIPluginManager struct {
	pluginsDir string
	logger     *log.Logger

	mu      sync.RWMutex
	plugins map[string]*UIPluginEntry
}

// NewUIPluginManager creates a manager that scans the given directory for UI
// plugins (subdirectories containing a "ui.json" manifest).
func NewUIPluginManager(pluginsDir string, logger *log.Logger) *UIPluginManager {
	if logger == nil {
		logger = log.New(os.Stderr, "[ui-plugins] ", log.LstdFlags)
	}
	return &UIPluginManager{
		pluginsDir: pluginsDir,
		logger:     logger,
		plugins:    make(map[string]*UIPluginEntry),
	}
}

// DiscoverPlugins scans the plugins directory and returns names of all
// subdirectories that contain a "ui.json" manifest file.
func (m *UIPluginManager) DiscoverPlugins() ([]string, error) {
	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		manifestPath := filepath.Join(m.pluginsDir, name, "ui.json")
		if _, statErr := os.Stat(manifestPath); statErr == nil {
			names = append(names, name)
		}
	}
	return names, nil
}

// LoadPlugin reads the "ui.json" manifest for the named plugin and registers
// it.  If the plugin is already loaded it is replaced (hot-reload semantics).
func (m *UIPluginManager) LoadPlugin(name string) error {
	manifestPath := filepath.Join(m.pluginsDir, name, "ui.json")
	data, err := os.ReadFile(manifestPath) //nolint:gosec // path built from trusted pluginsDir + name
	if err != nil {
		return fmt.Errorf("read ui.json for plugin %q: %w", name, err)
	}

	var manifest UIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse ui.json for plugin %q: %w", name, err)
	}

	if manifest.Name == "" {
		manifest.Name = name
	}
	if manifest.AssetDir == "" {
		manifest.AssetDir = "assets"
	}

	assetDir := filepath.Join(m.pluginsDir, name, manifest.AssetDir)

	m.mu.Lock()
	m.plugins[name] = &UIPluginEntry{
		Manifest:  manifest,
		AssetsDir: assetDir,
	}
	m.mu.Unlock()

	m.logger.Printf("UI plugin %q loaded (version %s)", name, manifest.Version)
	return nil
}

// UnloadPlugin removes a UI plugin from the manager.  Returns an error if the
// plugin is not currently loaded.
func (m *UIPluginManager) UnloadPlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.plugins[name]; !exists {
		return fmt.Errorf("UI plugin %q is not loaded", name)
	}
	delete(m.plugins, name)
	m.logger.Printf("UI plugin %q unloaded", name)
	return nil
}

// ReloadPlugin re-reads the manifest and assets directory from disk for the
// named plugin.  This is the primary hot-reload mechanism: deploy updated
// assets to the plugin directory, then call this method.
func (m *UIPluginManager) ReloadPlugin(name string) error {
	return m.LoadPlugin(name)
}

// IsLoaded returns true if the named UI plugin is currently loaded.
func (m *UIPluginManager) IsLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.plugins[name]
	return exists
}

// GetPlugin returns the entry for the named UI plugin.  The second return
// value is false if the plugin is not loaded.
func (m *UIPluginManager) GetPlugin(name string) (*UIPluginEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.plugins[name]
	return e, ok
}

// LoadedPlugins returns the names of all currently loaded UI plugins.
func (m *UIPluginManager) LoadedPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// ServeAssets returns an http.Handler that serves the static assets of the
// named plugin directly from its assets directory.  Returns nil if the plugin
// is not loaded or its assets directory does not exist.
func (m *UIPluginManager) ServeAssets(name string) http.Handler {
	m.mu.RLock()
	entry, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return http.FileServer(http.Dir(entry.AssetsDir)) //nolint:gosec // path comes from trusted pluginsDir
}

// UIPages converts the nav items declared in a loaded UI plugin's manifest
// into plugin.UIPageDef entries compatible with the PluginManager navigation
// system.
func (m *UIPluginManager) UIPages(name string) []plugin.UIPageDef {
	m.mu.RLock()
	entry, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return uiNavItemsToPageDefs(name, entry.Manifest.NavItems)
}

// AllUIPluginInfos returns summary information for every currently loaded UI
// plugin.
func (m *UIPluginManager) AllUIPluginInfos() []UIPluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]UIPluginInfo, 0, len(m.plugins))
	for name, entry := range m.plugins {
		result = append(result, UIPluginInfo{
			Name:        name,
			Version:     entry.Manifest.Version,
			Description: entry.Manifest.Description,
			NavItems:    entry.Manifest.NavItems,
			Loaded:      true,
		})
	}
	return result
}

// AsNativePlugin returns a plugin.NativePlugin implementation that surfaces
// the named UI plugin's navigation entries through the standard PluginManager
// API.  Returns nil if the plugin is not loaded.
//
// Use this to register a UI plugin's nav items with a PluginManager:
//
//	if np := uiMgr.AsNativePlugin("my-ui-plugin"); np != nil {
//	    _ = pluginMgr.Register(np)
//	    _ = pluginMgr.Enable("my-ui-plugin")
//	}
func (m *UIPluginManager) AsNativePlugin(name string) plugin.NativePlugin {
	m.mu.RLock()
	entry, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return &UIPluginNativePlugin{
		manager: m,
		name:    name,
		version: entry.Manifest.Version,
		desc:    entry.Manifest.Description,
	}
}

// UIPluginNativePlugin adapts a loaded UI plugin as a plugin.NativePlugin so
// its navigation entries are visible through the standard PluginManager API.
// It implements a read-through to the UIPluginManager so that hot-reloads
// automatically update the navigation data returned by UIPages.
type UIPluginNativePlugin struct {
	manager *UIPluginManager
	name    string
	version string
	desc    string
}

func (p *UIPluginNativePlugin) Name() string        { return p.name }
func (p *UIPluginNativePlugin) Version() string     { return p.version }
func (p *UIPluginNativePlugin) Description() string { return p.desc }

func (p *UIPluginNativePlugin) Dependencies() []plugin.PluginDependency { return nil }

// UIPages reads the current nav items from the UIPluginManager so that
// hot-reloads (which call UIPluginManager.ReloadPlugin) are reflected
// immediately without re-registering the plugin.
func (p *UIPluginNativePlugin) UIPages() []plugin.UIPageDef {
	return p.manager.UIPages(p.name)
}

func (p *UIPluginNativePlugin) RegisterRoutes(_ *http.ServeMux)        {}
func (p *UIPluginNativePlugin) OnEnable(_ plugin.PluginContext) error  { return nil }
func (p *UIPluginNativePlugin) OnDisable(_ plugin.PluginContext) error { return nil }

// Ensure UIPluginNativePlugin satisfies plugin.NativePlugin at compile time.
var _ plugin.NativePlugin = (*UIPluginNativePlugin)(nil)

// uiNavItemsToPageDefs converts a slice of UINavItem into plugin.UIPageDef
// entries, filling in defaults where needed.
func uiNavItemsToPageDefs(pluginName string, items []UINavItem) []plugin.UIPageDef {
	defs := make([]plugin.UIPageDef, 0, len(items))
	for _, item := range items {
		category := item.Category
		if category == "" {
			category = "plugin"
		}
		defs = append(defs, plugin.UIPageDef{
			ID:                 item.ID,
			Label:              item.Label,
			Icon:               item.Icon,
			Category:           category,
			Order:              item.Order,
			RequiredRole:       item.RequiredRole,
			RequiredPermission: item.RequiredPermission,
			APIEndpoint:        "/api/v1/plugins/ui/" + pluginName + "/assets/",
		})
	}
	return defs
}
