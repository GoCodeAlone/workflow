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

	if err := runEditorBundle([]string{"--registry=false", "--output", outPath}); err != nil {
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
	if err := runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir, "--output", outPath}); err != nil {
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

func TestRunEditorBundleIncludesRegistryContractsByDefault(t *testing.T) {
	origList := listEditorBundleRegistryPluginNames
	origFetch := fetchEditorBundleRegistryManifest
	t.Cleanup(func() {
		listEditorBundleRegistryPluginNames = origList
		fetchEditorBundleRegistryManifest = origFetch
	})
	listEditorBundleRegistryPluginNames = func() ([]string, error) {
		return []string{"workflow-plugin-registry-strict"}, nil
	}
	fetchEditorBundleRegistryManifest = func(name string) (*RegistryManifest, error) {
		return &RegistryManifest{
			Name: name,
			Contracts: []pluginContractDescriptor{
				{
					Kind:             "step",
					Type:             "step.registry_strict",
					Mode:             "strict",
					Input:            "workflow.registry.Input",
					Output:           "workflow.registry.Output",
					DescriptorSetRef: "proto/registry.pb",
				},
			},
		}, nil
	}

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
		Contracts map[string]struct {
			DescriptorSetRef string `json:"descriptorSetRef"`
			RequestMessage   string `json:"requestMessage"`
			ResponseMessage  string `json:"responseMessage"`
		} `json:"contracts"`
		Messages       map[string]any `json:"messages"`
		DescriptorSets map[string]struct {
			ExternalRef string `json:"externalRef"`
		} `json:"descriptorSets"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	contract := bundle.Contracts["step:step.registry_strict"]
	if contract.DescriptorSetRef != "proto/registry.pb" {
		t.Fatalf("descriptorSetRef = %q", contract.DescriptorSetRef)
	}
	if contract.RequestMessage != "workflow.registry.Input" || contract.ResponseMessage != "workflow.registry.Output" {
		t.Fatalf("typed I/O metadata missing: %+v", contract)
	}
	if bundle.Messages["workflow.registry.Input"] == nil || bundle.Messages["workflow.registry.Output"] == nil {
		t.Fatalf("expected registry message metadata placeholders, got %+v", bundle.Messages)
	}
	if bundle.DescriptorSets["proto/registry.pb"].ExternalRef != "proto/registry.pb" {
		t.Fatalf("descriptor set reference missing: %+v", bundle.DescriptorSets)
	}
}

func TestRunEditorBundlePreservesPerContractDescriptorSetReferences(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "workflow-plugin-multi-ref")
	if err := os.Mkdir(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"workflow-plugin-multi-ref"}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(`{
  "version": "v1",
  "contracts": [
    {
      "kind": "step",
      "type": "step.one",
      "mode": "strict",
      "input": "workflow.one.Input",
      "output": "workflow.one.Output",
      "descriptorSetRef": "proto/one.pb"
    },
    {
      "kind": "step",
      "type": "step.two",
      "mode": "strict",
      "input": "workflow.two.Input",
      "output": "workflow.two.Output",
      "descriptorSetRef": "proto/two.pb"
    }
  ]
}`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}

	outPath := filepath.Join(dir, "editor-bundle.json")
	if err := runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir, "--output", outPath}); err != nil {
		t.Fatalf("editor-bundle failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var bundle struct {
		Contracts map[string]struct {
			DescriptorSetRef string `json:"descriptorSetRef"`
		} `json:"contracts"`
		Messages map[string]struct {
			DescriptorSetRef string `json:"descriptorSetRef"`
		} `json:"messages"`
		DescriptorSets map[string]struct {
			ExternalRef string `json:"externalRef"`
		} `json:"descriptorSets"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	if got := bundle.Contracts["step:step.one"].DescriptorSetRef; got != "proto/one.pb" {
		t.Fatalf("step.one descriptorSetRef = %q", got)
	}
	if got := bundle.Contracts["step:step.two"].DescriptorSetRef; got != "proto/two.pb" {
		t.Fatalf("step.two descriptorSetRef = %q", got)
	}
	if got := bundle.Messages["workflow.one.Input"].DescriptorSetRef; got != "proto/one.pb" {
		t.Fatalf("workflow.one.Input descriptorSetRef = %q", got)
	}
	if got := bundle.Messages["workflow.two.Input"].DescriptorSetRef; got != "proto/two.pb" {
		t.Fatalf("workflow.two.Input descriptorSetRef = %q", got)
	}
	if bundle.DescriptorSets["proto/one.pb"].ExternalRef != "proto/one.pb" {
		t.Fatalf("descriptor set one reference missing: %+v", bundle.DescriptorSets)
	}
	if bundle.DescriptorSets["proto/two.pb"].ExternalRef != "proto/two.pb" {
		t.Fatalf("descriptor set two reference missing: %+v", bundle.DescriptorSets)
	}
}
