package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCITarget_NewTargetsSyntax(t *testing.T) {
	src := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
        config:
          ldflags: "-X main.version=1.0"
      - name: ui
        type: nodejs
        path: ./ui
      - name: custom-tool
        type: custom
        config:
          command: make build
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil || len(cfg.CI.Build.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %v", cfg.CI)
	}
	if cfg.CI.Build.Targets[0].Type != "go" {
		t.Errorf("want type=go, got %q", cfg.CI.Build.Targets[0].Type)
	}
	if cfg.CI.Build.Targets[1].Type != "nodejs" {
		t.Errorf("want type=nodejs, got %q", cfg.CI.Build.Targets[1].Type)
	}
}

func TestCITarget_LegacyBinariesCoerced(t *testing.T) {
	src := `
ci:
  build:
    binaries:
      - name: server
        path: ./cmd/server
        ldflags: "-X main.version=1.0"
      - name: wfctl
        path: ./cmd/wfctl
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil || len(cfg.CI.Build.Targets) != 2 {
		t.Fatalf("expected binaries coerced to 2 targets, got %v", cfg.CI)
	}
	srv := cfg.CI.Build.Targets[0]
	if srv.Type != "go" {
		t.Errorf("want coerced type=go, got %q", srv.Type)
	}
	if srv.Name != "server" {
		t.Errorf("want name=server, got %q", srv.Name)
	}
	if srv.Path != "./cmd/server" {
		t.Errorf("want path=./cmd/server, got %q", srv.Path)
	}
}

func TestCITarget_WithEnvironmentOverrides(t *testing.T) {
	src := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
        environments:
          staging:
            config:
              ldflags: "-X main.env=staging"
          prod:
            config:
              ldflags: "-X main.env=prod"
`
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	target := cfg.CI.Build.Targets[0]
	if len(target.Environments) != 2 {
		t.Errorf("want 2 env overrides, got %d", len(target.Environments))
	}
}
