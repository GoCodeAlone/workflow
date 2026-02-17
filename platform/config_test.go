package platform

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPlatformConfig_YAMLRoundTrip(t *testing.T) {
	original := &PlatformConfig{
		Org:         "acme-corp",
		Environment: "production",
		Provider: ProviderConfig{
			Name:   "aws",
			Config: map[string]any{"region": "us-east-1"},
		},
		Tiers: TiersConfig{
			Infrastructure: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "primary-vpc", Type: "network", Properties: map[string]any{"cidr": "10.0.0.0/16"}},
				},
				ConstraintsForDownstream: []ConstraintConfig{
					{Field: "memory", Operator: "<=", Value: "4Gi"},
				},
			},
			Application: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "api-service", Type: "container_runtime", DependsOn: []string{"shared-postgres"}},
				},
			},
		},
		Execution: ExecutionConfig{
			Tier1Mode:              "plan_and_approve",
			Tier3Mode:              "auto_apply",
			ReconciliationInterval: "5m",
			LockTimeout:            "10m",
		},
	}

	data, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PlatformConfig
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Org != original.Org {
		t.Errorf("Org: got %q, want %q", decoded.Org, original.Org)
	}
	if decoded.Environment != original.Environment {
		t.Errorf("Environment: got %q, want %q", decoded.Environment, original.Environment)
	}
	if decoded.Provider.Name != original.Provider.Name {
		t.Errorf("Provider.Name: got %q, want %q", decoded.Provider.Name, original.Provider.Name)
	}
	if decoded.Provider.Config["region"] != "us-east-1" {
		t.Errorf("Provider.Config[region]: got %v, want us-east-1", decoded.Provider.Config["region"])
	}
	if len(decoded.Tiers.Infrastructure.Capabilities) != 1 {
		t.Fatalf("Infrastructure capabilities count: got %d, want 1", len(decoded.Tiers.Infrastructure.Capabilities))
	}
	if decoded.Tiers.Infrastructure.Capabilities[0].Name != "primary-vpc" {
		t.Errorf("Infrastructure cap name: got %q, want %q", decoded.Tiers.Infrastructure.Capabilities[0].Name, "primary-vpc")
	}
	if len(decoded.Tiers.Application.Capabilities[0].DependsOn) != 1 {
		t.Fatalf("Application cap dependsOn count: got %d, want 1", len(decoded.Tiers.Application.Capabilities[0].DependsOn))
	}
	if decoded.Tiers.Application.Capabilities[0].DependsOn[0] != "shared-postgres" {
		t.Errorf("DependsOn[0]: got %q, want %q", decoded.Tiers.Application.Capabilities[0].DependsOn[0], "shared-postgres")
	}
}

