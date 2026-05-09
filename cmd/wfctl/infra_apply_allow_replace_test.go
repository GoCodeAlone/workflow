package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// W-6 / T6.1: --allow-replace flag on apply.
//
// Without the flag, a Replace (or Delete) action targeting a resource
// annotated `protected: true` must error before dispatch. With the flag
// listing the resource name, the protection is bypassed and the apply
// proceeds to the provider dispatch step.

// applyAllowReplaceSet is the per-invocation allow-list of resource
// names whose protected: true status is overridden for this apply.
// Set by runInfraApply from --allow-replace=<comma-separated>; reset
// to nil at the top of every invocation. Tests set it directly to
// drive the gate.
//
// Read-only inside the apply path (validateAllowReplaceProtected); the
// pkg-level pattern matches computeInfraPlan / applyV2ApplyPlanFn.
//
// The test interface promises:
//   - validateAllowReplaceProtected(plan, allow) returns nil when no
//     replace/delete action targets a protected resource.
//   - returns an error matching the design literal when a protected
//     resource is replaced/deleted without being listed in `allow`.

// TestValidateAllowReplaceProtected_NoProtectedActions_NoError verifies
// the gate is a no-op when the plan contains no replace/delete actions
// on protected resources.
func TestValidateAllowReplaceProtected_NoProtectedActions_NoError(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "vpc-1", Type: "infra.vpc"}},
			{Action: "update", Resource: interfaces.ResourceSpec{Name: "vpc-2", Type: "infra.vpc", Config: map[string]any{"protected": true}}},
		},
	}
	if err := validateAllowReplaceProtected(plan, nil); err != nil {
		t.Fatalf("expected no error for non-replace/delete actions on protected: got %v", err)
	}
}

// TestValidateAllowReplaceProtected_ReplaceProtected_WithoutAllowList_Errors
// is the canonical T6.1 spec case. The replace action targets a
// protected resource and the allow-list is empty — the gate must
// return an error mentioning the resource name and the override flag.
func TestValidateAllowReplaceProtected_ReplaceProtected_WithoutAllowList_Errors(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-db",
					Type:   "infra.database",
					Config: map[string]any{"protected": true},
				},
			},
		},
	}
	err := validateAllowReplaceProtected(plan, nil)
	if err == nil {
		t.Fatal("expected error for replace on protected resource without --allow-replace")
	}
	// T6.2 superseded T6.1's single-line literal with an aggregated
	// multi-blocker format that always emits a copy-paste flag value.
	// Assert the operator-facing essentials: resource name surfaces +
	// copy-paste flag is pre-formatted with the blocked name.
	msg := err.Error()
	if !strings.Contains(msg, "prod-db") {
		t.Errorf("expected error to mention resource name; got %q", msg)
	}
	if !strings.Contains(msg, "--allow-replace=prod-db") {
		t.Errorf("expected error to include copy-paste flag --allow-replace=prod-db; got %q", msg)
	}
}

// TestValidateAllowReplaceProtected_DeleteProtected_WithoutAllowList_Errors
// covers the design's "and would be %sd" for delete actions. ComputePlan
// emits delete actions with empty Resource.Config; the protected flag is
// recovered from Current.AppliedConfig.
func TestValidateAllowReplaceProtected_DeleteProtected_WithoutAllowList_Errors(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "delete",
				Resource: interfaces.ResourceSpec{
					Name: "prod-db",
					Type: "infra.database",
				},
				Current: &interfaces.ResourceState{
					Name:          "prod-db",
					Type:          "infra.database",
					AppliedConfig: map[string]any{"protected": true},
				},
			},
		},
	}
	err := validateAllowReplaceProtected(plan, nil)
	if err == nil {
		t.Fatal("expected error for delete on protected resource without --allow-replace")
	}
	// T6.2 batch format: assert the essentials — name surfaces + the
	// copy-paste flag is pre-formatted with the blocked name. The
	// "delete" verb is preserved in the per-line listing (covered by
	// the mixed-replace-and-delete test in T6.2 set).
	msg := err.Error()
	if !strings.Contains(msg, "prod-db") {
		t.Errorf("expected error to mention resource name; got %q", msg)
	}
	if !strings.Contains(msg, "--allow-replace=prod-db") {
		t.Errorf("expected error to include copy-paste flag --allow-replace=prod-db; got %q", msg)
	}
}

// TestValidateAllowReplaceProtected_ReplaceProtected_InAllowList_Allowed
// is the override path: when the resource name is in the allow set the
// gate returns nil so the apply can proceed.
func TestValidateAllowReplaceProtected_ReplaceProtected_InAllowList_Allowed(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-db",
					Type:   "infra.database",
					Config: map[string]any{"protected": true},
				},
			},
		},
	}
	allow := map[string]struct{}{"prod-db": {}}
	if err := validateAllowReplaceProtected(plan, allow); err != nil {
		t.Errorf("expected gate to pass when resource is in allow list: got %v", err)
	}
}

// TestValidateAllowReplaceProtected_NonProtectedReplace_NoError verifies
// replace on a non-protected resource is unaffected by the gate.
func TestValidateAllowReplaceProtected_NonProtectedReplace_NoError(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action:   "replace",
				Resource: interfaces.ResourceSpec{Name: "dev-vpc", Type: "infra.vpc"},
			},
		},
	}
	if err := validateAllowReplaceProtected(plan, nil); err != nil {
		t.Errorf("expected no error for non-protected replace: got %v", err)
	}
}

