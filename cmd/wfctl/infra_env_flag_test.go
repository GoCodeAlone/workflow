package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfraPlan_EnvFlagFiltersResources(t *testing.T) {
	dir := t.TempDir()
	cfg := `modules:
  - name: cloud-credentials
    type: cloud.account
    config:
      provider: mock
  - name: bmw-database
    type: infra.database
    config:
      size: small
    environments:
      staging:
        config:
          size: small
      prod:
        config:
          size: large
  - name: bmw-dns
    type: infra.dns
    environments:
      prod:
        config:
          domain: example.com
      staging: null
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("staging plan excludes dns", func(t *testing.T) {
		resources, err := planResourcesForEnv(path, "staging")
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range resources {
			if r.Name == "bmw-dns" {
				t.Fatalf("dns should be skipped under staging (null env)")
			}
			if r.Name == "bmw-database" && r.Config["size"] != "small" {
				t.Fatalf("want staging size=small, got %v", r.Config["size"])
			}
		}
	})

	t.Run("prod plan includes dns with prod sizing", func(t *testing.T) {
		resources, err := planResourcesForEnv(path, "prod")
		if err != nil {
			t.Fatal(err)
		}
		var sawDNS, sawLargeDB bool
		for _, r := range resources {
			if r.Name == "bmw-dns" {
				sawDNS = true
			}
			if r.Name == "bmw-database" && r.Config["size"] == "large" {
				sawLargeDB = true
			}
		}
		if !sawDNS {
			t.Fatal("prod plan should include dns")
		}
		if !sawLargeDB {
			t.Fatal("prod plan should have size=large")
		}
	})
}
