package wfctlhelpers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestApplyPlanWithHooks_PersistsBeforeLaterAction(t *testing.T) {
	driver := &hookOrderingDriver{}
	provider := &hookProvider{driver: driver}
	driver.create = func(spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
		switch spec.Name {
		case "first":
			return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-first"}, nil
		case "second":
			if !driver.hookRan {
				t.Fatal("second action ran before first action hook")
			}
			return nil, errors.New("second failed")
		default:
			t.Fatalf("unexpected resource %q", spec.Name)
			return nil, nil
		}
	}

	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{Action: "create", Resource: interfaces.ResourceSpec{Name: "first", Type: "infra.test"}},
		{Action: "create", Resource: interfaces.ResourceSpec{Name: "second", Type: "infra.test"}},
	}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceApplied: func(_ context.Context, _ interfaces.ResourceDriver, action interfaces.PlanAction, out interfaces.ResourceOutput) error {
			if action.Resource.Name == "first" && out.ProviderID == "id-first" {
				driver.hookRan = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if !driver.hookRan {
		t.Fatal("first action hook did not run")
	}
}

func TestApplyPlanWithHooks_CallsOnActionCompleteForFailure(t *testing.T) {
	driver := &hookOrderingDriver{
		create: func(interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
			return nil, errors.New("create failed")
		},
	}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "create",
		Resource: interfaces.ResourceSpec{Name: "bad", Type: "infra.test"},
	}}}

	var observed []interfaces.ActionOutcome
	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnActionComplete: func(_ context.Context, _ interfaces.PlanAction, outcome interfaces.ActionOutcome) {
			observed = append(observed, outcome)
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors len = %d, want 1", len(result.Errors))
	}
	if len(observed) != 1 {
		t.Fatalf("OnActionComplete calls = %d, want 1", len(observed))
	}
	if observed[0].Status != interfaces.ActionStatusError || !strings.Contains(observed[0].Error, "create failed") {
		t.Fatalf("observed outcome = %#v, want error outcome with create failure", observed[0])
	}
}

func TestApplyPlanWithHooks_DefaultReplaceDeleteHookRunsBeforeCreateFailure(t *testing.T) {
	driver := &replaceDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(_ context.Context, action interfaces.PlanAction) error {
			if action.Resource.Name == "db" {
				driver.deleteHookRan = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if !driver.deleteHookRan {
		t.Fatal("replace delete hook did not run before create failure")
	}
}

func TestApplyPlanWithHooks_DefaultReplaceDeleteHookErrorIsTopLevel(t *testing.T) {
	driver := &replaceDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "replace",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}
	hookErr := errors.New("state store down")

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error {
			return hookErr
		},
	})
	if !errors.Is(err, hookErr) {
		t.Fatalf("error = %v, want hookErr", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("result.Errors = %+v, want no per-action driver errors for hook failure", result.Errors)
	}
}

func TestApplyPlanWithHooks_FailedDeleteDoesNotRunDeleteHook(t *testing.T) {
	driver := &failedDeleteHookDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
		Action:   "delete",
		Resource: interfaces.ResourceSpec{Name: "db", Type: "infra.test"},
		Current:  &interfaces.ResourceState{Name: "db", Type: "infra.test", ProviderID: "old-id"},
	}}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{
		OnResourceDeleted: func(context.Context, interfaces.PlanAction) error {
			driver.deleteHookRan = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %d, want 1", len(result.Errors))
	}
	if driver.deleteHookRan {
		t.Fatal("delete hook ran after failed delete")
	}
}

