package external

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	opsMu   sync.Mutex
	mu      sync.RWMutex
	clients map[string]*goplugin.Client

	callbackServer *CallbackServer

	startPlugin func(name string) (*pluginLaunch, error)
}

type pluginLaunch struct {
	client  *goplugin.Client
	adapter *ExternalPluginAdapter
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

// SetCallbackServer configures the host callback server used by plugins that
// expose triggers or host callback features.
func (m *ExternalPluginManager) SetCallbackServer(server *CallbackServer) {
	m.mu.Lock()
	m.callbackServer = server
	m.mu.Unlock()
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
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.RLock()
	if _, exists := m.clients[name]; exists {
		m.mu.RUnlock()
		return nil, fmt.Errorf("plugin %q is already loaded", name)
	}
	m.mu.RUnlock()

	launch, err := m.startPluginUnlocked(name)
	if err != nil {
		return nil, err
	}
	if err := validatePluginLaunch(name, launch); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if _, exists := m.clients[name]; exists {
		m.mu.Unlock()
		launch.client.Kill()
		return nil, fmt.Errorf("plugin %q is already loaded", name)
	}
	m.clients[name] = launch.client
	m.mu.Unlock()
	m.logger.Printf("plugin %q loaded successfully", name)

	return launch.adapter, nil
}

func (m *ExternalPluginManager) startPluginUnlocked(name string) (*pluginLaunch, error) {
	if m.startPlugin != nil {
		return m.startPlugin(name)
	}

	pluginDir := filepath.Join(m.pluginsDir, name)
	manifestPath := filepath.Join(pluginDir, "plugin.json")
	// Resolve the binary path to absolute. os/exec.Cmd.Start evaluates a
	// relative Path *inside* cmd.Dir, so a relative binary path + relative
	// cmd.Dir would double-nest to "<pluginDir>/<pluginDir>/<name>", which
	// fails with ENOENT ("no such file or directory") even though the binary
	// exists at the intended location. Absolutising here makes Path + Dir
	// independent.
	binaryPath, err := filepath.Abs(filepath.Join(pluginDir, name))
	if err != nil {
		return nil, fmt.Errorf("resolve binary path for plugin %q: %w", name, err)
	}

	// Validate manifest
	manifest, err := pluginpkg.LoadManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest for plugin %q: %w", name, err)
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("validate manifest for plugin %q: %w", name, err)
	}

	// Verify binary integrity against the lockfile checksum before loading.
	// A mismatch is logged as a warning and the plugin is skipped, rather than
	// crashing the engine so other plugins can still be loaded.
	if err := pluginpkg.VerifyPluginIntegrity(m.pluginsDir, name); err != nil {
		m.logger.Printf("WARNING: skipping plugin %q — integrity check failed: %v", name, err)
		return nil, fmt.Errorf("integrity check failed for plugin %q: %w", name, err)
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

	m.mu.RLock()
	callbackServer := m.callbackServer
	m.mu.RUnlock()

	// Run the plugin subprocess with its own directory as the working directory.
	// This ensures plugins that extract embedded assets (e.g. ui_dist/) write to
	// their own directory rather than inheriting the parent's working directory,
	// which may not be writable (e.g. /app owned by root, process runs as nonroot).
	cmd := exec.Command(binaryPath) //nolint:gosec // G204: plugin binary path is from trusted data/plugins directory
	cmd.Dir = pluginDir
	pluginStderr := newPluginStderrForwarder(name, m.logger)

	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          goplugin.PluginSet{"plugin": &GRPCPlugin{CallbackServer: callbackServer}},
		Cmd:              cmd,
		Stderr:           pluginStderr,
		SyncStderr:       pluginStderr,
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

	adapter, err := NewExternalPluginAdapter(name, pluginClient, manifest)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("create adapter for plugin %q: %w", name, err)
	}

	return &pluginLaunch{client: client, adapter: adapter}, nil
}

type pluginStderrForwarder struct {
	name   string
	logger *log.Logger
	mu     sync.Mutex
	buf    string
}

func newPluginStderrForwarder(name string, logger *log.Logger) *pluginStderrForwarder {
	return &pluginStderrForwarder{name: name, logger: logger}
}

func (w *pluginStderrForwarder) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf += string(p)
	lines := strings.Split(w.buf, "\n")
	w.buf = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.logger.Printf("plugin %q stderr: %s", w.name, line)
	}
	return len(p), nil
}

// UnloadPlugin stops the named plugin subprocess and removes it from the internal map.
func (m *ExternalPluginManager) UnloadPlugin(name string) error {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	client, exists := m.clients[name]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("plugin %q is not loaded", name)
	}
	delete(m.clients, name)
	m.mu.Unlock()

	m.logger.Printf("unloading plugin %q", name)
	client.Kill()
	m.logger.Printf("plugin %q unloaded", name)

	return nil
}

// ReloadPlugin starts and validates the replacement before stopping the
// currently loaded plugin. Candidate failure leaves the old process registered.
func (m *ExternalPluginManager) ReloadPlugin(name string) (*ExternalPluginAdapter, error) {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.RLock()
	oldClient, wasLoaded := m.clients[name]
	m.mu.RUnlock()
	if !wasLoaded {
		launch, err := m.startPluginUnlocked(name)
		if err != nil {
			return nil, err
		}
		if err := validatePluginLaunch(name, launch); err != nil {
			return nil, err
		}
		m.mu.Lock()
		m.clients[name] = launch.client
		m.mu.Unlock()
		m.logger.Printf("plugin %q loaded successfully", name)
		return launch.adapter, nil
	}

	launch, err := m.startPluginUnlocked(name)
	if err != nil {
		m.logger.Printf("plugin %q reload failed; keeping existing plugin active: %v", name, err)
		return nil, fmt.Errorf("reload plugin %q: %w", name, err)
	}
	if err := validatePluginLaunch(name, launch); err != nil {
		m.logger.Printf("plugin %q reload failed; keeping existing plugin active: %v", name, err)
		return nil, fmt.Errorf("reload plugin %q: %w", name, err)
	}

	m.mu.Lock()
	m.clients[name] = launch.client
	m.mu.Unlock()
	oldClient.Kill()
	m.logger.Printf("plugin %q reloaded successfully", name)
	return launch.adapter, nil
}

func validatePluginLaunch(name string, launch *pluginLaunch) error {
	if launch == nil {
		return fmt.Errorf("plugin %q launch returned nil result", name)
	}
	if launch.client == nil {
		return fmt.Errorf("plugin %q launch returned nil client", name)
	}
	if launch.adapter == nil {
		return fmt.Errorf("plugin %q launch returned nil adapter", name)
	}
	return nil
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
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	m.mu.Lock()
	clients := m.clients
	m.clients = make(map[string]*goplugin.Client)
	m.mu.Unlock()

	for name, client := range clients {
		m.logger.Printf("shutting down plugin %q", name)
		client.Kill()
	}
	m.logger.Printf("all external plugins shut down")
}
