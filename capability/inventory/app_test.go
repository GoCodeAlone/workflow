package inventory

import (
	"context"
	"os"
	"path/filepath"
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

func TestCheckAppSkipsTenantPolicyWhenTenancyAbsent(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte(`modules:
  - name: database
    type: storage.postgres
    config:
      dsn: postgres://example
workflows: {}
triggers: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := CollectApp(context.Background(), AppOptions{
		WorkflowPaths: []string{workflowPath},
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}
	for _, finding := range CheckApp(profile) {
		if finding.Code == "tenant-evidence-missing" {
			t.Fatalf("unexpected tenant finding without tenancy: %#v", finding)
		}
	}
}

func TestCheckAppUsesConcreteStorageCapabilityForTenantPolicy(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte(`modules:
  - name: tenants
    type: tenancy.scope
    config:
      tenantId: request.tenant_id
  - name: files
    type: storage.s3
    config:
      bucket: uploads
workflows: {}
triggers: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := CollectApp(context.Background(), AppOptions{
		WorkflowPaths: []string{workflowPath},
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}
	assertProfileFinding(t, CheckApp(profile), "tenant-evidence-missing", "storage.object")
}

func TestCollectAppInfersAuthnForGenericAuthSignals(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte(`modules:
  - name: jwt
    type: auth.jwt
workflows: {}
triggers: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := CollectApp(context.Background(), AppOptions{
		WorkflowPaths: []string{workflowPath},
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}
	assertUsage(t, profile, "auth.authn", "inferred")
	assertNoUsage(t, profile, "auth.authz", "inferred")
}

func TestCollectAppProviderIndexTrimsManifestTypes(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(workflowPath, []byte(`modules:
  - name: authz
    type: auth.rbac
workflows: {}
triggers: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(dir, "plugins", "workflow-plugin-authz")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{
  "name": "workflow-plugin-authz",
  "version": "0.4.0",
  "author": "GoCodeAlone",
  "description": "Authorization provider",
  "license": "MIT",
  "moduleTypes": [" auth.rbac "],
  "stepTypes": [],
  "triggerTypes": []
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	profile, err := CollectApp(context.Background(), AppOptions{
		WorkflowPaths: []string{workflowPath},
		PluginDir:     filepath.Join(dir, "plugins"),
		TaxonomyPath:  "testdata/taxonomy.yaml",
		GeneratedAt:   fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectApp: %v", err)
	}
	for _, finding := range CheckApp(profile) {
		if finding.Code == "missing-provider" && finding.CapabilityID == "auth.authz" {
			t.Fatalf("unexpected missing provider for trimmed manifest type: %#v", finding)
		}
	}
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

func assertNoUsage(t *testing.T, profile *AppProfile, capabilityID, mode string) {
	t.Helper()
	for _, usage := range profile.Usage {
		if usage.CapabilityID == capabilityID && usage.Mode == mode {
			t.Fatalf("unexpected usage %q mode %q: %#v", capabilityID, mode, usage)
		}
	}
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
