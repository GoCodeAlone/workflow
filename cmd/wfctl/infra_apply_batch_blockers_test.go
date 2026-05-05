package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// W-6 / T6.2: validateAllowReplaceProtected emits ALL blockers in one
// pass, with a copy-paste-ready --allow-replace=<csv> flag value, so
// operators don't have to discover blockers one by one across N apply
// runs.
//
// Per plan T6.2:
//   "Plan with 5 protected resources requiring replace → error message
//    lists ALL 5 + pre-formatted --allow-replace=name1,name2,name3,
//    name4,name5 for copy-paste."

// TestValidateAllowReplaceProtected_FiveProtected_ReportsAll verifies
// the canonical plan-spec example: 5 protected replaces, none in the
// allow list, error names every resource AND emits the full
// copy-paste flag value with the names in plan-action order.
func TestValidateAllowReplaceProtected_FiveProtected_ReportsAll(t *testing.T) {
	names := []string{"vpc-1", "vpc-2", "db-1", "cache-1", "redis-1"}
	actions := make([]interfaces.PlanAction, 0, len(names))
	for _, n := range names {
		actions = append(actions, interfaces.PlanAction{
			Action: "replace",
			Resource: interfaces.ResourceSpec{
				Name:   n,
				Type:   "infra.vpc",
				Config: map[string]any{"protected": true},
			},
		})
	}
	plan := interfaces.IaCPlan{Actions: actions}

	err := validateAllowReplaceProtected(plan, nil)
	if err == nil {
		t.Fatal("expected error reporting all 5 protected blockers")
	}
	msg := err.Error()
	for _, n := range names {
		if !strings.Contains(msg, n) {
			t.Errorf("error message missing blocker %q\n  got: %s", n, msg)
		}
	}
	wantFlag := "--allow-replace=vpc-1,vpc-2,db-1,cache-1,redis-1"
	if !strings.Contains(msg, wantFlag) {
		t.Errorf("error message missing copy-paste flag value %q\n  got: %s", wantFlag, msg)
	}
}

// TestValidateAllowReplaceProtected_PartialAllowList_ReportsOnlyRemainder
// proves the allow-set is honored at batch-aggregation time: with
// some names allowed, the error lists ONLY the still-blocked names
// and the copy-paste flag includes only those.
func TestValidateAllowReplaceProtected_PartialAllowList_ReportsOnlyRemainder(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "replace", Resource: interfaces.ResourceSpec{Name: "vpc-1", Type: "infra.vpc", Config: map[string]any{"protected": true}}},
			{Action: "replace", Resource: interfaces.ResourceSpec{Name: "vpc-2", Type: "infra.vpc", Config: map[string]any{"protected": true}}},
			{Action: "replace", Resource: interfaces.ResourceSpec{Name: "db-1", Type: "infra.database", Config: map[string]any{"protected": true}}},
		},
	}
	allow := map[string]struct{}{"vpc-1": {}}

	err := validateAllowReplaceProtected(plan, allow)
	if err == nil {
		t.Fatal("expected error for the still-blocked resources")
	}
	msg := err.Error()
	if strings.Contains(msg, "vpc-1") {
		t.Errorf("error should not mention already-allowed resource vpc-1\n  got: %s", msg)
	}
	for _, n := range []string{"vpc-2", "db-1"} {
		if !strings.Contains(msg, n) {
			t.Errorf("error missing still-blocked resource %q\n  got: %s", n, msg)
		}
	}
	if !strings.Contains(msg, "--allow-replace=vpc-2,db-1") {
		t.Errorf("error missing copy-paste flag for remainder %q\n  got: %s", "--allow-replace=vpc-2,db-1", msg)
	}
}

// TestValidateAllowReplaceProtected_MixedReplaceAndDelete_ReportsBoth
// covers T6.1's gate scope: replace AND delete actions both contribute
// blockers when their resource is protected. The aggregated error must
// surface both, in plan-action order, and the copy-paste flag must
// list both names.
func TestValidateAllowReplaceProtected_MixedReplaceAndDelete_ReportsBoth(t *testing.T) {
	plan := interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "replace",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-vpc",
					Type:   "infra.vpc",
					Config: map[string]any{"protected": true},
				},
			},
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
		t.Fatal("expected error reporting both replace and delete blockers")
	}
	msg := err.Error()
	for _, n := range []string{"prod-vpc", "prod-db"} {
		if !strings.Contains(msg, n) {
			t.Errorf("error missing %q\n  got: %s", n, msg)
		}
	}
	if !strings.Contains(msg, "--allow-replace=prod-vpc,prod-db") {
		t.Errorf("error missing copy-paste flag with both names\n  got: %s", msg)
	}
}

// TestValidateAllowReplaceProtected_SingleBlocker_StillBatchFormat
// guards regression: a single blocker should still emit the
// aggregated batch format (not the legacy single-line format), so the
// operator-facing UX is consistent regardless of blocker count.
// The copy-paste flag is the most important artifact for automation.
func TestValidateAllowReplaceProtected_SingleBlocker_StillBatchFormat(t *testing.T) {
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
		t.Fatal("expected error for single protected blocker")
	}
	msg := err.Error()
	if !strings.Contains(msg, "prod-db") {
		t.Errorf("error missing resource name: %s", msg)
	}
	if !strings.Contains(msg, "--allow-replace=prod-db") {
		t.Errorf("error missing copy-paste flag: %s", msg)
	}
}

// TestValidateAllowReplaceProtected_OrderPreservesPlanActionOrder
// pins the deterministic ordering of names in both the listing and
// the copy-paste flag value: plan-action declaration order. Stable
// ordering matters for diff-stable test golden files and for
// operator coordination ("the order I see in plan output is the
// order I'll see in the error").
func TestValidateAllowReplaceProtected_OrderPreservesPlanActionOrder(t *testing.T) {
	// Intentionally non-alphabetic order so the test fails if the
	// implementation sorts names instead of preserving order.
	planOrder := []string{"zeta-vpc", "alpha-db", "mu-cache"}
	actions := make([]interfaces.PlanAction, 0, len(planOrder))
	for _, n := range planOrder {
		actions = append(actions, interfaces.PlanAction{
			Action: "replace",
			Resource: interfaces.ResourceSpec{
				Name:   n,
				Type:   "infra.vpc",
				Config: map[string]any{"protected": true},
			},
		})
	}
	err := validateAllowReplaceProtected(interfaces.IaCPlan{Actions: actions}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	want := "--allow-replace=zeta-vpc,alpha-db,mu-cache"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("expected plan-action-ordered csv %q\n  got: %s", want, err.Error())
	}
}
