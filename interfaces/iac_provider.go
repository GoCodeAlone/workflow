package interfaces

import "context"

// IaCProvider is the main interface that cloud provider plugins implement.
// Each provider (AWS, GCP, Azure, DO) implements this as a gRPC plugin.
type IaCProvider interface {
	Name() string
	Version() string
	Initialize(ctx context.Context, config map[string]any) error

	// Capabilities returns what resource types this provider supports.
	Capabilities() []IaCCapabilityDeclaration

	// Lifecycle
	Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*IaCPlan, error)
	Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error)
	Destroy(ctx context.Context, resources []ResourceRef) (*DestroyResult, error)

	// Observability
	Status(ctx context.Context, resources []ResourceRef) ([]ResourceStatus, error)
	DetectDrift(ctx context.Context, resources []ResourceRef) ([]DriftResult, error)

	// Migration
	Import(ctx context.Context, cloudID string, resourceType string) (*ResourceState, error)

	// Sizing
	ResolveSizing(resourceType string, size Size, hints *ResourceHints) (*ProviderSizing, error)

	// Resource drivers for fine-grained CRUD
	ResourceDriver(resourceType string) (ResourceDriver, error)

	// SupportedCanonicalKeys returns the subset of canonical IaC config keys
	// that this provider understands. Providers may return a subset; callers
	// use this to warn on unrecognised fields before applying a plan.
	// Built-in and stub providers return the full canonical key set.
	SupportedCanonicalKeys() []string

	// BootstrapStateBackend ensures the state backend resource (bucket/container)
	// exists on this provider. It is idempotent: if the resource already exists,
	// it returns the current metadata without error. cfg is the expanded
	// iac.state module config (backend, bucket, region, credentials, etc.).
	//
	// Providers that do not manage a state backend should return (nil, nil).
	// The caller prints each entry in result.EnvVars as `export KEY=VALUE` for
	// CI capture and writes result.Bucket back to the on-disk config.
	BootstrapStateBackend(ctx context.Context, cfg map[string]any) (*BootstrapResult, error)

	Close() error
}

// BootstrapResult contains metadata returned by a successful BootstrapStateBackend call.
type BootstrapResult struct {
	// Bucket is the name of the created or confirmed state bucket/container.
	Bucket string `json:"bucket,omitempty"`
	// Region is the region where the bucket resides.
	Region string `json:"region,omitempty"`
	// Endpoint is the S3-compatible API endpoint URL (if applicable).
	Endpoint string `json:"endpoint,omitempty"`
	// EnvVars is a map of environment variable names to values that should be
	// exported for CI capture (e.g. WFCTL_STATE_BUCKET, SPACES_BUCKET).
	EnvVars map[string]string `json:"env_vars,omitempty"`
}

// Size is the abstract sizing tier for a resource.
type Size string

const (
	SizeXS Size = "xs"
	SizeS  Size = "s"
	SizeM  Size = "m"
	SizeL  Size = "l"
	SizeXL Size = "xl"
)

// ResourceHints are optional fine-grained overrides on top of the Size tier.
type ResourceHints struct {
	CPU     string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty" yaml:"memory,omitempty"`
	Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
}

// ProviderSizing is the concrete sizing resolved by a provider for a resource type.
type ProviderSizing struct {
	InstanceType string         `json:"instance_type"`
	Specs        map[string]any `json:"specs"`
}

// IaCCapabilityDeclaration describes a resource type supported by a provider.
type IaCCapabilityDeclaration struct {
	ResourceType string   `json:"resource_type"` // infra.database, infra.vpc, etc.
	Tier         int      `json:"tier"`          // 1=infra, 2=shared, 3=app
	Operations   []string `json:"operations"`    // create, read, update, delete, scale
}

// ResourceSpec is the desired state declaration for a single resource.
type ResourceSpec struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config"`
	Size      Size           `json:"size,omitempty"`
	Hints     *ResourceHints `json:"hints,omitempty"`
	DependsOn []string       `json:"depends_on,omitempty"`
}

// ResourceRef is a lightweight reference to an existing resource.
type ResourceRef struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ProviderID string `json:"provider_id,omitempty"`
}

// ResourceStatus is the live status of a provisioned resource.
type ResourceStatus struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	ProviderID string         `json:"provider_id"`
	Status     string         `json:"status"` // running, stopped, degraded, unknown
	Outputs    map[string]any `json:"outputs"`
}

// DriftResult captures detected drift between declared and actual resource state.
type DriftResult struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Drifted  bool           `json:"drifted"`
	Expected map[string]any `json:"expected"`
	Actual   map[string]any `json:"actual"`
	Fields   []string       `json:"fields"` // which fields drifted
}
