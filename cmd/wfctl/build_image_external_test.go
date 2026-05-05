package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExternalContainerFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
  build:
    containers:
      - name: base-image
        external: true
        push_to:
          - docr
        source:
          ref: registry.digitalocean.com/myorg/base
          tag_from:
            - env: BASE_IMAGE_TAG
            - command: "echo fallback-tag"
      - name: api
        method: dockerfile
        dockerfile: Dockerfile.api
        push_to:
          - docr
        tag: v1.0.0
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestBuildImage_ExternalTagFromEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeExternalContainerFixture(t, dir)
	t.Setenv("BASE_IMAGE_TAG", "v2.3.4")
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("runBuildImage: %v", err)
	}

	out := buf.String()
	// External container should show the env-var tag.
	if !strings.Contains(out, "v2.3.4") {
		t.Errorf("expected BASE_IMAGE_TAG=v2.3.4 in output, got: %q", out)
	}
	// External container's line should NOT be a docker build.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "base-image") && strings.Contains(line, "docker build") {
			t.Errorf("external container should not trigger docker build, got line: %q", line)
		}
	}
}

func TestBuildImage_ExternalSkipsBuild_BuiltGoesThrough(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeExternalContainerFixture(t, dir)
	t.Setenv("BASE_IMAGE_TAG", "sha256:abc123")
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("runBuildImage: %v", err)
	}

	out := buf.String()
	// Built container (api) should trigger docker build.
	if !strings.Contains(out, "docker build") {
		t.Errorf("built container should trigger docker build, got: %q", out)
	}
	// Built image should reference Dockerfile.api.
	if !strings.Contains(out, "Dockerfile.api") {
		t.Errorf("built container should reference Dockerfile.api, got: %q", out)
	}
}

func TestBuildImage_ExternalTagFallback(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
  build:
    containers:
      - name: sidecar
        external: true
        tag: stable
        push_to:
          - docr
        source:
          ref: registry.digitalocean.com/myorg/sidecar
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")

	var buf bytes.Buffer
	if err := runBuildImageWithOutput([]string{"--config", cfgPath}, &buf); err != nil {
		t.Fatalf("runBuildImage: %v", err)
	}

	out := buf.String()
	// No TagFrom, falls back to ctr.Tag.
	if !strings.Contains(out, "stable") {
		t.Errorf("expected fallback tag 'stable' in output, got: %q", out)
	}
}
