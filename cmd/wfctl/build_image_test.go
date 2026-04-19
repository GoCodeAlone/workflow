package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunBuildImage_DockerfileDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
        platforms:
          - linux/amd64
          - linux/arm64
        push_to:
          - my-registry
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildImage([]string{"--config", cfgPath})
	if err != nil {
		t.Fatalf("dockerfile dry-run: %v", err)
	}
}

func TestRunBuildImage_KoDryRun(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    containers:
      - name: app
        method: ko
        ko_package: ./cmd/server
        push_to:
          - my-registry
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildImage([]string{"--config", cfgPath})
	if err != nil {
		t.Fatalf("ko dry-run: %v", err)
	}
}

func TestRunBuildImage_ExternalSkipsBuild(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    containers:
      - name: base
        external: true
        source:
          image: gcr.io/distroless/static
          tag: latest
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runBuildImage([]string{"--config", cfgPath})
	if err != nil {
		t.Fatalf("external dry-run: %v", err)
	}
}