// TestParseAllowReplaceFlag_EmptyAndCommaSeparated covers the parse
// helper that turns the --allow-replace=<csv> flag value into a set.
// Empty input → empty set (or nil); comma-separated names → set with
// each name. Whitespace around names is trimmed so operators copy-paste
// from plan output without surprises.
func TestParseAllowReplaceFlag_EmptyAndCommaSeparated(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		notWant []string
	}{
		{name: "empty", raw: "", notWant: []string{"any"}},
		{name: "single", raw: "vpc-1", want: []string{"vpc-1"}, notWant: []string{"vpc-2"}},
		{name: "csv", raw: "vpc-1,vpc-2,db-1", want: []string{"vpc-1", "vpc-2", "db-1"}, notWant: []string{"vpc-3"}},
		{name: "trim-spaces", raw: " vpc-1 , vpc-2 ", want: []string{"vpc-1", "vpc-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			set := parseAllowReplaceFlag(tc.raw)
			for _, name := range tc.want {
				if _, ok := set[name]; !ok {
					t.Errorf("expected %q in set, got %v", name, set)
				}
			}
			for _, name := range tc.notWant {
				if _, ok := set[name]; ok {
					t.Errorf("did not expect %q in set, got %v", name, set)
				}
			}
		})
	}
}

// TestApplyWithProviderAndStore_ProtectedReplace_WithoutAllowReplace_Errors
// integration-checks that the gate fires inside the live-diff apply
// path (applyWithProviderAndStore). The fake ComputePlan emits a
// replace on a protected resource; with no allow set, the apply
// returns the expected error before dispatching to the provider.
func TestApplyWithProviderAndStore_ProtectedReplace_WithoutAllowReplace_Errors(t *testing.T) {
	provider := &iactest.NoopProvider{ProviderName: "allow-replace-stub"}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, 0, len(specs))
		for _, s := range specs {
			actions = append(actions, interfaces.PlanAction{Action: "replace", Resource: s})
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	// Reset package-level allow set so test order doesn't matter.
	origAllow := applyAllowReplaceSet
	applyAllowReplaceSet = nil
	t.Cleanup(func() { applyAllowReplaceSet = origAllow })

	specs := []interfaces.ResourceSpec{
		{Name: "prod-db", Type: "infra.database", Config: map[string]any{"protected": true}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), provider, "stub", specs, nil, nil, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected gate error before dispatch")
	}
	if !strings.Contains(err.Error(), "prod-db") || !strings.Contains(err.Error(), "--allow-replace=prod-db") {
		t.Errorf("error missing expected fragments: %v", err)
	}
}

// TestApplyWithProviderAndStore_ProtectedReplace_WithAllowReplace_Proceeds
// confirms the override path: with applyAllowReplaceSet listing the
// protected resource, the gate is bypassed and the apply continues
// past the gate. We route through the v2 dispatch (sentinel return
// via applyV2ApplyPlanFn) so a sentinel error proves the call site
// reached dispatch — i.e. the gate did not short-circuit.
func TestApplyWithProviderAndStore_ProtectedReplace_WithAllowReplace_Proceeds(t *testing.T) {
	provider := &iactest.NoopProvider{ProviderName: "allow-replace-stub", DispatchVersion: "v2"}

	origCompute := computeInfraPlan
	computeInfraPlan = func(_ context.Context, _ interfaces.IaCProvider, specs []interfaces.ResourceSpec, _ []interfaces.ResourceState) (interfaces.IaCPlan, error) {
		actions := make([]interfaces.PlanAction, 0, len(specs))
		for _, s := range specs {
			actions = append(actions, interfaces.PlanAction{Action: "replace", Resource: s})
		}
		return interfaces.IaCPlan{Actions: actions}, nil
	}
	t.Cleanup(func() { computeInfraPlan = origCompute })

	dispatched := errors.New("v2 ApplyPlan reached")
	origApply := applyV2ApplyPlanFn
	applyV2ApplyPlanFn = func(_ context.Context, _ interfaces.IaCProvider, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
		return nil, dispatched
	}
	t.Cleanup(func() { applyV2ApplyPlanFn = origApply })

	origAllow := applyAllowReplaceSet
	applyAllowReplaceSet = map[string]struct{}{"prod-db": {}}
	t.Cleanup(func() { applyAllowReplaceSet = origAllow })

	specs := []interfaces.ResourceSpec{
		{Name: "prod-db", Type: "infra.database", Config: map[string]any{"protected": true}},
	}

	var w bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), provider, "stub", specs, nil, nil, &w, "test", "", nil)
	if err == nil || !errors.Is(err, dispatched) {
		t.Fatalf("expected gate to allow apply through to dispatch (sentinel %v); got %v", dispatched, err)
	}
}

// TestApplyPrecomputedPlanWithStore_ProtectedReplace_WithoutAllowReplace_Errors
// verifies the gate also fires through the --plan path
// (applyPrecomputedPlanWithStore). The protected guarantee must hold
// regardless of whether the operator runs apply or apply --plan.
func TestApplyPrecomputedPlanWithStore_ProtectedReplace_WithoutAllowReplace_Errors(t *testing.T) {
	provider := &iactest.NoopProvider{ProviderName: "allow-replace-stub"}

	origAllow := applyAllowReplaceSet
	applyAllowReplaceSet = nil
	t.Cleanup(func() { applyAllowReplaceSet = origAllow })

	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-db",
					Type:   "infra.database",
					Config: map[string]any{"protected": true},
				},
			},
		},
	}

	var w bytes.Buffer
	err := applyPrecomputedPlanWithStore(context.Background(), plan, provider, "stub", nil, &w, "test", "", nil)
	if err == nil {
		t.Fatal("expected gate error before dispatch via precomputed-plan path")
	}
	if !strings.Contains(err.Error(), "prod-db") || !strings.Contains(err.Error(), "--allow-replace=prod-db") {
		t.Errorf("precomputed-plan gate error missing expected fragments: %v", err)
	}
}
