package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// orderRecordingDriver records the order of Delete vs. Create
// invocations so doReplace's "delete THEN create" contract can be
// asserted exactly. createReturn is the canned Create output (carries
// the NEW ProviderID); deleteFn / createFn are optional hooks that
// override the default success behavior.
type orderRecordingDriver struct {
	*fakeDriver
	deleteAt     int // sequence number when Delete was called (0 if not called)
	createAt     int // sequence number when Create was called (0 if not called)
	step         int
	createReturn *interfaces.ResourceOutput
	deleteErr    error
	createErr    error
}

func (d *orderRecordingDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.deleteCount++
	d.step++
	d.deleteAt = d.step
	return d.deleteErr
}

func (d *orderRecordingDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCount++
	d.step++
	d.createAt = d.step
	if d.createErr != nil {
		return nil, d.createErr
	}
	if d.createReturn != nil {
		return d.createReturn, nil
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "fake-id-" + spec.Name}, nil
}

// orderRecordingProvider returns the orderRecordingDriver for any
// resource type.
type orderRecordingProvider struct {
	*fakeProvider
	driver *orderRecordingDriver
}

func newOrderRecordingProvider() *orderRecordingProvider {
	base := newFakeProvider()
	return &orderRecordingProvider{
		fakeProvider: base,
		driver:       &orderRecordingDriver{fakeDriver: base.driver},
	}
}

func (p *orderRecordingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// stateWithID is a helper that builds a ResourceState with a known
// ProviderID, used to seed the "old" side of a Replace action.
func stateWithID(name, providerID string) *interfaces.ResourceState {
	return &interfaces.ResourceState{Name: name, ProviderID: providerID}
}

// TestApplyPlan_Replace_DeletesThenCreates_PropagatesNewID is the
// canonical T3.4 test: Replace must (1) call Delete first, (2) call
// Create after the Delete, (3) thread the NEW ProviderID from Create
// into result.Resources.
func TestApplyPlan_Replace_DeletesThenCreates_PropagatesNewID(t *testing.T) {
	fp := newOrderRecordingProvider()
	fp.driver.createReturn = &interfaces.ResourceOutput{
		Name: "vpc", Type: "infra.vpc", ProviderID: "new-uuid",
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-uuid")},
	}}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if fp.driver.deleteAt == 0 || fp.driver.createAt == 0 {
		t.Errorf("Replace should call both Delete and Create; deleteAt=%d createAt=%d",
			fp.driver.deleteAt, fp.driver.createAt)
	}
	if fp.driver.createAt < fp.driver.deleteAt {
		t.Errorf("Create must run AFTER Delete; deleteAt=%d createAt=%d",
			fp.driver.deleteAt, fp.driver.createAt)
	}
	if len(result.Resources) != 1 || result.Resources[0].ProviderID != "new-uuid" {
		t.Errorf("expected new ProviderID in result.Resources, got %+v", result.Resources)
	}
}

// TestApplyPlan_Replace_PopulatesReplaceIDMap is the new-in-T3.4
// contract: result.ReplaceIDMap[action.Resource.Name] must equal the
// new ProviderID returned by Create. Keyed by the *replaced* resource's
// Name (per T3.0.4 godoc — fixed during T3.0.4 review). Lazy-init on
// first Replace.
func TestApplyPlan_Replace_PopulatesReplaceIDMap(t *testing.T) {
	fp := newOrderRecordingProvider()
	fp.driver.createReturn = &interfaces.ResourceOutput{
		Name: "vpc", Type: "infra.vpc", ProviderID: "new-uuid",
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-uuid")},
	}}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.ReplaceIDMap["vpc"]; got != "new-uuid" {
		t.Errorf("ReplaceIDMap[vpc]: got %q want %q (full map: %+v)", got, "new-uuid", result.ReplaceIDMap)
	}
}

// TestApplyPlan_Replace_MultipleActionsAllPopulate verifies the map
// accumulates across actions: two Replace actions in one plan must
// produce two entries in result.ReplaceIDMap, each keyed by its
// replaced resource's name.
func TestApplyPlan_Replace_MultipleActionsAllPopulate(t *testing.T) {
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-vpc")},
		{Action: "replace", Resource: spec("db", "infra.database"), Current: stateWithID("db", "old-db")},
	}}
	// Use a per-call provider that returns a fresh ID per resource so
	// the map entries are distinguishable.
	fp := &perResourceReplaceProvider{
		fakeProvider: newFakeProvider(),
		newIDs:       map[string]string{"vpc": "new-vpc-id", "db": "new-db-id"},
	}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.ReplaceIDMap["vpc"]; got != "new-vpc-id" {
		t.Errorf("ReplaceIDMap[vpc]: got %q want %q", got, "new-vpc-id")
	}
	if got := result.ReplaceIDMap["db"]; got != "new-db-id" {
		t.Errorf("ReplaceIDMap[db]: got %q want %q", got, "new-db-id")
	}
}

// perResourceReplaceProvider mints a fresh new-ID per resource Name so
// MultipleActionsAllPopulate can distinguish the two map entries. Each
// driver is a one-shot recorder owned by the provider.
type perResourceReplaceProvider struct {
	*fakeProvider
	newIDs map[string]string
	driver *perResourceReplaceDriver
}

func (p *perResourceReplaceProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if p.driver == nil {
		p.driver = &perResourceReplaceDriver{fakeDriver: p.fakeProvider.driver, newIDs: p.newIDs}
	}
	return p.driver, nil
}

type perResourceReplaceDriver struct {
	*fakeDriver
	newIDs map[string]string
}

