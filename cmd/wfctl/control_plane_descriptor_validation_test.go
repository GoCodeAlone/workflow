package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cpdescriptors "github.com/GoCodeAlone/workflow-plugin-control-plane/descriptors"
	cpdescriptorspb "github.com/GoCodeAlone/workflow-plugin-control-plane/descriptors/pb"
	cpenvelopes "github.com/GoCodeAlone/workflow-plugin-control-plane/envelopes"
	cpenvelopespb "github.com/GoCodeAlone/workflow-plugin-control-plane/envelopes/pb"
	cpregistry "github.com/GoCodeAlone/workflow-plugin-control-plane/registry"
	cpregistrypb "github.com/GoCodeAlone/workflow-plugin-control-plane/registry/pb"
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
	corrupted := corruptFirstMessageContractSchemaDigest(t, contracts)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.contracts.json"), corrupted, 0644); err != nil {
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

func TestControlPlaneReleasedValidatorsRejectInvalidInputs(t *testing.T) {
	cases := []struct {
		name     string
		validate func() error
	}{
		{
			name: "route action schema digest malformed",
			validate: func() error {
				descriptor := validControlPlaneRouteActionDescriptor()
				descriptor.InputSchemaDigest = "sha256:not-hex"
				return cpdescriptors.ValidateRouteActionDescriptor(descriptor)
			},
		},
		{
			name: "registry raw network provenance ref",
			validate: func() error {
				registration := validControlPlaneRegistration()
				registration.ProvenanceSource = "https://example.test/release"
				return cpregistry.ValidateDescriptorRegistration(registration)
			},
		},
		{
			name: "registry empty downgrade floor version",
			validate: func() error {
				registration := validControlPlaneRegistration()
				registration.DowngradeFloorVersion = ""
				return cpregistry.ValidateDescriptorRegistration(registration)
			},
		},
		{
			name: "registry stale revocation freshness",
			validate: func() error {
				registration := validControlPlaneRegistration()
				registration.RevocationFreshUntilUnixNano = registration.ValidatedAtUnixNano - 1
				return cpregistry.ValidateDescriptorRegistration(registration)
			},
		},
		{
			name: "envelope raw tenant handle",
			validate: func() error {
				envelope := validControlPlaneEnvelope()
				envelope.TenantHandle = "tenant:customer-1"
				return cpenvelopes.ValidateEnvelope(envelope)
			},
		},
		{
			name: "envelope raw actor handle",
			validate: func() error {
				envelope := validControlPlaneEnvelope()
				envelope.ActorHandle = "person@example.test"
				return cpenvelopes.ValidateEnvelope(envelope)
			},
		},
		{
			name: "envelope raw resource handle",
			validate: func() error {
				envelope := validControlPlaneEnvelope()
				envelope.ResourceHandle = "customer:resource-1"
				return cpenvelopes.ValidateEnvelope(envelope)
			},
		},
		{
			name: "provider handoff input schema digest mismatch",
			validate: func() error {
				descriptor := validControlPlaneRouteActionDescriptor()
				descriptor.ProviderHandoff.InputSchemaDigest = "sha256:not64"
				return cpdescriptors.ValidateRouteActionDescriptor(descriptor)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.validate(); err == nil {
				t.Fatal("expected released validator to reject invalid input")
			}
		})
	}
}

func TestControlPlaneDescriptorValidationDoesNotEnterRuntimeDeps(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/wfctl")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = controlPlaneCommandEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list wfctl deps: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if strings.Contains(string(out), controlPlaneModulePath) {
		t.Fatalf("non-test wfctl deps include %s", controlPlaneModulePath)
	}
}