func TestParsePlatformConfig_Full3Tier(t *testing.T) {
	raw := map[string]any{
		"org":         "acme-corp",
		"environment": "production",
		"provider": map[string]any{
			"name":   "aws",
			"config": map[string]any{"region": "us-east-1", "account_id": "123456"},
		},
		"tiers": map[string]any{
			"infrastructure": map[string]any{
				"capabilities": []any{
					map[string]any{
						"name": "primary-cluster",
						"type": "kubernetes_cluster",
						"properties": map[string]any{
							"version": "1.29",
						},
					},
					map[string]any{
						"name": "primary-vpc",
						"type": "network",
						"properties": map[string]any{
							"cidr": "10.0.0.0/16",
						},
					},
				},
				"constraints_for_downstream": []any{
					map[string]any{"field": "memory", "operator": "<=", "value": "4Gi"},
					map[string]any{"field": "cpu", "operator": "<=", "value": "2000m"},
				},
			},
			"shared_primitives": map[string]any{
				"capabilities": []any{
					map[string]any{
						"name": "shared-postgres",
						"type": "database",
						"properties": map[string]any{
							"engine":  "postgresql",
							"version": "15",
						},
					},
				},
				"constraints_for_downstream": []any{
					map[string]any{"field": "replicas", "operator": "<=", "value": 10},
				},
			},
			"application": map[string]any{
				"capabilities": []any{
					map[string]any{
						"name": "api-service",
						"type": "container_runtime",
						"properties": map[string]any{
							"replicas": 3,
							"memory":   "512Mi",
						},
						"dependsOn": []any{"shared-postgres"},
					},
				},
			},
		},
		"execution": map[string]any{
			"tier1_mode":              "plan_and_approve",
			"tier2_mode":              "plan_and_approve",
			"tier3_mode":              "auto_apply",
			"reconciliation_interval": "5m",
			"lock_timeout":            "10m",
		},
	}

	cfg, err := ParsePlatformConfig(raw)
	if err != nil {
		t.Fatalf("ParsePlatformConfig failed: %v", err)
	}

	if cfg.Org != "acme-corp" {
		t.Errorf("Org: got %q, want %q", cfg.Org, "acme-corp")
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment: got %q, want %q", cfg.Environment, "production")
	}
	if cfg.Provider.Name != "aws" {
		t.Errorf("Provider.Name: got %q, want %q", cfg.Provider.Name, "aws")
	}
	if cfg.Provider.Config["region"] != "us-east-1" {
		t.Errorf("Provider.Config[region]: got %v", cfg.Provider.Config["region"])
	}

	// Infrastructure tier
	if len(cfg.Tiers.Infrastructure.Capabilities) != 2 {
		t.Fatalf("Infrastructure caps: got %d, want 2", len(cfg.Tiers.Infrastructure.Capabilities))
	}
	if cfg.Tiers.Infrastructure.Capabilities[0].Name != "primary-cluster" {
		t.Errorf("Infra cap[0].Name: got %q", cfg.Tiers.Infrastructure.Capabilities[0].Name)
	}
	if len(cfg.Tiers.Infrastructure.ConstraintsForDownstream) != 2 {
		t.Fatalf("Infra constraints: got %d, want 2", len(cfg.Tiers.Infrastructure.ConstraintsForDownstream))
	}

	// Shared primitives tier
	if len(cfg.Tiers.SharedPrimitives.Capabilities) != 1 {
		t.Fatalf("SharedPrimitives caps: got %d, want 1", len(cfg.Tiers.SharedPrimitives.Capabilities))
	}
	if cfg.Tiers.SharedPrimitives.Capabilities[0].Type != "database" {
		t.Errorf("SharedPrimitives cap[0].Type: got %q", cfg.Tiers.SharedPrimitives.Capabilities[0].Type)
	}

	// Application tier
	if len(cfg.Tiers.Application.Capabilities) != 1 {
		t.Fatalf("Application caps: got %d, want 1", len(cfg.Tiers.Application.Capabilities))
	}
	if cfg.Tiers.Application.Capabilities[0].DependsOn[0] != "shared-postgres" {
		t.Errorf("Application cap dependsOn: got %v", cfg.Tiers.Application.Capabilities[0].DependsOn)
	}

	// Execution
	if cfg.Execution.Tier1Mode != "plan_and_approve" {
		t.Errorf("Tier1Mode: got %q", cfg.Execution.Tier1Mode)
	}
	if cfg.Execution.Tier3Mode != "auto_apply" {
		t.Errorf("Tier3Mode: got %q", cfg.Execution.Tier3Mode)
	}
	if cfg.Execution.ReconciliationInterval != "5m" {
		t.Errorf("ReconciliationInterval: got %q", cfg.Execution.ReconciliationInterval)
	}
}

func TestParsePlatformConfig_DefaultsApplied(t *testing.T) {
	raw := map[string]any{
		"org":         "test-org",
		"environment": "dev",
		"provider":    map[string]any{"name": "docker-compose"},
	}

	cfg, err := ParsePlatformConfig(raw)
	if err != nil {
		t.Fatalf("ParsePlatformConfig failed: %v", err)
	}

	if cfg.Execution.Tier3Mode != "auto_apply" {
		t.Errorf("Default Tier3Mode: got %q, want %q", cfg.Execution.Tier3Mode, "auto_apply")
	}
	if cfg.Execution.ReconciliationInterval != "5m" {
		t.Errorf("Default ReconciliationInterval: got %q, want %q", cfg.Execution.ReconciliationInterval, "5m")
	}
	if cfg.Execution.LockTimeout != "10m" {
		t.Errorf("Default LockTimeout: got %q, want %q", cfg.Execution.LockTimeout, "10m")
	}
}