func TestApplyPlanWithHooks_DeleteRemovesResourceFromLaterJIT(t *testing.T) {
	driver := &deleteThenDependentDriver{}
	provider := &hookProvider{driver: driver}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{
			Action:   "delete",
			Resource: interfaces.ResourceSpec{Name: "old", Type: "infra.test"},
			Current: &interfaces.ResourceState{
				Name:       "old",
				Type:       "infra.test",
				ProviderID: "id-old",
			},
		},
		{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name:   "dependent",
				Type:   "infra.test",
				Config: map[string]any{"target": "${old.id}"},
			},
		},
	}}

	result, err := ApplyPlanWithHooks(t.Context(), provider, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks returned top-level error: %v", err)
	}
	if driver.dependentCreateCalled {
		t.Fatal("dependent create reached driver with stale deleted resource output")
	}
	if len(result.Errors) != 1 {
		t.Fatalf("result.Errors = %+v, want one JIT error", result.Errors)
	}
	if !strings.Contains(result.Errors[0].Error, "jit substitution:") || !strings.Contains(result.Errors[0].Error, "old") {
		t.Fatalf("result error = %q, want unresolved old reference", result.Errors[0].Error)
	}
}

type hookOrderingDriver struct {
	iactest.NoopDriver
	create  func(interfaces.ResourceSpec) (*interfaces.ResourceOutput, error)
	hookRan bool
}

func (d *hookOrderingDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if d.create != nil {
		return d.create(spec)
	}
	return d.NoopDriver.Create(ctx, spec)
}

type hookProvider struct {
	iactest.NoopProvider
	driver interfaces.ResourceDriver
}

func (p *hookProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

type replaceDeleteHookDriver struct {
	iactest.NoopDriver
	deleteHookRan bool
}

func (d *replaceDeleteHookDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.NoopDriver.Delete(ctx, ref)
}

func (d *replaceDeleteHookDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if !d.deleteHookRan {
		return nil, errors.New("delete hook had not run")
	}
	return nil, errors.New("create failed")
}

type failedDeleteHookDriver struct {
	iactest.NoopDriver
	deleteHookRan bool
}

func (d *failedDeleteHookDriver) Delete(context.Context, interfaces.ResourceRef) error {
	return errors.New("delete failed")
}

type deleteThenDependentDriver struct {
	iactest.NoopDriver
	dependentCreateCalled bool
}

func (d *deleteThenDependentDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	return d.NoopDriver.Delete(ctx, ref)
}

func (d *deleteThenDependentDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	if spec.Name == "dependent" {
		d.dependentCreateCalled = true
		return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-dependent"}, nil
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-" + spec.Name}, nil
}

// TestApplyPlanWithHooks_PopulatesActions_CleanSuccess verifies the
// Phase 2 engine-side ActionOutcome population — every successful
// PlanAction gets a corresponding result.Actions entry with
// ActionStatusSuccess and ActionIndex matching loop position. Per
// workflow#640 Phase 2 + ADR 0040 invariant 1.
func TestApplyPlanWithHooks_PopulatesActions_CleanSuccess(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{
		ID: "plan-1",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"}},
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r2", Type: "infra.test"}},
		},
	}
	result, err := ApplyPlanWithHooks(t.Context(), p, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("top-level err: %v", err)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("expected 2 ActionOutcomes, got %d: %+v", len(result.Actions), result.Actions)
	}
	for i, a := range result.Actions {
		if a.ActionIndex != uint32(i) {
			t.Errorf("action %d: ActionIndex=%d, want %d", i, a.ActionIndex, i)
		}
		if a.Status != interfaces.ActionStatusSuccess {
			t.Errorf("action %d: Status=%v, want Success", i, a.Status)
		}
		if a.Error != "" {
			t.Errorf("action %d: Error=%q, want empty", i, a.Error)
		}
	}
}

// TestApplyPlanWithHooks_PopulatesActions_PreDispatchDriverError covers
// the CRITICAL cycle-1 plan-review C-1 invariant: every continue exit
// path (here, the driver-resolve error at apply.go:228-234) must still
// append an ActionOutcome so the post-loop length-validation assert
// never false-fires. Per ADR 0040.
func TestApplyPlanWithHooks_PopulatesActions_PreDispatchDriverError(t *testing.T) {
	p := &fakeProvider{driverErr: errors.New("driver resolution failed")}
	plan := &interfaces.IaCPlan{
		ID: "plan-1",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "unknown.resource"}},
		},
	}
	result, err := ApplyPlanWithHooks(t.Context(), p, plan, ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("expected no top-level err on driver-resolve failure (best-effort continue), got: %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 ActionOutcome (length-assert invariant), got %d: %+v", len(result.Actions), result.Actions)
	}
	// Phase 2.3 (workflow#698) reclassification: driver-resolve-error is
	// PRE-DISPATCH — driver's Create/Update/Delete RPC never called; cloud
	// state unchanged. Status maps to SKIPPED. Phase 2 mapped this to Error
	// which conflated pre-dispatch skip with dispatch-level failure.
	if result.Actions[0].Status != interfaces.ActionStatusSkipped {
		t.Errorf("driver-resolve-error status: want Skipped (Phase 2.3 pre-dispatch reclassification), got %v", result.Actions[0].Status)
	}
	if result.Actions[0].ActionIndex != 0 {
		t.Errorf("ActionIndex: want 0, got %d", result.Actions[0].ActionIndex)
	}
	if result.Actions[0].Error == "" {
		t.Errorf("Error: want non-empty, got empty")
	}
	// Cross-check: existing result.Errors path also populated so the
	// pre-Phase-2 contract is preserved.
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 result.Errors entry (legacy contract), got %d", len(result.Errors))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OnPlanComplete tests (workflow#695 Phase 2.5). Per plan task 2 +
// cycle-1 plan-review C-3 v1 semantic preservation: OnPlanComplete fires
// ONLY on the natural success-exit return at the end of
// applyPlanWithEnvProviderAndHooks, matching the DOProvider.Apply v1
// wrapper's "return without flushing on top-level err" behavior (the
// `if err != nil { return ... }` guard immediately after the wrapped
// ApplyPlan call in workflow-plugin-digitalocean internal/provider.go).
// Outer errors at the preflightProviderOwnedReplaceWithDeleteHooks
// early-return, the per-action loop's fatalErr early-return, and the
// post-loop length-invariant check ALL skip finalize.
// ─────────────────────────────────────────────────────────────────────────────

