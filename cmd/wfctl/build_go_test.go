package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildGoFixture(t *testing.T, dir string) string {
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
          os: linux
          arch: amd64
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestRunBuildGo_DryRun_PrintsPlan(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildGoFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildGo([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runBuildGo dry-run: %v", err)
	}
}

func TestRunBuildGo_TargetFlag_SelectsOne(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
      - name: worker
        type: go
        path: ./cmd/worker
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildGo([]string{"--config", cfgPath, "--target", "server"}); err != nil {
		t.Fatalf("runBuildGo --target: %v", err)
	}
}

func TestRunBuildGo_TargetFlag_UnknownTarget(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildGoFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildGo([]string{"--config", cfgPath, "--target", "nonexistent"})
	if err == nil {
		t.Fatal("want error for unknown target")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention target name, got: %v", err)
	}
}

func TestRunBuildGo_NoGoTargets(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: frontend
        type: nodejs
        path: ./ui
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildGo([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("no go targets should succeed with no-op: %v", err)
	}
}
