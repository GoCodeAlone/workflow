package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan verifies
// the production apply path threads the loaded provider (not nil) into
// platform.ComputePlan so v2 Diff dispatch operates against a real plugin
// process at apply time. This regression-gates the temporary "nil
// placeholder" stub used to keep the package compilable between commits.
//
// The test seam is `computeInfraPlan` (var-indirected platform.ComputePlan).
// The test swaps it for a spy that captures the provider arg and asserts
// identity equality with the loaded fake. With nil passed at the call
// site the assertion would fail.
func TestApplyWithProviderAndStore_PassesLiveProviderToComputePlan(t *testing.T) {
	fake := &iactest.NoopProvider{ProviderName: "apply-recording-stub"}

	var captured interfaces.IaCProvider
	stop := errors.New("stop after compute")

	orig := computeInfraPlan
	computeInfraPlan = func(_ context.Context, p interfaces.IaCProvider, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		captured = p
		// Return an error so applyWithProviderAndStore returns immediately
		// without dispatching a real Apply against the fake provider.
		return interfaces.IaCPlan{}, stop
	}
	t.Cleanup(func() { computeInfraPlan = orig })

	specs := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), fake, "stub", specs, nil, nil, &w, "test", "", nil)
	if !errors.Is(err, stop) {
		t.Fatalf("expected sentinel error %v, got %v", stop, err)
	}
	if captured == nil {
		t.Fatal("ComputePlan was not invoked or received a nil provider")
	}
	if captured != fake {
		t.Errorf("ComputePlan received provider %p; want %p (the loaded provider)", captured, fake)
	}
}

// TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers verifies
// T3.7's manifest-driven dispatch: a provider whose
// ComputePlanVersion() returns "v2" routes through
// wfctlhelpers.ApplyPlan instead of provider.Apply. The seam is
// applyV2ApplyPlanWithHooksFn (var-indirected wfctlhelpers.ApplyPlan).
//
// rev2/rev3-locked: there is NO env-var. The branch is purely
// plugin-author-controlled via plugin.json's
// iacProvider.computePlanVersion (read at provider load time and
// surfaced via the optional ComputePlanVersionDeclarer interface).
func TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers(t *testing.T) {
	v2Provider := &iactest.NoopProvider{
		ProviderName:    "v2-stub",
		DispatchVersion: "v2",
	}

	var v2Called atomic.Bool
	v2Stop := errors.New("stop after v2 ApplyPlan dispatched")

	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		v2Called.Store(true)
		return nil, v2Stop
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	// Force ComputePlan to emit a non-empty plan so applyWithProviderAndStore
	// reaches the dispatch branch instead of short-circuiting on
	// "no changes".
	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), v2Provider, "stub", specs, nil, nil, &w, "test", "", nil)
	if !errors.Is(err, v2Stop) {
		t.Fatalf("expected v2 sentinel error %v, got %v", v2Stop, err)
	}
	if !v2Called.Load() {
		t.Error("wfctlhelpers.ApplyPlan was not invoked despite manifest declaring v2")
	}
}

// TestApplyWithProviderAndStore_V1FallsThroughToProviderApply
// verifies that a provider that does NOT declare v2 (via the optional
// interface) routes through the legacy provider.Apply path, not
// wfctlhelpers.ApplyPlan. Default behaviour for un-migrated plugins.
func TestApplyWithProviderAndStore_V1FallsThroughToProviderApply(t *testing.T) {
	v1Provider := &v1RecordingProvider{}

	var v2Called atomic.Bool
	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		v2Called.Store(true)
		return nil, errors.New("v2 must not be invoked for v1 manifest")
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(context.Background(), v1Provider, "stub", specs, nil, nil, &w, "test", "", nil); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if !v1Provider.applyCalled.Load() {
		t.Error("legacy provider.Apply was not invoked for v1 manifest")
	}
	if v2Called.Load() {
		t.Error("v2 ApplyPlan was invoked for a v1 manifest — dispatch routed to wrong path")
	}
}

