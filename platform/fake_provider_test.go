package platform_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeProvider is a no-op interfaces.IaCProvider used by in-package
// ComputePlan tests. It satisfies the interface with zero-value returns and
// optionally exposes a per-resource-type ResourceDriver whose Diff method
// returns a caller-configured *interfaces.DiffResult.
//
// In T3.6a only the no-op shape is used (ComputePlan accepts the provider
// but does not dispatch to it). T3.6e extends this fake via
// newFakeProviderWithDiff and exercises the Diff dispatch path; T3.6f's
// cache test uses the diffCallCount counter to assert cache hits.
type fakeProvider struct {
	// driver is returned from ResourceDriver for any resource type. When nil,
	// ResourceDriver returns (nil, nil) — the no-op shape used by T3.6a.
	driver *fakeDriver
}

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
// ResourceDriver. Use this for T3.6a-style tests that exercise the legacy
// ConfigHash compare path without invoking provider.Diff.
func newFakeProvider(_ *testing.T) *fakeProvider {
	return &fakeProvider{}
}

// fakeDriver is a minimal interfaces.ResourceDriver whose Diff method
// returns a caller-supplied *interfaces.DiffResult. Other methods return
// zero values; tests should not exercise them.
type fakeDriver struct {
	diff          *interfaces.DiffResult
	diffErr       error
	diffCallCount atomic.Int64
}

func (d *fakeDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *fakeDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	d.diffCallCount.Add(1)
	return d.diff, d.diffErr
}
func (d *fakeDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *fakeDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *fakeDriver) SensitiveKeys() []string { return nil }