// TestApplyPlanWithHooks_OnPlanComplete_FiresOnCleanSuccess verifies that
// OnPlanComplete fires after a normally-completed apply loop.
func TestApplyPlanWithHooks_OnPlanComplete_FiresOnCleanSuccess(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{
		ID: "plan-1",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"}},
		},
	}
	var fired bool
	hooks := ApplyPlanHooks{
		OnPlanComplete: func(_ context.Context) error {
			fired = true
			return nil
		},
	}
	_, err := ApplyPlanWithHooks(t.Context(), p, plan, hooks)
	if err != nil {
		t.Fatalf("top-level err: %v", err)
	}
	if !fired {
		t.Error("OnPlanComplete did not fire on clean success")
	}
}

// TestApplyPlanWithHooks_OnPlanComplete_FiresOnEmptyPlan verifies that
// OnPlanComplete fires even when plan.Actions is empty — loopReached is
// set BEFORE the for-loop opens so a zero-iteration plan still finalizes.
// Regression guard: the v1 DOProvider.Apply wrapper flushes stale-queued
// state even for empty plans (no per-action work needed); the v2 hook
// must preserve that semantic.
func TestApplyPlanWithHooks_OnPlanComplete_FiresOnEmptyPlan(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{ID: "plan-empty", Actions: nil}
	var fired bool
	hooks := ApplyPlanHooks{OnPlanComplete: func(_ context.Context) error {
		fired = true
		return nil
	}}
	_, err := ApplyPlanWithHooks(t.Context(), p, plan, hooks)
	if err != nil {
		t.Fatalf("top-level err: %v", err)
	}
	if !fired {
		t.Error("OnPlanComplete did not fire on empty plan (regression: v1 wrapper flushes stale-queued state even for empty plans)")
	}
}