// TestApplyWithProviderAndStore_V2PrintsDriftReport verifies the
// drift-report wiring: when wfctlhelpers.ApplyPlan returns a result
// with a non-empty InputDriftReport, applyWithProviderAndStore
// prints the FormatStaleError block to the writer (stderr in
// production). Pre-T3.7 the helper existed but wasn't called from
// any production path.
func TestApplyWithProviderAndStore_V2PrintsDriftReport(t *testing.T) {
	v2Provider := &iactest.NoopProvider{ProviderName: "drift-stub", DispatchVersion: "v2"}

	driftResult := &interfaces.ApplyResult{
		InputDriftReport: []interfaces.DriftEntry{
			{Name: "BUCKET_NAME", PlanFingerprint: "abc", ApplyFingerprint: "def"},
		},
	}

	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		return driftResult, nil
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(context.Background(), v2Provider, "stub", specs, nil, nil, &w, "test", "", nil); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if !strings.Contains(w.String(), "BUCKET_NAME") {
		t.Errorf("expected drift report to mention BUCKET_NAME; got %q", w.String())
	}
}

// TestApplyWithProviderAndStore_V2PrintsDriftReportOnPartialFailure
// covers the rev3 fix for the T3.7 review IMPORTANT #1: when
// wfctlhelpers.ApplyPlan returns (resultWithDrift, applyErr), the
// drift report MUST still be printed. Operators most need the
// stale-input diagnostic when an apply fails — without it, the
// failed-apply error and the "which input changed" context are
// disconnected. Pre-fix the gate was `if err == nil`, which dropped
// drift on partial failure.
func TestApplyWithProviderAndStore_V2PrintsDriftReportOnPartialFailure(t *testing.T) {
	v2Provider := &iactest.NoopProvider{ProviderName: "drift-stub", DispatchVersion: "v2"}

	driftResult := &interfaces.ApplyResult{
		InputDriftReport: []interfaces.DriftEntry{
			{Name: "PARTIAL_FAILURE_VAR", PlanFingerprint: "abc", ApplyFingerprint: "def"},
		},
	}
	applyErr := errors.New("partial failure during apply")

	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		return driftResult, applyErr
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), v2Provider, "stub", specs, nil, nil, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected error from ApplyPlan to propagate, got nil")
	}
	if !strings.Contains(w.String(), "PARTIAL_FAILURE_VAR") {
		t.Errorf("expected drift report on partial failure to mention PARTIAL_FAILURE_VAR; got %q", w.String())
	}
}

func TestApplyWithProviderAndStore_V2PersistsBeforeLaterActionFailure(t *testing.T) {
	store := &fakeStateStore{}
	driver := &v2ImmediatePersistDriver{store: store}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{
		{Name: "first", Type: "infra.test", Config: map[string]any{"provider": "stub"}},
		{Name: "second", Type: "infra.test", Config: map[string]any{"provider": "stub"}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, nil, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected second action failure")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("saved state count = %d, want 1; saved=%+v", len(store.saved), store.saved)
	}
	if store.saved[0].Name != "first" {
		t.Fatalf("saved state = %+v, want first resource", store.saved[0])
	}
	if !driver.observedFirstStateBeforeSecond {
		t.Fatal("second driver action did not observe first resource persisted")
	}
}

func TestApplyWithProviderAndStore_V2FailedDeleteKeepsState(t *testing.T) {
	store := &fakeStateStore{saved: []interfaces.ResourceState{{
		Name:       "old",
		Type:       "infra.test",
		ProviderID: "id-old",
	}}}
	v2Provider := &v2DriverProvider{driver: &iactest.NoopDriver{DeleteErr: errors.New("delete failed")}}

	origCompute := computeInfraPlan
	computeInfraPlan = func(context.Context, interfaces.IaCProvider, []interfaces.ResourceSpec, []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		return interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
			Action:   "delete",
			Resource: interfaces.ResourceSpec{Name: "old", Type: "infra.test"},
			Current:  &interfaces.ResourceState{Name: "old", Type: "infra.test", ProviderID: "id-old"},
		}}}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", nil, store.saved, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected failed delete error")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 0 {
		t.Fatalf("deleted state entries = %v, want none after failed cloud delete", store.deleted)
	}
	if len(store.saved) != 1 || store.saved[0].Name != "old" {
		t.Fatalf("saved state = %+v, want original old state retained", store.saved)
	}
}

