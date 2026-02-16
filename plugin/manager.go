package plugin

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

// PluginInfo is the JSON representation of a plugin for API responses.
type PluginInfo struct {
	Name         string             `json:"name"`
	Version      string             `json:"version"`
	Description  string             `json:"description"`
	Enabled      bool               `json:"enabled"`
	UIPages      []UIPageDef        `json:"ui_pages"`
	Dependencies []PluginDependency `json:"dependencies"`
	EnabledAt    string             `json:"enabled_at,omitempty"`
	DisabledAt   string             `json:"disabled_at,omitempty"`
}

// PluginManager handles plugin registration, dependency resolution, lifecycle management,
// enable/disable state persistence, and HTTP route dispatch.
type PluginManager struct {
	mu      sync.RWMutex
	plugins map[string]NativePlugin // all registered plugins
	enabled map[string]bool         // enabled state
	muxes   map[string]*http.ServeMux
	db      *sql.DB
	logger  *slog.Logger
	ctx     PluginContext
}

// NewPluginManager creates a new PluginManager with SQLite-backed state persistence.
// It initializes the plugin_state table if it does not exist.
func NewPluginManager(db *sql.DB, logger *slog.Logger) *PluginManager {
	if logger == nil {
		logger = slog.Default()
	}
	pm := &PluginManager{
		plugins: make(map[string]NativePlugin),
		enabled: make(map[string]bool),
		muxes:   make(map[string]*http.ServeMux),
		db:      db,
		logger:  logger,
	}
	if err := pm.initDB(); err != nil {
		logger.Error("Failed to initialize plugin_state table", "error", err)
	}
	return pm
}

// SetContext sets the shared PluginContext used for OnEnable/OnDisable calls.
func (pm *PluginManager) SetContext(ctx PluginContext) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.ctx = ctx
}

