package main

import (
	"bytes"
	"context"
	"errors"
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
