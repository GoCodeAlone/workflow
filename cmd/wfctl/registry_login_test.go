package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRegistryLoginFixture(t *testing.T, dir string) string {
	t.Helper()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
      auth:
        env: DIGITALOCEAN_TOKEN
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return cfgPath
}

func TestRunRegistryLogin_DryRun_DO(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRegistryLoginFixture(t, dir)
	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")

	if err := runRegistryLogin([]string{"--config", cfgPath, "--dry-run"}); err != nil {
		t.Fatalf("runRegistryLogin --dry-run: %v", err)
	}
}

func TestRunRegistryLogin_RegistryFlag(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  registries:
    - name: docr
      type: do
      path: registry.digitalocean.com/myorg
      auth:
        env: DIGITALOCEAN_TOKEN
    - name: ghcr
      type: github
      path: ghcr.io/myorg
      auth:
        env: GITHUB_TOKEN
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")

	if err := runRegistryLogin([]string{"--config", cfgPath, "--registry", "docr", "--dry-run"}); err != nil {
		t.Fatalf("runRegistryLogin --registry docr: %v", err)
	}
}

func TestRunRegistryLogin_UnknownRegistry(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeRegistryLoginFixture(t, dir)

	err := runRegistryLogin([]string{"--config", cfgPath, "--registry", "nope", "--dry-run"})
	if err == nil {
		t.Fatal("want error for unknown registry")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should mention registry name, got: %v", err)
	}
}

func TestRunRegistryLogin_UnknownProvider(t *testing.T) {
	dir := t.TempDir()
	content := `
ci:
  registries:
    - name: custom-reg
      type: unsupported-provider
      path: custom.registry.io/org
`
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err := runRegistryLogin([]string{"--config", cfgPath, "--dry-run"})
	if err == nil {
		t.Fatal("want error for unknown provider type")
	}
	if !strings.Contains(err.Error(), "unsupported-provider") {
		t.Errorf("error should mention provider type, got: %v", err)
	}
}
