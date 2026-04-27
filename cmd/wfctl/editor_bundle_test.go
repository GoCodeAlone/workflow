package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunEditorBundleWritesCanonicalJSONBundle(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "editor-bundle.json")

	if err := runEditorBundle([]string{"--output", outPath}); err != nil {
		t.Fatalf("editor-bundle failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var bundle struct {
		Version       string         `json:"version"`
		ModuleSchemas map[string]any `json:"moduleSchemas"`
		StepSchemas   map[string]any `json:"stepSchemas"`
		CoercionRules map[string]any `json:"coercionRules"`
		Schemas       struct {
			App   map[string]any `json:"app"`
			Infra map[string]any `json:"infra"`
			Wfctl map[string]any `json:"wfctl"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	if bundle.Version == "" {
		t.Fatal("expected bundle schema version")
	}
	if len(bundle.ModuleSchemas) == 0 {
		t.Fatal("expected module schemas")
	}
	if len(bundle.StepSchemas) == 0 {
		t.Fatal("expected step schemas")
	}
	if len(bundle.CoercionRules) == 0 {
		t.Fatal("expected coercion rules")
	}
	if len(bundle.Schemas.App) == 0 || len(bundle.Schemas.Infra) == 0 || len(bundle.Schemas.Wfctl) == 0 {
		t.Fatalf("expected app/infra/wfctl schemas, got %+v", bundle.Schemas)
	}
}

func TestRunEditorBundleLoadsPluginContractDescriptorSetReference(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "workflow-plugin-strict")
	if err := os.Mkdir(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"workflow-plugin-strict"}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(`{
  "version": "v1",
  "descriptorSetRef": "proto/strict.pb",
  "contracts": [
    {
      "kind": "step",
      "type": "step.strict",
      "mode": "strict",
      "input": "workflow.strict.Input",
      "output": "workflow.strict.Output"
    }
  ]
}`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}

	outPath := filepath.Join(dir, "editor-bundle.json")
	if err := runEditorBundle([]string{"--plugin-dir", pluginDir, "--output", outPath}); err != nil {
		t.Fatalf("editor-bundle failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var bundle struct {
		Contracts map[string]struct {
			DescriptorSetRef string `json:"descriptorSetRef"`
			RequestMessage   string `json:"requestMessage"`
			ResponseMessage  string `json:"responseMessage"`
		} `json:"contracts"`
		DescriptorSets map[string]struct {
			ExternalRef string `json:"externalRef"`
		} `json:"descriptorSets"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	contract := bundle.Contracts["step:step.strict"]
	if contract.DescriptorSetRef != "proto/strict.pb" {
		t.Fatalf("descriptorSetRef = %q", contract.DescriptorSetRef)
	}
	if contract.RequestMessage != "workflow.strict.Input" || contract.ResponseMessage != "workflow.strict.Output" {
		t.Fatalf("typed I/O metadata missing: %+v", contract)
	}
	if bundle.DescriptorSets["proto/strict.pb"].ExternalRef != "proto/strict.pb" {
		t.Fatalf("descriptor set reference missing: %+v", bundle.DescriptorSets)
	}
}
