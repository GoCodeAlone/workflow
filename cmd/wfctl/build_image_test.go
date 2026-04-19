package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// TestRunBuildImage_HardenedProvenanceArgs verifies that --provenance and --sbom flags
// are appended to the docker build command when ci.build.security.hardened=true (T33).
func TestRunBuildImage_HardenedProvenanceArgs(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	t.Setenv("DOCKER_BUILDKIT", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("hardened dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "--provenance=mode=max") {
		t.Errorf("expected --provenance=mode=max in dry-run output, got: %q", out)
	}
	if !strings.Contains(out, "--sbom=true") {
		t.Errorf("expected --sbom=true in dry-run output, got: %q", out)
	}
}

// TestRunBuildImage_HardenedBuildKitWarning verifies the DOCKER_BUILDKIT warning is only
// emitted in dry-run mode (in live mode the env is forced via cmd.Env).
func TestRunBuildImage_HardenedBuildKitWarning(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	// Ensure DOCKER_BUILDKIT is unset to trigger the warning path.
	t.Setenv("DOCKER_BUILDKIT", "")

	// Dry-run: warning should appear.
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	var dryBuf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &dryBuf); err != nil {
		t.Fatalf("hardened dry-run: %v", err)
	}
	if !strings.Contains(dryBuf.String(), "DOCKER_BUILDKIT") {
		t.Errorf("expected DOCKER_BUILDKIT warning in dry-run output, got: %q", dryBuf.String())
	}
}

// TestRunBuildImage_NotHardenedNoProvenanceArgs verifies that provenance flags are
// NOT added when hardened=false (T33).
func TestRunBuildImage_NotHardenedNoProvenanceArgs(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    security:
      hardened: false
    containers:
      - name: app
        method: dockerfile
        dockerfile: Dockerfile
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("non-hardened dry-run: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "--provenance") {
		t.Errorf("expected no --provenance flag when hardened=false, got: %q", out)
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