// Register adds a plugin to the known set. It does not enable the plugin.
// Returns an error if a plugin with the same name is already registered.
func (pm *PluginManager) Register(p NativePlugin) error {
	if p == nil {
		return fmt.Errorf("plugin is nil")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("plugin name is empty")
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; exists {
		return fmt.Errorf("plugin %q is already registered", name)
	}
	pm.plugins[name] = p
	pm.logger.Info("Plugin registered", "plugin", name, "version", p.Version())
	return nil
}

// Enable enables a plugin and all its unsatisfied dependencies (topological order).
// Returns an error if the plugin is not registered, if a dependency is missing,
// or if a circular dependency is detected.
func (pm *PluginManager) Enable(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; !exists {
		return fmt.Errorf("plugin %q is not registered", name)
	}

	// Resolve enable order via topological sort
	order, err := pm.resolveEnableOrder(name)
	if err != nil {
		return err
	}

	// Enable each plugin in dependency order
	for _, pName := range order {
		if pm.enabled[pName] {
			continue
		}
		if err := pm.enableOne(pName); err != nil {
			return fmt.Errorf("enable %q: %w", pName, err)
		}
	}
	return nil
}

// Disable disables a plugin and all plugins that depend on it (reverse dependency order).
// Returns an error if the plugin is not registered.
func (pm *PluginManager) Disable(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.plugins[name]; !exists {
		return fmt.Errorf("plugin %q is not registered", name)
	}

	if !pm.enabled[name] {
		return nil // already disabled
	}

	// Find all enabled plugins that transitively depend on this one
	order, err := pm.resolveDisableOrder(name)
	if err != nil {
		return err
	}

	// Disable in reverse dependency order (dependents first)
	for _, pName := range order {
		if !pm.enabled[pName] {
			continue
		}
		if err := pm.disableOne(pName); err != nil {
			return fmt.Errorf("disable %q: %w", pName, err)
		}
	}
	return nil
}

// IsEnabled returns whether a plugin is currently enabled.
func (pm *PluginManager) IsEnabled(name string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.enabled[name]
}

// EnabledPlugins returns all currently enabled plugins sorted by name.
func (pm *PluginManager) EnabledPlugins() []NativePlugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var result []NativePlugin
	for name, p := range pm.plugins {
		if pm.enabled[name] {
			result = append(result, p)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// AllPlugins returns info about all registered plugins sorted by name.
func (pm *PluginManager) AllPlugins() []PluginInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make([]PluginInfo, 0, len(pm.plugins))
	for name, p := range pm.plugins {
		uiPages := p.UIPages()
		if uiPages == nil {
			uiPages = []UIPageDef{}
		}
		deps := p.Dependencies()
		if deps == nil {
			deps = []PluginDependency{}
		}

		info := PluginInfo{
			Name:         name,
			Version:      p.Version(),
			Description:  p.Description(),
			Enabled:      pm.enabled[name],
			UIPages:      uiPages,
			Dependencies: deps,
		}

		// Load timestamps from DB if available
		if pm.db != nil {
			var enabledAt, disabledAt sql.NullString
			row := pm.db.QueryRow(
				"SELECT enabled_at, disabled_at FROM plugin_state WHERE name = ?", name,
			)
			if err := row.Scan(&enabledAt, &disabledAt); err == nil {
				if enabledAt.Valid {
					info.EnabledAt = enabledAt.String
				}
				if disabledAt.Valid {
					info.DisabledAt = disabledAt.String
				}
			}
		}

		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// RestoreState re-enables all plugins that were previously enabled (from the plugin_state table).
// This is called after an engine restart or reload.
func (pm *PluginManager) RestoreState() error {
	if pm.db == nil {
		return nil
	}

	rows, err := pm.db.Query("SELECT name FROM plugin_state WHERE enabled = 1")
	if err != nil {
		return fmt.Errorf("query plugin_state: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan plugin_state row: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate plugin_state rows: %w", err)
	}

	// Enable in dependency order. Enable() handles dependency resolution,
	// so we just call it for each plugin and let it skip already-enabled ones.
	for _, name := range names {
		if err := pm.Enable(name); err != nil {
			pm.logger.Warn("Failed to restore plugin state", "plugin", name, "error", err)
		}
	}
	return nil
}

// ServeHTTP dispatches HTTP requests to the correct plugin's mux.
// Route pattern: /api/v1/admin/plugins/{name}/{path...}
// Returns 404 if the plugin is not found or not enabled.
func (pm *PluginManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Handle plugin list endpoint
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == strings.TrimSuffix(nativePluginAPIPrefix, "/") {
		pm.handleListPlugins(w, r)
		return
	}

	// Extract plugin name from path: /api/v1/admin/plugins/{name}/...
	rest := strings.TrimPrefix(path, nativePluginAPIPrefix+"/")
	if rest == path {
		// Path doesn't start with the expected prefix
		http.NotFound(w, r)
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	pluginName := parts[0]
	if pluginName == "" {
		http.NotFound(w, r)
		return
	}

	pm.mu.RLock()
	_, registered := pm.plugins[pluginName]
	isEnabled := pm.enabled[pluginName]
	mux := pm.muxes[pluginName]
	pm.mu.RUnlock()

	if !registered || !isEnabled || mux == nil {
		http.NotFound(w, r)
		return
	}

	// Strip the plugin prefix and dispatch to the plugin's mux
	prefix := nativePluginAPIPrefix + "/" + pluginName
	http.StripPrefix(prefix, mux).ServeHTTP(w, r)
}

// handleListPlugins serves GET /api/v1/admin/plugins — returns all plugins with status.
func (pm *PluginManager) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	plugins := pm.AllPlugins()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(plugins)
}

// initDB creates the plugin_state table if it doesn't exist.
func (pm *PluginManager) initDB() error {
	if pm.db == nil {
		return nil
	}
	_, err := pm.db.Exec(`CREATE TABLE IF NOT EXISTS plugin_state (
		name TEXT PRIMARY KEY,
		enabled BOOLEAN NOT NULL DEFAULT 0,
		version TEXT NOT NULL,
		enabled_at TEXT,
		disabled_at TEXT
	)`)
	if err != nil {
		return fmt.Errorf("create plugin_state table: %w", err)
	}
	return nil
}

// enableOne enables a single plugin (no dependency resolution). Caller must hold pm.mu.
func (pm *PluginManager) enableOne(name string) error {
	p := pm.plugins[name]

	// Check version constraints on dependencies
	for _, dep := range p.Dependencies() {
		depPlugin, ok := pm.plugins[dep.Name]
		if !ok {
			return fmt.Errorf("dependency %q not registered", dep.Name)
		}
		if dep.MinVersion != "" {
			ok, err := CheckVersion(depPlugin.Version(), ">="+dep.MinVersion)
			if err != nil {
				return fmt.Errorf("check version for dep %q: %w", dep.Name, err)
			}
			if !ok {
				return fmt.Errorf("dependency %q version %s does not satisfy >= %s",
					dep.Name, depPlugin.Version(), dep.MinVersion)
			}
		}
	}

	// Create a per-plugin mux and register routes
	mux := http.NewServeMux()
	p.RegisterRoutes(mux)
	pm.muxes[name] = mux

	// Call OnEnable
	if err := p.OnEnable(pm.ctx); err != nil {
		delete(pm.muxes, name)
		return fmt.Errorf("OnEnable: %w", err)
	}

	pm.enabled[name] = true
	pm.persistState(name, true, p.Version())
	pm.logger.Info("Plugin enabled", "plugin", name)
	return nil
}

// disableOne disables a single plugin (no dependent resolution). Caller must hold pm.mu.
func (pm *PluginManager) disableOne(name string) error {
	p := pm.plugins[name]

	// Call OnDisable
	if err := p.OnDisable(pm.ctx); err != nil {
		pm.logger.Warn("OnDisable error (continuing)", "plugin", name, "error", err)
	}

	// Remove routes
	delete(pm.muxes, name)
	pm.enabled[name] = false
	pm.persistState(name, false, p.Version())
	pm.logger.Info("Plugin disabled", "plugin", name)
	return nil
}

// persistState writes the plugin enable/disable state to the database.
func (pm *PluginManager) persistState(name string, enabled bool, version string) {
	if pm.db == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var enabledAt, disabledAt interface{}
	if enabled {
		enabledAt = now
	} else {
		disabledAt = now
	}

	_, err := pm.db.Exec(`INSERT INTO plugin_state (name, enabled, version, enabled_at, disabled_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			enabled = excluded.enabled,
			version = excluded.version,
			enabled_at = CASE WHEN excluded.enabled_at IS NOT NULL THEN excluded.enabled_at ELSE plugin_state.enabled_at END,
			disabled_at = CASE WHEN excluded.disabled_at IS NOT NULL THEN excluded.disabled_at ELSE plugin_state.disabled_at END`,
		name, enabled, version, enabledAt, disabledAt,
	)
	if err != nil {
		pm.logger.Error("Failed to persist plugin state", "plugin", name, "error", err)
	}
}

// resolveEnableOrder returns the topological order to enable a plugin and its dependencies.
// The target plugin will be last in the returned slice.
func (pm *PluginManager) resolveEnableOrder(name string) ([]string, error) {
	var order []string
	visited := make(map[string]bool)
	inStack := make(map[string]bool) // for cycle detection

	var visit func(n string) error
	visit = func(n string) error {
		if visited[n] {
			return nil
		}
		if inStack[n] {
			return fmt.Errorf("circular dependency detected involving %q", n)
		}

		p, ok := pm.plugins[n]
		if !ok {
			return fmt.Errorf("dependency %q is not registered", n)
		}

		inStack[n] = true
		for _, dep := range p.Dependencies() {
			if err := visit(dep.Name); err != nil {
				return err
			}
		}
		inStack[n] = false
		visited[n] = true
		order = append(order, n)
		return nil
	}

	if err := visit(name); err != nil {
		return nil, err
	}
	return order, nil
}

// resolveDisableOrder returns the order to disable a plugin and all its transitive dependents.
// The target plugin will be last in the returned slice (dependents disabled first).
func (pm *PluginManager) resolveDisableOrder(name string) ([]string, error) {
	// Build reverse dependency graph: for each plugin, who depends on it
	dependents := make(map[string][]string)
	for pName, p := range pm.plugins {
		for _, dep := range p.Dependencies() {
			dependents[dep.Name] = append(dependents[dep.Name], pName)
		}
	}

	// BFS from the target to find all enabled transitive dependents
	var order []string
	visited := make(map[string]bool)

	var visit func(n string)
	visit = func(n string) {
		if visited[n] {
			return
		}
		visited[n] = true
		// Visit dependents first (so they get disabled before what they depend on)
		for _, dep := range dependents[n] {
			if pm.enabled[dep] {
				visit(dep)
			}
		}
		order = append(order, n)
	}

	visit(name)
	return order, nil
}
