package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPluginManifestJSONRoundTrip(t *testing.T) {
	raw := `{
		"name": "workflow-plugin-agent",
		"version": "0.5.2",
		"description": "AI agent plugin",
		"capabilities": {
			"moduleTypes": ["agent.runner"],
			"stepTypes": ["step.agent_execute"],
			"triggerTypes": []
		},
		"moduleInfraRequirements": {
			"agent.runner": {
				"requires": [
					{
						"type": "database",
						"name": "agent-db",
						"description": "SQLite or Postgres for agent memory",
						"dockerImage": "postgres:16",
						"ports": [5432],
						"secrets": ["DATABASE_URL"],
						"providers": ["aws", "gcp"],
						"optional": false
					}
				]
			}
		}
	}`

	var manifest PluginManifestFile
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}

	if manifest.Name != "workflow-plugin-agent" {
		t.Errorf("Name: got %q, want %q", manifest.Name, "workflow-plugin-agent")
	}
	if manifest.Version != "0.5.2" {
		t.Errorf("Version: got %q, want %q", manifest.Version, "0.5.2")
	}
	if len(manifest.Capabilities.ModuleTypes) != 1 || manifest.Capabilities.ModuleTypes[0] != "agent.runner" {
		t.Errorf("ModuleTypes: got %v", manifest.Capabilities.ModuleTypes)
	}
	if len(manifest.Capabilities.StepTypes) != 1 || manifest.Capabilities.StepTypes[0] != "step.agent_execute" {
		t.Errorf("StepTypes: got %v", manifest.Capabilities.StepTypes)
	}

	spec, ok := manifest.ModuleInfraRequirements["agent.runner"]
	if !ok {
		t.Fatal("expected agent.runner in ModuleInfraRequirements")
	}
	if len(spec.Requires) != 1 {
		t.Fatalf("Requires len: got %d, want 1", len(spec.Requires))
	}
	req := spec.Requires[0]
	if req.Type != "database" {
		t.Errorf("Type: got %q, want database", req.Type)
	}
	if req.Name != "agent-db" {
		t.Errorf("Name: got %q, want agent-db", req.Name)
	}
	if req.DockerImage != "postgres:16" {
		t.Errorf("DockerImage: got %q, want postgres:16", req.DockerImage)
	}
	if len(req.Ports) != 1 || req.Ports[0] != 5432 {
		t.Errorf("Ports: got %v", req.Ports)
	}
	if len(req.Secrets) != 1 || req.Secrets[0] != "DATABASE_URL" {
		t.Errorf("Secrets: got %v", req.Secrets)
	}
	if len(req.Providers) != 2 {
		t.Errorf("Providers: got %v", req.Providers)
	}
	if req.Optional {
		t.Error("Optional should be false")
	}

	// Round-trip through JSON
	out, err := json.Marshal(&manifest)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var manifest2 PluginManifestFile
	if err := json.Unmarshal(out, &manifest2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if manifest2.Name != manifest.Name {
		t.Errorf("round-trip Name: got %q", manifest2.Name)
	}
}

func TestPluginManifestYAMLRoundTrip(t *testing.T) {
	raw := `
name: workflow-plugin-payments
version: "0.2.1"
description: Payment processing plugin
capabilities:
  moduleTypes:
    - payments.provider
  stepTypes:
    - step.payment_charge
  triggerTypes: []
moduleInfraRequirements:
  payments.provider:
    requires:
      - type: cache
        name: payment-cache
        description: Redis cache for idempotency
        dockerImage: redis:7
        ports:
          - 6379
        secrets:
          - REDIS_URL
        optional: true
`

	var manifest PluginManifestFile
	if err := yaml.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}

	if manifest.Name != "workflow-plugin-payments" {
		t.Errorf("Name: got %q", manifest.Name)
	}
	spec, ok := manifest.ModuleInfraRequirements["payments.provider"]
	if !ok {
		t.Fatal("expected payments.provider in ModuleInfraRequirements")
	}
	if len(spec.Requires) != 1 {
		t.Fatalf("Requires len: got %d", len(spec.Requires))
	}
	req := spec.Requires[0]
	if req.Type != "cache" {
		t.Errorf("Type: got %q", req.Type)
	}
	if !req.Optional {
		t.Error("Optional should be true")
	}
	if req.DockerImage != "redis:7" {
		t.Errorf("DockerImage: got %q", req.DockerImage)
	}
}

