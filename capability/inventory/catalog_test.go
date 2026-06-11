package inventory

import "testing"

func TestBuildCatalogExcludesUncategorizedRows(t *testing.T) {
	inv, err := CollectEcosystem(EcosystemOptions{
		RegistryDir:  "testdata/ecosystem/registry",
		RepoRoot:     "testdata/ecosystem/repos",
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	catalog := BuildCatalog(inv)
	if catalog.Metadata.Generator != "wfctl capability catalog" {
		t.Fatalf("generator = %q", catalog.Metadata.Generator)
	}
	for _, cap := range catalog.Capabilities {
		if cap.ID == "uncategorized:module:mystery.widget" {
			t.Fatalf("catalog included uncategorized row: %#v", cap)
		}
	}
	if catalog.Metadata.Counts["hiddenUncategorized"] == 0 {
		t.Fatalf("expected hidden uncategorized count, got %#v", catalog.Metadata.Counts)
	}
	assertCatalogProvider(t, catalog, "auth.authz", "auth")
	assertCatalogProvider(t, catalog, "auth.authz", "workflow-plugin-authz")
}

func TestBuildCapabilityCrossrefsIncludesPluginCapabilitiesAndDependencies(t *testing.T) {
	inv, err := CollectEcosystem(EcosystemOptions{
		RegistryDir:  "testdata/ecosystem/registry",
		RepoRoot:     "testdata/ecosystem/repos",
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	refs := BuildCapabilityCrossrefs(inv)
	authz := refs.Plugins["workflow-plugin-authz"]
	if authz.Name != "workflow-plugin-authz" {
		t.Fatalf("missing plugin ref: %#v", refs.Plugins)
	}
	github := refs.Plugins["github"]
	if github.Name != "github" {
		t.Fatalf("missing raw-only plugin ref for github: %#v", refs.Plugins)
	}
	if !contains(github.RawCapabilities, "module:mystery.widget") {
		t.Fatalf("github raw capabilities = %#v, want module:mystery.widget", github.RawCapabilities)
	}
	if !contains(authz.Capabilities, "auth.authz") {
		t.Fatalf("authz plugin capabilities = %#v, want auth.authz", authz.Capabilities)
	}
	if !contains(authz.Dependencies, "workflow-plugin-auth") {
		t.Fatalf("authz plugin deps = %#v, want workflow-plugin-auth", authz.Dependencies)
	}
	providers := refs.Capabilities["auth.authz"].Providers
	if !contains(providers, "workflow-plugin-authz") {
		t.Fatalf("auth.authz providers = %#v, want workflow-plugin-authz", providers)
	}
}

func assertCatalogProvider(t *testing.T, catalog *Catalog, capabilityID, providerName string) {
	t.Helper()
	for _, cap := range catalog.Capabilities {
		if cap.ID != capabilityID {
			continue
		}
		for _, provider := range cap.Providers {
			if provider.Name == providerName {
				return
			}
		}
		t.Fatalf("capability %q missing provider %q: %#v", capabilityID, providerName, cap.Providers)
	}
	t.Fatalf("capability %q not found in catalog", capabilityID)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