// TestApplyPlanWithHooks_OnPlanComplete_SurfacesErrorToCaller verifies that
// a finalize-side hook error surfaces to the caller (outer err wraps the
// sentinel) AND appends an ActionError with Resource="<plan-finalize>" to
// result.Errors so per-driver attribution is preserved alongside the
// finalize-attributed failure.
func TestApplyPlanWithHooks_OnPlanComplete_SurfacesErrorToCaller(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{
		ID: "plan-2",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"}},
		},
	}
	sentinel := errors.New("plugin finalize failed")
	hooks := ApplyPlanHooks{OnPlanComplete: func(_ context.Context) error { return sentinel }}
	result, err := ApplyPlanWithHooks(t.Context(), p, plan, hooks)
	if err == nil {
		t.Fatal("expected finalize error to surface to caller")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected outer err to wrap sentinel; got: %v", err)
	}
	if len(result.Errors) == 0 || result.Errors[len(result.Errors)-1].Resource != "<plan-finalize>" {
		t.Errorf("expected last result.Errors entry to have Resource=\"<plan-finalize>\"; got: %+v", result.Errors)
	}
	if last := result.Errors[len(result.Errors)-1]; last.Action != "finalize" {
		t.Errorf("expected last result.Errors entry to have Action=\"finalize\"; got: %q", last.Action)
	}
}

// TestApplyPlanWithHooks_OnPlanComplete_SkippedOnOuterError verifies the v1
// semantic-preservation gate (cycle-1 plan-review C-3): when a per-action
// hook returns an error, fatalErr bubbles to the per-action loop's
// `if fatalErr != nil { return ... }` early-return with non-nil outer err.
// OnPlanComplete MUST NOT fire on that exit, matching DOProvider.Apply's
// "return without flushing on top-level err" behavior (the
// `if err != nil { return ... }` guard immediately after the wrapped
// ApplyPlan call in workflow-plugin-digitalocean internal/provider.go).
func TestApplyPlanWithHooks_OnPlanComplete_SkippedOnOuterError(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{
		ID: "plan-fatal",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"}},
		},
	}
	hookSentinel := errors.New("post-apply hook failure")
	var finalizeFired bool
	hooks := ApplyPlanHooks{
		// OnResourceApplied returning err sets fatalErr inside the per-
		// action loop's deferred dispatch closure → bubbles to the loop's
		// `if fatalErr != nil { return ... }` early-return with non-nil
		// outer err. True outer-error path.
		OnResourceApplied: func(_ context.Context, _ interfaces.ResourceDriver, _ interfaces.PlanAction, _ interfaces.ResourceOutput) error {
			return hookSentinel
		},
		OnPlanComplete: func(_ context.Context) error {
			finalizeFired = true
			return nil
		},
	}
	_, err := ApplyPlanWithHooks(t.Context(), p, plan, hooks)
	if err == nil {
		t.Fatal("expected outer err from per-action hook fatalErr path")
	}
	if !errors.Is(err, hookSentinel) {
		t.Errorf("expected outer err to wrap hookSentinel; got: %v", err)
	}
	if finalizeFired {
		t.Error("OnPlanComplete fired on outer-error exit — v1 semantic preservation (C-3) violated")
	}
}

