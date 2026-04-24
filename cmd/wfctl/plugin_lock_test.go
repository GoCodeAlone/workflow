package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPluginLock_FromManifest(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Version string `yaml:"version"`
			Source  string `yaml:"source"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}
	entry, ok := parsed.Plugins["workflow-plugin-foo"]
	if !ok {
		t.Fatalf("plugin 'workflow-plugin-foo' not found in lockfile; got: %v", parsed.Plugins)
	}
	if entry.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", entry.Version)
	}
	if entry.Source != "github.com/GoCodeAlone/workflow-plugin-foo" {
		t.Errorf("source = %q, want github.com/GoCodeAlone/workflow-plugin-foo", entry.Source)
	}
}

func TestPluginLock_FromManifest_PreservesExisting(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "wfctl.yaml")
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")

	// Pre-populate lockfile with sha256 for an existing plugin.
	existingLock := `version: 1
generated_at: "2026-01-01T00:00:00Z"
plugins:
  workflow-plugin-foo:
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
    sha256: existing-sha256
`
	if err := os.WriteFile(lockPath, []byte(existingLock), 0o600); err != nil {
		t.Fatalf("write existing lockfile: %v", err)
	}

	manifest := `version: 1
plugins:
  - name: workflow-plugin-foo
    version: v1.2.3
    source: github.com/GoCodeAlone/workflow-plugin-foo
  - name: workflow-plugin-bar
    version: v2.0.0
    source: github.com/GoCodeAlone/workflow-plugin-bar
`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	if err := runPluginLockFromManifest(manifestPath, lockPath); err != nil {
		t.Fatalf("runPluginLockFromManifest: %v", err)
	}

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}

	var parsed struct {
		Plugins map[string]struct {
			Version string `yaml:"version"`
			SHA256  string `yaml:"sha256"`
		} `yaml:"plugins"`
	}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parse lockfile: %v", err)
	}

	foo := parsed.Plugins["workflow-plugin-foo"]
	if foo.SHA256 != "existing-sha256" {
		t.Errorf("existing sha256 not preserved: got %q, want existing-sha256", foo.SHA256)
	}
	if _, ok := parsed.Plugins["workflow-plugin-bar"]; !ok {
		t.Error("new plugin workflow-plugin-bar not added")
	}
}
