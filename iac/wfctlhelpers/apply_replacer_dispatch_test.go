package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeReplacerDriver implements both ResourceDriver AND ResourceReplacer
// to exercise the dispatch-to-replacer path. Embeds fakeDriver so all
// ResourceDriver methods are satisfied without duplication.
type fakeReplacerDriver struct {
	fakeDriver
	replaceCalled bool
	replaceErr    error
}

func (f *fakeReplacerDriver) Replace(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	f.replaceCalled = true
	if f.replaceErr != nil {
		return nil, f.replaceErr
	}
	return &interfaces.ResourceOutput{Name: spec.Name, ProviderID: "new-id"}, nil
}

// TestDoReplace_DispatchesToReplacerWhenAvailable verifies that doReplace
// routes to ResourceReplacer.Replace when the driver implements it, and
// does NOT fall through to DefaultReplace (Delete+Create).
func TestDoReplace_DispatchesToReplacerWhenAvailable(t *testing.T) {
	d := &fakeReplacerDriver{}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result, ApplyPlanHooks{}, false)
	if err != nil {
		t.Fatalf("doReplace: %v", err)
	}
	if !d.replaceCalled {
		t.Error("Replacer.Replace was not invoked")
	}
	// DefaultReplace (Delete+Create) should NOT have fired.
	if d.deleteCount != 0 || d.createCount != 0 {
		t.Errorf("DefaultReplace fired alongside Replacer: deleteCount=%d createCount=%d (both should be 0)",
			d.deleteCount, d.createCount)
	}
	if got := result.ReplaceIDMap["x"]; got != "new-id" {
		t.Errorf("ReplaceIDMap[x] = %q, want new-id", got)
	}
}

// TestDoReplace_DefaultReplaceWhenNoReplacerInterface verifies that doReplace
// falls back to DefaultReplace (Delete+Create) when the driver does NOT
// implement ResourceReplacer.
func TestDoReplace_DefaultReplaceWhenNoReplacerInterface(t *testing.T) {
	d := &fakeDriver{} // does NOT implement ResourceReplacer
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result, ApplyPlanHooks{}, false)
	if err != nil {
		t.Fatalf("doReplace: %v", err)
	}
	if d.deleteCount == 0 || d.createCount == 0 {
		t.Errorf("DefaultReplace did NOT fire: deleteCount=%d createCount=%d (both should be 1)",
			d.deleteCount, d.createCount)
	}
}

// TestDoReplace_ReplacerError_IsWrappedByBackstop verifies that a
// non-conforming error from ResourceReplacer.Replace gets the engine-side
// backstop prefix applied (Task 11 implements wrapDriverReplaceError;
// this test is the cross-task coupling assertion).
func TestDoReplace_ReplacerError_IsWrappedByBackstop(t *testing.T) {
	d := &fakeReplacerDriver{replaceErr: errors.New("kaboom")}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result, ApplyPlanHooks{}, false)
	if err == nil {
		t.Fatal("expected error from failing Replacer")
	}
	// wrapDriverReplaceError (also in this PR) applies the backstop prefix to
	// non-conforming errors. "kaboom" has no recognized replace prefix family,
	// so it must be wrapped as "replace: driver: kaboom".
	const wantPrefix = "replace: driver: "
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Errorf("expected backstop prefix %q on non-conforming error; got: %v", wantPrefix, err)
	}
}

func TestDoReplace_ReplacerWithoutHooksErrorsWhenStateHooksActive(t *testing.T) {
	d := &fakeReplacerDriver{}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error { return nil },
	}, true)
	if err == nil {
		t.Fatal("expected state-hook replace requirement error")
	}
	if !strings.Contains(err.Error(), "driver-owned ResourceReplacer is disabled") {
		t.Fatalf("error = %v, want ResourceReplacer rejection", err)
	}
	if d.replaceCalled {
		t.Fatal("plain ResourceReplacer mutated cloud despite active state hooks")
	}
}

func TestDoReplace_ReplacerAllowedWhenOnlyApplyHookActive(t *testing.T) {
	d := &fakeReplacerDriver{}
	action := interfaces.PlanAction{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "x", Type: "test"},
		Current:  &interfaces.ResourceState{Name: "x", ProviderID: "old-id"},
	}
	result := &interfaces.ApplyResult{}
	err := doReplace(context.Background(), d, action, result, ApplyPlanHooks{
		OnResourceApplied: func(context.Context, interfaces.ResourceDriver, interfaces.PlanAction, interfaces.ResourceOutput) error {
			return nil
		},
	}, false)
	if err != nil {
		t.Fatalf("doReplace: %v", err)
	}
	if !d.replaceCalled {
		t.Fatal("plain ResourceReplacer should be allowed when no delete hook is active")
	}
}

func TestApplyPlanWithHooks_ReplacerGuardAbortsBeforeLaterActions(t *testing.T) {
	replacer := &fakeReplacerDriver{}
	provider := &hookProvider{driver: replacer}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{
			Action:   "create",
			Resource: interfaces.ResourceSpec{Name: "before", Type: "test"},
		},
		{
			Action:   "replace",
			Resource: interfaces.ResourceSpec{Name: "parent", Type: "test"},
			Current:  &interfaces.ResourceState{Name: "parent", Type: "test", ProviderID: "old-id"},
		},
	}}

	result, err := ApplyPlanWithHooks(context.Background(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error { return nil },
	})
	if err == nil {
		t.Fatal("expected top-level replace guard error")
	}
	if !strings.Contains(err.Error(), "driver-owned ResourceReplacer is disabled") {
		t.Fatalf("error = %v, want driver-owned replacer guard", err)
	}
	if replacer.replaceCalled {
		t.Fatal("guard allowed ResourceReplacer to mutate cloud")
	}
	if replacer.createCount != 0 {
		t.Fatalf("preflight guard ran after earlier mutation: createCount=%d", replacer.createCount)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("result.Errors = %+v, want top-level abort before per-action errors", result.Errors)
	}
}

func TestApplyPlanWithHooks_ReplacerPreflightDriverErrorAbortsBeforeMutation(t *testing.T) {
	driver := &fakeDriver{}
	provider := &replaceDriverErrorProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: interfaces.ResourceSpec{Name: "before", Type: "test"}},
		{Action: "replace", Resource: interfaces.ResourceSpec{Name: "parent", Type: "missing"}},
	}}

	result, err := ApplyPlanWithHooks(context.Background(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error { return nil },
	})
	if err == nil {
		t.Fatal("expected top-level preflight driver error")
	}
	if !strings.Contains(err.Error(), "replace preflight resolve driver") {
		t.Fatalf("error = %v, want preflight driver resolution error", err)
	}
	if driver.createCount != 0 {
		t.Fatalf("preflight driver error ran after earlier mutation: createCount=%d", driver.createCount)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("result.Errors = %+v, want top-level abort before per-action errors", result.Errors)
	}
}

type replaceDriverErrorProvider struct {
	hookProvider
	driver interfaces.ResourceDriver
}

func (p *replaceDriverErrorProvider) ResourceDriver(typ string) (interfaces.ResourceDriver, error) {
	if typ == "missing" {
		return nil, errors.New("missing driver")
	}
	return p.driver, nil
}
