package main

import (
	"os"
	"path/filepath"
	"testing"
)

const twoPluginLockfile = `project:
  name: my-project
  version: "1.0.0"
git:
  repository: GoCodeAlone/my-project
plugins:
  authz:
    version: v0.3.1
    repository: GoCodeAlone/workflow-plugin-authz
    sha256: abc123deadbeef
  payments:
    version: v0.1.0
    repository: GoCodeAlone/workflow-plugin-payments
`

func TestLoadPluginLockfile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wfctl.yaml")
	if err := os.WriteFile(path, []byte(twoPluginLockfile), 0600); err != nil {
		t.Fatal(err)
	}

	lf, err := loadPluginLockfile(path)
	if err != nil {
		t.Fatalf("loadPluginLockfile: %v", err)
	}
	if len(lf.Plugins) != 2 {
		t.Fatalf("want 2 plugins, got %d", len(lf.Plugins))
	}

	authz, ok := lf.Plugins["authz"]
	if !ok {
		t.Fatal("expected 'authz' plugin entry")
	}
	if authz.Version != "v0.3.1" {
		t.Errorf("authz.Version = %q, want v0.3.1", authz.Version)
	}
	if authz.Repository != "GoCodeAlone/workflow-plugin-authz" {
		t.Errorf("authz.Repository = %q, want GoCodeAlone/workflow-plugin-authz", authz.Repository)
	}
	if authz.SHA256 != "abc123deadbeef" {
		t.Errorf("authz.SHA256 = %q, want abc123deadbeef", authz.SHA256)
	}

	payments, ok := lf.Plugins["payments"]
	if !ok {
		t.Fatal("expected 'payments' plugin entry")
	}
	if payments.Version != "v0.1.0" {
		t.Errorf("payments.Version = %q, want v0.1.0", payments.Version)
	}
}

func TestLoadPluginLockfile_Missing(t *testing.T) {
	lf, err := loadPluginLockfile("/nonexistent/.wfctl.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if len(lf.Plugins) != 0 {
		t.Errorf("expected empty plugins for missing file, got %v", lf.Plugins)
	}
}

func TestLoadPluginLockfile_NoPluginsSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wfctl.yaml")
	content := "project:\n  name: my-project\ngit:\n  repository: GoCodeAlone/my-project\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	lf, err := loadPluginLockfile(path)
	if err != nil {
		t.Fatalf("loadPluginLockfile: %v", err)
	}
	if len(lf.Plugins) != 0 {
		t.Errorf("expected empty plugins map, got %v", lf.Plugins)
	}
}

func TestPluginLockfile_Save_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".wfctl.yaml")

	// Write initial file with non-plugin sections
	initial := "project:\n  name: my-project\ngit:\n  repository: GoCodeAlone/my-project\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}

	// Load, add plugin, save
	lf, err := loadPluginLockfile(path)
	if err != nil {
		t.Fatalf("loadPluginLockfile: %v", err)
	}
	lf.Plugins["authz"] = PluginLockEntry{
		Version:    "v0.3.1",
		Repository: "GoCodeAlone/workflow-plugin-authz",
	}
	if err := lf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload and verify
	lf2, err := loadPluginLockfile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(lf2.Plugins) != 1 {
		t.Fatalf("want 1 plugin after reload, got %d", len(lf2.Plugins))
	}
	authz := lf2.Plugins["authz"]
	if authz.Version != "v0.3.1" {
		t.Errorf("authz.Version = %q, want v0.3.1", authz.Version)
	}
	// Verify that the non-plugin fields are preserved
	if lf2.raw["project"] == nil {
		t.Error("expected 'project' field to be preserved after save")
	}
	if lf2.raw["git"] == nil {
		t.Error("expected 'git' field to be preserved after save")
	}
}

func TestPluginInstall_FromConfig_NoRequires(t *testing.T) {
	dir := t.TempDir()
	cfg := "modules: []\n"
	cfgPath := writeLockTestFile(t, dir, "workflow.yaml", cfg)
	err := runPluginInstall([]string{"--from-config", cfgPath, "--plugin-dir", dir})
	if err != nil {
		t.Fatalf("--from-config with no requires: %v", err)
	}
}

func TestPluginLock_NoPlugins(t *testing.T) {
	dir := t.TempDir()
	cfg := "modules: []\n"
	cfgPath := writeLockTestFile(t, dir, "workflow.yaml", cfg)
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	err := runPluginLock([]string{"--config", cfgPath, "--lock-file", lockPath})
	if err != nil {
		t.Fatalf("plugin lock with no plugins: %v", err)
	}
	// Empty lockfile is created.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatal("lockfile not created")
	}
}

func TestPluginLock_PinsVersions(t *testing.T) {
	dir := t.TempDir()
	cfg := `requires:
  plugins:
    - name: workflow-plugin-ai
      version: "1.0.0"
`
	cfgPath := writeLockTestFile(t, dir, "workflow.yaml", cfg)
	lockPath := filepath.Join(dir, ".wfctl-lock.yaml")
	err := runPluginLock([]string{"--config", cfgPath, "--lock-file", lockPath})
	if err != nil {
		t.Fatalf("plugin lock: %v", err)
	}
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("lockfile is empty")
	}
}

func writeLockTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
