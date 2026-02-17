package platform

import "context"

// TierRole defines what a principal can do within a specific tier.
// Roles are scoped to a tier and context path combination.
type TierRole string

const (
	// RoleTierAdmin grants full CRUD on tier resources.
	RoleTierAdmin TierRole = "tier_admin"

	// RoleTierAuthor grants permission to create and update workflows for a tier.
	RoleTierAuthor TierRole = "tier_author"

	// RoleTierViewer grants read-only access to tier state.
	RoleTierViewer TierRole = "tier_viewer"

	// RoleTierApprover grants permission to approve plans for a tier.
	RoleTierApprover TierRole = "tier_approver"
)

// PlatformRBAC enforces tier-based access control. It validates that
// principals have the required roles for the operations they attempt,
// and that capability declarations do not violate parent tier constraints.
type PlatformRBAC interface {
	// CanAuthor checks if a principal can define workflows at the given tier
	// and context path.
	CanAuthor(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

	// CanApprove checks if a principal can approve plans at the given tier
	// and context path.
	CanApprove(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

	// CanView checks if a principal can read state at the given tier
	// and context path.
	CanView(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

	// EnforceConstraints validates that a set of capability declarations
	// does not exceed the constraints imposed by parent tiers.
	// This is called at plan time before any resources are provisioned.
	EnforceConstraints(pctx *PlatformContext, declarations []CapabilityDeclaration) ([]ConstraintViolation, error)
}
