package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditPluginManifestCanonical(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-good", `{
  "name": "workflow-plugin-good",
  "version": "0.1.0",
  "capabilities": {
    "moduleTypes": ["good.module"],
    "stepTypes": ["good.step"]
  }
}`)

	result := auditPluginRepo(dir)
	if result.ManifestShape != "canonical" {
		t.Fatalf("shape = %q", result.ManifestShape)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %v", result.Findings)
	}
}

func TestAuditPluginManifestLegacyShapes(t *testing.T) {
	cases := []struct {
		name    string
		content string
		shape   string
	}{
		{
			name: "top-level-types",
			content: `{
  "name": "workflow-plugin-legacy",
  "version": "0.1.0",
  "moduleTypes": ["legacy.module"],
  "stepTypes": ["legacy.step"]
}`,
			shape: "top-level-types",
		},
		{
			name: "capabilities-array",
			content: `{
  "name": "workflow-plugin-array",
  "version": "0.1.0",
  "capabilities": ["module", "step"]
}`,
			shape: "capabilities-array",
		},
		{
			name: "provider-resources",
			content: `{
  "name": "workflow-plugin-gcp",
  "version": "0.1.0",
  "type": "iac_provider",
  "resources": ["bucket"]
}`,
			shape: "provider-resources",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := writePluginAuditRepo(t, "workflow-plugin-"+tc.name, tc.content)
			result := auditPluginRepo(dir)
			if result.ManifestShape != tc.shape {
				t.Fatalf("shape = %q, want %q", result.ManifestShape, tc.shape)
			}
			if !hasPlanFinding(result.Findings, "WARN", "legacy_plugin_manifest") {
				t.Fatalf("expected legacy warning, got %v", result.Findings)
			}
		})
	}
}

func TestAuditPluginManifestMissingAndPlaceholder(t *testing.T) {
	missing := t.TempDir()
	if err := os.WriteFile(filepath.Join(missing, "go.mod"), []byte("module example.com/workflow-plugin-missing\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	missingResult := auditPluginRepo(missing)
	if missingResult.ManifestShape != "missing" {
		t.Fatalf("shape = %q", missingResult.ManifestShape)
	}
	if !hasPlanFinding(missingResult.Findings, "ERROR", "missing_plugin_manifest") {
		t.Fatalf("expected missing manifest error, got %v", missingResult.Findings)
	}

	placeholder := writePluginAuditRepo(t, "workflow-plugin-template", `{
  "name": "workflow-plugin-TEMPLATE",
  "version": "0.1.0",
  "capabilities": {}
}`)
	placeholderResult := auditPluginRepo(placeholder)
	if !hasPlanFinding(placeholderResult.Findings, "ERROR", "placeholder_plugin_identity") {
		t.Fatalf("expected placeholder identity error, got %v", placeholderResult.Findings)
	}
}

func TestAuditPluginReposDiscoversWorkflowPlugins(t *testing.T) {
	root := t.TempDir()
	writePluginAuditRepoAt(t, root, "workflow-plugin-good", `{
  "name": "workflow-plugin-good",
  "version": "0.1.0",
  "capabilities": {}
}`)
	writePluginAuditRepoAt(t, root, "not-a-plugin", `{
  "name": "not-a-plugin",
  "version": "0.1.0",
  "capabilities": {}
}`)
	if err := os.MkdirAll(filepath.Join(root, "workflow-plugin-not-repo"), 0o755); err != nil {
		t.Fatalf("mkdir non-repo: %v", err)
	}

	results, err := auditPluginRepos(root)
	if err != nil {
		t.Fatalf("audit repos: %v", err)
	}
	if len(results) != 1 || results[0].Name != "workflow-plugin-good" {
		t.Fatalf("results = %+v", results)
	}
}

func writePluginAuditRepo(t *testing.T, name, manifest string) string {
	t.Helper()
	root := t.TempDir()
	return writePluginAuditRepoAt(t, root, name, manifest)
}

func writePluginAuditRepoAt(t *testing.T, root, name, manifest string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir plugin repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/"+name+"\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	return dir
}