func TestApplyWithProviderAndStore_V2SuccessfulDeletePersistsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	store := &cancelAwareStateStore{fakeStateStore: fakeStateStore{saved: []interfaces.ResourceState{{
		Name:       "old",
		Type:       "infra.test",
		ProviderID: "id-old",
	}}}}
	v2Provider := &v2DriverProvider{driver: &cancelAfterDeleteDriver{cancel: cancel}}

	origCompute := computeInfraPlan
	computeInfraPlan = func(context.Context, interfaces.IaCProvider, []interfaces.ResourceSpec, []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		return interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
			Action:   "delete",
			Resource: interfaces.ResourceSpec{Name: "old", Type: "infra.test"},
			Current:  &interfaces.ResourceState{Name: "old", Type: "infra.test", ProviderID: "id-old"},
		}}}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	var w bytes.Buffer
	if err := applyWithProviderAndStore(ctx, v2Provider, "stub", nil, store.saved, store, &w, "test", "", nil); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 1 || store.deleted[0] != "old" {
		t.Fatalf("deleted state entries = %v, want [old]", store.deleted)
	}
}

func TestApplyWithProviderAndStore_V2SensitiveOutputWithoutSecretsRollsBack(t *testing.T) {
	store := &fakeStateStore{}
	driver := &v2SensitiveCreateDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "api-key",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, nil, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected sensitive routing failure")
	}
	if !strings.Contains(err.Error(), "compensating delete succeeded") {
		t.Fatalf("error = %v, want compensating delete success", err)
	}
	if driver.deleteCount != 1 {
		t.Fatalf("driver delete count = %d, want 1", driver.deleteCount)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved state = %+v, want none after routing failure", store.saved)
	}
}

func TestApplyWithProviderAndStore_V2InvalidProviderIDRollsBack(t *testing.T) {
	store := &fakeStateStore{}
	driver := &v2InvalidProviderIDDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "app",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, nil, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected ProviderID validation failure")
	}
	if !strings.Contains(err.Error(), "malformed ProviderID") || !strings.Contains(err.Error(), "compensating delete succeeded") {
		t.Fatalf("error = %v, want ProviderID rejection with compensation", err)
	}
	if driver.deleteCount != 2 {
		t.Fatalf("driver delete count = %d, want malformed-ID attempt plus name-only fallback", driver.deleteCount)
	}
	if !driver.nameOnlyDeleteSucceeded {
		t.Fatal("ProviderID validation compensation did not fall back to name-only delete")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved state = %+v, want none after ProviderID validation failure", store.saved)
	}
}

func TestApplyWithProviderAndStore_V2FillsMissingOutputIdentityFromSpec(t *testing.T) {
	store := &fakeStateStore{}
	driver := &v2MissingIdentityDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "app",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, nil, store, &w, "test", "", nil); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 1 {
		t.Fatalf("saved state count = %d, want 1", len(store.saved))
	}
	if store.saved[0].Name != "app" || store.saved[0].Type != "infra.test" {
		t.Fatalf("saved state identity = %s/%s, want app/infra.test", store.saved[0].Name, store.saved[0].Type)
	}
}

func TestApplyWithProviderAndStore_V2UpdateSaveFailureDoesNotDelete(t *testing.T) {
	current := interfaces.ResourceState{Name: "app", Type: "infra.test", ProviderID: "id-existing"}
	store := &fakeStateStore{saved: []interfaces.ResourceState{current}, saveErr: errors.New("state store down")}
	driver := &v2UpdateFailureDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		return interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
			Action:   "update",
			Resource: specs[0],
			Current:  &current[0],
		}}}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "app",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, []interfaces.ResourceState{current}, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected state save failure")
	}
	if strings.Contains(err.Error(), "compensating delete") {
		t.Fatalf("error = %v, update path must not compensate with delete", err)
	}
	if driver.deleteCount != 0 {
		t.Fatalf("driver delete count = %d, want 0 for update state-save failure", driver.deleteCount)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deleted) != 0 {
		t.Fatalf("deleted state entries = %v, want none after update state-save failure", store.deleted)
	}
}

func TestApplyWithProviderAndStore_V2UpdateProviderIDFailureDoesNotDelete(t *testing.T) {
	current := interfaces.ResourceState{Name: "app", Type: "infra.test", ProviderID: "id-existing"}
	store := &fakeStateStore{saved: []interfaces.ResourceState{current}}
	driver := &v2UpdateInvalidProviderIDDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, current []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		return interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
			Action:   "update",
			Resource: specs[0],
			Current:  &current[0],
		}}}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "app",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, []interfaces.ResourceState{current}, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected ProviderID validation failure")
	}
	if strings.Contains(err.Error(), "compensating delete") {
		t.Fatalf("error = %v, update validation path must not compensate with delete", err)
	}
	if driver.deleteCount != 0 {
		t.Fatalf("driver delete count = %d, want 0 for update validation failure", driver.deleteCount)
	}
}

