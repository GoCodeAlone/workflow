package platform

import "context"

// Provider is the top-level interface for an infrastructure provider.
// A provider manages a collection of resource drivers and maps abstract
// capabilities to provider-specific resource types. Providers are registered
// with the engine and selected based on the platform configuration.
type Provider interface {
	// Name returns the provider identifier (e.g., "aws", "docker-compose", "gcp").
	Name() string

	// Version returns the provider version string.
	Version() string

	// Initialize prepares the provider by authenticating and validating configuration.
	Initialize(ctx context.Context, config map[string]any) error

	// Capabilities returns the set of capability types this provider supports.
	// Used during plan-time to determine if a provider can satisfy a declaration.
	Capabilities() []CapabilityType

	// MapCapability resolves an abstract capability declaration to a provider-specific
	// resource plan. Returns an error if the capability cannot be satisfied.
	MapCapability(ctx context.Context, decl CapabilityDeclaration, pctx *PlatformContext) ([]ResourcePlan, error)

	// ResourceDriver returns the driver for a specific provider resource type.
	// Returns ErrResourceDriverNotFound if the resource type is not supported.
	ResourceDriver(resourceType string) (ResourceDriver, error)

	// CredentialBroker returns the provider's credential management interface.
	// Returns nil if the provider does not support credential brokering.
	CredentialBroker() CredentialBroker

	// StateStore returns the provider's state persistence interface.
	StateStore() StateStore

	// Healthy returns nil if the provider is reachable and authenticated.
	Healthy(ctx context.Context) error

	// Close releases any resources held by the provider.
	Close() error
}

// ProviderFactory is a constructor function for creating Provider instances.
type ProviderFactory func() Provider

// CapabilityType describes a capability a provider can satisfy.
// It includes schema information for the properties and constraints
// the capability accepts.
type CapabilityType struct {
	// Name is the capability type identifier (e.g., "container_runtime", "database").
	Name string `json:"name"`

	// Description is a human-readable description of the capability.
	Description string `json:"description"`

	// Tier indicates which infrastructure tier this capability belongs to.
	Tier Tier `json:"tier"`

	// Properties are the property schemas this capability accepts.
	Properties []PropertySchema `json:"properties"`

	// Constraints are the constraint schemas this capability can enforce.
	Constraints []PropertySchema `json:"constraints"`

	// Fidelity indicates how faithfully this provider implements the capability.
	Fidelity FidelityLevel `json:"fidelity"`
}

// PropertySchema describes a property accepted by a capability.
type PropertySchema struct {
	// Name is the property identifier.
	Name string `json:"name"`

	// Type is the property data type: "string", "int", "bool", "duration", "map", "list".
	Type string `json:"type"`

	// Required indicates whether the property must be provided.
	Required bool `json:"required"`

	// Description is a human-readable description of the property.
	Description string `json:"description"`

	// DefaultValue is the value used when the property is not specified.
	DefaultValue any `json:"defaultValue,omitempty"`
}

// ResourcePlan is the provider-specific plan for a single resource.
// It is the output of capability mapping and the input to resource drivers.
type ResourcePlan struct {
	// ResourceType is the provider-specific resource type (e.g., "aws.eks_nodegroup").
	ResourceType string `json:"resourceType"`

	// Name is the resource instance name.
	Name string `json:"name"`

	// Properties are the provider-specific properties for the resource.
	Properties map[string]any `json:"properties"`

	// DependsOn lists other resource names that must be created first.
	DependsOn []string `json:"dependsOn"`
}
