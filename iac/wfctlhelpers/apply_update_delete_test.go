package wfctlhelpers

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// providerIDCapturingDriver records the ResourceRef passed to Update /
// Delete so tests can assert ProviderID propagation from action.Current
// to the driver call.
type providerIDCapturingDriver struct {
	*fakeDriver
	updateRef  interfaces.ResourceRef
	updateSpec interfaces.ResourceSpec
	deleteRef  interfaces.ResourceRef
	deleteErr  error
	updateErr  error
}

func (d *providerIDCapturingDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updateCount++
	d.updateRef = ref
	d.updateSpec = spec
	if d.updateErr != nil {
		return nil, d.updateErr
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID}, nil
}

func (d *providerIDCapturingDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount++
	d.deleteRef = ref
	return d.deleteErr
}

// captureFakeProvider returns the providerIDCapturingDriver for any
// resource type so the caller can assert what ApplyPlan passed through.
type captureFakeProvider struct {
	*fakeProvider
	driver *providerIDCapturingDriver
}

func newCaptureFakeProvider() *captureFakeProvider {
	base := newFakeProvider()
	return &captureFakeProvider{
		fakeProvider: base,
		driver:       &providerIDCapturingDriver{fakeDriver: base.driver},
	}
}

func (p *captureFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

// TestApplyPlan_Update_PassesProviderID is the canonical T3.3 update
// test: the plan's update action carries action.Current with a known
// ProviderID; doUpdate must thread that ProviderID into the
// ResourceRef passed to driver.Update so the driver knows which
// resource to mutate. Threading it via Name alone would lose the
// upstream ID and force the driver to re-resolve.
func TestApplyPlan_Update_PassesProviderID(t *testing.T) {
	const knownID = "do-vpc-abc123"
	cur := &interfaces.ResourceState{
		Name:       "vpc-1",
		Type:       "infra.vpc",
		ProviderID: knownID,
	}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "update", Resource: spec("vpc-1", "infra.vpc"), Current: cur},
		},
	}
	fp := newCaptureFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if got := fp.driver.updateRef.ProviderID; got != knownID {
		t.Errorf("Update ResourceRef.ProviderID: got %q want %q", got, knownID)
	}
	if got := fp.driver.updateRef.Name; got != "vpc-1" {
		t.Errorf("Update ResourceRef.Name: got %q want %q", got, "vpc-1")
	}
	if got := fp.driver.updateRef.Type; got != "infra.vpc" {
		t.Errorf("Update ResourceRef.Type: got %q want %q", got, "infra.vpc")
	}
	// Driver returns a populated ResourceOutput; ApplyPlan must thread
	// it through to result.Resources.
	if len(result.Resources) != 1 || result.Resources[0].ProviderID != knownID {
		t.Errorf("Resources: expected one entry with ProviderID %q; got %+v", knownID, result.Resources)
	}
}

// TestApplyPlan_Update_NilCurrentIsHandledDefensively verifies the
// edge case where a malformed plan's update action lacks
// action.Current. ComputePlan upstream should never emit such a plan,
// but doUpdate must not panic — it should call Update with an empty
// ProviderID (matching the skeleton's behavior) so the driver can
// surface its own typed validation error.
func TestApplyPlan_Update_NilCurrentIsHandledDefensively(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "update", Resource: spec("orphan", "infra.vpc"), Current: nil},
		},
	}
	fp := newCaptureFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fp.driver.updateRef.ProviderID; got != "" {
		t.Errorf("nil Current must yield empty ProviderID; got %q", got)
	}
	// Per-action result behavior: driver returned non-nil success →
	// no errors, output appended. The contract is that doUpdate
	// doesn't synthesize a precondition error; the driver is the
	// authority on what an empty ProviderID means.
	if len(result.Errors) != 0 {
		t.Errorf("unexpected per-action errors for nil Current: %v", result.Errors)
	}
	// Lock the resource-append contract on the success path so a
	// regression that made doUpdate skip the append on nil Current
	// would fail this test loudly.
	if len(result.Resources) != 1 {
		t.Errorf("expected 1 Resources entry on driver-success path; got %d", len(result.Resources))
	}
}

