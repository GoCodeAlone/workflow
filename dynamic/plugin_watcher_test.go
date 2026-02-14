package dynamic

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestPluginWatcherStartStop(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	pw := NewPluginWatcher(loader, []string{dir}, WithPluginDebounce(50*time.Millisecond))
	if err := pw.Start(); err != nil {
		t.Fatalf("failed to start plugin watcher: %v", err)
	}
	if err := pw.Stop(); err != nil {
		t.Fatalf("failed to stop plugin watcher: %v", err)
	}
}

func TestPluginWatcherDevMode(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	pw := NewPluginWatcher(loader, []string{dir}, WithDevMode(true))
	if !pw.DevMode() {
		t.Error("expected dev mode to be enabled")
	}
}

func TestPluginWatcherLoadsExistingPlugins(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	// Write a valid plugin source file before starting the watcher
	source := `package component

func Name() string { return "existing-plugin" }
func Init(services map[string]interface{}) error { return nil }
func Execute(ctx interface{}, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "existing.go"), []byte(source), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	pw := NewPluginWatcher(loader, []string{dir},
		WithPluginDebounce(50*time.Millisecond),
		WithDevMode(true),
	)
	if err := pw.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer pw.Stop()

	// Give it a moment to load
	time.Sleep(100 * time.Millisecond)

	_, ok := registry.Get("existing")
	if !ok {
		t.Error("expected existing plugin to be loaded on startup")
	}
}

func TestPluginWatcherReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	var reloadMu sync.Mutex
	reloaded := make(map[string]bool)

	pw := NewPluginWatcher(loader, []string{dir},
		WithPluginDebounce(50*time.Millisecond),
		WithDevMode(true),
		WithOnReload(func(id string, err error) {
			reloadMu.Lock()
			reloaded[id] = err == nil
			reloadMu.Unlock()
		}),
	)
	if err := pw.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer pw.Stop()

	// Write a new plugin file
	source := `package component

func Name() string { return "dynamic-plugin" }
func Init(services map[string]interface{}) error { return nil }
`
	pluginPath := filepath.Join(dir, "dynamic_plugin.go")
	if err := os.WriteFile(pluginPath, []byte(source), 0644); err != nil {
		t.Fatalf("failed to write plugin: %v", err)
	}

	// Wait for the watcher to pick up the change
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		reloadMu.Lock()
		ok := reloaded["dynamic_plugin"]
		reloadMu.Unlock()
		if ok {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	reloadMu.Lock()
	ok := reloaded["dynamic_plugin"]
	reloadMu.Unlock()
	if !ok {
		t.Error("expected dynamic_plugin to be reloaded")
	}
}

func TestPluginWatcherMultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	pw := NewPluginWatcher(loader, []string{dir1, dir2},
		WithPluginDebounce(50*time.Millisecond),
		WithDevMode(true),
	)
	if err := pw.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer pw.Stop()

	// Write to dir1
	source1 := `package component
func Name() string { return "plugin-a" }
func Init(services map[string]interface{}) error { return nil }
`
	if err := os.WriteFile(filepath.Join(dir1, "plugin_a.go"), []byte(source1), 0644); err != nil {
		t.Fatalf("write plugin_a: %v", err)
	}

	// Write to dir2
	source2 := `package component
func Name() string { return "plugin-b" }
func Init(services map[string]interface{}) error { return nil }
`
	if err := os.WriteFile(filepath.Join(dir2, "plugin_b.go"), []byte(source2), 0644); err != nil {
		t.Fatalf("write plugin_b: %v", err)
	}

	// Wait for both to be loaded
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, aOk := registry.Get("plugin_a")
		_, bOk := registry.Get("plugin_b")
		if aOk && bOk {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}

	_, aOk := registry.Get("plugin_a")
	_, bOk := registry.Get("plugin_b")
	if !aOk {
		t.Error("expected plugin_a to be loaded")
	}
	if !bOk {
		t.Error("expected plugin_b to be loaded")
	}
}

func TestPluginWatcherCreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "plugins", "new")
	pool := NewInterpreterPool()
	registry := NewComponentRegistry()
	loader := NewLoader(pool, registry)

	pw := NewPluginWatcher(loader, []string{dir},
		WithPluginDebounce(50*time.Millisecond),
	)
	if err := pw.Start(); err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	defer pw.Stop()

	// Directory should have been created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected plugin directory to be created")
	}
}
