package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testWorkflowYAML = `
modules:
  - name: test-server
    type: http.server
    config:
      port: 8080
  - name: test-router
    type: http.router
    dependsOn:
      - test-server
workflows:
  test-flow:
    initial: start
triggers:
  test-trigger:
    type: http
`

func TestFileSource_Load(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(fp, []byte(testWorkflowYAML), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	src := NewFileSource(fp)
	cfg, err := src.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cfg.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(cfg.Modules))
	}
	if cfg.Modules[0].Name != "test-server" {
		t.Errorf("expected module name 'test-server', got %q", cfg.Modules[0].Name)
	}
	if cfg.Modules[0].Type != "http.server" {
		t.Errorf("expected module type 'http.server', got %q", cfg.Modules[0].Type)
	}
	if cfg.Modules[1].Name != "test-router" {
		t.Errorf("expected module name 'test-router', got %q", cfg.Modules[1].Name)
	}
	if cfg.Workflows["test-flow"] == nil {
		t.Error("expected 'test-flow' in workflows")
	}
	if cfg.Triggers["test-trigger"] == nil {
		t.Error("expected 'test-trigger' in triggers")
	}
}

func TestFileSource_Load_NonExistent(t *testing.T) {
	src := NewFileSource("/nonexistent/path/config.yaml")
	_, err := src.Load(context.Background())
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestFileSource_Hash(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(fp, []byte(testWorkflowYAML), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	src := NewFileSource(fp)
	ctx := context.Background()

	h1, err := src.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() error: %v", err)
	}
	if h1 == "" {
		t.Fatal("expected non-empty hash")
	}

	// Same file produces same hash.
	h2, err := src.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() second call error: %v", err)
	}
	if h1 != h2 {
		t.Errorf("expected identical hashes for same content, got %q and %q", h1, h2)
	}

	// Different content produces different hash.
	fp2 := filepath.Join(dir, "config2.yaml")
	if err := os.WriteFile(fp2, []byte(testWorkflowYAML+"\n# extra comment\n"), 0644); err != nil {
		t.Fatalf("write second temp file: %v", err)
	}
	src2 := NewFileSource(fp2)
	h3, err := src2.Hash(ctx)
	if err != nil {
		t.Fatalf("Hash() for second file error: %v", err)
	}
	if h1 == h3 {
		t.Errorf("expected different hashes for different content")
	}
}

func TestFileSource_Hash_NonExistent(t *testing.T) {
	src := NewFileSource("/nonexistent/path/config.yaml")
	_, err := src.Hash(context.Background())
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestFileSource_Name(t *testing.T) {
	path := "/some/path/to/config.yaml"
	src := NewFileSource(path)
	name := src.Name()
	if !strings.HasPrefix(name, "file:") {
		t.Errorf("expected name to start with 'file:', got %q", name)
	}
	if !strings.Contains(name, path) {
		t.Errorf("expected name to contain path %q, got %q", path, name)
	}
}

func TestFileSource_Path(t *testing.T) {
	path := "/some/path/to/config.yaml"
	src := NewFileSource(path)
	if src.Path() != path {
		t.Errorf("expected path %q, got %q", path, src.Path())
	}
}
