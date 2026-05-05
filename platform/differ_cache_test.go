package platform

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// The test-helper that swaps the package-level diff cache used to
// live here as the package-private setDiffCacheForTest. It was
// promoted to the exported platform.SetDiffCacheForTest in
// differ_test_helper.go (W-7 follow-up) so external test packages
// (notably iac/conformance) can install a deterministic no-op cache
// without relying on the WFCTL_DIFFCACHE env var, which only takes
// effect on the very first call to getDiffCache (sync.Once).

// TestComputePlan_CacheHitSkipsDiff verifies that running ComputePlan
// twice against unchanged inputs hits the diffcache on the second
// invocation: the per-driver Diff counter increments to 1 after the
// first call (cache miss → dispatch → cache.Put) and stays at 1 after
// the second call (cache hit → no dispatch).
//
// Run against an in-memory cache so the test owns the eviction
// horizon and doesn't read/write the developer's filesystem cache.
func TestComputePlan_CacheHitSkipsDiff(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewMemory())

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
	SetDiffCacheForTest(t, diffcache.NewMemory())

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
	SetDiffCacheForTest(t, diffcache.NewNoop())

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
// without exporting a setter from production code. driver is typed as
// interfaces.ResourceDriver so different test fixtures (counting,
// channel-gated) can share the provider shell.
type cacheTestProvider struct {
	name    string
	version string
	driver  interfaces.ResourceDriver
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
		// name is the subtest label (avoids using the raw empty string
		// from `in` as the t.Run name, which Go's testing package
		// silently rewrites to "#00" — readable in -v output but masks
		// the case identity in failure reports).
		name string
		in   string
		want int
	}{
		{"empty", "", planDiffConcurrencyDefault},
		{"non_numeric", "abc", planDiffConcurrencyDefault},
		{"negative", "-5", planDiffConcurrencyMin},
		{"zero", "0", planDiffConcurrencyMin},
		{"one", "1", 1},
		{"eight", "8", 8},
		{"thirty_two", "32", 32},
		{"thirty_three_clamped_to_max", "33", planDiffConcurrencyMax},
		{"one_hundred_clamped_to_max", "100", planDiffConcurrencyMax},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
	SetDiffCacheForTest(t, diffcache.NewNoop()) // disable cache so every dispatch hits the driver

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

// TestComputePlan_ParallelDiffDispatch_InFlightGoroutinesObserved
// strengthens the count-only parallel-dispatch test by proving that
// 2+ Diff goroutines run simultaneously, not just sequentially. Uses
// a channel-gated driver: each Diff invocation increments an
// in-flight counter, signals on `entered`, then blocks on `release`
// until the test releases all candidates at once. If the dispatch
// were accidentally serialized (g.SetLimit(1) regression), only one
// goroutine would enter Diff and the test would hang on the second
// `<-entered`.
func TestComputePlan_ParallelDiffDispatch_InFlightGoroutinesObserved(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewNoop())

	const n = 4
	driver := &channelGatedDriver{
		entered: make(chan struct{}, n),
		release: make(chan struct{}),
	}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	desired := make([]interfaces.ResourceSpec, n)
	current := make([]interfaces.ResourceState, n)
	for i := 0; i < n; i++ {
		name := "vpc-" + string(rune('A'+i))
		desired[i] = interfaces.ResourceSpec{Name: name, Type: "infra.vpc", Config: map[string]any{"region": "r" + string(rune('0'+i))}}
		current[i] = interfaces.ResourceState{Name: name, Type: "infra.vpc", ProviderID: "id-" + name}
	}

	// Run ComputePlan in a separate goroutine so the test can observe
	// in-flight Diff calls before the dispatch returns.
	done := make(chan error, 1)
	go func() {
		_, err := ComputePlan(context.Background(), provider, desired, current)
		done <- err
	}()

	// Wait for at least 2 Diff calls to enter concurrently. The default
	// concurrency is 8 (clamped above) so up to all 4 candidates can
	// run in parallel; we conservatively assert ≥2 to avoid relying on
	// scheduler timing for the upper bound.
	deadline := time.After(5 * time.Second)
	const minInFlight = 2
	for i := 0; i < minInFlight; i++ {
		select {
		case <-driver.entered:
		case <-deadline:
			t.Fatalf("only %d Diff goroutine(s) entered concurrently after 5s; expected ≥%d (regression toward serial dispatch)", i, minInFlight)
		}
	}
	if got := driver.inFlight.Load(); got < minInFlight {
		t.Errorf("inFlight peak = %d, want ≥%d", got, minInFlight)
	}

	// Release all blocked Diff calls and let ComputePlan finish.
	close(driver.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ComputePlan: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ComputePlan did not return after release; goroutines stuck")
	}

	if got := driver.diffCount.Load(); got != int64(n) {
		t.Errorf("Diff calls = %d, want %d (one per candidate)", got, n)
	}
}

