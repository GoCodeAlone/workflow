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
	// Apply was removed per workflow#699 (2026-05-17). v2 dispatch
	// routes through wfctlhelpers.ApplyPlanWithHooks (ResourceDriver
	// per-action + IaCProviderFinalizer.FinalizeApply post-loop). The
	// load-time Capabilities-RPC gate in cmd/wfctl/deploy_providers.go
	// rejects plugins whose CapabilitiesResponse.compute_plan_version
	// is not "v2".
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

// ProviderPlanner is an optional interface for v2 plugins that need custom
// plan logic (replacing platform.ComputePlan's default driver.Diff dispatch).
//
// Reserved as an extension hook for Tofu/Pulumi-style adapter plugins. Core
// wfctl's platform.ComputePlan + wfctlhelpers.ApplyPlan do NOT type-assert
// against this interface in v0.21.0 — adapter PRs that wish to use it will
// add the type-assertion at the dispatch site in their own design discussion.
//
// Plugins implementing this interface are accepted by the loader; the
// implementation is not yet exercised by core code.
type ProviderPlanner interface {
	PlanV2(ctx context.Context, desired []ResourceSpec, current []ResourceState) (IaCPlan, error)
}

// Enumerator is an OPTIONAL interface for providers that can list resources
// by tag across the cloud account. Used by `wfctl infra cleanup --tag <name>`.
// Providers without a tag-query API simply do not implement it; the cleanup
// subcommand skips them with a structured stdout log line so operators see
// the explicit skip rather than silent under-cleanup.
//
// The contract is intentionally narrow: implementations MUST return refs that
// the same provider's ResourceDriver(type).Delete can act on. ProviderID is
// recommended (the cleanup command may use it for log correlation), but Name
// + Type are the load-bearing identifiers Delete needs.
//
// Callers MUST type-assert against this interface and treat the negative
// case as a skip — providers may or may not implement Enumerator depending
// on whether their cloud API exposes a tag-query primitive. The
// implementation status of individual provider plugins is documented in
// docs/WFCTL.md `#### infra cleanup`, not here, so the API comment does
// not go stale every time a new plugin gains tag-query support.
type Enumerator interface {
	EnumerateByTag(ctx context.Context, tag string) ([]ResourceRef, error)
}

// EnumeratorAll is an OPTIONAL provider interface for resource types that
// don't support tagging (e.g. DO Spaces keys). Returns ALL resources of
// resourceType in the account regardless of tag, with full metadata
// (Outputs map populated) so callers can filter without re-reading.
//
// Returns []*ResourceOutput rather than []ResourceRef because filtering
// (drift detection, prune) needs the metadata. Implementations should
// paginate transparently. Returning *ResourceOutput keeps the contract
// consistent with ResourceDriver.Read which also returns this shape.
//
// Per ADR 0016. Used by `wfctl infra audit-keys` + `wfctl infra prune`.
type EnumeratorAll interface {
	EnumerateAll(ctx context.Context, resourceType string) ([]*ResourceOutput, error)
}

// DriftConfigDetector is an OPTIONAL interface a provider MAY implement to
// surface config-drift in addition to the existence-only Ghost / InSync /
// Unknown classifications produced by DetectDrift.
//
// specs is the per-ref applied-config map sourced from state. Callers build it
// from ResourceState.AppliedConfig (wrapped into ResourceSpec); missing or
// empty entries instruct the provider to fall back to existence-only behavior
// for that ref. The map key is ref.Name (matches ResourceState.Name).
//
// Callers MUST type-assert against this interface and fall back to
// IaCProvider.DetectDrift(refs) on the negative case. Providers that do
// not implement DriftConfigDetector continue to work unchanged.
//
// Providers SHOULD only return DriftClassConfig when they have high
// confidence the applied entry represents user-supplied config (not
// adoption-shaped Outputs reflow); see ResourceState.AppliedConfigSource
// (iac_state.go) for the canonical discriminator.
type DriftConfigDetector interface {
	DetectDriftWithSpecs(ctx context.Context, resources []ResourceRef, specs map[string]ResourceSpec) ([]DriftResult, error)
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

// DriftClass classifies the type of drift detected between IaC state and
// actual cloud state. Used by wfctl infra drift output and wfctl infra
// apply --refresh recovery semantics.
type DriftClass string

const (
	// DriftClassUnknown is the zero value; preserved for backwards compat
	// with consumers serialized before the Class field existed.
	DriftClassUnknown DriftClass = ""
	// DriftClassInSync — state and cloud agree.
	DriftClassInSync DriftClass = "in-sync"
	// DriftClassGhost — state has the resource; cloud Read returned
	// ErrResourceNotFound. Caller can prune via wfctl infra apply --refresh.
	DriftClassGhost DriftClass = "ghost"
	// DriftClassConfig — state and cloud both have the resource but configs
	// differ. Caller reconciles via wfctl infra apply (normal plan path).
	DriftClassConfig DriftClass = "config"
)

// DriftResult captures detected drift between declared and actual resource state.
type DriftResult struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Drifted  bool           `json:"drifted"`
	Class    DriftClass     `json:"class,omitempty"` // additive; omitted when Unknown
	Expected map[string]any `json:"expected,omitempty"`
	Actual   map[string]any `json:"actual,omitempty"`
	Fields   []string       `json:"fields,omitempty"` // which fields drifted
}