func TestApplyWithProviderAndStore_V2MismatchedOutputIdentityRollsBack(t *testing.T) {
	store := &fakeStateStore{}
	driver := &v2MismatchedIdentityDriver{}
	v2Provider := &v2DriverProvider{driver: driver}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{
		Name:   "app",
		Type:   "infra.test",
		Config: map[string]any{"provider": "stub"},
	}}

	var w bytes.Buffer
	err := applyWithProviderAndStore(t.Context(), v2Provider, "stub", specs, nil, store, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected output identity mismatch failure")
	}
	if !strings.Contains(err.Error(), "output type") || !strings.Contains(err.Error(), "compensating delete succeeded") {
		t.Fatalf("error = %v, want output identity rejection with compensation", err)
	}
	if driver.deleteCount != 3 {
		t.Fatalf("driver delete count = %d, want returned identity and desired identity delete attempts", driver.deleteCount)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.saved) != 0 {
		t.Fatalf("saved state = %+v, want none after identity mismatch", store.saved)
	}
}

// TestApplyWithProviderAndStore_V1Path_DeclarerReturnsEmpty pins the
// "Path B" v1 fallback (rev3 fix for T3.7 review IMPORTANT #3): a
// provider that DOES implement ComputePlanVersionDeclarer but
// returns "" (or any non-"v2" value) routes through the legacy
// provider.Apply path, not wfctlhelpers.ApplyPlan. This is the
// expected mid-transition state for v1 plugins after the SDK update
// lands but before they explicitly migrate. iactest.NoopProvider
// always implements the interface (the method exists on the type);
// leaving DispatchVersion empty exercises Path B specifically. Path
// A (provider doesn't implement the interface at all) is covered by
// TestApplyWithProviderAndStore_V1FallsThroughToProviderApply via
// v1RecordingProvider, which omits the method.
func TestApplyWithProviderAndStore_V1Path_DeclarerReturnsEmpty(t *testing.T) {
	v1Provider := &iactest.NoopProvider{ProviderName: "v1-empty-decl", DispatchVersion: ""}

	var v2Called atomic.Bool
	origApply := applyV2ApplyPlanWithHooksFn
	applyV2ApplyPlanWithHooksFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan, _ wfctlhelpers.ApplyPlanHooks) (*interfaces.ApplyResult, error) {
		v2Called.Store(true)
		return nil, errors.New("v2 must not be invoked when DispatchVersion is empty")
	}
	t.Cleanup(func() { applyV2ApplyPlanWithHooksFn = origApply })

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, len(specs))
		for i, s := range specs {
			actions[i] = interfaces.PlanAction{Action: "create", Resource: s}
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	specs := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}

	var w bytes.Buffer
	if err := applyWithProviderAndStore(context.Background(), v1Provider, "stub", specs, nil, nil, &w, "test", "", nil); err != nil {
		t.Fatalf("applyWithProviderAndStore: %v", err)
	}
	if v2Called.Load() {
		t.Error("v2 ApplyPlan was invoked when ComputePlanVersion() returned empty — dispatch routed to wrong path")
	}
}

// v1RecordingProvider is a minimal interfaces.IaCProvider that does
// NOT implement ComputePlanVersionDeclarer (the entire point of this
// fixture: prove the dispatch defaults to v1 for un-declared
// providers). Tracks Apply invocations so the v1-routing test can
// assert legacy dispatch fired.
type v1RecordingProvider struct {
	applyCalled atomic.Bool
}

var _ interfaces.IaCProvider = (*v1RecordingProvider)(nil)

func (p *v1RecordingProvider) Name() string                                         { return "v1-stub" }
func (p *v1RecordingProvider) Version() string                                      { return "0.0.0" }
func (p *v1RecordingProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *v1RecordingProvider) Capabilities() []interfaces.IaCCapabilityDeclaration {
	return nil
}
func (p *v1RecordingProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.applyCalled.Store(true)
	return &interfaces.ApplyResult{}, nil
}
func (p *v1RecordingProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *v1RecordingProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *v1RecordingProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *v1RecordingProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *v1RecordingProvider) SupportedCanonicalKeys() []string { return nil }
func (p *v1RecordingProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *v1RecordingProvider) Close() error { return nil }

