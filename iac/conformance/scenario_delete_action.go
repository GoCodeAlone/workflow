package conformance

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioDeleteActionInApplyInvokesDriverDelete asserts the v2 IaC
// dispatch contract for delete actions: a plan whose only action is
// `delete` MUST flow through wfctlhelpers.ApplyPlan to the matching
// ResourceDriver.Delete — not be silently dropped by a missing case-arm
// in the provider's dispatcher.
//
// This pins the latent-bug-fix from W-3a T3.3 — pre-T3.3, DOProvider's
// switch had no `case "delete":` arm, so wfctl's state-prune action
// silently skipped cloud-resource deletion. The v2 path
// (wfctlhelpers.ApplyPlan → dispatchAction → doDelete) closes that gap
// by always routing delete actions to the driver.
//
// Portable invariants asserted by the scenario body (any compliant
// provider must satisfy regardless of cloud target):
//
//  1. ApplyPlan returns no top-level error (success path).
//  2. result.Errors contains no entry for the delete action — neither
//     a default-case "unknown action" diagnostic nor a driver-side
//     failure surfaces. A provider whose dispatcher silently dropped
//     delete (the documented bug class) would produce result.Errors
//     with the canonical "unknown action \"delete\"" diagnostic.
//  3. result.Resources is empty — doDelete does NOT append to
//     result.Resources (a deleted resource has no successor output to
//     record), distinguishing it from doCreate / doUpdate / doReplace.
//
// The "driver.Delete actually invoked" invariant from the scenario name
// is observed in the in-tree self-test
// (TestScenario_DeleteActionInApplyInvokesDriverDelete) via
// iactest.NoopDriver.DeleteCallCount — the scenario body itself can't
// portably introspect a counter on an arbitrary provider's driver. For
// real providers the equivalent observation is the cloud resource
// being gone after apply (Read returns 404); that path is covered by
// the smoke gate (T7.13).
//
// Smoke=false (per design table — non-smoke scenarios run only when a
// caller opts in to the full suite). RequiresCloud=true gates real
// provider plugins on cfg.LiveCloud — the in-tree self-test invokes
// the body directly so the cloud-only filter does not skip it.
func scenarioDeleteActionInApplyInvokesDriverDelete(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	const knownID = "conformance-delete-id"
	cur := &interfaces.ResourceState{
		Name:       "old-vpc",
		Type:       "infra.vpc",
		ProviderID: knownID,
	}
	plan := &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{
				Action: "delete",
				Resource: interfaces.ResourceSpec{
					Name: "old-vpc",
					Type: "infra.vpc",
				},
				Current: cur,
			},
		},
	}

	result, err := wfctlhelpers.ApplyPlan(context.Background(), p, plan)
	if err != nil {
		t.Fatalf("ApplyPlan returned top-level error: %v", err)
	}
	if result == nil {
		t.Fatal("ApplyPlan returned nil result")
		return
	}
	if len(result.Errors) != 0 {
		// Most likely failure mode: provider's dispatch lacks a
		// `case "delete":` arm, so dispatchAction's default returns
		// `unknown action "delete"` and that surfaces as ActionError.
		t.Errorf("expected no per-action errors for delete action; got %d: %+v (provider's dispatch may be silently skipping delete — see T3.3)", len(result.Errors), result.Errors)
	}
	if len(result.Resources) != 0 {
		t.Errorf("doDelete must NOT append to result.Resources (a deleted resource has no successor output); got %d entries: %+v", len(result.Resources), result.Resources)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_DeleteActionInApplyInvokesDriverDelete",
		Smoke:         false,
		RequiresCloud: true,
		Run:           scenarioDeleteActionInApplyInvokesDriverDelete,
	})
}
