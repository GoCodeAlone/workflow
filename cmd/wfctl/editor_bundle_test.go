package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRunEditorBundleLoadsMessageContractDescriptor(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "editor-bundle.json")

	if err := runEditorBundle([]string{"--registry=false", "--plugin-dir", "testdata/plugins/message-contract", "--output", outPath}); err != nil {
		t.Fatalf("editor-bundle failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var bundle struct {
		Contracts map[string]struct {
			DescriptorSetRef string   `json:"descriptorSetRef"`
			ProtoPackage     string   `json:"protoPackage"`
			MessageNames     []string `json:"messageNames"`
			ProtocolVersion  string   `json:"protocolVersion"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatalf("bundle is not valid JSON: %v", err)
	}
	contract := bundle.Contracts["message:compute.network_audit_evidence.v1"]
	if contract.DescriptorSetRef != "descriptors/message.pb" {
		t.Fatalf("descriptorSetRef = %q", contract.DescriptorSetRef)
	}
	if contract.ProtoPackage != "workflow_plugin_compute_core.protocol.v1" {
		t.Fatalf("protoPackage = %q", contract.ProtoPackage)
	}
	if len(contract.MessageNames) != 2 || contract.MessageNames[0] != "NetworkAuditRecord" {
		t.Fatalf("messageNames = %v", contract.MessageNames)
	}
	if contract.ProtocolVersion != "compute.v1alpha1" {
		t.Fatalf("protocolVersion = %q", contract.ProtocolVersion)
	}
}

func TestRunEditorBundleRejectsMalformedPluginContractDescriptors(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "workflow-plugin-bad-contracts")
	if err := os.Mkdir(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"workflow-plugin-bad-contracts"}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(`{"contracts": [`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}

	err := runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir})
	if err == nil {
		t.Fatal("expected malformed plugin.contracts.json to fail")
	}
	if !strings.Contains(err.Error(), "invalid_plugin_contract_descriptors") {
		t.Fatalf("error = %v, want invalid_plugin_contract_descriptors", err)
	}
}

func TestRunEditorBundleRejectsMalformedServiceMethodContract(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "workflow-plugin-bad-service")
	if err := os.Mkdir(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"workflow-plugin-bad-service"}`), 0644); err != nil {
		t.Fatalf("write plugin manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(`{
  "version": "v1",
  "contracts": [
    {
      "kind": "service_method",
      "moduleType": "module.bad",
      "serviceName": "BadService",
      "mode": "strict",
      "input": "workflow.bad.Input",
      "output": "workflow.bad.Output"
    }
  ]
}`), 0644); err != nil {
		t.Fatalf("write plugin contracts: %v", err)
	}

	err := runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir})
	if err == nil {
		t.Fatal("expected malformed service_method descriptor to fail")
	}
	if !strings.Contains(err.Error(), "malformed service_method") {
		t.Fatalf("error = %v, want malformed service_method", err)
	}
}

func TestRunEditorBundleRejectsInvalidMessageContractDescriptors(t *testing.T) {
	cases := map[string]struct {
		contracts string
		want      string
	}{
		"deterministic missing field order": {
			contracts: `{
  "version": "v1",
  "contracts": [
    {
      "kind": "message",
      "mode": "strict",
      "protoPackage": "",
      "messageNames": [],
      "schemaDigest": "",
      "protocolVersion": ""
    }
  ]
}`,
			want: "message contract missing contractType",
		},
		"blank message name": {
			contracts: `{
  "version": "v1",
  "contracts": [
    {
      "kind": "message",
      "contractType": "message.bad",
      "mode": "strict",
      "protoPackage": "workflow.bad",
      "messageNames": [" "],
      "schemaDigest": "sha256:placeholder",
      "protocolVersion": "v1"
    }
  ]
}`,
			want: "message contract missing messageNames",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			pluginDir := filepath.Join(dir, "workflow-plugin-bad-message")
			if err := os.Mkdir(pluginDir, 0755); err != nil {
				t.Fatalf("mkdir plugin dir: %v", err)
			}
			if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"workflow-plugin-bad-message"}`), 0644); err != nil {
				t.Fatalf("write plugin manifest: %v", err)
			}
			if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(tc.contracts), 0644); err != nil {
				t.Fatalf("write plugin contracts: %v", err)
			}

			err := runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir})
			if err == nil {
				t.Fatal("expected invalid message descriptor to fail")
			}
			if !strings.Contains(err.Error(), "invalid_message_contract_descriptor") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want invalid_message_contract_descriptor and %q", err, tc.want)
			}
		})
	}
}

