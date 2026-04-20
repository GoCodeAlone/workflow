package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
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

// TestImageRefForContainer_RegistryNameResolvesToPath verifies that imageRefForContainer
// resolves a registry name to its path via ci.registries.
func TestImageRefForContainer_RegistryNameResolvesToPath(t *testing.T) {
	ctr := config.CIContainerTarget{Name: "buymywishlist", PushTo: []string{"docr"}}
	registries := []config.CIRegistry{
		{Name: "docr", Path: "registry.digitalocean.com/bmw-registry"},
	}
	got := imageRefForContainer(ctr, "abc123", registries)
	want := "registry.digitalocean.com/bmw-registry/buymywishlist:abc123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestImageRefForContainer_NoMatchFallback verifies fallback when no registry matches.
func TestImageRefForContainer_NoMatchFallback(t *testing.T) {
	ctr := config.CIContainerTarget{Name: "app", PushTo: []string{"unknown-reg"}}
	registries := []config.CIRegistry{
		{Name: "docr", Path: "registry.digitalocean.com/bmw-registry"},
	}
	got := imageRefForContainer(ctr, "latest", registries)
	want := "unknown-reg/app:latest"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestImageRefForContainer_EmptyPushTo verifies fallback when push_to is empty.
func TestImageRefForContainer_EmptyPushTo(t *testing.T) {
	ctr := config.CIContainerTarget{Name: "myapp"}
	got := imageRefForContainer(ctr, "v1.0", nil)
	want := "myapp:v1.0"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestImageRefForContainer_MultiplePushTo verifies first resolvable path is used.
func TestImageRefForContainer_MultiplePushTo(t *testing.T) {
	ctr := config.CIContainerTarget{Name: "app", PushTo: []string{"docr", "ghcr"}}
	registries := []config.CIRegistry{
		{Name: "docr", Path: "registry.digitalocean.com/bmw-registry"},
		{Name: "ghcr", Path: "ghcr.io/gocodalone"},
	}
	got := imageRefForContainer(ctr, "sha256abc", registries)
	want := "registry.digitalocean.com/bmw-registry/app:sha256abc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRunBuildImage_ImageRefUsesRegistryPath verifies the dry-run output shows registry path.
func TestRunBuildImage_ImageRefUsesRegistryPath(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/bmw-registry
  build:
    containers:
      - name: buymywishlist
        method: dockerfile
        dockerfile: Dockerfile
        push_to:
          - docr
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "registry.digitalocean.com/bmw-registry/buymywishlist") {
		t.Errorf("expected registry path in dry-run output, got: %q", out)
	}
	if strings.Contains(out, "docr/buymywishlist") {
		t.Errorf("expected no registry name prefix in dry-run output, got: %q", out)
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