// TestApplyPlan_Delete_NilCurrentIsHandledDefensively mirrors the
// Update nil-Current contract for doDelete: a delete action without
// action.Current must not panic; the empty ProviderID flows to the
// driver, which is the authority on what to do (most drivers will
// surface a typed validation error). doDelete itself does not
// synthesize a precondition error — same defensive shape as doUpdate.
func TestApplyPlan_Delete_NilCurrentIsHandledDefensively(t *testing.T) {
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "delete", Resource: spec("orphan", "infra.vpc"), Current: nil},
		},
	}
	fp := newCaptureFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := fp.driver.deleteRef.ProviderID; got != "" {
		t.Errorf("nil Current must yield empty ProviderID on Delete; got %q", got)
	}
	if got := fp.driver.deleteRef.Name; got != "orphan" {
		t.Errorf("Delete ResourceRef.Name: got %q want %q", got, "orphan")
	}
	if len(result.Errors) != 0 {
		t.Errorf("unexpected per-action errors for nil Current Delete: %v", result.Errors)
	}
	// Sanity: driver was called exactly once (latent bug-fix contract
	// from T3.3 — Delete must not be silently skipped).
	if fp.driver.deleteCount != 1 {
		t.Errorf("driver.Delete must be called exactly once; got %d", fp.driver.deleteCount)
	}
}

// TestApplyPlan_Delete_InvokesDriverDelete is the latent-bug-fix test
// from the design notes: today's DOProvider.Apply has no
// `case "delete":` arm, so wfctl's state-prune action silently skips
// cloud-resource deletion. doDelete CLOSES that gap by always calling
// driver.Delete. This test fails on the design's pre-T3.3 codepath.
func TestApplyPlan_Delete_InvokesDriverDelete(t *testing.T) {
	const knownID = "do-vpc-xyz789"
	cur := &interfaces.ResourceState{
		Name:       "old-vpc",
		Type:       "infra.vpc",
		ProviderID: knownID,
	}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "delete", Resource: spec("old-vpc", "infra.vpc"), Current: cur},
		},
	}
	fp := newCaptureFakeProvider()
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}
	if fp.driver.deleteCount != 1 {
		t.Errorf("driver.Delete must be called exactly once for delete action; got %d", fp.driver.deleteCount)
	}
	if got := fp.driver.deleteRef.ProviderID; got != knownID {
		t.Errorf("Delete ResourceRef.ProviderID: got %q want %q", got, knownID)
	}
}

// TestApplyPlan_Delete_DriverErrorRecorded verifies that a driver-level
// delete failure is recorded in result.Errors with the action +
// resource captured, matching the per-action error contract used by
// the rest of the dispatch loop.
func TestApplyPlan_Delete_DriverErrorRecorded(t *testing.T) {
	deleteErr := errors.New("delete failed: 503 service unavailable")
	cur := &interfaces.ResourceState{Name: "old-vpc", Type: "infra.vpc", ProviderID: "do-id"}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "delete", Resource: spec("old-vpc", "infra.vpc"), Current: cur},
		},
	}
	fp := newCaptureFakeProvider()
	fp.driver.deleteErr = deleteErr
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if result.Errors[0].Action != "delete" {
		t.Errorf("Action: got %q want %q", result.Errors[0].Action, "delete")
	}
	if result.Errors[0].Resource != "old-vpc" {
		t.Errorf("Resource: got %q want %q", result.Errors[0].Resource, "old-vpc")
	}
	if got := result.Errors[0].Error; got == "" || got != deleteErr.Error() {
		t.Errorf("Error: got %q want bare driver error %q", got, deleteErr.Error())
	}
}

// TestApplyPlan_Update_DriverErrorRecorded mirrors the Delete error
// recording test — driver Update failures must surface in
// result.Errors with the canonical fields populated.
func TestApplyPlan_Update_DriverErrorRecorded(t *testing.T) {
	updateErr := errors.New("update failed: 422 invalid spec")
	cur := &interfaces.ResourceState{Name: "vpc-1", Type: "infra.vpc", ProviderID: "do-id"}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "update", Resource: spec("vpc-1", "infra.vpc"), Current: cur},
		},
	}
	fp := newCaptureFakeProvider()
	fp.driver.updateErr = updateErr
	result, err := ApplyPlanWithHooks(context.Background(), fp, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("expected 1 per-action error; got %d (%v)", len(result.Errors), result.Errors)
	}
	if result.Errors[0].Action != "update" {
		t.Errorf("Action: got %q want %q", result.Errors[0].Action, "update")
	}
	if got := result.Errors[0].Error; got != updateErr.Error() {
		t.Errorf("Error: got %q want bare driver error %q", got, updateErr.Error())
	}
}