func TestPluginManifestNoInfraRequirements(t *testing.T) {
	raw := `{"name":"minimal","version":"1.0.0","description":"no infra","capabilities":{"moduleTypes":[],"stepTypes":[],"triggerTypes":[]}}`
	var manifest PluginManifestFile
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if manifest.ModuleInfraRequirements != nil {
		t.Errorf("expected nil ModuleInfraRequirements, got %v", manifest.ModuleInfraRequirements)
	}
}

func TestProviderManifestRoundTripConfig(t *testing.T) {
	_ = PluginManifestFile{CredentialSources: []CredentialSourceDecl{{Source: "literal-api"}}}
	raw := []byte(`{
		"name":"provider-plugin","version":"1.0.0","description":"provider declarations","capabilities":{"moduleTypes":[],"stepTypes":[],"triggerTypes":[]},
		"credentialSources":[{"source":"digitalocean.spaces","concurrencyMode":"provider_idempotent","outputs":[{"key":"accessKeyId","sensitive":false},{"key":"secretAccessKey"}],"identifierKey":"accessKeyId"}],
		"credentialResolvers":[{"provider":"aws","credentialTypes":["static","env"]}],
		"kubernetesBackends":[{"name":"gke","resourceType":"infra.gke_cluster"}],
		"containerRegistries":[{"type":"ghcr","operations":["login","logout","push","prune"]}],
		"secretStores":[{"type":"aws-secrets-manager","operations":["get","list","stat_all","check_access"],"scopes":["account","region"]}],
		"consumesContracts":[{"id":"workflow.provider.credential-issuer","protocol":{"min":1,"max":2}}]
	}`)
	var manifest PluginManifestFile
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("unmarshal provider manifest: %v", err)
	}
	out, err := json.Marshal(&manifest)
	if err != nil {
		t.Fatalf("marshal provider manifest: %v", err)
	}
	assertProviderManifestFieldsPreserved(t, raw, out)
}

