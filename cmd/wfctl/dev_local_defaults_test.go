package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// T38: local env should default to hardened=false, sbom=false for fast iteration.
func TestResolveBuildForEnv_LocalSkipsHardening(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	resolved := resolveBuildForEnv(cfg, "local")
	if resolved == nil {
		t.Fatal("resolved should not be nil")
	}
	if resolved.Security == nil {
		t.Fatal("resolved.Security should be set for local env")
	}
	if resolved.Security.Hardened {
		t.Error("local env: Hardened should be false for fast iteration")
	}
	if resolved.Security.SBOM {
		t.Error("local env: SBOM should be false for fast iteration")
	}
}

// T38: explicit security in local env override should be respected.
func TestResolveBuildForEnv_LocalExplicitSecurityPreserved(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server

environments:
  local:
    build:
      security:
        hardened: true
        sbom: true
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	resolved := resolveBuildForEnv(cfg, "local")
	if resolved == nil || resolved.Security == nil {
		t.Fatal("resolved security should be set")
	}
	if !resolved.Security.Hardened {
		t.Error("explicit hardened=true should be preserved in local env")
	}
}

// T38: non-local envs should keep hardened defaults.
func TestResolveBuildForEnv_NonLocalKeepsHardening(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	resolved := resolveBuildForEnv(cfg, "staging")
	if resolved == nil || resolved.Security == nil {
		t.Fatal("resolved security should be set for staging")
	}
	// Staging should keep hardened=true (from ApplyDefaults at load time).
	if !resolved.Security.Hardened {
		t.Error("non-local env should preserve hardened=true defaults")
	}
}

// T39: local env containers should get local cache injected.
func TestResolveBuildForEnv_LocalCacheInjected(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    containers:
      - name: api
        method: dockerfile
        dockerfile: Dockerfile
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	resolved := resolveBuildForEnv(cfg, "local")
	if resolved == nil || len(resolved.Containers) == 0 {
		t.Fatal("resolved should have containers")
	}
	api := resolved.Containers[0]
	if api.Cache == nil {
		t.Fatal("local env should inject cache into containers")
	}
	if len(api.Cache.From) == 0 {
		t.Fatal("local cache.from should be non-empty")
	}
	if api.Cache.From[0].Type != "local" {
		t.Errorf("local cache type should be 'local', got %q", api.Cache.From[0].Type)
	}
}
