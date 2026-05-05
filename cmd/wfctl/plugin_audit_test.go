package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "good.module", "mode": "strict", "config": "workflow.plugins.good.ModuleConfig"},
    {"kind": "step", "type": "good.step", "mode": "strict", "input": "workflow.plugins.good.StepInput", "output": "workflow.plugins.good.StepOutput"}
  ]
}`)

	result := auditPluginRepo(dir)
	if result.ManifestShape != "canonical" {
		t.Fatalf("shape = %q", result.ManifestShape)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %v", result.Findings)
	}
}

func TestAuditPluginStrictContractsMissingDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-strict-missing", `{
  "name": "workflow-plugin-strict-missing",
  "version": "0.1.0",
  "capabilities": {
    "moduleTypes": ["strict.module"],
    "stepTypes": ["strict.step"],
    "triggerTypes": ["strict.trigger"],
    "serviceMethods": ["StrictService/Call"]
  }
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	for _, code := range []string{
		"missing_module_contract_descriptor",
		"missing_step_contract_descriptor",
		"missing_trigger_contract_descriptor",
		"missing_service_method_contract_descriptor",
	} {
		if !hasPlanFinding(result.Findings, "ERROR", code) {
			t.Fatalf("expected %s error, got %v", code, result.Findings)
		}
	}
	if result.ContractCoverage.Modules.Total != 1 || result.ContractCoverage.Modules.Missing != 1 {
		t.Fatalf("module coverage = %+v", result.ContractCoverage.Modules)
	}
}

func TestAuditPluginStrictContractsWithGeneratedDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-strict-good", `{
  "name": "workflow-plugin-strict-good",
  "version": "0.1.0",
  "capabilities": {
    "moduleTypes": ["strict.module"],
    "stepTypes": ["strict.step"],
    "triggerTypes": ["strict.trigger"],
    "serviceMethods": ["StrictService/Call"]
  }
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "strict.module", "mode": "strict", "config": "workflow.plugins.strict.ModuleConfig"},
    {"kind": "step", "type": "strict.step", "mode": "strict", "input": "workflow.plugins.strict.StepInput", "output": "workflow.plugins.strict.StepOutput"},
    {"kind": "trigger", "type": "strict.trigger", "mode": "strict", "config": "workflow.plugins.strict.TriggerConfig"},
    {"kind": "service_method", "type": "StrictService/Call", "mode": "strict", "input": "workflow.plugins.strict.CallRequest", "output": "workflow.plugins.strict.CallResponse"}
  ]
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %v", result.Findings)
	}
	if result.ContractCoverage.Modules.Strict != 1 || result.ContractCoverage.Steps.Strict != 1 ||
		result.ContractCoverage.Triggers.Strict != 1 || result.ContractCoverage.ServiceMethods.Strict != 1 {
		t.Fatalf("coverage = %+v", result.ContractCoverage)
	}
}

func TestAuditPluginStrictContractsWithProtoShapedDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-strict-proto-shape", `{
  "name": "workflow-plugin-strict-proto-shape",
  "version": "0.1.0",
  "capabilities": {
    "moduleTypes": ["strict.module"],
    "stepTypes": ["strict.step"],
    "triggerTypes": ["strict.trigger"],
    "serviceMethods": ["StrictService/Call"]
  }
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "CONTRACT_KIND_MODULE", "module_type": "strict.module", "mode": "CONTRACT_MODE_STRICT_PROTO", "config_message": "workflow.plugins.strict.ModuleConfig"},
    {"kind": "CONTRACT_KIND_STEP", "step_type": "strict.step", "mode": "CONTRACT_MODE_STRICT_PROTO", "input_message": "workflow.plugins.strict.StepInput", "output_message": "workflow.plugins.strict.StepOutput"},
    {"kind": "CONTRACT_KIND_TRIGGER", "trigger_type": "strict.trigger", "mode": "CONTRACT_MODE_STRICT_PROTO", "config_message": "workflow.plugins.strict.TriggerConfig"},
    {"kind": "CONTRACT_KIND_SERVICE", "service_name": "StrictService", "method": "Call", "mode": "CONTRACT_MODE_STRICT_PROTO", "input_message": "workflow.plugins.strict.CallRequest", "output_message": "workflow.plugins.strict.CallResponse"}
  ]
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %v", result.Findings)
	}
}

func TestAuditPluginStrictContractsLegacyDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-strict-legacy", `{
  "name": "workflow-plugin-strict-legacy",
  "version": "0.1.0",
  "capabilities": {"moduleTypes": ["legacy.module"]}
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "legacy.module", "mode": "legacy_struct"}
  ]
}`)

	nonStrict := auditPluginRepoWithOptions(dir, pluginAuditOptions{})
	if !hasPlanFinding(nonStrict.Findings, "WARN", "legacy_module_contract_descriptor") {
		t.Fatalf("expected legacy descriptor warning, got %v", nonStrict.Findings)
	}

	strict := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	if !hasPlanFinding(strict.Findings, "ERROR", "legacy_module_contract_descriptor") {
		t.Fatalf("expected legacy descriptor error, got %v", strict.Findings)
	}
}

func TestAuditPluginStrictContractsMissingModeIsNotStrict(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-strict-missing-mode", `{
  "name": "workflow-plugin-strict-missing-mode",
  "version": "0.1.0",
  "capabilities": {"moduleTypes": ["strict.module"]}
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "strict.module", "config": "workflow.plugins.strict.ModuleConfig"}
  ]
}`)

	result := auditPluginRepoWithOptions(dir, pluginAuditOptions{StrictContracts: true})
	if !hasPlanFinding(result.Findings, "ERROR", "legacy_module_contract_descriptor") {
		t.Fatalf("expected missing mode to be rejected, got %v", result.Findings)
	}
}

func TestRunAuditPluginsStrictContractsIgnoresUnrelatedWarnings(t *testing.T) {
	root := t.TempDir()
	dir := writePluginAuditRepoAt(t, root, "workflow-plugin-legacy-shape", `{
  "name": "workflow-plugin-legacy-shape",
  "version": "0.1.0",
  "moduleTypes": ["legacy.module"]
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "legacy.module", "mode": "strict", "config": "workflow.plugins.legacy.ModuleConfig"}
  ]
}`)

	var out bytes.Buffer
	if err := runAuditWithOutput([]string{"plugins", "--repo-root", root, "--strict-contracts"}, &out); err != nil {
		t.Fatalf("expected strict-contracts to ignore non-contract warning, got %v\n%s", err, out.String())
	}
}

func TestRunAuditPluginsStrictContractsFailsOnMissingDescriptors(t *testing.T) {
	root := t.TempDir()
	writePluginAuditRepoAt(t, root, "workflow-plugin-strict-missing", `{
  "name": "workflow-plugin-strict-missing",
  "version": "0.1.0",
  "capabilities": {"moduleTypes": ["strict.module"], "stepTypes": ["strict.step"]}
}`)

	var out bytes.Buffer
	err := runAuditWithOutput([]string{"plugins", "--repo-root", root, "--strict-contracts"}, &out)
	if err == nil {
		t.Fatal("expected strict contracts audit failure")
	}
	for _, want := range []string{"missing_module_contract_descriptor", "missing_step_contract_descriptor", "module 0/1 strict"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in output:\n%s", want, out.String())
		}
	}
}

func TestRunValidatePluginStrictContractsMissingDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-validate-strict", `{
  "name": "workflow-plugin-validate-strict",
  "version": "0.1.0",
  "author": "tester",
  "description": "Strict contract validation test",
  "type": "external",
  "tier": "community",
  "license": "MIT",
  "capabilities": {"moduleTypes": ["validate.module"], "stepTypes": ["validate.step"]},
  "downloads": [{"os": "linux", "arch": "amd64", "url": "https://example.com/plugin.tar.gz"}]
}`)

	err := runPluginValidate([]string{"--file", filepath.Join(dir, "plugin.json"), "--strict-contracts"})
	if err == nil {
		t.Fatal("expected strict contract validation error")
	}
	if !strings.Contains(err.Error(), "validation error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunValidatePluginStrictContractsWithGeneratedDescriptors(t *testing.T) {
	dir := writePluginAuditRepo(t, "workflow-plugin-validate-strict-good", `{
  "name": "workflow-plugin-validate-strict-good",
  "version": "0.1.0",
  "author": "tester",
  "description": "Strict contract validation test",
  "type": "external",
  "tier": "community",
  "license": "MIT",
  "capabilities": {"moduleTypes": ["validate.module"], "stepTypes": ["validate.step"]},
  "downloads": [{"os": "linux", "arch": "amd64", "url": "https://example.com/plugin.tar.gz"}]
}`)
	writePluginContracts(t, dir, `{
  "version": "1",
  "contracts": [
    {"kind": "module", "type": "validate.module", "mode": "strict", "config": "workflow.plugins.validate.ModuleConfig"},
    {"kind": "step", "type": "validate.step", "mode": "strict", "input": "workflow.plugins.validate.StepInput", "output": "workflow.plugins.validate.StepOutput"}
  ]
}`)

	if err := runPluginValidate([]string{"--file", filepath.Join(dir, "plugin.json"), "--strict-contracts"}); err != nil {
		t.Fatalf("expected strict contract validation to pass, got: %v", err)
	}
}

func TestValidateStrictContractRegistryManifestIncludesServiceMethods(t *testing.T) {
	errs := validateStrictContractRegistryManifest(&RegistryManifest{
		Capabilities: &RegistryCapabilities{
			ServiceMethods: []string{"StrictService/Call"},
		},
	}, pluginAuditOptions{StrictContracts: true})
	if len(errs) == 0 {
		t.Fatal("expected missing service method descriptor validation error")
	}
	if !strings.Contains(errs[0].Message, "missing_service_method_contract_descriptor") {
		t.Fatalf("expected service method contract error, got %+v", errs)
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

	templateSubstring := writePluginAuditRepo(t, "workflow-plugin-template-tools", `{
  "name": "workflow-plugin-template-tools",
  "version": "0.1.0",
  "capabilities": {}
}`)
	templateSubstringResult := auditPluginRepo(templateSubstring)
	if hasPlanFinding(templateSubstringResult.Findings, "ERROR", "placeholder_plugin_identity") {
		t.Fatalf("unexpected placeholder identity error for non-placeholder template name: %v", templateSubstringResult.Findings)
	}
}

func TestAuditPluginManifestReadErrorIsNotInvalidJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod read-error behavior is platform-specific")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/workflow-plugin-unreadable\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	manifestPath := filepath.Join(repo, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"workflow-plugin-unreadable"}`), 0o000); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if _, err := os.ReadFile(manifestPath); err == nil {
		t.Skip("chmod 000 did not make plugin.json unreadable in this environment")
	}
	t.Cleanup(func() {
		_ = os.Chmod(manifestPath, 0o644)
	})

	result := auditPluginRepo(repo)
	if result.ManifestShape != "unreadable" {
		t.Fatalf("shape = %q, want unreadable", result.ManifestShape)
	}
	if !hasPlanFinding(result.Findings, "ERROR", "read_plugin_manifest") {
		t.Fatalf("expected read_plugin_manifest error, got %v", result.Findings)
	}
	summary := summarizePluginAudit([]pluginAuditResult{result})
	if summary.Invalid != 1 {
		t.Fatalf("invalid count = %d, want 1", summary.Invalid)
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

func writePluginContracts(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "plugin.contracts.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write plugin.contracts.json: %v", err)
	}
}
