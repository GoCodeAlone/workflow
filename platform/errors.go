package platform

import "fmt"

// ConstraintViolationError is returned when a capability declaration violates
// a constraint imposed by a parent tier.
type ConstraintViolationError struct {
	// Resource is the name of the resource that violated the constraint.
	Resource string

	// Constraint is the constraint field that was violated.
	Constraint string

	// Value is the actual value that caused the violation.
	Value any

	// Limit is the constraint limit that was exceeded.
	Limit any
}

// Error implements the error interface.
func (e *ConstraintViolationError) Error() string {
	return fmt.Sprintf("constraint violation on resource %q: field %q value %v exceeds limit %v",
		e.Resource, e.Constraint, e.Value, e.Limit)
}

// FidelityGapError is returned when a provider cannot satisfy a capability
// at the required fidelity level.
type FidelityGapError struct {
	// Provider is the provider that has the fidelity gap.
	Provider string

	// Capability is the capability type that is not fully supported.
	Capability string

	// Level is the actual fidelity level the provider offers.
	Level FidelityLevel

	// Details describes what is missing.
	Details string
}

// Error implements the error interface.
func (e *FidelityGapError) Error() string {
	return fmt.Sprintf("fidelity gap: provider %q implements %q at %q level: %s",
		e.Provider, e.Capability, e.Level, e.Details)
}

// TierBoundaryError is returned when an operation attempts to cross tier
// boundaries in a prohibited direction.
type TierBoundaryError struct {
	// SourceTier is the tier the operation originated from.
	SourceTier Tier

	// TargetTier is the tier the operation attempted to reach.
	TargetTier Tier

	// Operation is the operation that was attempted.
	Operation string

	// Reason explains why the operation is not allowed.
	Reason string
}

// Error implements the error interface.
func (e *TierBoundaryError) Error() string {
	return fmt.Sprintf("tier boundary violation: %s from tier %d to tier %d: %s",
		e.Operation, e.SourceTier, e.TargetTier, e.Reason)
}

// ResourceNotFoundError is returned when a requested resource does not exist
// in the provider or state store.
type ResourceNotFoundError struct {
	// Name is the resource name that was not found.
	Name string

	// Provider is the provider where the resource was expected.
	Provider string
}

// Error implements the error interface.
func (e *ResourceNotFoundError) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("resource %q not found in provider %q", e.Name, e.Provider)
	}
	return fmt.Sprintf("resource %q not found", e.Name)
}

// PlanConflictError is returned when a plan conflicts with another plan
// that is currently being applied or was recently applied.
type PlanConflictError struct {
	// PlanID is the conflicting plan's identifier.
	PlanID string

	// ConflictingResource is the resource that has a conflicting change.
	ConflictingResource string
}

// Error implements the error interface.
func (e *PlanConflictError) Error() string {
	return fmt.Sprintf("plan conflict: plan %q has a conflicting change for resource %q",
		e.PlanID, e.ConflictingResource)
}

// ResourceDriverNotFoundError is returned when a provider does not have a
// driver for the requested resource type.
type ResourceDriverNotFoundError struct {
	// ResourceType is the resource type that has no driver.
	ResourceType string

	// Provider is the provider that was queried.
	Provider string
}

// Error implements the error interface.
func (e *ResourceDriverNotFoundError) Error() string {
	return fmt.Sprintf("no resource driver for type %q in provider %q", e.ResourceType, e.Provider)
}

// LockConflictError is returned when a lock cannot be acquired because
// another operation holds it.
type LockConflictError struct {
	// ContextPath is the context path that is locked.
	ContextPath string

	// HeldBy is an identifier for the holder, if known.
	HeldBy string
}

// Error implements the error interface.
func (e *LockConflictError) Error() string {
	if e.HeldBy != "" {
		return fmt.Sprintf("lock conflict on %q: held by %q", e.ContextPath, e.HeldBy)
	}
	return fmt.Sprintf("lock conflict on %q", e.ContextPath)
}

// CapabilityUnsupportedError is returned when a provider does not support
// the requested capability type at all.
type CapabilityUnsupportedError struct {
	// Capability is the unsupported capability type.
	Capability string

	// Provider is the provider that does not support it.
	Provider string
}

// Error implements the error interface.
func (e *CapabilityUnsupportedError) Error() string {
	return fmt.Sprintf("capability %q is not supported by provider %q", e.Capability, e.Provider)
}

// NotScalableError is returned when a Scale operation is attempted on a
// resource type that does not support scaling.
type NotScalableError struct {
	// ResourceType is the resource type that does not support scaling.
	ResourceType string
}

// Error implements the error interface.
func (e *NotScalableError) Error() string {
	return fmt.Sprintf("resource type %q does not support scaling", e.ResourceType)
}

// Sentinel errors for common conditions.
var (
	// ErrPlanNotApproved is returned when attempting to apply a plan that
	// has not been approved.
	ErrPlanNotApproved = fmt.Errorf("plan has not been approved")

	// ErrPlanAlreadyApplied is returned when attempting to apply a plan
	// that has already been applied.
	ErrPlanAlreadyApplied = fmt.Errorf("plan has already been applied")

	// ErrPlanExpired is returned when a plan is too old to be applied safely.
	ErrPlanExpired = fmt.Errorf("plan has expired and must be regenerated")

	// ErrContextNotFound is returned when a platform context cannot be resolved.
	ErrContextNotFound = fmt.Errorf("platform context not found")

	// ErrProviderNotInitialized is returned when a provider method is called
	// before Initialize.
	ErrProviderNotInitialized = fmt.Errorf("provider has not been initialized")
)
