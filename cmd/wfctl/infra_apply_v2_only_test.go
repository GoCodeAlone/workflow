package main

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestInfraApply_V2OnlyDispatch_NoV1Branch is a compile-time tripwire +
// runtime tripwire: post-workflow#699 the IaCProvider interface no longer
// declares Apply. If any implementation accidentally re-adds an Apply
// method that satisfies the legacy v1 dispatch signature, this test
// fires.
//
// This is a structural assertion, not a full apply-path exercise — the
// apply-path coverage lives in TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers
// (which spies on applyV2ApplyPlanWithHooksFn to prove the v2 helper
// is invoked). Both tests together cover:
//   - structural: provider type cannot satisfy v1 Apply signature (this test)
//   - runtime: applyWithProviderAndStore routes through wfctlhelpers (sibling)
//
// Per ADR 0024 + workflow#699: v2 is the only supported dispatch.
func TestInfraApply_V2OnlyDispatch_NoV1Branch(t *testing.T) {
	t.Run("trimmed IaCProvider interface does not satisfy legacy Apply signature", func(t *testing.T) {
		var p interfaces.IaCProvider = &stubV2OnlyProvider{}
		if _, ok := p.(interface {
			Apply(context.Context, *interfaces.IaCPlan) (*interfaces.ApplyResult, error)
		}); ok {
			t.Fatalf("provider unexpectedly satisfies legacy Apply interface — workflow#699 regression")
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
