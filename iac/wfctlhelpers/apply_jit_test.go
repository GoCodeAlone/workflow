package wfctlhelpers

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// jitRecordingDriver remembers every ResourceSpec.Config map it receives
// across Create / Update so JIT-substitution tests can assert exactly what
// the driver saw post-substitution. Per-instance: each test gets its own
// driver via newJITRecordingProvider().
//
// Per-resource Create return values are configurable via createReturns —
// when set, the driver returns the matching ResourceOutput for spec.Name;
// otherwise it falls back to the standard fake-id-<name> shape.
type jitRecordingDriver struct {
	*fakeDriver
	mu             sync.Mutex
	seenConfigs    map[string]map[string]any // resource Name → Config seen on Create
	seenUpdateCfgs map[string]map[string]any // resource Name → Config seen on Update
	createReturns  map[string]*interfaces.ResourceOutput
}

func (d *jitRecordingDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCount++
	d.mu.Lock()
	if d.seenConfigs == nil {
		d.seenConfigs = make(map[string]map[string]any)
	}
	d.seenConfigs[spec.Name] = spec.Config
	d.mu.Unlock()
	if out, ok := d.createReturns[spec.Name]; ok && out != nil {
		return out, nil
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "fake-id-" + spec.Name}, nil
}

func (d *jitRecordingDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCount++
	d.mu.Lock()
	if d.seenUpdateCfgs == nil {
		d.seenUpdateCfgs = make(map[string]map[string]any)
	}
	d.seenUpdateCfgs[spec.Name] = spec.Config
	d.mu.Unlock()
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

// jitRecordingProvider returns the same jitRecordingDriver for every
// resource type so a single instance records all dispatches in one
// ApplyPlan call.
type jitRecordingProvider struct {
	*fakeProvider
	driver *jitRecordingDriver
}

func newJITRecordingProvider() *jitRecordingProvider {
	base := newFakeProvider()
	return &jitRecordingProvider{
		fakeProvider: base,
		driver:       &jitRecordingDriver{fakeDriver: base.driver},
	}
}

func (p *jitRecordingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// specWithConfig builds a ResourceSpec with a Config map for JIT tests.
// Mirrors the existing spec() helper but adds the Config field that
// JIT-substitution targets.
func specWithConfig(name, typ string, cfg map[string]any) interfaces.ResourceSpec {
	return interfaces.ResourceSpec{Name: name, Type: typ, Config: cfg}
}

// TestApplyPlan_JIT_TwoCreate_BSpecResolvesAID is the canonical T5.2
// scenario from the plan: a 2-action plan (create A, then create B) where
// B's Config references ${A.id}. ApplyPlan must call ResolveSpec on B
// before dispatching, so the driver sees B's Config["vpc_uuid"] resolved
// to A's freshly-minted ProviderID.
func TestApplyPlan_JIT_TwoCreate_BSpecResolvesAID(t *testing.T) {
	fp := newJITRecordingProvider()
	fp.driver.createReturns = map[string]*interfaces.ResourceOutput{
		"a-vpc": {Name: "a-vpc", Type: "infra.vpc", ProviderID: "vpc-uuid-12345"},
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: specWithConfig("a-vpc", "infra.vpc", map[string]any{
			"cidr": "10.0.0.0/16",
		})},
		{Action: "create", Resource: specWithConfig("b-app", "infra.app", map[string]any{
			"vpc_uuid": "${a-vpc.id}",
		})},
	}}

	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected per-action errors: %+v", result.Errors)
	}

	bConfig := fp.driver.seenConfigs["b-app"]
	if bConfig == nil {
		t.Fatalf("driver did not receive B's Config; seenConfigs=%+v", fp.driver.seenConfigs)
	}
	if got, want := bConfig["vpc_uuid"], "vpc-uuid-12345"; got != want {
		t.Errorf("B.Config[vpc_uuid] post-substitution: got %q want %q (full B Config: %+v)",
			got, want, bConfig)
	}
}

