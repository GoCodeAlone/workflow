package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Verifies that wfctl infra honors imports: in config files.
func TestDiscoverInfraModules_HonorsImports(t *testing.T) {
	dir := t.TempDir()

	shared := `modules:
  - name: cloud-credentials
    type: cloud.account
    config:
      provider: mock
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

	state, platforms, accounts, err := discoverInfraModules(filepath.Join(dir, "main.yaml"))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	_ = state
	_ = platforms
	if len(accounts) != 1 {
		t.Fatalf("want 1 cloud.account from imported shared.yaml, got %d", len(accounts))
	}
	if accounts[0].Name != "cloud-credentials" {
		t.Fatalf("want cloud-credentials, got %s", accounts[0].Name)
	}
}
