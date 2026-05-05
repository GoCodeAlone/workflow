package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

const appYAMLWithPlugins = `modules: []
requires:
  plugins:
    - name: workflow-plugin-digitalocean
      version: v0.7.6
      source: github.com/GoCodeAlone/workflow-plugin-digitalocean
    - name: workflow-plugin-supply-chain
      version: v0.3.0
      source: github.com/GoCodeAlone/workflow-plugin-supply-chain
`

func TestMigratePlugins_CreatesManifestAndLockfile(t *testing.T) {
	dir := t.TempDir()
	appYAMLPath := filepath.Join(dir, "workflow.yaml")
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	if err := os.WriteFile(appYAMLPath, []byte(appYAMLWithPlugins), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runMigratePlugins([]string{
		"--config", appYAMLPath,
		"--manifest", manifestPath,
		"--lock-file", lockPath,
	})
	if err != nil {
		t.Fatalf("runMigratePlugins: %v", err)
	}

	// Verify manifest created.
	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.Plugins) != 2 {
		t.Fatalf("want 2 plugins, got %d: %+v", len(m.Plugins), m.Plugins)
	}
	var foundDO, foundSC bool
	for _, p := range m.Plugins {
		switch p.Name {
		case "workflow-plugin-digitalocean":
			foundDO = true
			if p.Version != "v0.7.6" {
				t.Errorf("do version = %q, want v0.7.6", p.Version)
			}
			if p.Source != "github.com/GoCodeAlone/workflow-plugin-digitalocean" {
				t.Errorf("do source = %q", p.Source)
			}
		case "workflow-plugin-supply-chain":
			foundSC = true
			if p.Version != "v0.3.0" {
				t.Errorf("sc version = %q, want v0.3.0", p.Version)
			}
		}
	}
	if !foundDO || !foundSC {
		t.Errorf("missing plugins; foundDO=%v foundSC=%v", foundDO, foundSC)
	}

	// Verify lockfile created.
	lf, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		t.Fatalf("load lockfile: %v", err)
	}
	if _, ok := lf.Plugins["workflow-plugin-digitalocean"]; !ok {
		t.Error("workflow-plugin-digitalocean not in lockfile")
	}
	if _, ok := lf.Plugins["workflow-plugin-supply-chain"]; !ok {
		t.Error("workflow-plugin-supply-chain not in lockfile")
	}
}

func TestMigratePlugins_NoRequires(t *testing.T) {
	dir := t.TempDir()
	appYAMLPath := filepath.Join(dir, "workflow.yaml")
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	content := "modules: []\n"
	if err := os.WriteFile(appYAMLPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	err := runMigratePlugins([]string{
		"--config", appYAMLPath,
		"--manifest", manifestPath,
		"--lock-file", lockPath,
	})
	if err != nil {
		t.Fatalf("runMigratePlugins with no requires: %v", err)
	}
}

func TestMigratePlugins_IdempotentOnSecondRun(t *testing.T) {
	dir := t.TempDir()
	appYAMLPath := filepath.Join(dir, "workflow.yaml")
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	if err := os.WriteFile(appYAMLPath, []byte(appYAMLWithPlugins), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{
		"--config", appYAMLPath,
		"--manifest", manifestPath,
		"--lock-file", lockPath,
	}
	if err := runMigratePlugins(args); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := runMigratePlugins(args); err != nil {
		t.Fatalf("second run: %v", err)
	}

	m, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Plugins) != 2 {
		t.Errorf("want 2 plugins after idempotent run, got %d", len(m.Plugins))
	}
}
