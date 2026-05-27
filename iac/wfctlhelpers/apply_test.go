package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeDriver counts CRUD-method invocations so dispatch tests can prove
// each per-action sub-function ran the right number of times. Counters
// are integers (not booleans) because the Replace action decomposes into
// Delete-then-Create on the driver — a boolean recorder can't tell
// "Replace dispatched correctly" apart from "explicit Create + explicit
// Delete also ran in the same plan." Each test gets a fresh instance via
// newFakeProvider().
type fakeDriver struct {
	createCount int
	updateCount int
	deleteCount int
}

func (d *fakeDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCount++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "fake-id-" + spec.Name}, nil
}

func (d *fakeDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}

func (d *fakeDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCount++
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

func (d *fakeDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.deleteCount++
	return nil
}

func (d *fakeDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{}, nil
}

func (d *fakeDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: true}, nil
}

func (d *fakeDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

func (d *fakeDriver) SensitiveKeys() []string { return nil }

// fakeProvider returns the same fakeDriver for every resource type so a
// single instance can record all dispatch invocations within one
// ApplyPlan call. The driverErr field, when non-nil, is returned from
// ResourceDriver instead of the driver — used by tests that need to
// exercise the resolve-driver-error path.
type fakeProvider struct {
	driver    *fakeDriver
	driverErr error
}

func newFakeProvider() *fakeProvider { return &fakeProvider{driver: &fakeDriver{}} }

func (p *fakeProvider) Name() string                                         { return "fake" }
func (p *fakeProvider) Version() string                                      { return "0.0.0" }
func (p *fakeProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *fakeProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *fakeProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return &interfaces.IaCPlan{}, nil
}
func (p *fakeProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return &interfaces.ApplyResult{}, nil
}
func (p *fakeProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *fakeProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *fakeProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *fakeProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *fakeProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *fakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if p.driverErr != nil {
		return nil, p.driverErr
	}
	return p.driver, nil
}
func (p *fakeProvider) SupportedCanonicalKeys() []string { return nil }
func (p *fakeProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *fakeProvider) Close() error { return nil }

// spec is a minimal ResourceSpec helper for tests.
func spec(name, typ string) interfaces.ResourceSpec {
	return interfaces.ResourceSpec{Name: name, Type: typ}
}

// state is a minimal ResourceState helper for tests; sets a deterministic
// ProviderID so doUpdate / doDelete / doReplace have something to thread.
func state(name string) *interfaces.ResourceState {
	return &interfaces.ResourceState{Name: name, ProviderID: "old-id-" + name}
}

// TestApplyPlan_HandlesAllFourActions is the T3.1 dispatch smoke test:
// one PlanAction of each kind (create/update/replace/delete) must reach
// the ResourceDriver. Asserts via integer call counts so a missing
// "case replace" arm in dispatchAction is detectable: a 4-action plan
// [create, update, replace, delete] must produce exactly 2 Create, 1
// Update, 2 Delete. If Replace dispatch were silently dropped, those
// counts would shift to 1/1/1 and the assertion would fail.
func TestApplyPlan_HandlesAllFourActions(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.vpc")},
			{Action: "update", Resource: spec("b", "infra.vpc"), Current: state("b")},
			{Action: "replace", Resource: spec("c", "infra.vpc"), Current: state("c")},
			{Action: "delete", Resource: spec("d", "infra.vpc"), Current: state("d")},
		},
	}
	fp := newFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
	// Replace = Delete + Create on the driver.
	// Plan = [create, update, replace, delete] →
	//   Create: 1 (from create) + 1 (from replace) = 2
	//   Update: 1 (from update)
	//   Delete: 1 (from delete) + 1 (from replace) = 2
	if got, want := fp.driver.createCount, 2; got != want {
		t.Errorf("Create call count: got %d, want %d", got, want)
	}
	if got, want := fp.driver.updateCount, 1; got != want {
		t.Errorf("Update call count: got %d, want %d", got, want)
	}
	if got, want := fp.driver.deleteCount, 2; got != want {
		t.Errorf("Delete call count: got %d, want %d", got, want)
	}
}