// channelGatedDriver is a ResourceDriver that blocks every Diff call
// on a shared release channel so tests can observe in-flight
// concurrency. inFlight is the *current* (live) count of in-progress
// Diff goroutines — incremented on Diff entry, decremented on return.
// It is NOT a peak/high-water-mark counter; the parallel-dispatch
// assertion is made via the `entered` channel (which receives one
// signal per goroutine that has reached the gate), not via
// inFlight. The atomic counter is retained for diagnostic logging
// and as a sanity invariant (reaches zero after release).
type channelGatedDriver struct {
	entered   chan struct{}
	release   chan struct{}
	diffCount atomic.Int64
	inFlight  atomic.Int64 // current in-flight count (NOT peak); see docstring
}

var _ interfaces.ResourceDriver = (*channelGatedDriver)(nil)

func (d *channelGatedDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *channelGatedDriver) Read(_ context.Context, _ interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *channelGatedDriver) Update(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *channelGatedDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }
func (d *channelGatedDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	// inFlight is bumped/decremented for diagnostic visibility + the
	// "drains to zero after release" sanity invariant. The actual
	// parallel-dispatch assertion is made via the `entered` channel
	// (one signal per goroutine that has reached this gate); see the
	// channelGatedDriver docstring.
	d.inFlight.Add(1)
	defer d.inFlight.Add(-1)
	d.diffCount.Add(1)
	d.entered <- struct{}{}
	<-d.release
	return &interfaces.DiffResult{NeedsUpdate: true}, nil
}
func (d *channelGatedDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}
func (d *channelGatedDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}
func (d *channelGatedDriver) SensitiveKeys() []string { return nil }

// TestPluginVersionKey_NoCollisionOnAtSeparator covers the rev3 fix
// for the cache-collision risk introduced when PluginVersion was
// composed via `name + "@" + version`. Two genuinely-different
// providers — `("foo", "bar@1.0")` vs `("foo@bar", "1.0")` — would
// both produce the literal string `"foo@bar@1.0"` and serve each
// other's cached DiffResults. The sha256(name + "\x00" + version)
// composition pins these as distinct keys.
func TestPluginVersionKey_NoCollisionOnAtSeparator(t *testing.T) {
	a := &cacheTestProvider{name: "foo", version: "bar@1.0"}
	b := &cacheTestProvider{name: "foo@bar", version: "1.0"}
	keyA := pluginVersionKey(a)
	keyB := pluginVersionKey(b)
	if keyA == keyB {
		t.Errorf("pluginVersionKey collision: %q == %q for distinct (name, version) pairs", keyA, keyB)
	}
}

// TestPluginVersionKey_NilProvider returns the empty key without
// panicking; classifyModification's nil-provider path doesn't reach
// the cache lookup, but defending the helper protects future callers.
func TestPluginVersionKey_NilProvider(t *testing.T) {
	if got := pluginVersionKey(nil); got != "" {
		t.Errorf("pluginVersionKey(nil) = %q, want empty", got)
	}
}

// TestPluginVersionKey_Stable verifies the helper is deterministic —
// the same (name, version) pair always produces the same key.
func TestPluginVersionKey_Stable(t *testing.T) {
	p := &cacheTestProvider{name: "do", version: "v0.10.0"}
	first := pluginVersionKey(p)
	second := pluginVersionKey(p)
	if first != second {
		t.Errorf("pluginVersionKey not deterministic: %q vs %q", first, second)
	}
	if first == "" {
		t.Error("pluginVersionKey returned empty string for non-nil provider")
	}
}

