package platform

// CapabilityMapper translates abstract capability declarations into
// provider-specific resource plans. Each provider has one mapper that
// understands how to convert capability-level abstractions into the
// concrete resources the provider manages.
type CapabilityMapper interface {
	// CanMap returns true if this mapper can handle the given capability type.
	CanMap(capabilityType string) bool

	// Map translates a capability declaration into one or more resource plans.
	// The PlatformContext provides parent tier outputs and constraints.
	Map(decl CapabilityDeclaration, pctx *PlatformContext) ([]ResourcePlan, error)

	// ValidateConstraints checks if a capability declaration satisfies all
	// constraints imposed by parent tiers. Returns any constraint violations found.
	ValidateConstraints(decl CapabilityDeclaration, constraints []Constraint) []ConstraintViolation
}

// ConstraintViolation describes a single constraint check failure.
// It identifies which constraint was violated and the actual value
// that caused the violation.
type ConstraintViolation struct {
	// Constraint is the constraint that was violated.
	Constraint Constraint `json:"constraint"`

	// Actual is the value that violated the constraint.
	Actual any `json:"actual"`

	// Message is a human-readable description of the violation.
	Message string `json:"message"`
}
