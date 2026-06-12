package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const controlPlaneModulePath = "github.com/GoCodeAlone/workflow-plugin-control-plane"

func TestControlPlaneReleasedModuleFixture(t *testing.T) {
	moduleDir := controlPlaneReleasedModuleDir(t)

	goMod, err := os.ReadFile(filepath.Join(moduleDir, "go.mod"))
	if err != nil {
		t.Fatalf("read released module go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module "+controlPlaneModulePath) {
		t.Fatalf("released module go.mod missing module path %q", controlPlaneModulePath)
	}
	for _, rel := range []string{
		"plugin.json",
		"plugin.contracts.json",
		"descriptorsets/control_plane.binpb",
	} {
		if _, err := os.Stat(filepath.Join(moduleDir, rel)); err != nil {
			t.Fatalf("released module missing %s: %v", rel, err)
		}
	}
}

func TestControlPlaneDescriptorBundle(t *testing.T) {
	moduleDir := controlPlaneReleasedModuleDir(t)
	outPath := filepath.Join(t.TempDir(), "editor-bundle.json")

	if err := runEditorBundle([]string{"--registry=false", "--plugin-dir", moduleDir, "--output", outPath}); err != nil {
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

	for _, contractType := range []string{
		"control_plane.descriptors.v1alpha1",
		"control_plane.envelopes.v1alpha1",
		"control_plane.registry.v1alpha1",
	} {
		contractID := "message:" + contractType
		contract, ok := bundle.Contracts[contractID]
		if !ok {
			t.Fatalf("bundle missing contract %s", contractID)
		}
		if contract.ProtocolVersion != "control-plane.v1alpha1" {
			t.Fatalf("%s protocolVersion = %q", contractID, contract.ProtocolVersion)
		}
		if contract.DescriptorSetRef != "descriptorsets/control_plane.binpb" {
			t.Fatalf("%s descriptorSetRef = %q", contractID, contract.DescriptorSetRef)
		}
	}

	for _, messageName := range []string{
		"workflow_plugin_control_plane.descriptors.v1alpha1.RouteActionDescriptor",
		"workflow_plugin_control_plane.envelopes.v1alpha1.ControlPlaneEnvelope",
		"workflow_plugin_control_plane.registry.v1alpha1.DescriptorRegistration",
	} {
		message, ok := bundle.Messages[messageName]
		if !ok {
			t.Fatalf("bundle missing message metadata %s", messageName)
		}
		if message.DescriptorSetRef != "descriptorsets/control_plane.binpb" {
			t.Fatalf("%s descriptorSetRef = %q", messageName, message.DescriptorSetRef)
		}
	}

	if bundle.DescriptorSets["descriptorsets/control_plane.binpb"].ExternalRef != "descriptorsets/control_plane.binpb" {
		t.Fatalf("descriptor set reference missing: %+v", bundle.DescriptorSets)
	}
}

func TestControlPlaneDescriptorBundleRejectsInvalidSchemaDigest(t *testing.T) {
	moduleDir := controlPlaneReleasedModuleDir(t)
	pluginDir := filepath.Join(t.TempDir(), "workflow-plugin-control-plane")
	if err := os.MkdirAll(filepath.Join(pluginDir, "descriptorsets"), 0755); err != nil {
		t.Fatalf("mkdir fixture plugin dir: %v", err)
	}
	copyControlPlaneFixtureFile(t, moduleDir, pluginDir, "plugin.json")
	copyControlPlaneFixtureFile(t, moduleDir, pluginDir, "descriptorsets/control_plane.binpb")

	contracts, err := os.ReadFile(filepath.Join(moduleDir, "plugin.contracts.json"))
	if err != nil {
		t.Fatalf("read released plugin contracts: %v", err)
	}
	corrupted := strings.Replace(string(contracts), `"schemaDigest": "sha256:aa889aa79d7e571b9bac757c1b41858a6140a02d6896fe221f041b8ced608842"`, `"schemaDigest": ""`, 1)
	if corrupted == string(contracts) {
		t.Fatal("failed to corrupt released schemaDigest fixture")
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), []byte(corrupted), 0644); err != nil {
		t.Fatalf("write corrupted plugin contracts: %v", err)
	}

	err = runEditorBundle([]string{"--registry=false", "--plugin-dir", pluginDir})
	if err == nil {
		t.Fatal("expected invalid schemaDigest to fail")
	}
	if !strings.Contains(err.Error(), "invalid_message_contract_descriptor") {
		t.Fatalf("error = %v, want invalid_message_contract_descriptor", err)
	}
}

func controlPlaneReleasedModuleDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", controlPlaneModulePath)
	cmd.Env = append(os.Environ(), "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("resolve released control-plane module: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	moduleDir := strings.TrimSpace(string(out))
	if moduleDir == "" {
		t.Fatal("released control-plane module dir is empty")
	}
	return moduleDir
}

func copyControlPlaneFixtureFile(t *testing.T, srcDir, dstDir, rel string) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(srcDir, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, rel), data, 0644); err != nil {
		t.Fatalf("write fixture %s: %v", rel, err)
	}
}
