package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPluginListAcceptsPluginDirFlag verifies that -plugin-dir is accepted by
// runPluginList and correctly used as the directory to scan.
func TestPluginListAcceptsPluginDirFlag(t *testing.T) {
	dir := t.TempDir()

	// Create a fake installed plugin directory with a minimal plugin.json.
	pluginDir := filepath.Join(dir, "myplugin")
	if err := os.MkdirAll(pluginDir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"myplugin","version":"1.0.0","author":"test","description":"test plugin"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	// Should succeed using -plugin-dir.
	if err := runPluginList([]string{"-plugin-dir", dir}); err != nil {
		t.Errorf("-plugin-dir: runPluginList returned unexpected error: %v", err)
	}
}

// TestPluginListAcceptsLegacyDataDirFlag verifies that the deprecated -data-dir flag
// still works as an alias for -plugin-dir.
func TestPluginListAcceptsLegacyDataDirFlag(t *testing.T) {
	dir := t.TempDir()

	// Should succeed using -data-dir (deprecated alias).
	if err := runPluginList([]string{"-data-dir", dir}); err != nil {
		t.Errorf("-data-dir: runPluginList returned unexpected error: %v", err)
	}
}
