package main

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan verifies
// T3.6c's contract: the production apply path threads the loaded provider
// (not nil) into platform.ComputePlan so v2 Diff dispatch (T3.6e) operates
// against a real plugin process at apply time. This regression-gates the
// "nil placeholder" stub T3.6b temporarily inserted to keep the package
// compiling between commits.
//
// The test seam is `computeInfraPlan` (var-indirected platform.ComputePlan).
// The test swaps it for a spy that captures the provider arg. After T3.6c
// the assertion `gotProvider != nil && gotProvider == fake` holds; before
// T3.6c the spy would observe `gotProvider == nil` and the assertion
// fails — closing the loop on the temporary placeholder.
func TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan(t *testing.T) {
	fake := &applyV2RecordingProvider{}

	var captured atomic.Pointer[interfaces.IaCProvider]
	stop := errors.New("stop after compute")

	orig := computeInfraPlan
	computeInfraPlan = func(_ context.Context, p interfaces.IaCProvider, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		captured.Store(&p)
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
	got := captured.Load()
	if got == nil || *got == nil {
		t.Fatal("ComputePlan was not invoked or received a nil provider — T3.6c regression")
	}
	if *got != fake {
		t.Errorf("ComputePlan received provider %p; want %p (the loaded provider)", *got, fake)
	}
}

// applyV2RecordingProvider is a minimal interfaces.IaCProvider that
// satisfies the type contract for applyWithProviderAndStore's setup phase
// (ResolveSizing). All other methods are zero-value stubs because the
// test short-circuits via the computeInfraPlan spy before any other
// provider method is exercised.
type applyV2RecordingProvider struct{}

var _ interfaces.IaCProvider = (*applyV2RecordingProvider)(nil)

func (p *applyV2RecordingProvider) Name() string                                         { return "apply-v2-recording" }
func (p *applyV2RecordingProvider) Version() string                                      { return "0.0.0" }
func (p *applyV2RecordingProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *applyV2RecordingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (p *applyV2RecordingProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) SupportedCanonicalKeys() []string { return nil }
func (p *applyV2RecordingProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *applyV2RecordingProvider) Close() error { return nil }
