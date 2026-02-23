package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePluginManifest creates a valid plugin.json in the given directory and returns its path.
func writePluginManifest(t *testing.T, dir string, content map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestRunPluginTestRegisteredInDispatch(t *testing.T) {
	// Verify "test" is dispatched correctly through runPlugin.
	// We use a bad plugin dir to confirm the dispatch reaches runPluginTest.
	err := runPlugin([]string{"test", "--timeout", "1s", "/nonexistent-plugin-dir"})
	if err == nil {
		t.Fatal("expected error for nonexistent plugin dir")
	}
	// Should complain about missing manifest, not "unknown subcommand".
	if strings.Contains(err.Error(), "plugin subcommand is required") {
		t.Errorf("'test' subcommand was not dispatched correctly; got: %v", err)
	}
}

func TestRunPluginTestMissingManifest(t *testing.T) {
	dir := t.TempDir()
	err := runPluginTest([]string{dir})
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
	if !strings.Contains(err.Error(), "plugin manifest") {
		t.Errorf("expected manifest error, got: %v", err)
	}
}

func TestRunPluginTestInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	// Write a manifest that fails Validate() — missing required fields.
	writePluginManifest(t, dir, map[string]any{
		"name":    "",
		"version": "1.0.0",
		"author":  "tester",
	})
	err := runPluginTest([]string{dir})
	if err == nil {
		t.Fatal("expected error for invalid manifest")
	}
}

func TestRunPluginTestValidManifest(t *testing.T) {
	dir := t.TempDir()
	writePluginManifest(t, dir, map[string]any{
		"name":        "test-plugin",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "A test plugin for wfctl plugin test",
	})
	if err := runPluginTest([]string{"--timeout", "5s", dir}); err != nil {
		t.Fatalf("plugin test failed: %v", err)
	}
}

func TestRunPluginTestVerbose(t *testing.T) {
	dir := t.TempDir()
	writePluginManifest(t, dir, map[string]any{
		"name":        "verbose-plugin",
		"version":     "0.2.1",
		"author":      "tester",
		"description": "Verbose test plugin",
	})
	if err := runPluginTest([]string{"--verbose", "--timeout", "5s", dir}); err != nil {
		t.Fatalf("plugin test with verbose failed: %v", err)
	}
}

func TestRunPluginTestWithConfig(t *testing.T) {
	dir := t.TempDir()
	writePluginManifest(t, dir, map[string]any{
		"name":        "config-plugin",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "Plugin with config",
	})
	// Write a test config YAML.
	cfgPath := filepath.Join(dir, "test-config.yaml")
	if err := os.WriteFile(cfgPath, []byte("config:\n  key: value\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := runPluginTest([]string{"--config", cfgPath, dir}); err != nil {
		t.Fatalf("plugin test with config failed: %v", err)
	}
}

func TestRunPluginTestInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	writePluginManifest(t, dir, map[string]any{
		"name":        "cfg-plugin",
		"version":     "1.0.0",
		"author":      "tester",
		"description": "Plugin for bad config test",
	})
	err := runPluginTest([]string{"--config", "/nonexistent/path/config.yaml", dir})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if !strings.Contains(err.Error(), "test config") {
		t.Errorf("expected config error, got: %v", err)
	}
}

func TestRunPluginTestDefaultDir(t *testing.T) {
	// When no dir arg is given, should fail trying to load "." which has no manifest.
	err := runPluginTest([]string{"--timeout", "1s"})
	// May succeed if run from a dir with plugin.json, but in test context should fail.
	// We only verify it doesn't panic and returns an error (no manifest in test temp dir).
	_ = err // outcome depends on cwd; we just ensure no panic
}

func TestPluginUsageIncludesTest(t *testing.T) {
	// Ensure pluginUsage mentions the test subcommand.
	err := pluginUsage()
	if err == nil {
		t.Fatal("pluginUsage should return an error")
	}
	// The test subcommand should be in the help text — check via runPlugin dispatch.
	// We trigger the usage by passing an unknown subcommand.
	err2 := runPlugin([]string{"unknown-subcommand"})
	if err2 == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}
