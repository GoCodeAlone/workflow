package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestUpdateManifestVersion_UpdatesVersion(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

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

	if err := updateManifestVersion("workflow-plugin-foo", "v1.5.0", manifestPath, lockPath); err != nil {
		t.Fatalf("updateManifestVersion: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-foo" {
			if p.Version != "v1.5.0" {
				t.Errorf("version = %q, want v1.5.0", p.Version)
			}
			return
		}
	}
	t.Error("workflow-plugin-foo not found in manifest")
}

func TestUpdateManifestVersion_OtherPluginsUnchanged(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

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

	if err := updateManifestVersion("workflow-plugin-foo", "v1.5.0", manifestPath, lockPath); err != nil {
		t.Fatalf("updateManifestVersion: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, p := range m.Plugins {
		if p.Name == "workflow-plugin-bar" && p.Version != "v2.0.0" {
			t.Errorf("workflow-plugin-bar version changed unexpectedly: %q", p.Version)
		}
	}
}

func TestUpdateManifestVersion_NotInManifestReturnsError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.0.0
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	err := updateManifestVersion("workflow-plugin-nonexistent", "v1.5.0", manifestPath, lockPath)
	if err == nil {
		t.Fatal("expected error when plugin not in manifest")
	}
}