func TestParsePlatformConfig_PartialTier3Only(t *testing.T) {
	raw := map[string]any{
		"org":         "minimal-org",
		"environment": "staging",
		"provider":    map[string]any{"name": "docker-compose"},
		"tiers": map[string]any{
			"application": map[string]any{
				"capabilities": []any{
					map[string]any{
						"name": "web-app",
						"type": "container_runtime",
						"properties": map[string]any{
							"image":    "nginx:latest",
							"replicas": 1,
						},
					},
				},
			},
		},
	}

	cfg, err := ParsePlatformConfig(raw)
	if err != nil {
		t.Fatalf("ParsePlatformConfig failed: %v", err)
	}

	if len(cfg.Tiers.Infrastructure.Capabilities) != 0 {
		t.Errorf("Infrastructure should be empty, got %d caps", len(cfg.Tiers.Infrastructure.Capabilities))
	}
	if len(cfg.Tiers.SharedPrimitives.Capabilities) != 0 {
		t.Errorf("SharedPrimitives should be empty, got %d caps", len(cfg.Tiers.SharedPrimitives.Capabilities))
	}
	if len(cfg.Tiers.Application.Capabilities) != 1 {
		t.Fatalf("Application should have 1 cap, got %d", len(cfg.Tiers.Application.Capabilities))
	}
	if cfg.Tiers.Application.Capabilities[0].Name != "web-app" {
		t.Errorf("Application cap name: got %q, want %q", cfg.Tiers.Application.Capabilities[0].Name, "web-app")
	}
}

func TestParsePlatformConfig_NilMap(t *testing.T) {
	_, err := ParsePlatformConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil map")
	}
}

func TestValidatePlatformConfig_MissingOrg(t *testing.T) {
	cfg := &PlatformConfig{
		Environment: "prod",
		Provider:    ProviderConfig{Name: "aws"},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing org")
	}
}

func TestValidatePlatformConfig_MissingEnvironment(t *testing.T) {
	cfg := &PlatformConfig{
		Org:      "acme",
		Provider: ProviderConfig{Name: "aws"},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
}

func TestValidatePlatformConfig_MissingProvider(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "prod",
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing provider name")
	}
}

func TestValidatePlatformConfig_NilConfig(t *testing.T) {
	err := ValidatePlatformConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestValidatePlatformConfig_MissingCapabilityName(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "prod",
		Provider:    ProviderConfig{Name: "aws"},
		Tiers: TiersConfig{
			Application: TierConfig{
				Capabilities: []CapabilityConfig{
					{Type: "container_runtime"}, // missing name
				},
			},
		},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing capability name")
	}
}

func TestValidatePlatformConfig_MissingCapabilityType(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "prod",
		Provider:    ProviderConfig{Name: "aws"},
		Tiers: TiersConfig{
			Application: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "api-service"}, // missing type
				},
			},
		},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing capability type")
	}
}

func TestValidatePlatformConfig_InvalidOperator(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "prod",
		Provider:    ProviderConfig{Name: "aws"},
		Tiers: TiersConfig{
			Infrastructure: TierConfig{
				ConstraintsForDownstream: []ConstraintConfig{
					{Field: "memory", Operator: "!!", Value: "4Gi"},
				},
			},
		},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid operator")
	}
}

func TestValidatePlatformConfig_ValidFull(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "production",
		Provider:    ProviderConfig{Name: "aws", Config: map[string]any{"region": "us-east-1"}},
		Tiers: TiersConfig{
			Infrastructure: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "vpc", Type: "network"},
				},
				ConstraintsForDownstream: []ConstraintConfig{
					{Field: "memory", Operator: "<=", Value: "4Gi"},
				},
			},
			SharedPrimitives: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "db", Type: "database"},
				},
			},
			Application: TierConfig{
				Capabilities: []CapabilityConfig{
					{Name: "api", Type: "container_runtime", DependsOn: []string{"db"}},
				},
			},
		},
		Execution: ExecutionConfig{
			Tier1Mode:              "plan_and_approve",
			Tier3Mode:              "auto_apply",
			ReconciliationInterval: "5m",
			LockTimeout:            "10m",
		},
	}
	if err := ValidatePlatformConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got: %v", err)
	}
}

func TestValidatePlatformConfig_MissingConstraintField(t *testing.T) {
	cfg := &PlatformConfig{
		Org:         "acme",
		Environment: "prod",
		Provider:    ProviderConfig{Name: "aws"},
		Tiers: TiersConfig{
			SharedPrimitives: TierConfig{
				ConstraintsForDownstream: []ConstraintConfig{
					{Operator: "<=", Value: 10}, // missing field
				},
			},
		},
	}
	err := ValidatePlatformConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing constraint field")
	}
}