// PlanDiagnosticSeverity classifies the severity of a plan-validation
// PlanDiagnostic returned by a provider that implements ProviderValidator.
// Exit-code mapping for `wfctl infra align`:
//   - PlanDiagnosticError → always fails the run (regardless of --strict).
//   - PlanDiagnosticWarning → advisory by default; fails the run under --strict.
//   - PlanDiagnosticInfo → never affects the exit code (even under --strict).
//
// Note: distinct from the unrelated Troubleshooter Diagnostic type
// (iac_resource_driver.go), which describes runtime/deploy events. The plan
// spec for T4.1 originally proposed `Diagnostic` for the plan-validation type;
// renamed to PlanDiagnostic to remain purely additive (W-4 contract) without
// disturbing existing Troubleshooter consumers.
type PlanDiagnosticSeverity int

const (
	// PlanDiagnosticInfo is purely informational — surfaced to the user but
	// never fails an align run.
	PlanDiagnosticInfo PlanDiagnosticSeverity = iota
	// PlanDiagnosticWarning flags a likely-misconfiguration that does not
	// block apply but should be reviewed (advisory under --strict).
	PlanDiagnosticWarning
	// PlanDiagnosticError indicates a constraint violation that the provider
	// would reject at apply time. Fails an align run under --strict.
	PlanDiagnosticError
)

// PlanDiagnostic is a single finding emitted by a ProviderValidator
// implementation against an IaCPlan. PlanDiagnostics surface cross-resource
// constraints (e.g. a database referencing an unknown VPC) at plan time rather
// than at the provider's API call.
type PlanDiagnostic struct {
	// Severity is Error|Warning|Info; see PlanDiagnosticSeverity.
	Severity PlanDiagnosticSeverity `json:"severity"`
	// Resource is the offending resource name; empty for plan-level findings.
	Resource string `json:"resource,omitempty"`
	// Field is a dotted/bracketed field path within Resource (e.g. "vpc_ref"
	// or "tags[0].key"); empty for resource-level findings.
	Field string `json:"field,omitempty"`
	// Message is a human-readable description of the finding.
	Message string `json:"message"`
}

// ProviderValidator is an OPTIONAL interface that an IaCProvider implementation
// MAY also satisfy to expose provider-side cross-resource constraint validation
// at plan time. Consumers (e.g. R-A10 in cmd/wfctl/infra_align*.go) use a
// type-assertion to discover whether a given provider implements ValidatePlan;
// providers that do not implement it continue to work unchanged.
//
// ValidatePlan is read-only: it MUST NOT mutate plan and MUST NOT make remote
// calls. The returned slice may be nil (no diagnostics).
type ProviderValidator interface {
	ValidatePlan(plan *IaCPlan) []PlanDiagnostic
}

// ProviderCredentialRevoker is an OPTIONAL interface a provider MAY implement
// to support revoking previously-issued provider_credential credentials.
// Used by `wfctl infra bootstrap --force-rotate <name>` to invalidate the OLD
// credential at the upstream provider AFTER the new one has been minted and
// stored (mint-new-then-revoke-old ordering; see ADR 0012).
//
// source is the provider_credential source string (e.g. "digitalocean.spaces").
// credentialID is the provider-specific identifier of the OLD credential
// (e.g. the access_key for DO Spaces — stored as <name>_access_key).
//
// Callers MUST type-assert before calling and treat the negative case as a
// "log warning, not implemented" path — providers that do not implement this
// interface are valid; revocation just does not happen automatically.
//
// Error contract:
//   - nil → successfully revoked (or credential was already absent)
//   - non-nil → revocation failed; caller logs warning + emits telemetry but
//     MUST NOT roll back the newly-stored credential (the new key is valid)
type ProviderCredentialRevoker interface {
	RevokeProviderCredential(ctx context.Context, source string, credentialID string) error
}

// LogCaptureRequest describes a bounded provider log capture. Providers may
// support only a subset of fields; unsupported values should return a clear
// error instead of silently changing scope.
type LogCaptureRequest struct {
	ResourceName    string
	ResourceType    string
	ProviderID      string
	ComponentName   string
	LogType         string
	TailLines       int
	Follow          bool
	DurationSeconds int64
	DeploymentID    string
}

// LogChunk is one provider-emitted log payload. Data is already formatted for
// the caller's output stream; Source is optional metadata such as "historic" or
// "live".
type LogChunk struct {
	Data   []byte
	Source string
	EOF    bool
}

// LogCaptureSink receives log chunks from a provider.
type LogCaptureSink interface {
	WriteLogChunk(LogChunk) error
}

// LogCaptureProvider is an OPTIONAL provider interface for ad-hoc operational
// log capture. `wfctl logs capture` discovers it through the typed optional
// IaCProviderLogCapture service.
type LogCaptureProvider interface {
	CaptureLogs(ctx context.Context, req LogCaptureRequest, sink LogCaptureSink) error
}