type v2DriverProvider struct {
	iactest.NoopProvider
	driver interfaces.ResourceDriver
}

func (p *v2DriverProvider) ComputePlanVersion() string { return "v2" }

func (p *v2DriverProvider) ResourceDriver(string) (interfaces.ResourceDriver, error) {
	return p.driver, nil
}

type v2ImmediatePersistDriver struct {
	iactest.NoopDriver
	store                          *fakeStateStore
	observedFirstStateBeforeSecond bool
}

func (d *v2ImmediatePersistDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	switch spec.Name {
	case "first":
		return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "id-first"}, nil
	case "second":
		d.store.mu.Lock()
		for _, saved := range d.store.saved {
			if saved.Name == "first" {
				d.observedFirstStateBeforeSecond = true
				break
			}
		}
		d.store.mu.Unlock()
		return nil, errors.New("second create failed")
	default:
		return nil, errors.New("unexpected resource")
	}
}

type v2SensitiveCreateDriver struct {
	iactest.NoopDriver
	deleteCount int
}

func (d *v2SensitiveCreateDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: "id-sensitive",
		Outputs:    map[string]any{"token": "secret"},
		Sensitive:  map[string]bool{"token": true},
	}, nil
}

func (d *v2SensitiveCreateDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount++
	return d.NoopDriver.Delete(ctx, ref)
}

type v2InvalidProviderIDDriver struct {
	iactest.NoopDriver
	deleteCount             int
	nameOnlyDeleteSucceeded bool
}

func (d *v2InvalidProviderIDDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
	return interfaces.IDFormatUUID
}

func (d *v2InvalidProviderIDDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: "not-a-uuid",
		Outputs:    map[string]any{"url": "https://example.test"},
	}, nil
}

func (d *v2InvalidProviderIDDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount++
	if ref.ProviderID != "" {
		return interfaces.ErrResourceNotFound
	}
	d.nameOnlyDeleteSucceeded = true
	return d.NoopDriver.Delete(ctx, ref)
}

type cancelAfterDeleteDriver struct {
	iactest.NoopDriver
	cancel context.CancelFunc
}

func (d *cancelAfterDeleteDriver) Delete(context.Context, interfaces.ResourceRef) error {
	d.cancel()
	return nil
}

type cancelAwareStateStore struct {
	fakeStateStore
}

func (s *cancelAwareStateStore) DeleteResource(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.fakeStateStore.DeleteResource(ctx, name)
}

type v2MissingIdentityDriver struct {
	iactest.NoopDriver
}

func (d *v2MissingIdentityDriver) Create(context.Context, interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		ProviderID: "id-missing-identity",
		Outputs:    map[string]any{"url": "https://example.test"},
	}, nil
}

type v2UpdateFailureDriver struct {
	iactest.NoopDriver
	deleteCount int
}

func (d *v2UpdateFailureDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: ref.ProviderID,
		Outputs:    map[string]any{"url": "https://updated.example.test"},
	}, nil
}

func (d *v2UpdateFailureDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount++
	return d.NoopDriver.Delete(ctx, ref)
}

type v2UpdateInvalidProviderIDDriver struct {
	v2UpdateFailureDriver
}

func (d *v2UpdateInvalidProviderIDDriver) ProviderIDFormat() interfaces.ProviderIDFormat {
	return interfaces.IDFormatUUID
}

func (d *v2UpdateInvalidProviderIDDriver) Update(_ context.Context, _ interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       spec.Type,
		ProviderID: "not-a-uuid",
		Outputs:    map[string]any{"url": "https://updated.example.test"},
	}, nil
}

type v2MismatchedIdentityDriver struct {
	iactest.NoopDriver
	deleteCount int
}

func (d *v2MismatchedIdentityDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{
		Name:       spec.Name,
		Type:       "infra.other",
		ProviderID: "id-mismatched-identity",
		Outputs:    map[string]any{"url": "https://example.test"},
	}, nil
}

func (d *v2MismatchedIdentityDriver) Delete(ctx context.Context, ref interfaces.ResourceRef) error {
	d.deleteCount++
	if ref.Type == "infra.other" {
		return errors.New("reported identity not deletable")
	}
	return d.NoopDriver.Delete(ctx, ref)
}
