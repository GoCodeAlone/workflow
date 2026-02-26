package external

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	goplugin "github.com/GoCodeAlone/go-plugin"
	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
)

// ExternalPluginManager discovers, loads, and manages external plugin subprocesses.
// Each plugin lives in its own subdirectory under the plugins directory and communicates
// with the host via gRPC through the go-plugin framework.
type ExternalPluginManager struct {
	pluginsDir string
	logger     *log.Logger

	mu      sync.RWMutex
	clients map[string]*goplugin.Client
}

// NewExternalPluginManager creates a new manager that scans the given directory for plugins.
func NewExternalPluginManager(pluginsDir string, logger *log.Logger) *ExternalPluginManager {
	if logger == nil {
		logger = log.New(os.Stderr, "[external-plugins] ", log.LstdFlags)
	}
	return &ExternalPluginManager{
		pluginsDir: pluginsDir,
		logger:     logger,
		clients:    make(map[string]*goplugin.Client),
	}
}

// DiscoverPlugins scans the plugins directory for subdirectories that contain
// a plugin.json manifest and an executable binary matching the directory name.
// It returns the list of discovered plugin names.
func (m *ExternalPluginManager) DiscoverPlugins() ([]string, error) {
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
		manifestPath := filepath.Join(m.pluginsDir, name, "plugin.json")
		binaryPath := filepath.Join(m.pluginsDir, name, name)

		// Check manifest exists
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}
		// Check binary exists
		if _, err := os.Stat(binaryPath); err != nil {
			continue
		}

		names = append(names, name)
	}
	return names, nil
}

// LoadPlugin starts the named plugin subprocess, performs the handshake, and
// creates an ExternalPluginAdapter. The plugin must have been previously
// discovered via DiscoverPlugins.
func (m *ExternalPluginManager) LoadPlugin(name string) (*ExternalPluginAdapter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.clients[name]; exists {
		return nil, fmt.Errorf("plugin %q is already loaded", name)
	}

	pluginDir := filepath.Join(m.pluginsDir, name)
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	binaryPath := filepath.Join(pluginDir, name)

	// Validate manifest
	manifest, err := pluginpkg.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest for plugin %q: %w", name, err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate manifest for plugin %q: %w", name, err)
	}

	// Verify binary is executable
	info, err := os.Stat(binaryPath) //nolint:gosec // G703: plugin binary path from trusted data/plugins directory
	if err != nil {
		return nil, fmt.Errorf("stat binary for plugin %q: %w", name, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("plugin %q binary path is a directory", name)
	}

	m.logger.Printf("starting plugin %q (version %s)", name, manifest.Version)

	// Run the plugin subprocess with its own directory as the working directory.
	// This ensures plugins that extract embedded assets (e.g. ui_dist/) write to
	// their own directory rather than inheriting the parent's working directory,
	// which may not be writable (e.g. /app owned by root, process runs as nonroot).
	cmd := exec.Command(binaryPath) //nolint:gosec // G204: plugin binary path is from trusted data/plugins directory
	cmd.Dir = pluginDir

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          goplugin.PluginSet{"plugin": &GRPCPlugin{}},
		Cmd:              cmd,
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
	})

	// Connect to the plugin process via gRPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("connect to plugin %q: %w", name, err)
	}

	// Dispense the plugin interface
	raw, err := rpcClient.Dispense("plugin")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("dispense plugin %q: %w", name, err)
	}

	pluginClient, ok := raw.(*PluginClient)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("plugin %q: dispensed object is not *PluginClient (got %T)", name, raw)
	}

	adapter, err := NewExternalPluginAdapter(name, pluginClient)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("create adapter for plugin %q: %w", name, err)
	}

	m.clients[name] = client
	m.logger.Printf("plugin %q loaded successfully", name)

	return adapter, nil
}

// UnloadPlugin stops the named plugin subprocess and removes it from the internal map.
func (m *ExternalPluginManager) UnloadPlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, exists := m.clients[name]
	if !exists {
		return fmt.Errorf("plugin %q is not loaded", name)
	}

	m.logger.Printf("unloading plugin %q", name)
	client.Kill()
	delete(m.clients, name)
	m.logger.Printf("plugin %q unloaded", name)

	return nil
}

// ReloadPlugin unloads and then loads the named plugin.
func (m *ExternalPluginManager) ReloadPlugin(name string) (*ExternalPluginAdapter, error) {
	// Unload first (ignore error if not loaded)
	_ = m.UnloadPlugin(name)

	return m.LoadPlugin(name)
}

// LoadedPlugins returns the names of all currently loaded plugins.
func (m *ExternalPluginManager) LoadedPlugins() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.clients))
	for name := range m.clients {
		names = append(names, name)
	}
	return names
}

// IsLoaded returns true if the named plugin is currently loaded.
func (m *ExternalPluginManager) IsLoaded(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.clients[name]
	return exists
}

// Shutdown kills all loaded plugin subprocesses.
func (m *ExternalPluginManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, client := range m.clients {
		m.logger.Printf("shutting down plugin %q", name)
		client.Kill()
	}
	m.clients = make(map[string]*goplugin.Client)
	m.logger.Printf("all external plugins shut down")
}
