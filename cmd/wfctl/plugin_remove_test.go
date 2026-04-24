package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestPluginRemove_RemovesFromManifestAndLockfile(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	pluginDir := filepath.Join(dir, "plugins")

	// Create manifest with two plugins.
	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
  - name: workflow-plugin-bar
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-bar
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create lockfile with both plugins.
	lf := &config.WfctlLockfile{
		Version: 1,
		Plugins: map[string]config.WfctlLockPluginEntry{
			"workflow-plugin-foo": {Version: "v1.0.0", Source: "github.com/GoCodeAlone/workflow-plugin-foo"},
			"workflow-plugin-bar": {Version: "v2.0.0", Source: "github.com/GoCodeAlone/workflow-plugin-bar"},
		},
	}
	if err := config.SaveWfctlLockfile(lockPath, lf); err != nil {
		t.Fatal(err)
	}

	// Create a fake installed plugin binary dir.
	fooDir := filepath.Join(pluginDir, "workflow-plugin-foo")
	if err := os.MkdirAll(fooDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := removeFromManifestAndLockfile("workflow-plugin-foo", manifestPath, lockPath)
	if err != nil {
		t.Fatalf("removeFromManifestAndLockfile: %v", err)
	}

	// Verify manifest no longer has workflow-plugin-foo.
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-foo" {
			t.Errorf("workflow-plugin-foo still in manifest after remove")
		}
	}
	if len(m.Plugins) != 1 || m.Plugins[0].Name != "workflow-plugin-bar" {
		t.Errorf("unexpected manifest: %+v", m.Plugins)
	}

	// Verify lockfile no longer has workflow-plugin-foo.
	loaded, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if _, ok := loaded.Plugins["workflow-plugin-foo"]; ok {
		t.Error("workflow-plugin-foo still in lockfile after remove")
	}
	if _, ok := loaded.Plugins["workflow-plugin-bar"]; !ok {
		t.Error("workflow-plugin-bar should remain in lockfile")
	}
}

func TestPluginRemove_NoManifestNoError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	// No manifest or lockfile — should succeed silently.
	err := removeFromManifestAndLockfile("workflow-plugin-foo", manifestPath, lockPath)
	if err != nil {
		t.Fatalf("expected no error when manifest absent, got: %v", err)
	}
}
