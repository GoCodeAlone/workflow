package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewEmptyWorkflowConfig(t *testing.T) {
	cfg := NewEmptyWorkflowConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Modules) != 0 {
		t.Errorf("expected empty modules, got %d", len(cfg.Modules))
	}
	if cfg.Workflows == nil {
		t.Error("expected non-nil workflows map")
	}
	if cfg.Triggers == nil {
		t.Error("expected non-nil triggers map")
	}
}

func TestLoadFromFile_ValidYAML(t *testing.T) {
	content := `
modules:
  - name: my-server
    type: http.server
    config:
      port: 8080
  - name: my-router
    type: http.router
    dependsOn:
      - my-server
workflows:
  order-flow:
    initial: new
triggers:
  http-trigger:
    type: http
`
	dir := t.TempDir()
	fp := filepath.Join(dir, "test-config.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadFromFile(fp)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if len(cfg.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "my-server" {
		t.Errorf("expected module name 'my-server', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[0].Type != "http.server" {
		t.Errorf("expected module type 'http.server', got %q", cfg.Modules[0].Type)
	}
	if cfg.Modules[1].DependsOn[0] != "my-server" {
		t.Errorf("expected dependsOn 'my-server', got %q", cfg.Modules[1].DependsOn[0])
	}
	if cfg.Workflows["order-flow"] == nil {
		t.Error("expected order-flow workflow")
	}
	if cfg.Triggers["http-trigger"] == nil {
		t.Error("expected http-trigger trigger")
	}
}

func TestLoadFromFile_NonExistent(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/file.yaml")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestLoadFromFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(fp, []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadFromFile(fp)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadFromFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "empty.yaml")
	if err := os.WriteFile(fp, []byte(""), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadFromFile(fp)
	if err != nil {
		t.Fatalf("LoadFromFile failed on empty file: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config from empty file")
	}
}

func TestLoadFromFile_ModuleConfig(t *testing.T) {
	content := `
modules:
  - name: api-handler
    type: api.handler
    config:
      resource: orders
      basePath: /api/v1
`
	dir := t.TempDir()
	fp := filepath.Join(dir, "modules.yaml")
	if err := os.WriteFile(fp, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	cfg, err := LoadFromFile(fp)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	mod := cfg.Modules[0]
	if mod.Config["resource"] != "orders" {
		t.Errorf("expected config resource 'orders', got %v", mod.Config["resource"])
	}
	if mod.Config["basePath"] != "/api/v1" {
		t.Errorf("expected config basePath '/api/v1', got %v", mod.Config["basePath"])
	}
}

func TestExternalPluginDeclParsing(t *testing.T) {
	yaml := `
modules: []
workflows: {}
triggers: {}
plugins:
  external:
    - name: my-plugin
      autoFetch: true
      version: ">=0.1.0"
    - name: pinned-plugin
      autoFetch: false
      version: "0.2.0"
    - name: no-version-plugin
      autoFetch: true
`
	cfg, err := LoadFromString(yaml)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}
	if cfg.Plugins == nil {
		t.Fatal("expected Plugins section to be non-nil")
	}
	if len(cfg.Plugins.External) != 3 {
		t.Fatalf("expected 3 external plugin decls, got %d", len(cfg.Plugins.External))
	}

	// First plugin: autoFetch=true with version constraint
	p0 := cfg.Plugins.External[0]
	if p0.Name != "my-plugin" {
		t.Errorf("plugins[0].name = %q, want %q", p0.Name, "my-plugin")
	}
	if !p0.AutoFetch {
		t.Errorf("plugins[0].autoFetch = false, want true")
	}
	if p0.Version != ">=0.1.0" {
		t.Errorf("plugins[0].version = %q, want %q", p0.Version, ">=0.1.0")
	}

	// Second plugin: autoFetch=false with exact version
	p1 := cfg.Plugins.External[1]
	if p1.Name != "pinned-plugin" {
		t.Errorf("plugins[1].name = %q, want %q", p1.Name, "pinned-plugin")
	}
	if p1.AutoFetch {
		t.Errorf("plugins[1].autoFetch = true, want false")
	}
	if p1.Version != "0.2.0" {
		t.Errorf("plugins[1].version = %q, want %q", p1.Version, "0.2.0")
	}

	// Third plugin: no version specified
	p2 := cfg.Plugins.External[2]
	if p2.Name != "no-version-plugin" {
		t.Errorf("plugins[2].name = %q, want %q", p2.Name, "no-version-plugin")
	}
	if !p2.AutoFetch {
		t.Errorf("plugins[2].autoFetch = false, want true")
	}
	if p2.Version != "" {
		t.Errorf("plugins[2].version = %q, want empty", p2.Version)
	}
}

func TestExternalPluginDeclParsing_NoPluginsSection(t *testing.T) {
	yaml := `
modules: []
workflows: {}
triggers: {}
`
	cfg, err := LoadFromString(yaml)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}
	if cfg.Plugins != nil {
		t.Errorf("expected Plugins to be nil when not declared, got %+v", cfg.Plugins)
	}
}
