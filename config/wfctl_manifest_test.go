package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestWfctlManifest_LoadsPluginEntries(t *testing.T) {
	raw := []byte(`
version: 1
plugins:
  - name: workflow-plugin-digitalocean
    version: v0.7.6
    source: github.com/GoCodeAlone/workflow-plugin-digitalocean
    auth:
      env: GH_TOKEN
  - name: workflow-plugin-supply-chain
    version: v0.3.0
    source: github.com/GoCodeAlone/workflow-plugin-supply-chain
    verify:
      identity: "https://github.com/GoCodeAlone/workflow-plugin-supply-chain/.github/workflows/release.yml@refs/tags/v0.3.0"
`)
	var m WfctlManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Version != 1 {
		t.Errorf("version = %d, want 1", m.Version)
	}
	if len(m.Plugins) != 2 {
		t.Fatalf("plugins count = %d, want 2", len(m.Plugins))
	}
	p := m.Plugins[0]
	if p.Name != "workflow-plugin-digitalocean" || p.Version != "v0.7.6" {
		t.Errorf("plugin[0] = %+v", p)
	}
	if p.Auth == nil || p.Auth.Env != "GH_TOKEN" {
		t.Errorf("plugin[0].auth = %+v", p.Auth)
	}
	p2 := m.Plugins[1]
	if p2.Verify == nil || p2.Verify.Identity == "" {
		t.Errorf("plugin[1].verify = %+v", p2.Verify)
	}
}

func TestWfctlManifest_EmptyPluginsList(t *testing.T) {
	raw := []byte("version: 1\nplugins: []\n")
	var m WfctlManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Plugins) != 0 {
		t.Errorf("want 0 plugins, got %d", len(m.Plugins))
	}
}

func TestWfctlManifest_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wfctl.yaml")
	content := []byte("version: 1\nplugins:\n  - name: foo\n    version: v1.0.0\n    source: github.com/foo/bar\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	m, err := LoadWfctlManifest(path)
	if err != nil {
		t.Fatalf("LoadWfctlManifest: %v", err)
	}
	if len(m.Plugins) != 1 || m.Plugins[0].Name != "foo" {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestWfctlManifest_LoadFromFile_NotFound(t *testing.T) {
	_, err := LoadWfctlManifest("/nonexistent/wfctl.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