func TestRunEditorBundleFailsWhenDSLReferenceCannotLoad(t *testing.T) {
	orig := loadEditorBundleDSLReferenceFunc
	t.Cleanup(func() { loadEditorBundleDSLReferenceFunc = orig })
	loadEditorBundleDSLReferenceFunc = func() (*DSLReferenceOutput, error) {
		return nil, errors.New("broken reference")
	}

	err := runEditorBundle([]string{"--registry=false"})
	if err == nil {
		t.Fatal("expected DSL reference load failure")
	}
	if !strings.Contains(err.Error(), "load DSL reference: broken reference") {
		t.Fatalf("error = %v, want DSL reference context", err)
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

func TestRunEditorBundleFailsWhenRegistryManifestFetchFails(t *testing.T) {
	origList := listEditorBundleRegistryPluginNames
	origFetch := fetchEditorBundleRegistryManifest
	t.Cleanup(func() {
		listEditorBundleRegistryPluginNames = origList
		fetchEditorBundleRegistryManifest = origFetch
	})
	listEditorBundleRegistryPluginNames = func() ([]string, error) {
		return []string{"workflow-plugin-bad-registry"}, nil
	}
	fetchEditorBundleRegistryManifest = func(name string) (*RegistryManifest, error) {
		return nil, os.ErrNotExist
	}

	err := runEditorBundle(nil)
	if err == nil {
		t.Fatal("expected registry fetch failure")
	}
	if !strings.Contains(err.Error(), "workflow-plugin-bad-registry") {
		t.Fatalf("error = %v, want plugin name", err)
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
    },
    {
      "kind": "message",
      "contractType": "message.one",
      "mode": "strict",
      "protoPackage": "workflow.one",
      "messageNames": ["Event"],
      "schemaDigest": "sha256:one",
      "protocolVersion": "v1",
      "descriptorSetRef": "proto/message-one.pb"
    },
    {
      "kind": "message",
      "contractType": "message.two",
      "mode": "strict",
      "protoPackage": "workflow.two",
      "messageNames": ["Event"],
      "schemaDigest": "sha256:two",
      "protocolVersion": "v1",
      "descriptorSetRef": "proto/message-two.pb"
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
	if got := bundle.Contracts["message:message.one"].DescriptorSetRef; got != "proto/message-one.pb" {
		t.Fatalf("message.one descriptorSetRef = %q", got)
	}
	if got := bundle.Contracts["message:message.two"].DescriptorSetRef; got != "proto/message-two.pb" {
		t.Fatalf("message.two descriptorSetRef = %q", got)
	}
	if got := bundle.Messages["workflow.one.Event"].DescriptorSetRef; got != "proto/message-one.pb" {
		t.Fatalf("workflow.one.Event descriptorSetRef = %q", got)
	}
	if got := bundle.Messages["workflow.two.Event"].DescriptorSetRef; got != "proto/message-two.pb" {
		t.Fatalf("workflow.two.Event descriptorSetRef = %q", got)
	}
	if bundle.DescriptorSets["proto/one.pb"].ExternalRef != "proto/one.pb" {
		t.Fatalf("descriptor set one reference missing: %+v", bundle.DescriptorSets)
	}
	if bundle.DescriptorSets["proto/two.pb"].ExternalRef != "proto/two.pb" {
		t.Fatalf("descriptor set two reference missing: %+v", bundle.DescriptorSets)
	}
}
