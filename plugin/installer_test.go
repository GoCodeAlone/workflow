package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeTestManifest creates a plugin.json file in the given directory with a valid manifest.
func writeTestManifest(t *testing.T, dir string, manifest *PluginManifest) {
	t.Helper()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func newTestManifest(name, version string) *PluginManifest {
	return &PluginManifest{
		Name:        name,
		Version:     version,
		Author:      "test-author",
		Description: "A test plugin",
	}
}

func TestInstallFromBundle(t *testing.T) {
	// Create a temp bundle directory with plugin.json and a dummy file
	bundleDir := t.TempDir()
	manifest := newTestManifest("test-plugin", "1.0.0")
	writeTestManifest(t, bundleDir, manifest)

	// Write a dummy source file to the bundle
	dummyFile := filepath.Join(bundleDir, "README.md")
	if err := os.WriteFile(dummyFile, []byte("# Test Plugin"), 0644); err != nil {
		t.Fatalf("write dummy file: %v", err)
	}

	installDir := t.TempDir()
	localReg := NewLocalRegistry()
	installer := NewPluginInstaller(nil, localReg, nil, installDir)

	if err := installer.InstallFromBundle(bundleDir); err != nil {
		t.Fatalf("InstallFromBundle: %v", err)
	}

	// Verify the plugin was copied
	destManifest := filepath.Join(installDir, "test-plugin", "plugin.json")
	if _, err := os.Stat(destManifest); err != nil {
		t.Errorf("expected manifest at %s, got error: %v", destManifest, err)
	}

	// Verify the README was copied
	destReadme := filepath.Join(installDir, "test-plugin", "README.md")
	if _, err := os.Stat(destReadme); err != nil {
		t.Errorf("expected README at %s, got error: %v", destReadme, err)
	}

	// Verify it was registered in the local registry
	entry, ok := localReg.Get("test-plugin")
	if !ok {
		t.Fatal("expected plugin to be registered in local registry")
	}
	if entry.Manifest.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", entry.Manifest.Version)
	}
}

func TestInstallFromBundle_MissingManifest(t *testing.T) {
	bundleDir := t.TempDir()
	installDir := t.TempDir()
	installer := NewPluginInstaller(nil, nil, nil, installDir)

	err := installer.InstallFromBundle(bundleDir)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestIsInstalled(t *testing.T) {
	installDir := t.TempDir()
	installer := NewPluginInstaller(nil, nil, nil, installDir)

	// Not installed initially
	if installer.IsInstalled("test-plugin") {
		t.Error("expected plugin to not be installed")
	}

	// Create the plugin directory with manifest
	pluginDir := filepath.Join(installDir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := newTestManifest("test-plugin", "1.0.0")
	writeTestManifest(t, pluginDir, manifest)

	// Now should be installed
	if !installer.IsInstalled("test-plugin") {
		t.Error("expected plugin to be installed")
	}
}

func TestUninstall(t *testing.T) {
	installDir := t.TempDir()
	localReg := NewLocalRegistry()
	installer := NewPluginInstaller(nil, localReg, nil, installDir)

	// Create and register a plugin
	pluginDir := filepath.Join(installDir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := newTestManifest("test-plugin", "1.0.0")
	writeTestManifest(t, pluginDir, manifest)
	if err := localReg.Register(manifest, nil, pluginDir); err != nil {
		t.Fatalf("register: %v", err)
	}

	if !installer.IsInstalled("test-plugin") {
		t.Fatal("expected plugin to be installed before uninstall")
	}

	if err := installer.Uninstall("test-plugin"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if installer.IsInstalled("test-plugin") {
		t.Error("expected plugin to be uninstalled")
	}

	// Verify it was unregistered from local registry
	if _, ok := localReg.Get("test-plugin"); ok {
		t.Error("expected plugin to be unregistered from local registry")
	}
}

func TestUninstall_NotInstalled(t *testing.T) {
	installDir := t.TempDir()
	installer := NewPluginInstaller(nil, nil, nil, installDir)

	err := installer.Uninstall("nonexistent")
	if err == nil {
		t.Fatal("expected error for uninstalling nonexistent plugin")
	}
}

func TestScanInstalled(t *testing.T) {
	installDir := t.TempDir()
	localReg := NewLocalRegistry()
	installer := NewPluginInstaller(nil, localReg, nil, installDir)

	// Create two plugin directories (ScanDirectory expects subdirs with plugin.json)
	for _, name := range []string{"plugin-a", "plugin-b"} {
		pluginDir := filepath.Join(installDir, name)
		if err := os.MkdirAll(pluginDir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		manifest := newTestManifest(name, "1.0.0")
		writeTestManifest(t, pluginDir, manifest)
	}

	// Scan should find both plugins
	entries, err := installer.ScanInstalled()
	if err != nil {
		t.Fatalf("ScanInstalled: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	// Verify they are in the local registry
	for _, name := range []string{"plugin-a", "plugin-b"} {
		if _, ok := localReg.Get(name); !ok {
			t.Errorf("expected plugin %q to be registered", name)
		}
	}
}

func TestScanInstalled_EmptyDir(t *testing.T) {
	installDir := t.TempDir()
	localReg := NewLocalRegistry()
	installer := NewPluginInstaller(nil, localReg, nil, installDir)

	entries, err := installer.ScanInstalled()
	if err != nil {
		t.Fatalf("ScanInstalled: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestScanInstalled_NonexistentDir(t *testing.T) {
	localReg := NewLocalRegistry()
	installer := NewPluginInstaller(nil, localReg, nil, "/tmp/nonexistent-installer-dir-test")

	entries, err := installer.ScanInstalled()
	if err != nil {
		t.Fatalf("ScanInstalled should not error for nonexistent dir: %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries, got %v", entries)
	}
}

func TestInstall_NoRemoteRegistry(t *testing.T) {
	installDir := t.TempDir()
	installer := NewPluginInstaller(nil, nil, nil, installDir)

	err := installer.Install(nil, "some-plugin", "1.0.0")
	if err == nil {
		t.Fatal("expected error when no remote registry configured")
	}
	if err.Error() != "no remote registry configured" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInstall_AlreadyInstalled(t *testing.T) {
	installDir := t.TempDir()
	installer := NewPluginInstaller(nil, nil, nil, installDir)

	// Create an installed plugin
	pluginDir := filepath.Join(installDir, "test-plugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := newTestManifest("test-plugin", "1.0.0")
	writeTestManifest(t, pluginDir, manifest)

	// Should return nil (no-op) for already installed plugin
	err := installer.Install(nil, "test-plugin", "1.0.0")
	if err != nil {
		t.Fatalf("expected nil for already installed plugin, got: %v", err)
	}
}

func TestInstallDir(t *testing.T) {
	installer := NewPluginInstaller(nil, nil, nil, "/some/dir")
	if installer.InstallDir() != "/some/dir" {
		t.Errorf("expected /some/dir, got %s", installer.InstallDir())
	}
}

func TestCopyDir(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "dest")

	// Create source structure
	if err := os.MkdirAll(filepath.Join(src, "subdir"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, "subdir", "nested.txt"), []byte("world"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := copyDir(src, dst); err != nil {
		t.Fatalf("copyDir: %v", err)
	}

	// Verify files were copied
	data, err := os.ReadFile(filepath.Join(dst, "file.txt"))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("expected 'hello', got %q", string(data))
	}

	data, err = os.ReadFile(filepath.Join(dst, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("expected 'world', got %q", string(data))
	}
}
