package interfaces

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for common IaC resource operation response categories.
// Use errors.Is to identify them after wrapping.
var (
	ErrResourceNotFound      = errors.New("iac: resource not found")      // 404/410
	ErrResourceAlreadyExists = errors.New("iac: resource already exists") // 409 Conflict
	ErrRateLimited           = errors.New("iac: rate limited")            // 429
	ErrTransient             = errors.New("iac: transient error")         // 502/503/504
	ErrUnauthorized          = errors.New("iac: unauthorized")            // 401
	ErrForbidden             = errors.New("iac: forbidden")               // 403
	ErrValidation            = errors.New("iac: validation error")        // 400/422
)

// ResourceDriver handles CRUD for a single resource type within a provider.
type ResourceDriver interface {
	Create(ctx context.Context, spec ResourceSpec) (*ResourceOutput, error)
	Read(ctx context.Context, ref ResourceRef) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, spec ResourceSpec) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
	Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error)
	HealthCheck(ctx context.Context, ref ResourceRef) (*HealthResult, error)
	Scale(ctx context.Context, ref ResourceRef, replicas int) (*ResourceOutput, error)
	// SensitiveKeys returns output keys whose values should be masked in logs and plan output.
	SensitiveKeys() []string
}

// ResourceAdoptionLocator is an optional interface ResourceDriver
// implementations may provide when a desired ResourceSpec can be resolved to a
// live provider resource before local state exists. wfctl infra apply uses this
// to adopt existing resources into state and then computes an update/delete
// plan from real current state instead of blindly creating duplicates.
type ResourceAdoptionLocator interface {
	AdoptionRef(spec ResourceSpec) (ResourceRef, bool, error)
}

// ResourceOutput is the concrete output of a provisioned or read resource.
type ResourceOutput struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	ProviderID string          `json:"provider_id"`
	Outputs    map[string]any  `json:"outputs"`             // IPs, endpoints, connection strings
	Sensitive  map[string]bool `json:"sensitive,omitempty"` // keys whose values are sensitive
	Status     string          `json:"status"`
}

// DiffResult summarises the differences between desired and actual resource state.
type DiffResult struct {
	NeedsUpdate  bool          `json:"needs_update"`
	NeedsReplace bool          `json:"needs_replace"`
	Changes      []FieldChange `json:"changes"`
}

// FieldChange describes a single field-level difference.
type FieldChange struct {
	Path     string `json:"path"`
	Old      any    `json:"old"`
	New      any    `json:"new"`
	ForceNew bool   `json:"force_new"` // change requires resource replacement
}

// HealthResult is the outcome of a health check for a resource.
type HealthResult struct {
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

// Diagnostic is a single troubleshooting finding returned by a Troubleshooter.
// It describes a recent provider-side event (deployment, job run, etc.) with
// enough context to understand why a health check failed without visiting the
// provider's console.
type Diagnostic struct {
	ID     string    `json:"id"`               // provider-side identifier (e.g. deployment ID)
	Phase  string    `json:"phase"`            // terminal or current phase
	Cause  string    `json:"cause"`            // human-readable root cause or error summary
	At     time.Time `json:"at"`               // when the event was created or last updated
	Detail string    `json:"detail,omitempty"` // optional verbose tail (log excerpt, stack)
}

// Troubleshooter is an optional interface that ResourceDrivers may implement.
// wfctl calls Troubleshoot automatically when a health-check poll times out or
// a deploy operation returns a generic error, surfacing provider-side context
// that would otherwise require visiting the provider's web console.
//
// Implementations should return the N most recent relevant events (deployments,
// job runs, etc.) in reverse-chronological order.  Returning an error is
// non-fatal — wfctl logs it and continues with the original failure message.
type Troubleshooter interface {
	Troubleshoot(ctx context.Context, ref ResourceRef, failureMsg string) ([]Diagnostic, error)
}

// ProviderIDFormat identifies the shape of provider-specific resource
// identifiers so wfctl can validate them at the driver boundary without
// knowing provider-specific semantics.
//
// The zero value IDFormatUnknown disables validation for backward
// compatibility — drivers that don't opt in get today's behavior.
type ProviderIDFormat int

const (
	// IDFormatUnknown disables validation (zero value).
	IDFormatUnknown ProviderIDFormat = iota
	// IDFormatUUID is the canonical 36-character hyphenated UUID shape.
	IDFormatUUID
	// IDFormatDomainName is an RFC 1035 domain name.
	IDFormatDomainName
	// IDFormatARN is an AWS-style colon-separated ARN.
	IDFormatARN
	// IDFormatFreeform allows any non-empty string.
	IDFormatFreeform
)

// String returns a stable identifier for logs and error messages.
func (f ProviderIDFormat) String() string {
	switch f {
	case IDFormatUUID:
		return "uuid"
	case IDFormatDomainName:
		return "domain_name"
	case IDFormatARN:
		return "arn"
	case IDFormatFreeform:
		return "freeform"
	default:
		return "unknown"
	}
}

// ProviderIDValidator is an optional interface ResourceDriver implementations
// may provide to declare the shape of their ProviderIDs. wfctl uses the
// declaration to validate ProviderIDs at two boundaries:
//
//   - Input: before Update/Delete, probe ref.ProviderID against the declared
//     format. On mismatch, wfctl logs a warning but still calls the driver so
//     its own heal logic (if any) can run.
//   - Output: after Apply, probe r.ProviderID before persisting to state.
//     Mismatch for non-Unknown formats is a HARD failure — the driver has a
//     bug and state must not be corrupted. Freeform accepts any non-empty
//     ProviderID; Unknown disables output validation.
//
// Drivers that do not implement this interface receive today's behavior:
// no validation, no warning, no failure.
type ProviderIDValidator interface {
	ProviderIDFormat() ProviderIDFormat
}
