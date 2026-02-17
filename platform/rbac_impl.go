package platform

import (
	"context"
	"fmt"
	"sync"
)

// AuthzPolicy defines the allowed operations for a role within specific tiers.
type AuthzPolicy struct {
	// Role is the tier role this policy applies to.
	Role TierRole

	// AllowedTiers is the set of tiers the role may access.
	AllowedTiers []Tier

	// AllowedOperations is the set of operation names the role may perform.
	// An empty slice means no operations are allowed.
	AllowedOperations []string
}

// TierAuthorizer checks whether a role is permitted to perform an operation
// on a given tier. It is the low-level authorization primitive used by
// middleware and the PlatformRBAC implementation.
type TierAuthorizer interface {
	// Authorize checks whether the given role may perform operation on tier.
	// Returns nil if authorized, or a TierBoundaryError if not.
	Authorize(ctx context.Context, role TierRole, tier Tier, operation string) error

	// RegisterPolicy adds or replaces a policy for the given role.
	RegisterPolicy(policy AuthzPolicy)
}

// StdTierAuthorizer is the standard implementation of TierAuthorizer.
// It holds a list of AuthzPolicy entries and checks incoming requests
// against them.
type StdTierAuthorizer struct {
	mu       sync.RWMutex
	policies map[TierRole]AuthzPolicy
}

// NewStdTierAuthorizer creates a StdTierAuthorizer populated with default
// policies for the built-in roles.
//
// Default policies:
//   - RoleTierAdmin: all tiers, all operations
//   - RoleTierAuthor: Tier 2 and 3, read and write operations
//   - RoleTierViewer: all tiers, read-only operations
//   - RoleTierApprover: Tier 1 and 2, read and approve operations
func NewStdTierAuthorizer() *StdTierAuthorizer {
	a := &StdTierAuthorizer{
		policies: make(map[TierRole]AuthzPolicy),
	}

	allTiers := []Tier{TierInfrastructure, TierSharedPrimitive, TierApplication}

	a.policies[RoleTierAdmin] = AuthzPolicy{
		Role:              RoleTierAdmin,
		AllowedTiers:      allTiers,
		AllowedOperations: []string{"read", "write", "delete", "approve", "create", "update"},
	}

	a.policies[RoleTierAuthor] = AuthzPolicy{
		Role:              RoleTierAuthor,
		AllowedTiers:      []Tier{TierSharedPrimitive, TierApplication},
		AllowedOperations: []string{"read", "write", "create", "update"},
	}

	a.policies[RoleTierViewer] = AuthzPolicy{
		Role:              RoleTierViewer,
		AllowedTiers:      allTiers,
		AllowedOperations: []string{"read"},
	}

	a.policies[RoleTierApprover] = AuthzPolicy{
		Role:              RoleTierApprover,
		AllowedTiers:      []Tier{TierInfrastructure, TierSharedPrimitive},
		AllowedOperations: []string{"read", "approve"},
	}

	return a
}

// Authorize checks whether role is permitted to perform operation on tier.
// It returns nil when authorized or a *TierBoundaryError when the role
// lacks sufficient privileges.
func (a *StdTierAuthorizer) Authorize(_ context.Context, role TierRole, tier Tier, operation string) error {
	a.mu.RLock()
	policy, ok := a.policies[role]
	a.mu.RUnlock()

	if !ok {
		return &TierBoundaryError{
			SourceTier: tier,
			TargetTier: tier,
			Operation:  operation,
			Reason:     fmt.Sprintf("no policy found for role %q", role),
		}
	}

	if !tierInSlice(tier, policy.AllowedTiers) {
		return &TierBoundaryError{
			SourceTier: tier,
			TargetTier: tier,
			Operation:  operation,
			Reason:     fmt.Sprintf("role %q is not allowed to access tier %s", role, tier),
		}
	}

	if !stringInSlice(operation, policy.AllowedOperations) {
		return &TierBoundaryError{
			SourceTier: tier,
			TargetTier: tier,
			Operation:  operation,
			Reason:     fmt.Sprintf("role %q is not allowed to perform operation %q on tier %s", role, operation, tier),
		}
	}

	return nil
}

// RegisterPolicy adds or replaces a policy for the given role.
func (a *StdTierAuthorizer) RegisterPolicy(policy AuthzPolicy) {
	a.mu.Lock()
	a.policies[policy.Role] = policy
	a.mu.Unlock()
}

// tierInSlice returns true if tier is present in the slice.
func tierInSlice(tier Tier, tiers []Tier) bool {
	for _, t := range tiers {
		if t == tier {
			return true
		}
	}
	return false
}

// stringInSlice returns true if s is present in the slice.
func stringInSlice(s string, ss []string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}
