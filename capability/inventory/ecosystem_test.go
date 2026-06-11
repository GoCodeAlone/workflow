package inventory

import (
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 6, 11, 4, 45, 0, 0, time.UTC)

func TestCollectEcosystemMarksReleasedAndLocal(t *testing.T) {
	inv, err := CollectEcosystem(EcosystemOptions{
		RegistryDir:     "testdata/ecosystem/registry",
		RepoRoot:        "testdata/ecosystem/repos",
		TaxonomyPath:    "testdata/taxonomy.yaml",
		GeneratedAt:     fixedTime,
		WorkflowVersion: "0.75.3",
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	assertProviderStatus(t, inv, "auth", "released")
	assertProviderStatus(t, inv, "workflow-plugin-authz", "local-only")

	if inv.Metadata.Generator != "wfctl capability ecosystem" {
		t.Fatalf("Metadata.Generator = %q", inv.Metadata.Generator)
	}
	if inv.Metadata.GeneratedAt != fixedTime.Format(time.RFC3339) {
		t.Fatalf("Metadata.GeneratedAt = %q", inv.Metadata.GeneratedAt)
	}
	if inv.Metadata.Counts["releasedProviders"] != 2 {
		t.Fatalf("releasedProviders = %d, want 2", inv.Metadata.Counts["releasedProviders"])
	}
	if inv.Metadata.Counts["localProviders"] != 2 {
		t.Fatalf("localProviders = %d, want 2", inv.Metadata.Counts["localProviders"])
	}
}

func TestCollectEcosystemUncategorizedRawTypes(t *testing.T) {
	inv, err := CollectEcosystem(EcosystemOptions{
		RegistryDir:  "testdata/ecosystem/registry",
		RepoRoot:     "testdata/ecosystem/repos",
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	assertFinding(t, inv, "uncategorized:module:mystery.widget", "needs-review")
	assertCapabilityEvidence(t, inv, "uncategorized:module:mystery.widget", "moduleTypes[0]")
	assertCapabilityEvidenceSource(t, inv, "uncategorized:module:mystery.widget", "registry-manifest")
	assertCapabilityEvidenceSource(t, inv, "http.server", "plugin-manifest")
}

func TestCollectEcosystemSkipsPluginReposWithoutManifest(t *testing.T) {
	_, err := CollectEcosystem(EcosystemOptions{
		RegistryDir:  "testdata/ecosystem/registry",
		RepoRoot:     "testdata/ecosystem/repos",
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}
}

func assertProviderStatus(t *testing.T, inv *Inventory, providerName, status string) {
	t.Helper()
	for _, cap := range inv.Capabilities {
		for _, provider := range cap.Providers {
			if provider.Name == providerName && provider.ReleaseStatus == status {
				return
			}
		}
	}
	t.Fatalf("provider %q with status %q not found in inventory: %#v", providerName, status, inv.Capabilities)
}

func assertFinding(t *testing.T, inv *Inventory, capabilityID, code string) {
	t.Helper()
	for _, finding := range inv.Findings {
		if finding.CapabilityID == capabilityID && finding.Code == code {
			return
		}
	}
	for _, cap := range inv.Capabilities {
		for _, finding := range cap.Findings {
			if finding.CapabilityID == capabilityID && finding.Code == code {
				return
			}
		}
	}
	t.Fatalf("finding %q for capability %q not found", code, capabilityID)
}

func assertCapabilityEvidence(t *testing.T, inv *Inventory, capabilityID, detail string) {
	t.Helper()
	for _, cap := range inv.Capabilities {
		if cap.ID != capabilityID {
			continue
		}
		for _, evidence := range cap.Evidence {
			if evidence.Detail == detail {
				return
			}
		}
		t.Fatalf("capability %q did not include evidence detail %q: %#v", capabilityID, detail, cap.Evidence)
	}
	t.Fatalf("capability %q not found", capabilityID)
}

func assertCapabilityEvidenceSource(t *testing.T, inv *Inventory, capabilityID, sourceKind string) {
	t.Helper()
	for _, cap := range inv.Capabilities {
		if cap.ID != capabilityID {
			continue
		}
		for _, evidence := range cap.Evidence {
			if evidence.SourceKind == sourceKind {
				return
			}
		}
		t.Fatalf("capability %q did not include source kind %q: %#v", capabilityID, sourceKind, cap.Evidence)
	}
	t.Fatalf("capability %q not found", capabilityID)
}
