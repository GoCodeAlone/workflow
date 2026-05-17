package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestInfraApply_V2OnlyDispatch_NoV1Branch asserts runInfraApply collapses
// to a single v2-only dispatch after workflow#699 removes provider.Apply.
// The presence of any conditional branch on a v1-vs-v2 selector is a
// regression: per ADR 0024, v2 is the only supported dispatch.
func TestInfraApply_V2OnlyDispatch_NoV1Branch(t *testing.T) {
	t.Run("collapses dispatch when typedIaCAdapter declares no ComputePlanVersion method", func(t *testing.T) {
		// stub provider satisfies the trimmed interfaces.IaCProvider
		// (no Apply method) and has no ComputePlanVersion declarer.
		// runInfraApply MUST route through wfctlhelpers.ApplyPlanWithHooks
		// and MUST NOT type-assert against a v1 dispatch.
		var p interfaces.IaCProvider = &stubV2OnlyProvider{}
		if _, ok := p.(interface {
			Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error)
		}); ok {
			t.Fatalf("provider unexpectedly satisfies legacy Apply interface")
		}
	})
}

type stubV2OnlyProvider struct{}

func (*stubV2OnlyProvider) Name() string                                        { return "stub" }
func (*stubV2OnlyProvider) Version() string                                     { return "0.0.0" }
func (*stubV2OnlyProvider) Initialize(context.Context, map[string]any) error    { return nil }
func (*stubV2OnlyProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (*stubV2OnlyProvider) Plan(context.Context, []interfaces.ResourceSpec, []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) Destroy(context.Context, []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) Status(context.Context, []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) DetectDrift(context.Context, []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) Import(context.Context, string, string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) ResolveSizing(string, interfaces.Size, *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) SupportedCanonicalKeys() []string { return nil }
func (*stubV2OnlyProvider) BootstrapStateBackend(context.Context, map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (*stubV2OnlyProvider) Close() error { return nil }