// TestComputePlan_DriverReturnsNilDiff_EmitsNothing covers the (nil,
// nil) return shape of ResourceDriver.Diff: a driver that knows the
// resource has no changes returns nil rather than a zero-value
// DiffResult. ComputePlan must treat that as a no-op (no plan
// action). Was implicitly covered by the no-changes test in T3.6e but
// pinned explicitly here per T3.6e adversarial review #3.
func TestComputePlan_DriverReturnsNilDiff_EmitsNothing(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewNoop())

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

// TestComputePlan_EmptyProviderID_BypassesCache pins the bypass for
// the empty-ProviderID hash-collision risk: the cache key shape
// (PluginVersion, Type, ProviderID, SHAConfig, SHAOutputs) does not
// include the resource Name, so two existing-state resources of the
// same Type with ProviderID=="" + matching SHAConfig + SHAOutputs
// would otherwise serve each other's cached DiffResult and
// misclassify actions. classifyModification skips Get/Put when
// ProviderID is empty and always re-dispatches. This test exercises
// two same-Type resources whose only differentiator is Name (each
// has ProviderID=="") and asserts the driver receives a Diff call
// for each, not just one.
func TestComputePlan_EmptyProviderID_BypassesCache(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewMemory())

	driver := &cacheTestDriver{diff: &interfaces.DiffResult{NeedsUpdate: true}}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	// Two resources, same Type + Config + Outputs, distinct Names —
	// hash-collide on (Type, ProviderID="", SHAConfig, SHAOutputs).
	desired := []interfaces.ResourceSpec{
		{Name: "vpc-a", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
		{Name: "vpc-b", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc-a", Type: "infra.vpc", ProviderID: "", Outputs: map[string]any{"cidr": "10.0.0.0/16"}},
		{Name: "vpc-b", Type: "infra.vpc", ProviderID: "", Outputs: map[string]any{"cidr": "10.0.0.0/16"}},
	}

	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 2 {
		t.Errorf("Diff calls = %d, want 2 (empty ProviderID bypasses cache; both resources re-dispatched)", got)
	}
	// Sanity: a second invocation also re-dispatches both, since cache
	// is bypassed entirely on the empty-ProviderID path.
	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 4 {
		t.Errorf("Diff calls after 2nd ComputePlan = %d, want 4 (no cache hits when ProviderID is empty)", got)
	}
}

// TestComputePlan_NilDiffResult_CachesAsZeroValue pins the round-5
// fix: providers that return (nil, nil) from driver.Diff to indicate
// "no changes" (a documented option in the (DiffResult|nil, error|nil)
// return shape) get the same cache benefit as providers that return
// &DiffResult{}. Before the fix, the cache.Put was guarded by
// `fresh != nil`, so nil-as-no-op convention providers re-Diffed on
// every ComputePlan call, undermining the cache contract. The fix
// caches a zero-value DiffResult on (nil, nil) returns; classifyModification's
// downstream switch treats zero-value the same as nil (no plan
// action), so the semantic is preserved while the cache stays
// effective.
func TestComputePlan_NilDiffResult_CachesAsZeroValue(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewMemory())

	driver := &cacheTestDriver{diff: nil} // nil-as-no-op convention
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	desired := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc", Type: "infra.vpc", ProviderID: "pid-vpc", Outputs: map[string]any{"cidr": "10.0.0.0/16"}},
	}

	plan1, err := ComputePlan(context.Background(), provider, desired, current)
	if err != nil {
		t.Fatalf("first ComputePlan: %v", err)
	}
	if len(plan1.Actions) != 0 {
		t.Errorf("first ComputePlan: expected no actions for nil-DiffResult; got %+v", plan1.Actions)
	}
	if got := driver.diffCount.Load(); got != 1 {
		t.Errorf("after first ComputePlan: Diff calls = %d, want 1 (cache miss)", got)
	}

	plan2, err := ComputePlan(context.Background(), provider, desired, current)
	if err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if len(plan2.Actions) != 0 {
		t.Errorf("second ComputePlan: expected no actions on cache hit; got %+v", plan2.Actions)
	}
	if got := driver.diffCount.Load(); got != 1 {
		t.Errorf("after second ComputePlan: Diff calls = %d, want 1 (cache hit on zero-value DiffResult; round-5 fix)", got)
	}
}