// TestApplyPlan_ReplaceDispatchesViaDeleteThenCreate isolates Replace
// dispatch from explicit create/delete actions: a plan containing ONLY a
// single Replace action must produce exactly 1 Delete + 1 Create. If
// Replace were routed to default (unknown action) or to a different arm,
// these counts would not both be 1.
func TestApplyPlan_ReplaceDispatchesViaDeleteThenCreate(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "replace", Resource: spec("c", "infra.vpc"), Current: state("c")},
		},
	}
	fp := newFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
	if got, want := fp.driver.deleteCount, 1; got != want {
		t.Errorf("Replace.deleteCount: got %d, want %d", got, want)
	}
	if got, want := fp.driver.createCount, 1; got != want {
		t.Errorf("Replace.createCount: got %d, want %d", got, want)
	}
	if got, want := fp.driver.updateCount, 0; got != want {
		t.Errorf("Replace.updateCount: got %d, want %d", got, want)
	}
}

// TestApplyPlan_UnknownActionRecordsError verifies the dispatch's
// default-case behavior: an action with an unrecognised kind should not
// crash but must surface a per-action error in result.Errors so an
// operator running a malformed plan sees the diagnostic instead of a
// silently dropped action.
func TestApplyPlan_UnknownActionRecordsError(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "frobnicate", Resource: spec("x", "infra.vpc")},
		},
	}
	fp := newFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("ApplyPlan should not return a top-level error for a per-action issue; got %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error, got %d (%v)", len(result.Errors), result.Errors)
	}
	if result.Errors[0].Resource != "x" || result.Errors[0].Action != "frobnicate" {
		t.Errorf("unexpected error shape: %+v", result.Errors[0])
	}
}

// TestApplyPlan_PreservesPlanID checks the ApplyResult.PlanID echo. Future
// log-correlation paths rely on this so an operator can match an apply
// invocation back to the persisted plan.
func TestApplyPlan_PreservesPlanID(t *testing.T) {
	plan := &interfaces.IaCPlan{ID: "plan-12345"}
	fp := newFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "plan-12345" {
		t.Errorf("PlanID: got %q want %q", result.PlanID, "plan-12345")
	}
}

// errResolveDriver is a sentinel for the ResourceDriver-error test path.
var errResolveDriver = errors.New("driver lookup failed")

// TestApplyPlan_ResolveDriverErrorRecordsActionError covers the branch
// where p.ResourceDriver returns an error: the per-action error must be
// recorded with the canonical "resolve driver:" prefix and the loop must
// continue to the next action so a single bad type doesn't abort apply.
func TestApplyPlan_ResolveDriverErrorRecordsActionError(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("bad", "infra.unknown")},
			{Action: "create", Resource: spec("good", "infra.vpc")},
		},
	}
	fp := newFakeProvider()
	fp.driverErr = errResolveDriver
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("top-level error not expected for per-action driver-resolve failure; got %v", err)
	}
	// fp.driverErr applies to BOTH actions in this test (driver lookup
	// hits the same fake), so both record the resolve-driver error. The
	// important contracts are: the loop continued past action[0] (so
	// len(Errors)==2 not 1), and the prefix is canonical.
	if len(result.Errors) != 2 {
		t.Fatalf("expected 2 per-action errors (loop must continue past action[0]); got %d (%v)",
			len(result.Errors), result.Errors)
	}
	for i, e := range result.Errors {
		if !strings.HasPrefix(e.Error, "resolve driver:") {
			t.Errorf("Errors[%d].Error: want prefix %q; got %q", i, "resolve driver:", e.Error)
		}
		if e.Action != "create" {
			t.Errorf("Errors[%d].Action: got %q want %q", i, e.Action, "create")
		}
	}
	if result.Errors[0].Resource != "bad" || result.Errors[1].Resource != "good" {
		t.Errorf("expected Resource ordering [bad, good]; got [%s, %s]",
			result.Errors[0].Resource, result.Errors[1].Resource)
	}
}

// TestApplyPlan_LoopContinuesAfterPerActionFailure verifies the
// best-effort contract: action[0]'s driver lookup fails, action[1]'s
// resource type uses a different driver path that succeeds. Both effects
// must be observable in the result.
func TestApplyPlan_LoopContinuesAfterPerActionFailure(t *testing.T) {
	// Use a per-call fakeProvider variant whose ResourceDriver returns
	// an error for one type and the real driver for the other.
	fp := &selectiveFakeProvider{
		fakeProvider: newFakeProvider(),
		errorType:    "infra.unknown",
	}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("bad", "infra.unknown")},
			{Action: "create", Resource: spec("good", "infra.vpc")},
		},
	}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 error from action[0]; got %d (%v)", len(result.Errors), result.Errors)
	}
	if fp.driver.createCount != 1 {
		t.Errorf("action[1] should have reached the driver; createCount=%d want 1", fp.driver.createCount)
	}
	// Successful action's output should be in result.Resources.
	if len(result.Resources) != 1 || result.Resources[0].Name != "good" {
		t.Errorf("expected one Resources entry for action[1]; got %+v", result.Resources)
	}
}

