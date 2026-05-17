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
// the v2 dispatch path: applyWithProviderAndStore routes through
// wfctlhelpers.ApplyPlan (the only supported path post-workflow#699).
// The seam is applyV2ApplyPlanWithHooksFn (var-indirected
// wfctlhelpers.ApplyPlan).
//
// Post-workflow#699: there is no v1 fallback; provider.Apply,
// ComputePlanVersion(), ComputePlanVersionDeclarer, and the manifest's
// iacProvider.computePlanVersion v1/v2 dispatch decision are all gone.
// Load-time enforcement at discoverAndLoadIaCProvider rejects providers
// whose typed CapabilitiesResponse.compute_plan_version != "v2".
func TestApplyWithProviderAndStore_V2RoutesThroughWfctlhelpers(t *testing.T) {
	v2Provider := &iactest.NoopProvider{
		ProviderName: "v2-stub",
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

// TestApplyWithProviderAndStore_V1FallsThroughToProviderApply +
// TestApplyWithProviderAndStore_V1Path_DeclarerReturnsEmpty +
// v1RecordingProvider stub were deleted per workflow#699 — v1 dispatch
// has no remaining surface to exercise; the runtime gate in
// discoverAndLoadIaCProvider rejects v1 plugins at load time and
// IaCProvider.Apply is gone from the interface.

// TestApplyWithProviderAndStore_V2PrintsDriftReport verifies the
// drift-report wiring: when wfctlhelpers.ApplyPlan returns a result
// with a non-empty InputDriftReport, applyWithProviderAndStore
// prints the FormatStaleError block to the writer (stderr in
// production). Pre-T3.7 the helper existed but wasn't called from
// any production path.
func TestApplyWithProviderAndStore_V2PrintsDriftReport(t *testing.T) {
	v2Provider := &iactest.NoopProvider{ProviderName: "drift-stub"}

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
	v2Provider := &iactest.NoopProvider{ProviderName: "drift-stub"}

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

type v2DriverProvider struct {
	iactest.NoopProvider
	driver interfaces.ResourceDriver
}

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