// TestComputePlan_UnhashableInputs_BypassCache pins the round-9 fix:
// when spec.Config or rs.Outputs contains a non-marshalable value
// (channel, function, cycle), configHashErr surfaces the marshal
// failure and ComputePlan bypasses the diff cache for that resource —
// rather than collapsing all unmarshalable inputs to the constant
// sha256("") hash and risking cache-key collisions across distinct
// resources serving each other's cached DiffResult.
//
// In practice IaC configs come from YAML and cannot contain these
// types, so this is defensive coverage for custom-provider Outputs
// or future code paths.
func TestComputePlan_UnhashableInputs_BypassCache(t *testing.T) {
	SetDiffCacheForTest(t, diffcache.NewMemory())

	driver := &cacheTestDriver{diff: &interfaces.DiffResult{NeedsUpdate: true}}
	provider := &cacheTestProvider{name: "fake", version: "0.0.0-test", driver: driver}

	// Two resources, both with non-marshalable values in Outputs (a
	// channel, which json.Marshal rejects). Without the bypass, both
	// would hash to the constant sha256("") and the second resource
	// would be served the first's cached DiffResult.
	unmarshalable := make(chan int)
	desired := []interfaces.ResourceSpec{
		{Name: "vpc-a", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
		{Name: "vpc-b", Type: "infra.vpc", Config: map[string]any{"region": "sfo3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc-a", Type: "infra.vpc", ProviderID: "pid-a", Outputs: map[string]any{"poison": unmarshalable}},
		{Name: "vpc-b", Type: "infra.vpc", ProviderID: "pid-b", Outputs: map[string]any{"poison": unmarshalable}},
	}

	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 2 {
		t.Errorf("Diff calls = %d, want 2 (one per resource on first pass)", got)
	}
	// Second pass: both resources should re-Diff (cache bypassed),
	// not share a cached entry. Without the round-9 fix, both
	// resources collapse to the same SHAOutputs="" key on the first
	// pass, only one cache.Put happens (overwriting), and the
	// second pass hits cache for both — diffCount would stop at 3
	// (one re-dispatch + one bogus hit).
	if _, err := ComputePlan(context.Background(), provider, desired, current); err != nil {
		t.Fatalf("second ComputePlan: %v", err)
	}
	if got := driver.diffCount.Load(); got != 4 {
		t.Errorf("Diff calls after 2nd ComputePlan = %d, want 4 (no cache hits when Outputs are unhashable; round-9 fix)", got)
	}
}

// TestConfigHashErr_PropagatesMarshalFailure is a unit-level pin for
// configHashErr. Marshalable inputs return (hash, nil); a non-
// marshalable value (channel) returns ("", non-nil-err) so cache-key
// callers can deterministically bypass the cache.
//
// Backward compatibility: the un-suffixed configHash silently swallows
// the marshal error and returns the sha256-of-best-effort-bytes hash
// (matches the pre-T3.6 implementation byte-for-byte) so any persisted
// ResolvedConfigHash / ConfigHash values computed under the old
// behavior stay stable. configHashErr is the strict variant that must
// be used for cache keys; configHash is the legacy ABI.
func TestConfigHashErr_PropagatesMarshalFailure(t *testing.T) {
	good := map[string]any{"region": "nyc3", "size": 4}
	gotHash, gotErr := configHashErr(good)
	if gotErr != nil {
		t.Fatalf("configHashErr(good): unexpected err: %v", gotErr)
	}
	if len(gotHash) != 64 {
		t.Errorf("configHashErr(good): hash len = %d, want 64 (sha256 hex)", len(gotHash))
	}

	bad := map[string]any{"poison": make(chan int)}
	gotHash, gotErr = configHashErr(bad)
	if gotErr == nil {
		t.Errorf("configHashErr(bad): expected non-nil error for unmarshalable input; got hash=%q", gotHash)
	}
	if gotHash != "" {
		t.Errorf("configHashErr(bad): hash = %q, want empty string when err != nil", gotHash)
	}

	// Backward-compatible wrapper: configHash returns the legacy
	// sha256-of-best-effort hash for unmarshalable inputs (matches the
	// pre-T3.6 implementation byte-for-byte; cache callers MUST use
	// configHashErr instead). The exact value here is sha256(nil),
	// which is what json.Marshal followed by sha256.Sum256 produces
	// when Marshal returns nil + err.
	got := configHash(bad)
	const sha256OfNil = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != sha256OfNil {
		t.Errorf("configHash(bad) = %q, want %q (legacy sha256(nil) — pre-T3.6 byte-for-byte stability)", got, sha256OfNil)
	}
}
