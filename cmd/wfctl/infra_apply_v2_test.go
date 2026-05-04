package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan verifies
// the production apply path threads the loaded provider (not nil) into
// platform.ComputePlan so v2 Diff dispatch operates against a real plugin
// process at apply time. This regression-gates the temporary "nil
// placeholder" stub used to keep the package compilable between commits.
//
// The test seam is `computeInfraPlan` (var-indirected platform.ComputePlan).
// The test swaps it for a spy that captures the provider arg and asserts
// identity equality with the loaded fake. With nil passed at the call
// site the assertion would fail.
func TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan(t *testing.T) {
	fake := &iactest.NoopProvider{ProviderName: "apply-recording-stub"}

	var captured interfaces.IaCProvider
	stop := errors.New("stop after compute")

	orig := computeInfraPlan
	computeInfraPlan = func(_ context.Context, p interfaces.IaCProvider, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		captured = p
		// Return an error so applyWithProviderAndStore returns immediately
		// without dispatching a real Apply against the fake provider.
		return interfaces.IaCPlan{}, stop
	}
	t.Cleanup(func() { computeInfraPlan = orig })

	specs := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), fake, "stub", specs, nil, nil, &w, "test")
	if !errors.Is(err, stop) {
		t.Fatalf("expected sentinel error %v, got %v", stop, err)
	}
	if captured == nil {
		t.Fatal("ComputePlan was not invoked or received a nil provider")
	}
	if captured != fake {
		t.Errorf("ComputePlan received provider %p; want %p (the loaded provider)", captured, fake)
	}
}

// TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers verifies
// T3.7's manifest-driven dispatch: a provider whose
// ComputePlanVersion() returns "v2" routes through
// wfctlhelpers.ApplyPlan instead of provider.Apply. The seam is
// applyV2ApplyPlanFn (var-indirected wfctlhelpers.ApplyPlan).
//
// rev2/rev3-locked: there is NO env-var. The branch is purely
// plugin-author-controlled via plugin.json's
// iacProvider.computePlanVersion (read at provider load time and
// surfaced via the optional ComputePlanVersionDeclarer interface).
func TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers(t *testing.T) {
	v2Provider := &iactest.NoopProvider{
		ProviderName:    "v2-stub",
		DispatchVersion: "v2",
	}

	var v2Called atomic.Bool
	v2Stop := errors.New("stop after v2 ApplyPlan dispatched")

	origApply := applyV2ApplyPlanFn
	applyV2ApplyPlanFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		v2Called.Store(true)
		return nil, v2Stop
	}
	t.Cleanup(func() { applyV2ApplyPlanFn = origApply })

	// Force ComputePlan to emit a non-empty plan so applyWithProviderAndStore
	// reaches the dispatch branch instead of short-circuiting on
	// "no changes".
	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), v2Provider, "stub", specs, nil, nil, &w, "test")
	if !errors.Is(err, v2Stop) {
		t.Fatalf("expected v2 sentinel error %v, got %v", v2Stop, err)
	}
	if !v2Called.Load() {
		t.Error("wfctlhelpers.ApplyPlan was not invoked despite manifest declaring v2")
	}
}

// TestApplyWithProviderAndStore_V1FallsThroughToProviderApply
// verifies that a provider that does NOT declare v2 (via the optional
// interface) routes through the legacy provider.Apply path, not
// wfctlhelpers.ApplyPlan. Default behaviour for un-migrated plugins.
func TestApplyWithProviderAndStore_V1FallsThroughToProviderApply(t *testing.T) {
	v1Provider := &v1RecordingProvider{}

	var v2Called atomic.Bool
	origApply := applyV2ApplyPlanFn
	applyV2ApplyPlanFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		v2Called.Store(true)
		return nil, errors.New("v2 must not be invoked for v1 manifest")
	}
	t.Cleanup(func() { applyV2ApplyPlanFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(context.Background(), v1Provider, "stub", specs, nil, nil, &w, "test"); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if !v1Provider.applyCalled.Load() {
		t.Error("legacy provider.Apply was not invoked for v1 manifest")
	}
	if v2Called.Load() {
		t.Error("v2 ApplyPlan was invoked for a v1 manifest — dispatch routed to wrong path")
	}
}

// TestApplyWithProviderAndStore_V2PrintsDriftReport verifies the
// drift-report wiring: when wfctlhelpers.ApplyPlan returns a result
// with a non-empty InputDriftReport, applyWithProviderAndStore
// prints the FormatStaleError block to the writer (stderr in
// production). Pre-T3.7 the helper existed but wasn't called from
// any production path.
func TestApplyWithProviderAndStore_V2PrintsDriftReport(t *testing.T) {
	v2Provider := &iactest.NoopProvider{ProviderName: "drift-stub", DispatchVersion: "v2"}

	driftResult := &interfaces.ApplyResult{
		InputDriftReport: []interfaces.DriftEntry{
			{Name: "BUCKET_NAME", PlanFingerprint: "abc", ApplyFingerprint: "def"},
		},
	}

	origApply := applyV2ApplyPlanFn
	applyV2ApplyPlanFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		return driftResult, nil
	}
	t.Cleanup(func() { applyV2ApplyPlanFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(context.Background(), v2Provider, "stub", specs, nil, nil, &w, "test"); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if !strings.Contains(w.String(), "BUCKET_NAME") {
		t.Errorf("expected drift report to mention BUCKET_NAME; got %q", w.String())
	}
}

// v1RecordingProvider is a minimal interfaces.IaCProvider that does
// NOT implement ComputePlanVersionDeclarer (the entire point of this
// fixture: prove the dispatch defaults to v1 for un-declared
// providers). Tracks Apply invocations so the v1-routing test can
// assert legacy dispatch fired.
type v1RecordingProvider struct {
	applyCalled atomic.Bool
}

var _ interfaces.IaCProvider = (*v1RecordingProvider)(nil)

func (p *v1RecordingProvider) Name() string                                         { return "v1-stub" }
func (p *v1RecordingProvider) Version() string                                      { return "0.0.0" }
func (p *v1RecordingProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *v1RecordingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (p *v1RecordingProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.applyCalled.Store(true)
	return &interfaces.ApplyResult{}, nil
}
func (p *v1RecordingProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *v1RecordingProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *v1RecordingProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *v1RecordingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *v1RecordingProvider) SupportedCanonicalKeys() []string { return nil }
func (p *v1RecordingProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Close() error { return nil }
