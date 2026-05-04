package interfaces

import (
	"context"
	"testing"
)

func TestProviderPlanner_TypeAssertionCompiles(t *testing.T) {
	var _ ProviderPlanner = (*mockPlanner)(nil)
}

type mockPlanner struct{}

func (m *mockPlanner) PlanV2(ctx context.Context, desired []ResourceSpec, current []ResourceState) (IaCPlan, error) {
	return IaCPlan{}, nil
}

// Sanity: ProviderPlanner is purely additive — providers that don't implement
// it remain valid IaCProvider implementations. Verified at the type system
// level (this test passing means the interface compiles + the type-assertion
// pattern works for downstream callers).
