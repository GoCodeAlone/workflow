package platform

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// setDiffCacheForTest swaps the package-level diff cache for c and
// restores the previous instance via t.Cleanup. Lives in this
// internal-package test file so it can touch the unexported
// planDiffCache var without exposing a production-visible setter.
func setDiffCacheForTest(t *testing.T, c diffcache.Cache) {
	t.Helper()
	planDiffCacheMu.Lock()
	prev := planDiffCache
	planDiffCache = c
	planDiffCacheMu.Unlock()
	t.Cleanup(func() {
		planDiffCacheMu.Lock()
		planDiffCache = prev
		planDiffCacheMu.Unlock()
	})
}

// TestComputePlan_CacheHitSkipsDiff verifies that running ComputePlan
// twice against unchanged inputs hits the diffcache on the second
// invocation: the per-driver Diff counter increments to 1 after the
// first call (cache miss → dispatch → cache.Put) and stays at 1 after
// the second call (cache hit → no dispatch).
//
// Run against an in-memory cache so the test owns the eviction
// horizon and doesn't read/write the developer's filesystem cache.
func TestComputePlan_CacheHitSkipsDiff(t *testing.T) {
	setDiffCacheForTest(t, diffcache.NewMemory())

	driver := &cacheTestDriver{
		diff: &interfaces.DiffResult{NeedsUpdate: true},
	}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	desired := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc", Type: "infra.vpc", ProviderID: "old-id", Outputs: map[string]any{"cidr": "10.0.0.0/16"}},
	}

	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("first ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 1 {
		t.Errorf("after first ComputePlan: Diff calls = %d, want 1 (cache miss)", got)
	}

	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 1 {
		t.Errorf("after second ComputePlan: Diff calls = %d, want 1 (cache hit, no dispatch)", got)
	}
}

// TestComputePlan_CacheMissesOnDifferentInputs verifies that varying
// any cache-key field (config, outputs, providerID) forces a re-Diff:
// the second invocation must hit the provider, not the cache.
func TestComputePlan_CacheMissesOnDifferentInputs(t *testing.T) {
	setDiffCacheForTest(t, diffcache.NewMemory())

	driver := &cacheTestDriver{
		diff: &interfaces.DiffResult{NeedsUpdate: true},
	}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	specA := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	specB := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc", Type: "infra.vpc", ProviderID: "old-id"},
	}

	if _, err := ComputePlan(context.Background(), provider, specA, current); err != nil {
		t.Fatalf("first ComputePlan: %v", err)
	}
	if _, err := ComputePlan(context.Background(), provider, specB, current); err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 2 {
		t.Errorf("Diff calls = %d, want 2 (different SHAConfig keys → both miss)", got)
	}
}

// TestComputePlan_NoopCacheNeverHits verifies that the disabled
// cache (NewNoop) never returns a hit even after Put — apply
// behaviour in cache-disabled mode is correct because every call
// re-dispatches Diff.
func TestComputePlan_NoopCacheNeverHits(t *testing.T) {
	setDiffCacheForTest(t, diffcache.NewNoop())

	driver := &cacheTestDriver{
		diff: &interfaces.DiffResult{NeedsUpdate: true},
	}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	desired := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc", Type: "infra.vpc", ProviderID: "old-id"},
	}

	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("first ComputePlan: %v", err)
	}
	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 2 {
		t.Errorf("Diff calls = %d, want 2 (noop cache never hits)", got)
	}
}

// cacheTestProvider is a minimal in-package fake satisfying
// interfaces.IaCProvider. Lives in the internal-package test file (not
// platform_test) so the same fake can drive cache-injection tests
// without exporting a setter from production code.
type cacheTestProvider struct {
	name    string
	version string
	driver  *cacheTestDriver
}

var _ interfaces.IaCProvider = (*cacheTestProvider)(nil)