func (d *perResourceReplaceDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.deleteCount++
	return nil
}

func (d *perResourceReplaceDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.createCount++
	id := d.newIDs[spec.Name]
	if id == "" {
		id = "fallback-id"
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: id}, nil
}

// TestApplyPlan_Replace_DeleteFailsDoesNotCreate verifies that when
// the Delete sub-step of a Replace fails, Create is NOT called and
// result.ReplaceIDMap is NOT populated for that resource. The
// per-action error must surface with the canonical "replace: delete:"
// prefix that doReplace decorates (not bare driver error — Replace
// decomposes, so the prefix tells the operator which sub-step failed).
func TestApplyPlan_Replace_DeleteFailsDoesNotCreate(t *testing.T) {
	fp := newOrderRecordingProvider()
	fp.driver.deleteErr = errors.New("delete failed: 503")
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-uuid")},
	}}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if fp.driver.createAt != 0 {
		t.Errorf("Create must not run when Delete fails; createAt=%d", fp.driver.createAt)
	}
	if _, present := result.ReplaceIDMap["vpc"]; present {
		t.Errorf("ReplaceIDMap must not contain vpc when Delete failed; got %+v", result.ReplaceIDMap)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if !strings.HasPrefix(result.Errors[0].Error, "replace: delete:") {
		t.Errorf("expected canonical 'replace: delete:' prefix; got %q", result.Errors[0].Error)
	}
}

// TestApplyPlan_Replace_CtxCancelAfterDelete_SkipsCreate verifies the
// post-Delete cancellation guard: if ctx is canceled between Delete
// and Create, doReplace must NOT call Create — instead returning a
// wrapped error with the canonical "replace: canceled after delete:"
// prefix. The half-replaced state is the operator's recovery surface
// (Delete happened, Create did not, ReplaceIDMap stays empty for the
// resource).
func TestApplyPlan_Replace_CtxCancelAfterDelete_SkipsCreate(t *testing.T) {
	// cancelOnDeleteDriver cancels the context inside Delete after
	// recording the call but before returning. This simulates
	// SIGTERM/SIGINT arriving exactly between the Delete and Create
	// driver calls.
	ctx, cancel := context.WithCancel(context.Background())
	fp := &cancelOnDeleteFakeProvider{
		fakeProvider: newFakeProvider(),
		cancel:       cancel,
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-uuid")},
	}}
	result, err := ApplyPlanWithHooks(ctx, fp, plan, ApplyPlanHooks{})
	if err != nil {
		// ApplyPlan's loop check catches the cancellation at the next
		// iteration, but the per-action ctx check inside doReplace
		// fires first. Either path yields a per-action error rather
		// than a top-level error from this single-action plan.
		t.Fatalf("ApplyPlan should not surface top-level error: %v", err)
	}
	if fp.driver.deleteCount != 1 {
		t.Errorf("Delete should have run before cancellation; deleteCount=%d", fp.driver.deleteCount)
	}
	if fp.driver.createCount != 0 {
		t.Errorf("Create must NOT run after ctx cancellation; createCount=%d", fp.driver.createCount)
	}
	if _, present := result.ReplaceIDMap["vpc"]; present {
		t.Errorf("ReplaceIDMap must not contain vpc when Create skipped; got %+v", result.ReplaceIDMap)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if !strings.HasPrefix(result.Errors[0].Error, "replace: canceled after delete:") {
		t.Errorf("expected canonical 'replace: canceled after delete:' prefix; got %q", result.Errors[0].Error)
	}
}

// cancelOnDeleteFakeProvider returns a driver whose Delete cancels a
// supplied context.CancelFunc as a side-effect, simulating exact
// post-Delete pre-Create cancellation.
type cancelOnDeleteFakeProvider struct {
	*fakeProvider
	cancel context.CancelFunc
	driver *cancelOnDeleteDriver
}

func (p *cancelOnDeleteFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if p.driver == nil {
		p.driver = &cancelOnDeleteDriver{fakeDriver: p.fakeProvider.driver, cancel: p.cancel}
	}
	return p.driver, nil
}

type cancelOnDeleteDriver struct {
	*fakeDriver
	cancel context.CancelFunc
}

func (d *cancelOnDeleteDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error {
	d.deleteCount++
	d.cancel() // ctx is now canceled; Create must not run.
	return nil
}

// TestApplyPlan_Replace_CreateFailsLeavesMapEmpty verifies the
// post-Delete pre-Create failure window: Delete succeeded but Create
// failed → ReplaceIDMap stays empty for this resource (no spurious
// new-ID entry). The plan rollback note says operators inspect this
// state checkpoint to know which resources are in a half-replaced
// state; "absent from ReplaceIDMap + Resource was Replace target" is
// the canonical signal.
func TestApplyPlan_Replace_CreateFailsLeavesMapEmpty(t *testing.T) {
	fp := newOrderRecordingProvider()
	fp.driver.createErr = errors.New("create failed: 422")
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "replace", Resource: spec("vpc", "infra.vpc"), Current: stateWithID("vpc", "old-uuid")},
	}}
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if fp.driver.deleteAt == 0 {
		t.Errorf("Delete should still have run (failure was in Create, not Delete)")
	}
	if _, present := result.ReplaceIDMap["vpc"]; present {
		t.Errorf("ReplaceIDMap must not contain vpc when Create failed; got %+v", result.ReplaceIDMap)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if !strings.HasPrefix(result.Errors[0].Error, "replace: create:") {
		t.Errorf("expected canonical 'replace: create:' prefix; got %q", result.Errors[0].Error)
	}
}
