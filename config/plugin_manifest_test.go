package config

import (
	"encoding/json"
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
