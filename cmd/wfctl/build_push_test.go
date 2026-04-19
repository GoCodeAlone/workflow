package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildPushFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
    - name: ghcr
      type: github
      path: ghcr.io/myorg
  build:
    containers:
      - name: api
        method: dockerfile
        dockerfile: Dockerfile
        tag: latest
        push_to:
          - docr
          - ghcr
      - name: worker
        method: dockerfile
        dockerfile: Dockerfile.worker
        tag: latest
        push_to:
          - docr
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestRunBuildPush_DryRun_PrintsPlannedPushes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeBuildPushFixture(t, dir)

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildPush([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("runBuildPush dry-run: %v", err)
	}
}

func TestRunBuildPush_UnknownRegistry(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
  build:
    containers:
      - name: api
        method: dockerfile
        dockerfile: Dockerfile
        tag: latest
        push_to:
          - unknown-registry
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildPush([]string{"--config", cfgPath})
	if err == nil {
		t.Fatal("want error for unknown registry reference")
	}
	if !strings.Contains(err.Error(), "unknown-registry") {
		t.Errorf("error should mention unknown registry, got: %v", err)
	}
}

func TestRunBuildPush_NoContainers(t *testing.T) {
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
	if err := runBuildPush([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("no containers should be a no-op: %v", err)
	}
}

func TestRunBuildPush_ExternalContainerSkipped(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
  build:
    containers:
      - name: base
        external: true
        push_to:
          - docr
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	if err := runBuildPush([]string{"--config", cfgPath}); err != nil {
		t.Fatalf("external containers should be skipped (no push): %v", err)
	}
}