// selectiveFakeProvider returns errResolveDriver for one resource type
// and the wrapped fakeProvider's driver for any other type. Used by
// TestApplyPlan_LoopContinuesAfterPerActionFailure to exercise mixed
// success/failure ordering across a multi-action plan.
type selectiveFakeProvider struct {
	*fakeProvider
	errorType string
}

func (p *selectiveFakeProvider) ResourceDriver(typ string) (interfaces.ResourceDriver, error) {
	if typ == p.errorType {
		return nil, errResolveDriver
	}
	return p.driver, nil
}

// TestApplyPlan_CtxCancellationStopsLoop verifies the loop respects
// context cancellation between actions. Drivers may honor ctx individually,
// TestApplyPlan_OnBeforeAction_abortsFatal pins the OnBeforeAction hook
// contract: a non-nil error from OnBeforeAction is FATAL — it aborts the
// per-action loop with no further actions dispatched, no further hook
// invocations, and a top-level error wrapping the hook's error so callers
// see the policy/gate denial unambiguously. Mirrors the design's Phase 3a
// "OnBeforeAction error tier specified as FATAL" decision (cycle 3.5 I-NEW-2).
func TestApplyPlan_OnBeforeAction_abortsFatal(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.dns")},
			{Action: "create", Resource: spec("b", "infra.dns")},
		},
	}
	fp := newFakeProvider()
	var beforeCalls int
	hooks := ApplyPlanHooks{
		OnBeforeAction: func(_ context.Context, a interfaces.PlanAction) error {
			beforeCalls++
			if a.Resource.Name == "a" {
				return errors.New("policy denied: a is not delegated for this owner")
			}
			return nil
		},
	}
	_, err := ApplyPlanWithHooks(context.Background(), fp, plan, hooks)
	if err == nil || !strings.Contains(err.Error(), "policy denied") {
		t.Fatalf("expected top-level error wrapping policy denial; got %v", err)
	}
	if beforeCalls != 1 {
		t.Errorf("OnBeforeAction calls = %d; want 1 (abort on first failure, second action not reached)", beforeCalls)
	}
	if fp.driver.createCount != 0 {
		t.Errorf("createCount = %d; want 0 (fatal hook must abort before any driver call)", fp.driver.createCount)
	}
}

// TestApplyPlan_OnBeforeAction_nilAllowsAll pins the success path: when
// OnBeforeAction returns nil for every action, the hook is non-blocking and
// the apply proceeds as if no hook were wired. Catches the regression where
// a nil-return path is misinterpreted as failure due to a stale fatalErr
// assignment.
func TestApplyPlan_OnBeforeAction_nilAllowsAll(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.dns")},
			{Action: "create", Resource: spec("b", "infra.dns")},
		},
	}
	fp := newFakeProvider()
	var beforeCalls int
	hooks := ApplyPlanHooks{
		OnBeforeAction: func(_ context.Context, _ interfaces.PlanAction) error {
			beforeCalls++
			return nil
		},
	}
	_, err := ApplyPlanWithHooks(context.Background(), fp, plan, hooks)
	if err != nil {
		t.Fatalf("OnBeforeAction returning nil should not abort apply; got %v", err)
	}
	if beforeCalls != 2 {
		t.Errorf("OnBeforeAction calls = %d; want 2 (one per action)", beforeCalls)
	}
	if fp.driver.createCount != 2 {
		t.Errorf("createCount = %d; want 2 (every action dispatches when hook returns nil)", fp.driver.createCount)
	}
}

// but the loop itself must check at the iteration boundary so a
// long-running multi-action apply terminates promptly on Ctrl-C / deadline.
func TestApplyPlan_CtxCancellationStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before invocation so the very first iteration aborts.
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.vpc")},
			{Action: "create", Resource: spec("b", "infra.vpc")},
		},
	}
	fp := newFakeProvider()
	_, err := ApplyPlanWithHooks(ctx, fp, plan, ApplyPlanHooks{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled top-level error; got %v", err)
	}
	if fp.driver.createCount != 0 {
		t.Errorf("no driver calls should run after ctx cancellation; createCount=%d", fp.driver.createCount)
	}
}
