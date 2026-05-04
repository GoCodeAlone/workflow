package interfaces_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestProviderPlanner_TypeAssertionCompiles verifies the optional-interface
// pattern: (1) a type CAN implement ProviderPlanner, and (2) a type that
// implements IaCProvider is NOT required to implement ProviderPlanner —
// the additivity claim ADR 009 makes about the v0.21.0 ship is enforced
// here at the type system level. The negative case reuses the package's
// existing mockProvider fixture (defined in iac_test.go), which intentionally
// does not implement PlanV2.
func TestProviderPlanner_TypeAssertionCompiles(t *testing.T) {
	// (1) plannerStub implements ProviderPlanner.
	var _ interfaces.ProviderPlanner = (*plannerStub)(nil)

	// (2) mockProvider implements IaCProvider but NOT ProviderPlanner.
	// If a future change accidentally moved PlanV2 onto IaCProvider (or
	// made ProviderPlanner a required embedded interface), this runtime
	// assertion would fail.
	var p interfaces.IaCProvider = (*mockProvider)(nil)
	if _, ok := p.(interfaces.ProviderPlanner); ok {
		t.Errorf("mockProvider should not satisfy ProviderPlanner; the optional-interface idiom requires the negative case to assert false")
	}
}

type plannerStub struct{}

func (p *plannerStub) PlanV2(ctx context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
	return interfaces.IaCPlan{}, nil
}
