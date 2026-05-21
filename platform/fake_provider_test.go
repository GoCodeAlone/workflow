package platform_test

import (
	"context"
	"sync/atomic"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProvider is a no-op interfaces.IaCProvider used by in-package
// ComputePlan tests. It satisfies the interface with zero-value returns
// and optionally exposes a per-resource-type ResourceDriver whose Diff
// method returns a caller-configured *interfaces.DiffResult.
//
// The driver field is nil by default — that's the no-op shape used when
// the test only needs ComputePlan to dispatch via the legacy ConfigHash
// compare. To exercise the Diff-dispatch path use newFakeProviderWithDiff
// (or set driver on a value constructed directly).
type fakeProvider struct {
	// driver is returned from ResourceDriver for any resource type. When
	// nil, ResourceDriver returns (nil, nil) — ComputePlan must fall back
	// to the legacy ConfigHash compare in that case (per nil-tolerance
	// contract on ComputePlan godoc).
	driver *fakeDriver
}

// Compile-time interface conformance — fails the build if
// interfaces.IaCProvider drifts in a way that breaks this stub.
var _ interfaces.IaCProvider = (*fakeProvider)(nil)

func (f *fakeProvider) Name() string                                         { return "fake" }
func (f *fakeProvider) Version() string                                      { return "0.0.0-test" }
func (f *fakeProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (f *fakeProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (f *fakeProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (f *fakeProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (f *fakeProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (f *fakeProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (f *fakeProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (f *fakeProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (f *fakeProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (f *fakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if f.driver == nil {
		return nil, nil
	}
	return f.driver, nil
}
func (f *fakeProvider) SupportedCanonicalKeys() []string { return nil }
func (f *fakeProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (f *fakeProvider) Close() error { return nil }

// newFakeProvider returns a no-op fakeProvider that does not expose any
// ResourceDriver. ComputePlan dispatched through this provider must fall
// back to the legacy ConfigHash compare path (per the nil-tolerance
// contract).
func newFakeProvider() *fakeProvider {
	return &fakeProvider{}
}

// newFakeProviderWithDiff returns a fakeProvider whose ResourceDriver
// (for any type) returns a fakeDriver pre-configured to yield diff. Use
// to exercise ComputePlan's Diff-dispatch path with deterministic
// per-resource diff results.
func newFakeProviderWithDiff(diff *interfaces.DiffResult) *fakeProvider {
	return &fakeProvider{driver: &fakeDriver{diff: diff}}
}

// fakeDriver is a minimal interfaces.ResourceDriver whose Diff method
// returns a caller-supplied *interfaces.DiffResult. Other methods return
// zero values; tests should not exercise them.
type fakeDriver struct {
	diff          *interfaces.DiffResult
	diffErr       error
	diffCallCount atomic.Int64
	adopt         bool
	adoptRef      interfaces.ResourceRef
	readOutput    *interfaces.ResourceOutput
}

// Compile-time interface conformance check.
var _ interfaces.ResourceDriver = (*fakeDriver)(nil)

func (d *fakeDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return d.readOutput, nil
}
func (d *fakeDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	d.diffCallCount.Add(1)
	if d.diffErr != nil {
		return nil, d.diffErr
	}
	return d.diff, nil
}
func (d *fakeDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *fakeDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) SensitiveKeys() []string { return nil }

func (d *fakeDriver) AdoptionRef(spec interfaces.ResourceSpec) (interfaces.ResourceRef, bool, error) {
	if !d.adopt {
		return interfaces.ResourceRef{}, false, nil
	}
	ref := d.adoptRef
	if ref.Name == "" {
		ref.Name = spec.Name
	}
	if ref.Type == "" {
		ref.Type = spec.Type
	}
	return ref, true, nil
}