// TestApplyPlanWithHooks_OnPlanComplete_DoesNotFireOnPreloopError verifies
// that pre-loop preflight failures (the
// preflightProviderOwnedReplaceWithDeleteHooks early-return) skip
// OnPlanComplete — loopReached is still false when preflight returns
// early, so the deferred closure's first short-circuit triggers and
// finalize never runs. Regression guard: no cloud work happened, so no
// finalize work should happen either.
func TestApplyPlanWithHooks_OnPlanComplete_DoesNotFireOnPreloopError(t *testing.T) {
	// fakeReplacerDriver implements interfaces.ResourceReplacer (defined in
	// apply_replacer_dispatch_test.go); combined with deleteHookActive
	// (OnResourceDeleted set), preflightProviderOwnedReplaceWithDeleteHooks
	// returns its engine-side ResourceReplacer rejection error.
	provider := &hookProvider{driver: &fakeReplacerDriver{}}
	plan := &interfaces.IaCPlan{
		ID: "plan-preflight-fail",
		Actions: []interfaces.PlanAction{{
			Action:   "replace",
			Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"},
			Current:  &interfaces.ResourceState{Name: "r1", Type: "infra.test", ProviderID: "old-id"},
		}},
	}
	var finalizeFired bool
	hooks := ApplyPlanHooks{
		OnResourceDeleted: func(_ context.Context, _ interfaces.PlanAction) error { return nil },
		OnPlanComplete: func(_ context.Context) error {
			finalizeFired = true
			return nil
		},
	}
	_, err := ApplyPlanWithHooks(t.Context(), provider, plan, hooks)
	if err == nil {
		t.Fatal("expected preflight error (replace + delete-hook active + ResourceReplacer driver)")
	}
	if !strings.Contains(err.Error(), "driver-owned ResourceReplacer is disabled") {
		t.Errorf("expected preflight rejection error; got: %v", err)
	}
	if finalizeFired {
		t.Error("OnPlanComplete fired on pre-loop preflight error — should not fire when loopReached=false")
	}
}

// TestApplyPlanWithHooks_OnPlanComplete_RecoversFromPanic verifies that a
// panic inside the caller-provided OnPlanComplete closure does NOT
// propagate past the deferred closure — it's caught by recover() and
// surfaced as a finalize-attributed err entry on result.Errors plus an
// outer err. Symmetry with the drift defer's recover() (apply.go's
// post-loop input-drift postcondition) which has the same posture for
// caller-provided env-provider closures.
func TestApplyPlanWithHooks_OnPlanComplete_RecoversFromPanic(t *testing.T) {
	p := newFakeProvider()
	plan := &interfaces.IaCPlan{
		ID: "plan-panic",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "r1", Type: "infra.test"}},
		},
	}
	hooks := ApplyPlanHooks{
		OnPlanComplete: func(_ context.Context) error {
			panic("finalize impl bug")
		},
	}
	result, err := ApplyPlanWithHooks(t.Context(), p, plan, hooks)
	if err == nil {
		t.Fatal("expected outer err from recovered OnPlanComplete panic")
	}
	if !strings.Contains(err.Error(), "OnPlanComplete panicked") {
		t.Errorf("expected err to mention panic recovery; got: %v", err)
	}
	if !strings.Contains(err.Error(), "finalize impl bug") {
		t.Errorf("expected err to carry panic value; got: %v", err)
	}
	if len(result.Errors) == 0 || result.Errors[len(result.Errors)-1].Resource != "<plan-finalize>" {
		t.Errorf("expected last result.Errors entry to have Resource=\"<plan-finalize>\"; got: %+v", result.Errors)
	}
}

// TestStatusHelpers_Phase23 pins the 3 phase-specific helper functions
// per workflow#698 Phase 2.3 to their plan-spec'd return values.
func TestStatusHelpers_Phase23(t *testing.T) {
	if got := statusForPreDispatchSkip(); got != interfaces.ActionStatusSkipped {
		t.Errorf("statusForPreDispatchSkip() = %v; want ActionStatusSkipped", got)
	}
	if got := statusForDispatchError("create"); got != interfaces.ActionStatusError {
		t.Errorf("statusForDispatchError(\"create\") = %v; want ActionStatusError", got)
	}
	if got := statusForDispatchError("update"); got != interfaces.ActionStatusError {
		t.Errorf("statusForDispatchError(\"update\") = %v; want ActionStatusError", got)
	}
	if got := statusForDispatchError("delete"); got != interfaces.ActionStatusDeleteFailed {
		t.Errorf("statusForDispatchError(\"delete\") = %v; want ActionStatusDeleteFailed", got)
	}
	if got := statusForPostHookFailure(); got != interfaces.ActionStatusCompensationFailed {
		t.Errorf("statusForPostHookFailure() = %v; want ActionStatusCompensationFailed", got)
	}
}
