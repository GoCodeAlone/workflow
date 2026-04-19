package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func writeDevBuildFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
        config:
          ldflags: "-s -w"
      - name: worker
        type: go
        path: ./cmd/worker

environments:
  local:
    build:
      targets:
        - name: server
          type: go
          path: ./cmd/server
          config:
            ldflags: ""
            race: true
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestResolveBuildForEnv_MergesLocalOverrides(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDevBuildFixture(t, dir)

	cfg, err := config.LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	resolved := resolveBuildForEnv(cfg, "local")
	if resolved == nil {
		t.Fatal("resolved build config should not be nil")
	}

	// The local env overrides the server target's ldflags.
	var serverTarget *config.CITarget
	for i := range resolved.Targets {
		if resolved.Targets[i].Name == "server" {
			serverTarget = &resolved.Targets[i]
		}
	}
	if serverTarget == nil {
		t.Fatal("server target missing from resolved config")
	}
	// Local env should set race=true for server.
	if race, ok := serverTarget.Config["race"].(bool); !ok || !race {
		t.Errorf("expected server.config.race=true from local override, got config=%v", serverTarget.Config)
	}
}

func TestResolveBuildForEnv_NoOverride_ReturnsBase(t *testing.T) {
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
		t.Fatal("should return base config when no local env override")
	}
	if len(resolved.Targets) != 1 || resolved.Targets[0].Name != "server" {
		t.Errorf("expected 1 server target, got %v", resolved.Targets)
	}
}

func TestResolveBuildForEnv_NilCI_ReturnsNil(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	resolved := resolveBuildForEnv(cfg, "local")
	if resolved != nil {
		t.Errorf("nil CI should return nil resolved build, got %+v", resolved)
	}
}

func TestRunDevBuild_DryRun(t *testing.T) {
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
      targets:
        - name: server
          type: go
          path: ./cmd/server
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runDevBuild(cfgPath, "local"); err != nil {
		t.Fatalf("runDevBuild dry-run: %v", err)
	}
}
