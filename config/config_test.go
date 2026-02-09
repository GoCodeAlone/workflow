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
