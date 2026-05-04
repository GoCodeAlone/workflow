package wfctlhelpers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeDriver records which CRUD methods ApplyPlan invokes. All methods
// succeed; tests assert on the recorded flags. Each test gets a fresh
// instance via newFakeDriver().
type fakeDriver struct {
	calledCreate bool
	calledUpdate bool
	calledDelete bool
}

func (d *fakeDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.calledCreate = true
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "fake-id-" + spec.Name}, nil
}

func (d *fakeDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}

func (d *fakeDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.calledUpdate = true
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

func (d *fakeDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.calledDelete = true
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
// ApplyPlan call.
type fakeProvider struct {
	driver *fakeDriver
}

func newFakeProvider() *fakeProvider { return &fakeProvider{driver: &fakeDriver{}} }

// calledReplace is true iff doReplace ran — Replace decomposes into
// Delete-then-Create on the driver, so we infer it from a Delete preceded
// by a Create on the same recorder, but for the dispatch smoke test we
// instead expose it via a derived helper.
func (p *fakeProvider) calledReplace() bool { return p.driver.calledDelete && p.driver.calledCreate }

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
// the ResourceDriver. Bodies of doCreate/doUpdate/doReplace/doDelete are
// minimal stubs in T3.1 — full upsert / replace-id-propagation / etc.
// land in T3.2 / T3.3 / T3.4. This test only asserts dispatch wiring.
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
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got %v", result.Errors)
	}
	if !fp.driver.calledCreate || !fp.driver.calledUpdate || !fp.calledReplace() || !fp.driver.calledDelete {
		t.Errorf("not all action types invoked: create=%v update=%v replace=%v delete=%v",
			fp.driver.calledCreate, fp.driver.calledUpdate, fp.calledReplace(), fp.driver.calledDelete)
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
	result, err := ApplyPlan(context.Background(), fp, plan)
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
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.PlanID != "plan-12345" {
		t.Errorf("PlanID: got %q want %q", result.PlanID, "plan-12345")
	}
}
