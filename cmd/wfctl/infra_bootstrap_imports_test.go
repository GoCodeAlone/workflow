package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Verifies that bootstrapStateBackend resolves imports: in config files.
func TestBootstrapStateBackend_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: /tmp/test-iac-state
`
	main := `imports:
  - shared.yaml
modules:
  - name: bmw-database
    type: infra.database
    config:
      size: small
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(shared), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yaml"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}

	// bootstrapStateBackend should find the iac.state from the imported shared.yaml.
	// The filesystem backend is a no-op (no network needed), so this just exercises
	// the discovery path without error.
	err := bootstrapStateBackend(context.Background(), filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("bootstrapStateBackend: %v", err)
	}
}
