package platform

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// validOperators lists the constraint operators recognized by the platform.
var validOperators = map[string]bool{
	"<=":     true,
	">=":     true,
	"==":     true,
	"in":     true,
	"not_in": true,
}

// PlatformConfig is the top-level platform abstraction configuration.
// It describes the organization, environment, provider, tier layout,
// and execution policy for a deployment.
type PlatformConfig struct {
	// Org is the organization identifier (e.g., "acme-corp").
	Org string `yaml:"org" json:"org"`

	// Environment is the target environment name (e.g., "production", "staging").
	Environment string `yaml:"environment" json:"environment"`

	// Provider configures the infrastructure provider to use.
	Provider ProviderConfig `yaml:"provider" json:"provider"`

	// Tiers defines the three-tier capability layout.
	Tiers TiersConfig `yaml:"tiers" json:"tiers"`

	// Execution controls per-tier execution modes and reconciliation settings.
	Execution ExecutionConfig `yaml:"execution" json:"execution"`
}

// ProviderConfig identifies and configures an infrastructure provider.
type ProviderConfig struct {
	// Name is the provider identifier (e.g., "aws", "docker-compose", "gcp").
	Name string `yaml:"name" json:"name"`

	// Config holds provider-specific configuration key-value pairs.
	Config map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

// TiersConfig groups the three infrastructure tiers.
type TiersConfig struct {
	// Infrastructure is Tier 1: compute, networking, IAM.
	Infrastructure TierConfig `yaml:"infrastructure" json:"infrastructure"`

	// SharedPrimitives is Tier 2: namespaces, queues, shared databases.
	SharedPrimitives TierConfig `yaml:"shared_primitives" json:"shared_primitives"`

	// Application is Tier 3: app deployments, scaling policies.
	Application TierConfig `yaml:"application" json:"application"`
}

// TierConfig describes the capabilities and downstream constraints for a single tier.
type TierConfig struct {
	// Capabilities lists the abstract resources declared in this tier.
	Capabilities []CapabilityConfig `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`

	// ConstraintsForDownstream are hard limits imposed on lower tiers.
	ConstraintsForDownstream []ConstraintConfig `yaml:"constraints_for_downstream,omitempty" json:"constraints_for_downstream,omitempty"`
}

// CapabilityConfig is the YAML representation of a single capability declaration.
type CapabilityConfig struct {
	// Name is the unique identifier for this capability within its tier.
	Name string `yaml:"name" json:"name"`

	// Type is the abstract capability type (e.g., "container_runtime", "database").
	Type string `yaml:"type" json:"type"`

	// Properties are provider-agnostic configuration values.
	Properties map[string]any `yaml:"properties,omitempty" json:"properties,omitempty"`

	// DependsOn lists other capability names that must be provisioned first.
	DependsOn []string `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
}

// ConstraintConfig is the YAML representation of a constraint imposed by a parent tier.
type ConstraintConfig struct {
	// Field is the property being constrained (e.g., "memory", "replicas").
	Field string `yaml:"field" json:"field"`

	// Operator is the comparison operator: "<=", ">=", "==", "in", "not_in".
	Operator string `yaml:"operator" json:"operator"`

	// Value is the constraint limit value.
	Value any `yaml:"value" json:"value"`
}

// ExecutionConfig controls how each tier's changes are applied and how
// often drift detection runs.
type ExecutionConfig struct {
	// Tier1Mode controls Tier 1 execution: "plan_and_approve" or "auto_apply".
	Tier1Mode string `yaml:"tier1_mode" json:"tier1_mode"`

	// Tier2Mode controls Tier 2 execution: "plan_and_approve" or "auto_apply".
	Tier2Mode string `yaml:"tier2_mode" json:"tier2_mode"`

	// Tier3Mode controls Tier 3 execution: "plan_and_approve" or "auto_apply".
	Tier3Mode string `yaml:"tier3_mode" json:"tier3_mode"`

	// ReconciliationInterval is how often drift detection runs (e.g., "5m").
	ReconciliationInterval string `yaml:"reconciliation_interval" json:"reconciliation_interval"`

	// LockTimeout is the advisory lock TTL (e.g., "10m").
	LockTimeout string `yaml:"lock_timeout" json:"lock_timeout"`
}

// ParsePlatformConfig converts a raw YAML map (typically from the
// "platform" key in WorkflowConfig) into a typed PlatformConfig.
// It re-marshals the map through YAML to leverage struct tags for parsing.
func ParsePlatformConfig(raw map[string]any) (*PlatformConfig, error) {
	if raw == nil {
		return nil, fmt.Errorf("platform config is nil")
	}

	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal platform config: %w", err)
	}

	var cfg PlatformConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse platform config: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults sets default values for fields that were not specified.
func applyDefaults(cfg *PlatformConfig) {
	if cfg.Execution.Tier3Mode == "" {
		cfg.Execution.Tier3Mode = "auto_apply"
	}
	if cfg.Execution.ReconciliationInterval == "" {
		cfg.Execution.ReconciliationInterval = "5m"
	}
	if cfg.Execution.LockTimeout == "" {
		cfg.Execution.LockTimeout = "10m"
	}
}

// ValidatePlatformConfig checks that required fields are present and that
// constraint operators are recognized.
func ValidatePlatformConfig(cfg *PlatformConfig) error {
	if cfg == nil {
		return fmt.Errorf("platform config is nil")
	}
	if cfg.Org == "" {
		return fmt.Errorf("platform config: org is required")
	}
	if cfg.Environment == "" {
		return fmt.Errorf("platform config: environment is required")
	}
	if cfg.Provider.Name == "" {
		return fmt.Errorf("platform config: provider.name is required")
	}

	// Validate all tiers.
	tiers := []struct {
		name string
		tier TierConfig
	}{
		{"infrastructure", cfg.Tiers.Infrastructure},
		{"shared_primitives", cfg.Tiers.SharedPrimitives},
		{"application", cfg.Tiers.Application},
	}

	for _, t := range tiers {
		for i, cap := range t.tier.Capabilities {
			if cap.Name == "" {
				return fmt.Errorf("platform config: tiers.%s.capabilities[%d].name is required", t.name, i)
			}
			if cap.Type == "" {
				return fmt.Errorf("platform config: tiers.%s.capabilities[%d].type is required (capability %q)", t.name, i, cap.Name)
			}
		}
		for i, c := range t.tier.ConstraintsForDownstream {
			if c.Field == "" {
				return fmt.Errorf("platform config: tiers.%s.constraints_for_downstream[%d].field is required", t.name, i)
			}
			if !validOperators[c.Operator] {
				return fmt.Errorf("platform config: tiers.%s.constraints_for_downstream[%d].operator %q is invalid; must be one of <=, >=, ==, in, not_in", t.name, i, c.Operator)
			}
		}
	}

	return nil
}