func (p *cacheTestProvider) Name() string                                         { return p.name }
func (p *cacheTestProvider) Version() string                                      { return p.version }
func (p *cacheTestProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *cacheTestProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (p *cacheTestProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *cacheTestProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *cacheTestProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *cacheTestProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *cacheTestProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *cacheTestProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *cacheTestProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *cacheTestProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}
func (p *cacheTestProvider) SupportedCanonicalKeys() []string { return nil }
func (p *cacheTestProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *cacheTestProvider) Close() error { return nil }

// cacheTestDriver records the number of Diff invocations so cache-hit
// tests can assert deduplication. Diff returns the configured diff
// (or diffErr).
type cacheTestDriver struct {
	diff      *interfaces.DiffResult
	diffErr   error
	diffCount atomic.Int64
}

var _ interfaces.ResourceDriver = (*cacheTestDriver)(nil)

func (d *cacheTestDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *cacheTestDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *cacheTestDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *cacheTestDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *cacheTestDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	d.diffCount.Add(1)
	if d.diffErr != nil {
		return nil, d.diffErr
	}
	return d.diff, nil
}
func (d *cacheTestDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *cacheTestDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *cacheTestDriver) SensitiveKeys() []string { return nil }

// TestParseConcurrencyEnv covers the env-var parsing/clamping that
// gates plan-time Diff fan-out. Extracted from planDiffConcurrency
// (which uses sync.Once and is therefore unit-untestable in-process)
// so the boundary cases can be exercised without process restart.
func TestParseConcurrencyEnv(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", planDiffConcurrencyDefault},
		{"abc", planDiffConcurrencyDefault},
		{"-5", planDiffConcurrencyMin},
		{"0", planDiffConcurrencyMin},
		{"1", 1},
		{"8", 8},
		{"32", 32},
		{"33", planDiffConcurrencyMax},
		{"100", planDiffConcurrencyMax},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := parseConcurrencyEnv(tc.in); got != tc.want {
				t.Errorf("parseConcurrencyEnv(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestComputePlan_ParallelDispatch_AllCandidatesObserveDiff verifies
// that errgroup fans out across multiple modification candidates: with
// N=5 distinct existing resources, the driver's Diff counter must
// reach 5 (each resource dispatched exactly once) and the resulting
// plan must contain 5 actions in DependsOn order. Without parallel
// dispatch, an accidental g.SetLimit(1) would still pass — but a
// future refactor that drops the errgroup loop entirely (regressing
// to a serial loop that happens to skip one) would fail this test.
func TestComputePlan_ParallelDispatch_AllCandidatesObserveDiff(t *testing.T) {
	setDiffCacheForTest(t, diffcache.NewNoop()) // disable cache so every dispatch hits the driver

	driver := &cacheTestDriver{diff: &interfaces.DiffResult{NeedsUpdate: true}}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	const n = 5
	desired := make([]interfaces.ResourceSpec, n)
	current := make([]interfaces.ResourceState, n)
	for i := 0; i < n; i++ {
		name := "vpc-" + string(rune('A'+i))
		// Each resource has a distinct config so cache-key differs even if we re-enabled caching.
		desired[i] = interfaces.ResourceSpec{Name: name, Type: "infra.vpc", Config: map[string]any{"region": "r" + string(rune('0'+i))}}
		current[i] = interfaces.ResourceState{Name: name, Type: "infra.vpc", ProviderID: "id-" + name}
	}

	plan, err := ComputePlan(context.Background(), provider, desired, current)
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != int64(n) {
		t.Errorf("Diff calls = %d, want %d (one per modification candidate)", got, n)
	}
	if len(plan.Actions) != n {
		t.Fatalf("plan.Actions = %d, want %d", len(plan.Actions), n)
	}
	for i, a := range plan.Actions {
		if a.Action != "update" {
			t.Errorf("plan.Actions[%d].Action = %q, want update", i, a.Action)
		}
	}
}

// TestComputePlan_DriverReturnsNilDiff_EmitsNothing covers the (nil,
// nil) return shape of ResourceDriver.Diff: a driver that knows the
// resource has no changes returns nil rather than a zero-value
// DiffResult. ComputePlan must treat that as a no-op (no plan
// action). Was implicitly covered by the no-changes test in T3.6e but
// pinned explicitly here per T3.6e adversarial review #3.
func TestComputePlan_DriverReturnsNilDiff_EmitsNothing(t *testing.T) {
	setDiffCacheForTest(t, diffcache.NewNoop())

	driver := &cacheTestDriver{diff: nil} // explicit nil
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}
	current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}

	plan, err := ComputePlan(context.Background(), provider, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("expected no plan actions when Diff returns nil; got %+v", plan.Actions)
	}
	if got := driver.diffCount.Load(); got != 1 {
		t.Errorf("Diff was called %d times, want 1 (verifies dispatch reached driver)", got)
	}
}