func controlPlaneReleasedModuleDir(t *testing.T) string {
	t.Helper()

	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", controlPlaneModulePath)
	cmd.Env = controlPlaneCommandEnv()
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

func controlPlaneCommandEnv() []string {
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GOWORK=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOWORK=off")
}

func corruptFirstMessageContractSchemaDigest(t *testing.T, data []byte) []byte {
	t.Helper()

	var file pluginContractDescriptorFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse released plugin contracts: %v", err)
	}
	for i := range file.Contracts {
		if normalizePluginContractKind(file.Contracts[i].Kind) != "message" {
			continue
		}
		file.Contracts[i].SchemaDigest = ""
		corrupted, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			t.Fatalf("marshal corrupted plugin contracts: %v", err)
		}
		return append(corrupted, '\n')
	}
	t.Fatal("released plugin contracts have no message contract to corrupt")
	return nil
}

func validControlPlaneRouteActionDescriptor() *cpdescriptorspb.RouteActionDescriptor {
	return &cpdescriptorspb.RouteActionDescriptor{
		ProtocolVersion:   cpdescriptors.Version,
		DescriptorId:      "descriptor.admin.deploy",
		OperationId:       "deploy_app",
		RouteHandle:       "route://admin/deploy-app",
		AuthClassRef:      "authclass://admin/deploy",
		AuditClassRef:     "auditclass://deployment/apply",
		InputSchemaDigest: controlPlaneDigest("a"),
		ProviderHandoff: &cpdescriptorspb.ProviderHandoffRef{
			ProviderPluginId:      "workflow-plugin-digitalocean",
			ProviderPluginVersion: "v2.0.15",
			CapabilityId:          "app-platform",
			CapabilityVersion:     "v1",
			InputSchemaDigest:     controlPlaneDigest("b"),
			ActionNonce:           "nonce-001",
			IdempotencyKey:        "idem-001",
		},
		Admin: &cpdescriptorspb.AdminContribution{
			ResourceRef:                      "resource://app-platform/service",
			ActionClassRef:                   "actionclass://deployment/apply",
			RiskRef:                          "risk://destructive",
			SeverityRef:                      "severity://high",
			PermissionExplanationTemplateRef: "template://permission/deployment",
			RenderRef:                        "render://admin/action-form",
		},
	}
}

func validControlPlaneEnvelope() *cpenvelopespb.ControlPlaneEnvelope {
	return &cpenvelopespb.ControlPlaneEnvelope{
		ProtocolVersion:    cpenvelopes.Version,
		EnvelopeId:         "evt-001",
		Kind:               "audit",
		TenantHandle:       "opaque-tenant-001",
		ActorHandle:        "opaque-actor-001",
		ResourceHandle:     "opaque-resource-001",
		CorrelationId:      "corr-001",
		OccurredAtUnixNano: 100,
		ObservedAtUnixNano: 200,
		Retention: &cpenvelopespb.RetentionMetadata{
			RedactionState:             "active",
			RetentionExpiresAtUnixNano: 1000,
			CorrelationRekeyId:         "rekey-001",
		},
	}
}

func validControlPlaneRegistration() *cpregistrypb.DescriptorRegistration {
	return &cpregistrypb.DescriptorRegistration{
		ProtocolVersion:              cpregistry.Version,
		DescriptorDigest:             controlPlaneDigest("c"),
		SchemaDigest:                 controlPlaneDigest("d"),
		PluginId:                     "workflow-plugin-control-plane",
		PluginVersion:                "v0.1.0",
		PluginPackageDigest:          controlPlaneDigest("e"),
		SigningRootRef:               "trustroot://workflow/control-plane",
		ProvenanceSource:             "provenance://github/release",
		AllowlistEpoch:               1,
		DowngradeFloorVersion:        "v0.1.0",
		RevocationFreshUntilUnixNano: 300,
		FetchedAtUnixNano:            100,
		ValidatedAtUnixNano:          200,
		ParserCompatibilityVersion:   "v1",
		TrustRootGeneration:          1,
	}
}

func controlPlaneDigest(ch string) string {
	return "sha256:" + strings.Repeat(ch, 64)
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
