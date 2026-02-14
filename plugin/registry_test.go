package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func validManifest(name, version string) *PluginManifest {
	return &PluginManifest{
		Name:        name,
		Version:     version,
		Author:      "Test Author",
		Description: "A test plugin",
	}
}

func TestLocalRegistryRegisterAndGet(t *testing.T) {
	r := NewLocalRegistry()
	m := validManifest("test-plugin", "1.0.0")

	if err := r.Register(m, nil, ""); err != nil {
		t.Fatalf("Register error: %v", err)
	}

	entry, ok := r.Get("test-plugin")
	if !ok {
		t.Fatal("expected plugin to be found")
	}
	if entry.Manifest.Name != "test-plugin" {
		t.Errorf("Name = %q, want %q", entry.Manifest.Name, "test-plugin")
	}
}

func TestLocalRegistryRegisterNilManifest(t *testing.T) {
	r := NewLocalRegistry()
	if err := r.Register(nil, nil, ""); err == nil {
		t.Error("expected error for nil manifest")
	}
}

func TestLocalRegistryRegisterInvalidManifest(t *testing.T) {
	r := NewLocalRegistry()
	m := &PluginManifest{Name: ""} // invalid
	if err := r.Register(m, nil, ""); err == nil {
		t.Error("expected error for invalid manifest")
	}
}

func TestLocalRegistryUnregister(t *testing.T) {
	r := NewLocalRegistry()
	m := validManifest("test-plugin", "1.0.0")
	_ = r.Register(m, nil, "")

	if err := r.Unregister("test-plugin"); err != nil {
		t.Fatalf("Unregister error: %v", err)
	}

	_, ok := r.Get("test-plugin")
	if ok {
		t.Error("expected plugin to be removed")
	}
}

func TestLocalRegistryUnregisterNotFound(t *testing.T) {
	r := NewLocalRegistry()
	if err := r.Unregister("nonexistent"); err == nil {
		t.Error("expected error for unregistering nonexistent plugin")
	}
}

func TestLocalRegistryList(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("plugin-a", "1.0.0"), nil, "")
	_ = r.Register(validManifest("plugin-b", "2.0.0"), nil, "")

	entries := r.List()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestLocalRegistryVersionUpgrade(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("my-plugin", "1.0.0"), nil, "")

	// Upgrade should work
	if err := r.Register(validManifest("my-plugin", "1.1.0"), nil, ""); err != nil {
		t.Fatalf("upgrade error: %v", err)
	}

	entry, _ := r.Get("my-plugin")
	if entry.Manifest.Version != "1.1.0" {
		t.Errorf("Version = %q, want %q", entry.Manifest.Version, "1.1.0")
	}
}

func TestLocalRegistryVersionDowngradeRejected(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("my-plugin", "2.0.0"), nil, "")

	err := r.Register(validManifest("my-plugin", "1.0.0"), nil, "")
	if err == nil {
		t.Error("expected error for version downgrade")
	}
}

func TestLocalRegistryCheckDependencies(t *testing.T) {
	r := NewLocalRegistry()

	// Register the dependency first
	_ = r.Register(validManifest("dep-plugin", "1.5.0"), nil, "")

	// Register a plugin that depends on dep-plugin
	m := validManifest("my-plugin", "1.0.0")
	m.Dependencies = []Dependency{
		{Name: "dep-plugin", Constraint: ">=1.0.0"},
	}
	if err := r.Register(m, nil, ""); err != nil {
		t.Fatalf("Register with satisfied dependency error: %v", err)
	}
}

func TestLocalRegistryCheckDependenciesUnsatisfied(t *testing.T) {
	r := NewLocalRegistry()

	m := validManifest("my-plugin", "1.0.0")
	m.Dependencies = []Dependency{
		{Name: "missing-plugin", Constraint: ">=1.0.0"},
	}
	if err := r.Register(m, nil, ""); err == nil {
		t.Error("expected error for unsatisfied dependency")
	}
}

func TestLocalRegistryCheckDependenciesVersionMismatch(t *testing.T) {
	r := NewLocalRegistry()
	_ = r.Register(validManifest("dep-plugin", "0.5.0"), nil, "")

	m := validManifest("my-plugin", "1.0.0")
	m.Dependencies = []Dependency{
		{Name: "dep-plugin", Constraint: ">=1.0.0"},
	}
	if err := r.Register(m, nil, ""); err == nil {
		t.Error("expected error for version mismatch")
	}
}

func TestLocalRegistryConcurrency(t *testing.T) {
	r := NewLocalRegistry()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := "plugin-" + string(rune('a'+idx%26))
			m := validManifest(name, "1.0.0")
			_ = r.Register(m, nil, "")
		}(i)
	}

	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
		}()
	}

	wg.Wait()
}

func TestLocalRegistryScanDirectory(t *testing.T) {
	dir := t.TempDir()

	// Create a valid plugin subdirectory
	pluginDir := filepath.Join(dir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}

	manifest := validManifest("test-plugin", "1.0.0")
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// No loader, so component will be nil
	r := NewLocalRegistry()
	loaded, err := r.ScanDirectory(dir, nil)
	if err != nil {
		t.Fatalf("ScanDirectory error: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 loaded plugin, got %d", len(loaded))
	}
	if loaded[0].Manifest.Name != "test-plugin" {
		t.Errorf("Name = %q, want %q", loaded[0].Manifest.Name, "test-plugin")
	}

	// Verify it was registered
	_, ok := r.Get("test-plugin")
	if !ok {
		t.Error("expected plugin to be registered after scan")
	}
}

func TestLocalRegistryScanDirectorySkipsNonPlugin(t *testing.T) {
	dir := t.TempDir()

	// Create a non-plugin subdirectory (no manifest)
	nonPlugin := filepath.Join(dir, "not-a-plugin")
	if err := os.MkdirAll(nonPlugin, 0755); err != nil {
		t.Fatal(err)
	}

	r := NewLocalRegistry()
	loaded, err := r.ScanDirectory(dir, nil)
	if err != nil {
		t.Fatalf("ScanDirectory error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 loaded plugins, got %d", len(loaded))
	}
}

func TestLocalRegistryScanDirectoryNotExist(t *testing.T) {
	r := NewLocalRegistry()
	_, err := r.ScanDirectory("/nonexistent/path", nil)
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestSaveManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	m := validManifest("save-test", "1.0.0")

	if err := SaveManifest(path, m); err != nil {
		t.Fatalf("SaveManifest error: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest error: %v", err)
	}
	if loaded.Name != m.Name || loaded.Version != m.Version {
		t.Errorf("loaded manifest does not match saved: got %+v", loaded)
	}
}
