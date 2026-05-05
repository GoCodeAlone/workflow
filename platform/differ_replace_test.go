package platform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// TestComputePlan_NeedsReplaceEmitsReplaceAction is the binding test for
// the v2 IaC contract: when a provider's Diff returns NeedsReplace=true,
// ComputePlan emits Action="replace" rather than coercing it to update.
// Pre-W-3b ComputePlan was provider-agnostic and only knew how to compute
// create/update/delete — Replace was unrepresentable. This test ships in
// the same commit as the implementation; the rev3 fix for the cycle-2
// self-contradiction (no skipped TDD harness from prior commits).
func TestComputePlan_NeedsReplaceEmitsReplaceAction(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc", Type: "infra.vpc", ProviderID: "old", AppliedConfig: map[string]any{"region": "nyc1"}},
	}
	fp := newFakeProviderWithDiff(&interfaces.DiffResult{
		NeedsReplace: true,
		Changes:      []interfaces.FieldChange{{Path: "region", Old: "nyc1", New: "nyc3", ForceNew: true}},
	})
	plan, err := platform.ComputePlan(context.Background(), fp, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].Action != "replace" {
		t.Errorf("expected replace action, got %q (%+v)", plan.Actions[0].Action, plan.Actions[0])
	}
	if plan.Actions[0].Resource.Name != "vpc" {
		t.Errorf("resource name = %q, want %q", plan.Actions[0].Resource.Name, "vpc")
	}
}

// TestComputePlan_ForceNewWithoutNeedsReplace_StillEmitsReplace covers
// the latent bug-fix surface from design issue C: a provider that sets
// NeedsUpdate=true with one or more ForceNew=true field changes (but
// forgets to set NeedsReplace) must still surface as replace in the
// plan. Pre-W-3b this would silently downgrade to update — a Delete +
// Create pair that should have happened wouldn't.
func TestComputePlan_ForceNewWithoutNeedsReplace_StillEmitsReplace(t *testing.T) {
	desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}}}
	current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}
	fp := newFakeProviderWithDiff(&interfaces.DiffResult{
		NeedsUpdate: true,
		Changes:     []interfaces.FieldChange{{Path: "region", ForceNew: true}},
	})
	plan, err := platform.ComputePlan(context.Background(), fp, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].Action != "replace" {
		t.Errorf("ForceNew should imply replace; got %q (%+v)", plan.Actions[0].Action, plan.Actions[0])
	}
}

// TestComputePlan_NeedsUpdateWithoutForceNew_EmitsUpdate verifies the
// negative case: a Diff with NeedsUpdate=true and no ForceNew changes
// stays as update (no over-eager replace emission).
func TestComputePlan_NeedsUpdateWithoutForceNew_EmitsUpdate(t *testing.T) {
	desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}}}
	current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}
	fp := newFakeProviderWithDiff(&interfaces.DiffResult{
		NeedsUpdate: true,
		Changes:     []interfaces.FieldChange{{Path: "tags", ForceNew: false}},
	})
	plan, err := platform.ComputePlan(context.Background(), fp, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].Action != "update" {
		t.Errorf("expected update action, got %q (%+v)", plan.Actions[0].Action, plan.Actions[0])
	}
}

// TestComputePlan_DiffReturnsNoChanges_EmitsNothing verifies the no-op
// shape: when Diff returns NeedsUpdate=false and NeedsReplace=false,
// ComputePlan emits no action for that resource.
func TestComputePlan_DiffReturnsNoChanges_EmitsNothing(t *testing.T) {
	desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}}}
	current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}
	fp := newFakeProviderWithDiff(&interfaces.DiffResult{}) // no flags set
	plan, err := platform.ComputePlan(context.Background(), fp, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 0 {
		t.Errorf("expected no actions when Diff yields no changes; got %+v", plan.Actions)
	}
}

// TestComputePlan_NilProvider_FallsBackToConfigHash verifies the
// nil-provider tolerance contract documented on ComputePlan: when the
// caller passes nil (e.g., legacy fixtures, configs without
// iac.provider modules), ComputePlan reverts to the legacy ConfigHash
// compare path rather than panicking on a nil ResourceDriver call.
func TestComputePlan_NilProvider_FallsBackToConfigHash(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: map[string]any{"engine": "postgres"}},
	}
	current := []interfaces.ResourceState{
		{Name: "db", Type: "infra.database", ConfigHash: "stale-hash"},
	}
	plan, err := platform.ComputePlan(context.Background(), nil, desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "update" {
		t.Errorf("expected single update action via ConfigHash fallback; got %+v", plan.Actions)
	}
}

// TestComputePlan_NilDriver_FallsBackToConfigHash verifies that when
// p.ResourceDriver(typ) returns (nil, nil) (e.g., the no-op fakeProvider
// shape), ComputePlan reverts to ConfigHash compare for that resource
// rather than dispatching Diff against a nil driver.
func TestComputePlan_NilDriver_FallsBackToConfigHash(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "db", Type: "infra.database", Config: map[string]any{"engine": "postgres"}},
	}
	current := []interfaces.ResourceState{
		{Name: "db", Type: "infra.database", ConfigHash: "stale-hash"},
	}
	// newFakeProvider() returns a provider whose ResourceDriver yields
	// (nil, nil) — the driver-absent case.
	plan, err := platform.ComputePlan(context.Background(), newFakeProvider(), desired, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "update" {
		t.Errorf("expected single update action via ConfigHash fallback when driver is nil; got %+v", plan.Actions)
	}
}

// TestComputePlan_DriverDiffError_PropagatesAsError verifies that
// errors from driver.Diff abort the plan computation rather than being
// silently swallowed. Plan correctness depends on Diff dispatch
// completing for every existing resource; surfacing the error lets
// operators see the underlying cause (rate limit, network, etc.).
func TestComputePlan_DriverDiffError_PropagatesAsError(t *testing.T) {
	desired := []interfaces.ResourceSpec{{Name: "vpc", Type: "infra.vpc"}}
	current := []interfaces.ResourceState{{Name: "vpc", Type: "infra.vpc", ProviderID: "old"}}

	driver := &fakeDriver{diffErr: errSentinel}
	fp := &fakeProvider{driver: driver}

	_, err := platform.ComputePlan(context.Background(), fp, desired, current)
	if err == nil {
		t.Fatal("expected error from driver.Diff to propagate, got nil")
	}
}

// errSentinel is a package-private sentinel used by the Diff-error
// propagation test.
var errSentinel = errors.New("synthetic Diff failure")
