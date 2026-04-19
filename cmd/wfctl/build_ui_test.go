package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildUIFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  build:
    targets:
      - name: frontend
        type: nodejs
        path: ./ui
        config:
          script: build
          node_version: "20"
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestRunBuildUIPlugin_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildUIFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildUIPlugin([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runBuildUIPlugin dry-run: %v", err)
	}
}

func TestRunBuildUIPlugin_TargetFlag(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: frontend
        type: nodejs
        path: ./ui
        config:
          script: build
      - name: admin
        type: nodejs
        path: ./admin
        config:
          script: build
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildUIPlugin([]string{"--config", cfgPath, "--target", "frontend"}); err != nil {
		t.Fatalf("runBuildUIPlugin --target: %v", err)
	}
}

func TestRunBuildUIPlugin_UnknownTarget(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildUIFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildUIPlugin([]string{"--config", cfgPath, "--target", "missing"})
	if err == nil {
		t.Fatal("want error for unknown target")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention target name, got: %v", err)
	}
}

func TestRunBuildUIPlugin_NoNodejsTargets(t *testing.T) {
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
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildUIPlugin([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("no nodejs targets should be a no-op: %v", err)
	}
}
