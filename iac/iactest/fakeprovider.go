// Package iactest provides shared fakes for tests that exercise the
// IaCProvider / ResourceDriver contract. Lifting these out of individual
// test files prevents the no-op-stub proliferation that otherwise occurs
// every time a new package needs to satisfy the interface (one no-op was
// already duplicated three times across this repo before consolidation).
//
// The types here are deliberately minimal: each method returns a zero
// value or a caller-configured field. Tests that need richer behavior
// should compose these (embed the struct and override specific methods)
// rather than introducing a parallel implementation.
package iactest

import (
	"context"
	"sync/atomic"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// NoopProvider is a minimal interfaces.IaCProvider whose every method
// returns the zero value (nil error, nil result). It satisfies the
// interface for compile-time checks and exists so callers do not need to
// hand-roll a 14-method shell every time they need a placeholder.
//
// When a test needs to observe Diff dispatch or driver behavior, supply
// a non-nil Driver — ResourceDriver(typ) returns it for any resource
// type — and configure that driver's behavior via NoopDriver fields.
type NoopProvider struct {
	// Driver, when non-nil, is returned from ResourceDriver(typ) for
	// every resource type. Leave nil for the pure-no-op shape used by
	// callers that only need to satisfy the interface signature.
	Driver *NoopDriver

	// ProviderName overrides the Name() return value. Defaults to
	// "iactest-noop" when empty.
	ProviderName string

	// ProviderVersion overrides the Version() return value. Defaults
	// to "0.0.0-iactest" when empty.
	ProviderVersion string
}

// Compile-time interface conformance check — fails the build if
// interfaces.IaCProvider drifts in a way that breaks this stub.
var _ interfaces.IaCProvider = (*NoopProvider)(nil)

// Name returns ProviderName when set, otherwise "iactest-noop".
func (p *NoopProvider) Name() string {
	if p.ProviderName != "" {
		return p.ProviderName
	}
	return "iactest-noop"
}

// Version returns ProviderVersion when set, otherwise "0.0.0-iactest".
func (p *NoopProvider) Version() string {
	if p.ProviderVersion != "" {
		return p.ProviderVersion
	}
	return "0.0.0-iactest"
}

// Initialize is a no-op.
func (p *NoopProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }

// Capabilities returns nil.
func (p *NoopProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }

// Plan returns (nil, nil).
func (p *NoopProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}

// Apply returns (nil, nil).
func (p *NoopProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}

// Destroy returns (nil, nil).
func (p *NoopProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}

// Status returns (nil, nil).
func (p *NoopProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}

// DetectDrift returns (nil, nil).
func (p *NoopProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}

// Import returns (nil, nil).
func (p *NoopProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}

// ResolveSizing returns (nil, nil).
func (p *NoopProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}

// ResourceDriver returns Driver (which may be nil — callers that dispatch
// Diff against the result must guard nil). Returning nil with no error
// matches the contract platform.ComputePlan tolerates: a missing driver
// for a resource type means the plan falls back to the legacy compare.
func (p *NoopProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if p.Driver == nil {
		return nil, nil
	}
	return p.Driver, nil
}

// SupportedCanonicalKeys returns nil.
func (p *NoopProvider) SupportedCanonicalKeys() []string { return nil }

// BootstrapStateBackend returns (nil, nil).
func (p *NoopProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}

// Close is a no-op.
func (p *NoopProvider) Close() error { return nil }

// NoopDriver is a minimal interfaces.ResourceDriver whose Diff method
// returns DiffResult (or DiffErr when set) and tracks call count so
// cache-hit tests can assert deduplication. Other lifecycle methods
// (Delete, Read) also bump per-method counters and accept a configured
// return value, so dispatch-coverage tests (notably
// iac/conformance/scenario_delete_action.go and
// iac/conformance/scenario_outputs_refresh.go) can pin driver-side
// invocation and observe live-shape reconciliation. Methods without an
// explicit counter return zero values.
type NoopDriver struct {
	// DiffResult is returned from Diff(). When nil with DiffErr also nil,
	// callers receive the plain (nil, nil) shape (treated by ComputePlan
	// as "no changes — skip").
	DiffResult *interfaces.DiffResult

	// DiffErr is returned from Diff() when set; takes precedence over
	// DiffResult.
	DiffErr error

	// DeleteErr is returned from Delete() when set. The default zero
	// value (nil) makes Delete a no-op success — matching the rest of
	// the NoopDriver shape.
	DeleteErr error

	// ReadResult is returned from Read(). When nil with ReadErr also
	// nil, callers receive the plain (nil, nil) shape that pre-T7.5
	// code expects from the Noop fake. Tests that exercise
	// iac/refreshoutputs.Refresh set this to a *ResourceOutput whose
	// Outputs map carries the live-shape keys the scenario asserts
	// reconciliation for.
	ReadResult *interfaces.ResourceOutput

	// ReadErr is returned from Read() when set; takes precedence over
	// ReadResult.
	ReadErr error

	// DiffCallCount is bumped on every Diff invocation. Exposed via
	// atomic.Int64 so cache-hit tests under -race do not need separate
	// synchronization.
	DiffCallCount atomic.Int64

	// DeleteCallCount is bumped on every Delete invocation. Exposed via
	// atomic.Int64 so dispatch-coverage tests (e.g.,
	// iac/conformance/scenarios_test.go) under -race can assert
	// driver.Delete was invoked without separate synchronization.
	DeleteCallCount atomic.Int64

	// ReadCallCount is bumped on every Read invocation. Exposed via
	// atomic.Int64 so refresh-outputs tests can assert driver.Read was
	// dispatched without separate synchronization.
	ReadCallCount atomic.Int64
}

// Compile-time interface conformance check.
var _ interfaces.ResourceDriver = (*NoopDriver)(nil)

// Create returns (nil, nil).
func (d *NoopDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// Read bumps ReadCallCount and returns the configured ReadResult/
// ReadErr pair (ReadErr takes precedence when non-nil). The (nil, nil)
// shape is preserved when neither field is set, matching pre-T7.5
// callers that only need the no-op signature.
func (d *NoopDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	d.ReadCallCount.Add(1)
	if d.ReadErr != nil {
		return nil, d.ReadErr
	}
	return d.ReadResult, nil
}

// Update returns (nil, nil).
func (d *NoopDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// Delete bumps DeleteCallCount and returns DeleteErr (nil by default).
// The counter is the dispatch-coverage hook used by conformance
// scenarios that need to assert the v2 apply path actually reached
// driver.Delete (closing the latent gap from T3.3 where DOProvider's
// case-arm-less dispatch silently skipped Delete).
func (d *NoopDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.DeleteCallCount.Add(1)
	return d.DeleteErr
}

// Diff bumps DiffCallCount and returns the configured DiffResult/DiffErr
// pair (DiffErr takes precedence when non-nil).
func (d *NoopDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	d.DiffCallCount.Add(1)
	if d.DiffErr != nil {
		return nil, d.DiffErr
	}
	return d.DiffResult, nil
}

// HealthCheck returns (nil, nil).
func (d *NoopDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}

// Scale returns (nil, nil).
func (d *NoopDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// SensitiveKeys returns nil.
func (d *NoopDriver) SensitiveKeys() []string { return nil }