// TestApplyPlan_JIT_PreSyncedFromActionCurrentState verifies that
// syncedOutputs is pre-populated from action.Current entries — modules
// already in state can be referenced by later actions in the same plan
// without depending on their action running first.
func TestApplyPlan_JIT_PreSyncedFromActionCurrentState(t *testing.T) {
	fp := newJITRecordingProvider()
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		// "vpc" is in state; the only action against it here is delete (a
		// no-op for syncedOutputs propagation, but Current.Outputs +
		// Current.ProviderID seeds syncedOutputs at start-of-apply).
		{Action: "delete", Resource: spec("old-thing", "infra.misc"),
			Current: &interfaces.ResourceState{Name: "old-thing", ProviderID: "old"}},
		// New action references the *existing* in-state module's outputs
		// (here: the "vpc" Current passed alongside another action).
		{Action: "create", Resource: specWithConfig("app", "infra.app", map[string]any{
			"vpc_uuid":  "${vpc.id}",
			"vpc_cidr":  "${vpc.cidr}",
			"db_secret": "${vpc.region}",
		}),
			Current: nil,
		},
	}}
	// Plant "vpc" via a third action.Current — could be on any non-app
	// action in the same plan. We attach it to the first action's
	// Current.Name to demonstrate that all action.Current entries seed
	// syncedOutputs, not just the action's own.
	plan.Actions[0].Current = &interfaces.ResourceState{
		Name: "vpc", ProviderID: "vpc-state-id",
		Outputs: map[string]any{"cidr": "10.0.0.0/16", "region": "nyc3"},
	}

	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected per-action errors: %+v", result.Errors)
	}

	appConfig := fp.driver.seenConfigs["app"]
	if appConfig == nil {
		t.Fatalf("driver did not receive app's Config")
	}
	if got, want := appConfig["vpc_uuid"], "vpc-state-id"; got != want {
		t.Errorf("vpc_uuid: got %q want %q", got, want)
	}
	if got, want := appConfig["vpc_cidr"], "10.0.0.0/16"; got != want {
		t.Errorf("vpc_cidr: got %q want %q", got, want)
	}
	if got, want := appConfig["db_secret"], "nyc3"; got != want {
		t.Errorf("db_secret: got %q want %q", got, want)
	}
}

// TestApplyPlan_JIT_UnresolvedRef_RecordsActionErrorAndSkipsDispatch
// verifies the JIT failure contract: a reference that cannot be resolved
// (unknown module / field / env var) must NOT be silently swallowed. The
// per-action error surfaces with the canonical "jit substitution:" prefix
// and the driver MUST NOT see the un-resolved spec — Create count for
// the offending action must be 0.
func TestApplyPlan_JIT_UnresolvedRef_RecordsActionErrorAndSkipsDispatch(t *testing.T) {
	fp := newJITRecordingProvider()
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: specWithConfig("dependent", "infra.app", map[string]any{
			"vpc_uuid": "${ghost.id}", // ghost has no state, no replace-id
		})},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error from unresolved ref; got %d (%+v)",
			len(result.Errors), result.Errors)
	}
	got := result.Errors[0]
	if got.Resource != "dependent" || got.Action != "create" {
		t.Errorf("error shape: got %+v", got)
	}
	if !strings.HasPrefix(got.Error, "jit substitution:") {
		t.Errorf("error must use canonical 'jit substitution:' prefix; got %q", got.Error)
	}
	if !strings.Contains(got.Error, "ghost") {
		t.Errorf("error should mention the missing module name; got %q", got.Error)
	}
	if fp.driver.createCount != 0 {
		t.Errorf("dispatch must be skipped on JIT failure; createCount=%d", fp.driver.createCount)
	}
}

// TestApplyPlan_JIT_NoRefsInConfig_PassesThroughUnchanged covers the
// common case (most plan actions have no JIT refs): the driver receives
// the same Config it had in plan.Actions[i].Resource.Config — no
// behavior change. Locks the contract that JIT is purely additive.
func TestApplyPlan_JIT_NoRefsInConfig_PassesThroughUnchanged(t *testing.T) {
	fp := newJITRecordingProvider()
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: specWithConfig("vpc", "infra.vpc", map[string]any{
			"cidr":   "10.0.0.0/16",
			"region": "nyc3",
		})},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected per-action errors: %+v", result.Errors)
	}
	got := fp.driver.seenConfigs["vpc"]
	if got["cidr"] != "10.0.0.0/16" || got["region"] != "nyc3" {
		t.Errorf("Config altered for ref-free spec; got %+v", got)
	}
}

// TestApplyPlan_JIT_LoopContinuesAfterPerActionJITError is a
// best-effort-loop check: if action[0]'s JIT fails, action[1] (which
// has no refs) must still dispatch. Mirrors the
// TestApplyPlan_LoopContinuesAfterPerActionFailure contract for the new
// JIT failure mode.
func TestApplyPlan_JIT_LoopContinuesAfterPerActionJITError(t *testing.T) {
	fp := newJITRecordingProvider()
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: specWithConfig("bad", "infra.app", map[string]any{
			"x": "${ghost.id}",
		})},
		{Action: "create", Resource: specWithConfig("good", "infra.vpc", map[string]any{
			"cidr": "10.0.0.0/16",
		})},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected exactly 1 per-action error from action[0]; got %d (%+v)",
			len(result.Errors), result.Errors)
	}
	if fp.driver.createCount != 1 {
		t.Errorf("action[1] should have dispatched; createCount=%d want 1", fp.driver.createCount)
	}
	if fp.driver.seenConfigs["good"]["cidr"] != "10.0.0.0/16" {
		t.Errorf("action[1] Config not seen by driver; seenConfigs=%+v", fp.driver.seenConfigs)
	}
}
