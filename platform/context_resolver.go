package platform

import "context"

// PlatformContext is the hierarchical context flowing through the tier system.
// It carries organization, environment, and application identifiers along with
// the accumulated outputs and constraints from parent tiers. Context flows
// downward: Tier 1 outputs feed Tier 2 inputs, Tier 2 outputs feed Tier 3.
type PlatformContext struct {
	// Org is the organization identifier.
	Org string `json:"org"`

	// Environment is the deployment environment name (e.g., "production", "staging", "dev").
	Environment string `json:"environment"`

	// Application is the application name. Empty for Tier 1 and Tier 2 contexts.
	Application string `json:"application"`

	// Tier is the infrastructure tier this context operates at.
	Tier Tier `json:"tier"`

	// ParentOutputs are resource outputs inherited from parent tiers, keyed by resource name.
	ParentOutputs map[string]*ResourceOutput `json:"parentOutputs"`

	// Constraints are the accumulated constraints from all parent tiers.
	Constraints []Constraint `json:"constraints"`

	// Credentials holds resolved credential values for this scope.
	// This field is never serialized to prevent credential leakage.
	Credentials map[string]string `json:"-"`

	// Labels are metadata key-value pairs for resource tagging.
	Labels map[string]string `json:"labels"`

	// Annotations are metadata key-value pairs for operational metadata.
	Annotations map[string]string `json:"annotations"`
}

// ContextPath returns the full hierarchical path: "org/env" or "org/env/app".
func (pc *PlatformContext) ContextPath() string {
	path := pc.Org + "/" + pc.Environment
	if pc.Application != "" {
		path += "/" + pc.Application
	}
	return path
}

// ContextResolver builds PlatformContext instances by aggregating parent tier
// state. It reads state stores to populate parent outputs and constraints,
// enabling the downward flow of context through the tier hierarchy.
type ContextResolver interface {
	// ResolveContext builds a PlatformContext for the given tier and identifiers.
	// It reads parent tier state stores to populate ParentOutputs and Constraints.
	ResolveContext(ctx context.Context, org, env, app string, tier Tier) (*PlatformContext, error)

	// PropagateOutputs writes resource outputs into the context store so that
	// downstream tiers can resolve them via ResolveContext.
	PropagateOutputs(ctx context.Context, pctx *PlatformContext, outputs []*ResourceOutput) error

	// ValidateTierBoundary ensures a workflow operating at the given tier
	// does not attempt to modify resources outside its scope. Returns any
	// constraint violations found.
	ValidateTierBoundary(pctx *PlatformContext, declarations []CapabilityDeclaration) []ConstraintViolation
}
