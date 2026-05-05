package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildCustomFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  build:
    targets:
      - name: codegen
        type: custom
        config:
          command: "make generate"
          outputs:
            - ./gen/types.go
          timeout: 60s
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestRunBuildCustom_DryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildCustomFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildCustom([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runBuildCustom dry-run: %v", err)
	}
}

func TestRunBuildCustom_TargetFlag(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  build:
    targets:
      - name: codegen
        type: custom
        config:
          command: "make gen"
      - name: docs
        type: custom
        config:
          command: "make docs"
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildCustom([]string{"--config", cfgPath, "--target", "codegen"}); err != nil {
		t.Fatalf("runBuildCustom --target: %v", err)
	}
}

func TestRunBuildCustom_UnknownTarget(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildCustomFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildCustom([]string{"--config", cfgPath, "--target", "nope"})
	if err == nil {
		t.Fatal("want error for unknown target")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should mention target name, got: %v", err)
	}
}

func TestRunBuildCustom_NoCustomTargets(t *testing.T) {
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
	if err := runBuildCustom([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("no custom targets should be a no-op: %v", err)
	}
}
