package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeWorkflowWithPlugins writes a workflow.yaml with requires.plugins entries.
func writeWorkflowWithPlugins(t *testing.T, dir string, plugins []struct{ name, version string }) string {
	t.Helper()
	content := "requires:\n  plugins:\n"
	for _, p := range plugins {
		if p.version != "" {
			content += "    - name: " + p.name + "\n      version: " + p.version + "\n"
		} else {
			content += "    - name: " + p.name + "\n"
		}
	}
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write workflow.yaml: %v", err)
	}
	return cfgPath
}

// fakeInstalledPlugin creates a fake installed plugin directory with plugin.json.
func fakeInstalledPlugin(t *testing.T, pluginDir, name, version string) {
	t.Helper()
	dir := filepath.Join(pluginDir, name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"name":"` + name + `","version":"` + version + `"}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
}

func TestInstallFromConfig_SkipsAlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	// Pre-install "myplugin" so it should be skipped.
	fakeInstalledPlugin(t, pluginDir, "myplugin", "1.0.0")

	cfgPath := writeWorkflowWithPlugins(t, dir, []struct{ name, version string }{
		{"myplugin", "1.0.0"},
	})

	if err := installFromWorkflowConfig(cfgPath, pluginDir, ""); err != nil {
		t.Fatalf("installFromWorkflowConfig: %v", err)
	}
}

func TestInstallFromConfig_NoPlugins(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	content := "name: my-workflow\n"
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	if err := installFromWorkflowConfig(cfgPath, pluginDir, ""); err != nil {
		t.Fatalf("empty requires should be a no-op: %v", err)
	}
}

func TestInstallFromConfig_FlagWired(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	// Pre-install both plugins so no network calls needed.
	fakeInstalledPlugin(t, pluginDir, "auth", "2.0.0")
	fakeInstalledPlugin(t, pluginDir, "logger", "1.5.0")

	cfgPath := writeWorkflowWithPlugins(t, dir, []struct{ name, version string }{
		{"auth", ""},
		{"logger", "1.5.0"},
	})

	// Test via the CLI flag interface.
	if err := runPluginInstall([]string{
		"--from-config", cfgPath,
		"--plugin-dir", pluginDir,
	}); err != nil {
		t.Fatalf("runPluginInstall --from-config: %v", err)
	}
}

func TestInstallFromConfig_MissingConfigFile(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	err := installFromWorkflowConfig(filepath.Join(dir, "nonexistent.yaml"), pluginDir, "")
	if err == nil {
		t.Fatal("want error for missing config file")
	}
}
