package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPluginValidateContract_GoodPasses(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/good"})
	if err != nil {
		t.Fatalf("expected PASS for good fixture, got %v", err)
	}
}

func TestRunPluginValidateContract_BadMissingCapsFails(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/bad-missing-caps"})
	if err == nil {
		t.Fatal("expected FAIL for bad-missing-caps fixture, got nil")
	}
	if !strings.Contains(err.Error(), "contract check") {
		t.Errorf("error should mention contract check, got %v", err)
	}
}

func TestRunPluginValidateContract_BadMissingLdflagFails(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugin_validate_contract/bad-missing-ldflag"})
	if err == nil {
		t.Fatal("expected FAIL for bad-missing-ldflag fixture, got nil")
	}
}

func TestRunPluginValidateContract_ForPublishGoodTag(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"testdata/plugin_validate_contract/good",
	})
	if err != nil {
		t.Fatalf("expected PASS for good fixture + good tag, got %v", err)
	}
}

func TestRunPluginValidateContract_ForPublishBadTag(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3-rc.1",
		"testdata/plugin_validate_contract/good",
	})
	if err == nil {
		t.Fatal("expected FAIL for prerelease tag, got nil")
	}
	if !strings.Contains(err.Error(), "contract check") {
		t.Errorf("error should mention contract check, got %v", err)
	}
}

func TestRunPluginValidateContract_ForPublishBadTagShape(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "release-2026",
		"testdata/plugin_validate_contract/good",
	})
	if err == nil {
		t.Fatal("expected FAIL for non-semver tag, got nil")
	}
}

func TestRunPluginValidateContract_ReleaseDirGoodMatches(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"--release-dir", "testdata/plugin_validate_contract/release-dir-good/.release",
		"testdata/plugin_validate_contract/release-dir-good",
	})
	if err != nil {
		t.Fatalf("expected PASS for release-dir-good, got %v", err)
	}
}

func TestRunPluginValidateContract_ReleaseDirStaleFails(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--for-publish", "--tag", "v1.2.3",
		"--release-dir", "testdata/plugin_validate_contract/release-dir-stale/.release",
		"testdata/plugin_validate_contract/release-dir-stale",
	})
	if err == nil {
		t.Fatal("expected FAIL for release-dir-stale (.release plugin.json has 1.0.0 not 1.2.3)")
	}
}

func TestRunPluginValidateContract_GithubRefNameFallback(t *testing.T) {
	t.Setenv("GITHUB_REF_NAME", "v1.2.3")
	err := runPluginValidateContract([]string{
		"--for-publish",
		"testdata/plugin_validate_contract/good",
	})
	if err != nil {
		t.Fatalf("expected PASS via GITHUB_REF_NAME fallback, got %v", err)
	}
}

func TestRunPluginValidateContract_MissingArg(t *testing.T) {
	err := runPluginValidateContract([]string{})
	if err == nil {
		t.Fatal("expected error for missing plugin-dir arg")
	}
}

func TestRunPluginValidateContract_MessageContractStaticProfile(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--require-contract-kind", "message",
		"testdata/plugins/message-contract",
	})
	if err != nil {
		t.Fatalf("expected descriptor-only message contract to pass, got %v", err)
	}
}

func TestRunPluginValidateContract_MessageContractRuntimeProfile(t *testing.T) {
	err := runPluginValidateContract([]string{
		"--require-contract-kind", "message",
		"testdata/plugins/message-runtime-contract",
	})
	if err != nil {
		t.Fatalf("expected runtime-backed message contract to keep release checks and pass, got %v", err)
	}
}

func TestRunPluginValidateContract_ProviderDescriptorArrayIsIgnoredByStrictContracts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{
  "name": "workflow-plugin-provider-contracts",
  "version": "0.1.0",
  "author": "Workflow",
  "description": "provider descriptor array compatibility",
  "license": "MIT",
  "type": "external",
  "tier": "community",
  "minEngineVersion": "0.57.4",
  "capabilities": {
    "stepTypes": ["step.provider_contracts"]
  },
  "contracts": [
    {
      "id": "product-capture.browser.v1",
      "path": "contracts/product-capture-provider.json",
      "schema": "schemas/product-capture-provider.schema.json"
    }
  ]
}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.contracts.json"), []byte(`[
  {
    "id": "product-capture.browser.v1",
    "path": "contracts/product-capture-provider.json",
    "schema": "schemas/product-capture-provider.schema.json"
  }
]`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "plugin"), 0755); err != nil {
		t.Fatalf("mkdir cmd/plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "plugin", "main.go"), []byte(`package main

import sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"

func main() {
	version := sdk.ResolveBuildVersion("0.1.0")
	_ = sdk.IaCServeOptions{BuildVersion: version}
}
`), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(`builds:
  - main: ./cmd/plugin
    ldflags:
      - -s -w -X main.Version={{.Version}}
`), 0644); err != nil {
		t.Fatalf("write goreleaser config: %v", err)
	}

	err := runPluginValidateContract([]string{"--for-publish", "--tag", "v0.1.0", dir})
	if err != nil {
		t.Fatalf("expected provider descriptor contracts array to be ignored by strict plugin-contract validation, got %v", err)
	}
}

func TestRunPluginValidateContract_MessageContractGoreleaserOnlyRuntimeSurface(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{
  "name": "message-goreleaser-runtime",
  "version": "1.0.0",
  "author": "Workflow",
  "description": "runtime surface outside cmd/root",
  "capabilities": {},
  "contracts": "plugin.contracts.json"
}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.contracts.json"), []byte(`{
  "version": "v1",
  "contracts": [
    {
      "kind": "message",
      "contractType": "compute.network_audit_evidence.v1",
      "protoPackage": "workflow_plugin_compute_core.protocol.v1",
      "messageNames": ["NetworkAuditRecord"],
      "schemaDigest": "sha256:0123456789abcdef",
      "protocolVersion": "compute.v1alpha1",
      "mode": "strict"
    }
  ]
}`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte(`builds:
  - main: ./plugin
    ldflags:
      - -s -w -X main.Version={{.Version}}
`), 0644); err != nil {
		t.Fatalf("write goreleaser config: %v", err)
	}

	err := runPluginValidateContract([]string{"--require-contract-kind", "message", dir})
	if err == nil {
		t.Fatal("expected non-cmd runtime surface to keep executable release checks")
	}
}

func TestRunPluginValidateContract_UnknownContractKindFails(t *testing.T) {
	err := runPluginValidateContract([]string{"testdata/plugins/unknown-contract-kind"})
	if err == nil {
		t.Fatal("expected unknown contract kind fixture to fail")
	}
	if !strings.Contains(err.Error(), "contract check") {
		t.Fatalf("error = %v, want contract check", err)
	}
}
