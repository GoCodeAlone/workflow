package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestPluginAdd_NewPlugin(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	// Start with an existing manifest.
	existing := `version: 1
plugins:
  - name: workflow-plugin-existing
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-existing
`
	if err := os.WriteFile(manifestPath, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPluginAdd([]string{
		"--manifest", manifestPath,
		"--lock-file", lockPath,
		"workflow-plugin-foo@v1.2.3",
	})
	if err != nil {
		t.Fatalf("runPluginAdd: %v", err)
	}

	// Verify manifest updated.
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Plugins) != 2 {
		t.Fatalf("want 2 plugins, got %d", len(m.Plugins))
	}
	var found bool
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-foo" && p.Version == "v1.2.3" {
			found = true
		}
	}
	if !found {
		t.Errorf("workflow-plugin-foo@v1.2.3 not found in manifest: %+v", m.Plugins)
	}

	// Verify lockfile created.
	lf, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if _, ok := lf.Plugins["workflow-plugin-foo"]; !ok {
		t.Errorf("workflow-plugin-foo not in lockfile; entries: %v", lf.Plugins)
	}
}

func TestPluginAdd_NoVersion(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	err := runPluginAdd([]string{
		"--manifest", manifestPath,
		"--lock-file", lockPath,
		"workflow-plugin-noversion",
	})
	if err != nil {
		t.Fatalf("runPluginAdd without version: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Plugins) != 1 || m.Plugins[0].Name != "workflow-plugin-noversion" {
		t.Errorf("unexpected manifest: %+v", m.Plugins)
	}
}

func TestPluginAdd_DuplicateReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	initial := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runPluginAdd([]string{
		"--manifest", manifestPath,
		"--lock-file", lockPath,
		"workflow-plugin-foo@v2.0.0",
	})
	if err == nil {
		t.Fatal("expected error for duplicate plugin")
	}
}

func TestPluginAdd_MissingNameReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	err := runPluginAdd([]string{
		"--manifest", manifestPath,
		"--lock-file", lockPath,
	})
	if err == nil {
		t.Fatal("expected error when no plugin name provided")
	}
}
