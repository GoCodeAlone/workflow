package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRegistryPush_NoContainersNoop(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  registries:
    - name: my-reg
      type: do
      path: registry.digitalocean.com/my-registry
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	// No container targets → push is a no-op.
	err := runRegistryPush([]string{"--config", cfgPath, "--dry-run"})
	if err != nil {
		t.Fatalf("push with no containers: %v", err)
	}
}

func TestRunRegistryPush_DryRunPrintsRefs(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  registries:
    - name: my-reg
      type: do
      path: registry.digitalocean.com/my-registry
  build:
    containers:
      - name: app
        push_to:
          - my-reg
`
	cfgPath := filepath.Join(dir, "ci.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WFCTL_BUILD_DRY_RUN", "1")
	err := runRegistryPush([]string{"--config", cfgPath, "--dry-run"})
	if err != nil {
		t.Fatalf("dry-run push: %v", err)
	}
}