func assertProviderManifestFieldsPreserved(t *testing.T, wantJSON, gotJSON []byte) {
	t.Helper()
	var want, got map[string]json.RawMessage
	if err := json.Unmarshal(wantJSON, &want); err != nil {
		t.Fatalf("decode expected manifest: %v", err)
	}
	if err := json.Unmarshal(gotJSON, &got); err != nil {
		t.Fatalf("decode round-trip manifest: %v", err)
	}
	for _, field := range []string{"credentialSources", "credentialResolvers", "kubernetesBackends", "containerRegistries", "secretStores", "consumesContracts"} {
		var wantValue, gotValue any
		if err := json.Unmarshal(want[field], &wantValue); err != nil {
			t.Fatalf("decode expected %s: %v", field, err)
		}
		if err := json.Unmarshal(got[field], &gotValue); err != nil {
			t.Fatalf("decode round-trip %s: %v", field, err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("%s not preserved: got %s, want %s", field, got[field], want[field])
		}
	}
}

func TestProviderDeclarationsValidate(t *testing.T) {
	valid := func() ProviderDeclarations {
		return ProviderDeclarations{
			CredentialSources: []CredentialSourceDecl{{
				Source:          "digitalocean.spaces",
				ConcurrencyMode: CredentialConcurrencyProviderIdempotent,
				Outputs:         []CredentialOutputDecl{{Key: "accessKeyId"}, {Key: "secretAccessKey"}},
				IdentifierKey:   "accessKeyId",
			}},
			CredentialResolvers: []CredentialResolverDecl{{Provider: "aws", CredentialTypes: []string{"static", "env"}}},
			KubernetesBackends:  []KubernetesBackendDecl{{Name: "gke", ResourceType: "infra.gke_cluster"}},
			ContainerRegistries: []ContainerRegistryDecl{{Type: "ghcr", Operations: []string{"login", "logout", "push", "prune"}}},
			SecretStores:        []SecretStoreDecl{{Type: "aws-secrets-manager", Operations: []string{"get", "list", "stat_all", "check_access"}, Scopes: []string{"account", "region"}}},
			ConsumesContracts:   []ConsumedContractDecl{{ID: "workflow.provider.credential-issuer", Protocol: ProtocolVersionRange{Min: 1, Max: 2}}},
		}
	}

	tests := []struct {
		name   string
		modify func(*ProviderDeclarations)
		want   string
	}{
		{"duplicate credential source", func(d *ProviderDeclarations) {
			d.CredentialSources = append(d.CredentialSources, d.CredentialSources[0])
		}, "duplicate credential source"},
		{"empty credential source", func(d *ProviderDeclarations) { d.CredentialSources[0].Source = "" }, "credential source is required"},
		{"missing concurrency mode", func(d *ProviderDeclarations) { d.CredentialSources[0].ConcurrencyMode = "" }, "concurrency mode"},
		{"unknown concurrency mode", func(d *ProviderDeclarations) { d.CredentialSources[0].ConcurrencyMode = "parallel" }, "concurrency mode"},
		{"missing outputs", func(d *ProviderDeclarations) { d.CredentialSources[0].Outputs = nil }, "output is required"},
		{"empty output key", func(d *ProviderDeclarations) { d.CredentialSources[0].Outputs[0].Key = "" }, "output key is required"},
		{"duplicate output key", func(d *ProviderDeclarations) {
			d.CredentialSources[0].Outputs = append(d.CredentialSources[0].Outputs, d.CredentialSources[0].Outputs[0])
		}, "duplicate output"},
		{"missing identifier output", func(d *ProviderDeclarations) { d.CredentialSources[0].IdentifierKey = "" }, "identifierKey"},
		{"unknown identifier output", func(d *ProviderDeclarations) { d.CredentialSources[0].IdentifierKey = "credentialID" }, "identifierKey"},
		{"duplicate resolver provider", func(d *ProviderDeclarations) {
			d.CredentialResolvers = append(d.CredentialResolvers, d.CredentialResolvers[0])
		}, "duplicate credential resolver"},
		{"empty resolver provider", func(d *ProviderDeclarations) { d.CredentialResolvers[0].Provider = "" }, "resolver provider is required"},
		{"unknown resolver provider", func(d *ProviderDeclarations) { d.CredentialResolvers[0].Provider = "digitalocean" }, "unsupported resolver provider"},
		{"missing resolver credential types", func(d *ProviderDeclarations) { d.CredentialResolvers[0].CredentialTypes = nil }, "credential type is required"},
		{"duplicate resolver credential type", func(d *ProviderDeclarations) { d.CredentialResolvers[0].CredentialTypes = []string{"env", "env"} }, "duplicate credential type"},
		{"unknown resolver credential type", func(d *ProviderDeclarations) {
			d.CredentialResolvers[0].CredentialTypes = []string{"application_default"}
		}, "unsupported credential type"},
		{"reserved kind backend", func(d *ProviderDeclarations) { d.KubernetesBackends[0].Name = "kind" }, "reserved"},
		{"reserved k3s backend", func(d *ProviderDeclarations) { d.KubernetesBackends[0].Name = "k3s" }, "reserved"},
		{"reserved kind backend with surrounding whitespace", func(d *ProviderDeclarations) { d.KubernetesBackends[0].Name = " kind " }, "reserved"},
		{"reserved k3s backend with surrounding whitespace", func(d *ProviderDeclarations) { d.KubernetesBackends[0].Name = "k3s " }, "reserved"},
		{"duplicate backend", func(d *ProviderDeclarations) {
			d.KubernetesBackends = append(d.KubernetesBackends, d.KubernetesBackends[0])
		}, "duplicate kubernetes backend"},
		{"canonical duplicate backend", func(d *ProviderDeclarations) {
			d.KubernetesBackends = []KubernetesBackendDecl{
				{Name: "foo", ResourceType: "infra.foo"},
				{Name: " foo ", ResourceType: "infra.foo.v2"},
			}
		}, "duplicate kubernetes backend"},
		{"empty backend name", func(d *ProviderDeclarations) { d.KubernetesBackends[0].Name = "" }, "backend name is required"},
		{"empty backend resource type", func(d *ProviderDeclarations) { d.KubernetesBackends[0].ResourceType = "" }, "resourceType is required"},
		{"backend resource type with surrounding whitespace", func(d *ProviderDeclarations) { d.KubernetesBackends[0].ResourceType = " infra.gke_cluster " }, "surrounding whitespace"},
		{"duplicate registry", func(d *ProviderDeclarations) {
			d.ContainerRegistries = append(d.ContainerRegistries, d.ContainerRegistries[0])
		}, "duplicate container registry"},
		{"empty registry type", func(d *ProviderDeclarations) { d.ContainerRegistries[0].Type = "" }, "registry type is required"},
		{"empty registry operations", func(d *ProviderDeclarations) { d.ContainerRegistries[0].Operations = nil }, "registry operation is required"},
		{"duplicate registry operation", func(d *ProviderDeclarations) { d.ContainerRegistries[0].Operations = []string{"login", "login"} }, "duplicate container registry operation"},
		{"unknown registry operation", func(d *ProviderDeclarations) { d.ContainerRegistries[0].Operations = []string{"delete"} }, "unsupported container registry operation"},
		{"duplicate secret store", func(d *ProviderDeclarations) { d.SecretStores = append(d.SecretStores, d.SecretStores[0]) }, "duplicate secret store"},
		{"empty secret store type", func(d *ProviderDeclarations) { d.SecretStores[0].Type = "" }, "secret store type is required"},
		{"empty secret store operations", func(d *ProviderDeclarations) { d.SecretStores[0].Operations = nil }, "secret store operation is required"},
		{"duplicate secret store operation", func(d *ProviderDeclarations) { d.SecretStores[0].Operations = []string{"get", "get"} }, "duplicate secret store operation"},
		{"mutation secret store operation", func(d *ProviderDeclarations) { d.SecretStores[0].Operations = []string{"put"} }, "unsupported secret store operation"},
		{"empty secret store scopes", func(d *ProviderDeclarations) { d.SecretStores[0].Scopes = nil }, "secret store scope is required"},
		{"empty secret store scope", func(d *ProviderDeclarations) { d.SecretStores[0].Scopes = []string{""} }, "secret store scope is required"},
		{"duplicate secret store scope", func(d *ProviderDeclarations) { d.SecretStores[0].Scopes = []string{"account", "account"} }, "duplicate secret store scope"},
		{"duplicate consumed contract", func(d *ProviderDeclarations) {
			d.ConsumesContracts = append(d.ConsumesContracts, d.ConsumesContracts[0])
		}, "duplicate consumed contract"},
		{"empty consumed contract id", func(d *ProviderDeclarations) { d.ConsumesContracts[0].ID = "" }, "contract id is required"},
		{"zero protocol minimum", func(d *ProviderDeclarations) { d.ConsumesContracts[0].Protocol.Min = 0 }, "protocol minimum"},
		{"zero protocol maximum", func(d *ProviderDeclarations) { d.ConsumesContracts[0].Protocol.Max = 0 }, "protocol maximum"},
		{"inverted protocol range", func(d *ProviderDeclarations) { d.ConsumesContracts[0].Protocol = ProtocolVersionRange{Min: 3, Max: 2} }, "protocol range"},
	}

	if err := valid().Validate(); err != nil {
		t.Fatalf("valid declarations rejected: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			declarations := valid()
			tt.modify(&declarations)
			err := declarations.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestCredentialOutputDeclSensitiveDefault(t *testing.T) {
	falseValue := false
	trueValue := true
	tests := []struct {
		name string
		decl CredentialOutputDecl
		want bool
	}{
		{"omitted defaults sensitive", CredentialOutputDecl{Key: "secret"}, true},
		{"explicit false", CredentialOutputDecl{Key: "public-id", Sensitive: &falseValue}, false},
		{"explicit true", CredentialOutputDecl{Key: "secret", Sensitive: &trueValue}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.decl.IsSensitive(); got != tt.want {
				t.Fatalf("IsSensitive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPluginManifestRequirementV2YAML(t *testing.T) {
	raw := `
name: workflow-plugin-observability
version: "0.1.2"
description: Observability plugin
capabilities:
  moduleTypes:
    - observability.telemetry
  stepTypes: []
  triggerTypes: []
moduleInfraRequirementsV2:
  observability.telemetry:
    requires:
      - key: observability.telemetry.default
        kind: observability
        source: observability.telemetry
        resourceTypeHint: infra.container_service
        environment: production
        runtimes:
          - kubernetes
          - digitalocean_app_platform
        telemetrySignals:
          - traces
          - metrics
          - logs
        observabilityBackends:
          - otel
          - datadog
        deploymentModes:
          - sidecar
          - sibling_service
        vendorFeatures:
          - datadog.apm
        parameters:
          collector: otel
`

	var manifest PluginManifestFile
	if err := yaml.Unmarshal([]byte(raw), &manifest); err != nil {
		t.Fatalf("unmarshal YAML: %v", err)
	}
	spec := manifest.ModuleInfraRequirementsV2["observability.telemetry"]
	if spec == nil {
		t.Fatal("expected observability.telemetry v2 infra requirements")
	}
	if len(spec.Requires) != 1 {
		t.Fatalf("Requires len = %d, want 1", len(spec.Requires))
	}
	req := spec.Requires[0]
	if req.Key != "observability.telemetry.default" {
		t.Fatalf("Key = %q", req.Key)
	}
	if req.Kind != "observability" {
		t.Fatalf("Kind = %q", req.Kind)
	}
	if len(req.TelemetrySignals) != 3 {
		t.Fatalf("TelemetrySignals = %v", req.TelemetrySignals)
	}
	if req.Parameters["collector"] != "otel" {
		t.Fatalf("Parameters = %v", req.Parameters)
	}
}
