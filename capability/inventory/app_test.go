package inventory

import (
	"context"
	"strings"
	"testing"
)

func TestCollectAppProfilesDeclaredAndInferredUsage(t *testing.T) {
	profile, err := CollectApp(context.Background(), AppOptions{
		ManifestPath:  "testdata/app/wfctl.yaml",
		WorkflowPaths: []string{"testdata/app/workflow.yaml"},
		PluginDir:     "testdata/app/plugins",
		LockfilePath:  "testdata/app/.wfctl-lock.yaml",
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}

	assertUsage(t, profile, "auth.authz", "declared")
	assertUsage(t, profile, "secrets.management", "inferred")
	assertUsage(t, profile, "tenancy.scope", "inferred")
	assertUsage(t, profile, "storage.database", "declared")
	assertEvidence(t, profile, "auth.authz", "wfctl.yaml")
	assertEvidence(t, profile, "auth.authz", "workflow.yaml")

	if profile.Metadata.Counts["declaredPlugins"] != 2 {
		t.Fatalf("declaredPlugins = %d, want 2", profile.Metadata.Counts["declaredPlugins"])
	}
}

func TestCheckAppFindsProviderAndTenantPolicyGaps(t *testing.T) {
	profile, err := CollectApp(context.Background(), AppOptions{
		ManifestPath:  "testdata/app/wfctl.yaml",
		WorkflowPaths: []string{"testdata/app/workflow.yaml"},
		PluginDir:     "testdata/app/plugins",
		LockfilePath:  "testdata/app/.wfctl-lock.yaml",
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}

	findings := CheckApp(profile)
	assertProfileFinding(t, findings, "missing-provider", "uncategorized:module:custom.missing")
	assertProfileFinding(t, findings, "tenant-evidence-missing", "storage.database")
}

func assertUsage(t *testing.T, profile *AppProfile, capabilityID, mode string) {
	t.Helper()
	for _, usage := range profile.Usage {
		if usage.CapabilityID == capabilityID && usage.Mode == mode {
			return
		}
	}
	t.Fatalf("usage %q mode %q not found: %#v", capabilityID, mode, profile.Usage)
}

func assertEvidence(t *testing.T, profile *AppProfile, capabilityID, pathFragment string) {
	t.Helper()
	for _, usage := range profile.Usage {
		if usage.CapabilityID != capabilityID {
			continue
		}
		for _, evidence := range usage.Evidence {
			if strings.Contains(evidence.SourcePath, pathFragment) {
				return
			}
		}
	}
	t.Fatalf("evidence containing %q not found for %q", pathFragment, capabilityID)
}

func assertProfileFinding(t *testing.T, findings []Finding, code, capabilityID string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Code == code && finding.CapabilityID == capabilityID {
			return
		}
	}
	t.Fatalf("finding %q for %q not found: %#v", code, capabilityID, findings)
}
