package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// TestInfraMultiEnv_E2E exercises planResourcesForEnv against the realistic
// two-env fixture in testdata/infra-multi-env.yaml.
func TestInfraMultiEnv_E2E(t *testing.T) {
	fixture := testdataPath("infra-multi-env.yaml")

	t.Run("staging plan excludes dns, uses small db", func(t *testing.T) {
		resources, err := planResourcesForEnv(fixture, "staging")
		if err != nil {
			t.Fatal(err)
		}

		nameSet := make(map[string]bool)
		for _, r := range resources {
			nameSet[r.Name] = true
		}

		if nameSet["bmw-dns"] {
			t.Fatal("staging plan should not include bmw-dns (staging: null)")
		}
		if !nameSet["bmw-database"] {
			t.Fatal("staging plan should include bmw-database")
		}

		for _, r := range resources {
			if r.Name == "bmw-database" {
				if r.Config["size"] != "db-s-1vcpu-1gb" {
					t.Fatalf("staging db size: want db-s-1vcpu-1gb, got %v", r.Config["size"])
				}
				if r.Region != "nyc3" {
					t.Fatalf("staging db region: want nyc3, got %q", r.Region)
				}
			}
		}

		// Region defaults from top-level environments.staging.region
		for _, r := range resources {
			if r.Name == "bmw-firewall" && r.Region != "nyc3" {
				t.Fatalf("bmw-firewall staging region: want nyc3 (from top-level), got %q", r.Region)
			}
		}
	})

	t.Run("prod plan includes dns with large db", func(t *testing.T) {
		resources, err := planResourcesForEnv(fixture, "prod")
		if err != nil {
			t.Fatal(err)
		}

		var sawDNS, sawLargeDB, sawApp bool
		for _, r := range resources {
			switch r.Name {
			case "bmw-dns":
				sawDNS = true
			case "bmw-database":
				if r.Config["size"] == "db-s-2vcpu-4gb" {
					sawLargeDB = true
				}
				if r.Region != "nyc1" {
					t.Fatalf("prod db region: want nyc1, got %q", r.Region)
				}
			case "bmw-app":
				sawApp = true
				if r.Config["instance_count"] != 2 {
					t.Fatalf("prod app instance_count: want 2, got %v", r.Config["instance_count"])
				}
			}
		}

		if !sawDNS {
			t.Fatal("prod plan should include bmw-dns")
		}
		if !sawLargeDB {
			t.Fatal("prod plan should have large db size")
		}
		if !sawApp {
			t.Fatal("prod plan should include bmw-app")
		}
	})

	t.Run("staging resource count excludes dns", func(t *testing.T) {
		stagingRes, err := planResourcesForEnv(fixture, "staging")
		if err != nil {
			t.Fatal(err)
		}
		prodRes, err := planResourcesForEnv(fixture, "prod")
		if err != nil {
			t.Fatal(err)
		}
		// prod has dns, staging does not
		if len(prodRes) != len(stagingRes)+1 {
			t.Fatalf("prod should have 1 more resource than staging (dns), got prod=%d staging=%d",
				len(prodRes), len(stagingRes))
		}
	})
}
