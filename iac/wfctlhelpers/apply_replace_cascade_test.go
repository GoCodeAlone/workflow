package wfctlhelpers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestApplyPlan_ReplaceCascade_DependentCreateGetsNewParentID is the
// canonical T5.3 scenario: a 2-action plan where action[0] is a Replace
// of "parent" (Delete + Create yielding a NEW ProviderID), and action[1]
// is a Create of "dependent" whose Config references ${parent.id}.
//
// The cascade contract (specified in W-5):
//
//  1. Action 0 (Replace parent): doReplace populates
//     result.ReplaceIDMap["parent"] = new-uuid via the post-Delete Create.
//  2. Action 1 (Create dependent): the loop's pre-dispatch
//     jitsubst.ResolveSpec call sees result.ReplaceIDMap["parent"] =
//     new-uuid and substitutes ${parent.id} → "new-uuid" before the
//     driver's Create receives the spec.
//
// The key assertion: dependent's Create call receives Config["vpc_ref"]
// = "new-uuid" (the post-Replace ProviderID), NOT the unresolved literal
// "${parent.id}". A regression in either T3.4's ReplaceIDMap population
// or T5.2's loop-level substitution causes this test to fail with the
// unresolved literal showing up in seenConfigs.
func TestApplyPlan_ReplaceCascade_DependentCreateGetsNewParentID(t *testing.T) {
	fp := newJITRecordingProvider()
	// Parent's Create (the post-Delete one) returns the freshly-minted
	// ProviderID that JIT substitution should propagate downstream.
	fp.driver.createReturns = map[string]*interfaces.ResourceOutput{
		"parent": {Name: "parent", Type: "infra.vpc", ProviderID: "vpc-new-uuid-after-replace"},
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{
			Action: "replace",
			Resource: interfaces.ResourceSpec{
				Name: "parent", Type: "infra.vpc",
				Config: map[string]any{"cidr": "10.0.0.0/16"},
			},
			Current: &interfaces.ResourceState{Name: "parent", ProviderID: "vpc-old-uuid"},
		},
		{
			Action: "create",
			Resource: specWithConfig("dependent", "infra.app", map[string]any{
				"vpc_ref": "${parent.id}",
			}),
		},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected per-action errors: %+v", result.Errors)
	}
	// ReplaceIDMap must record the new ProviderID for parent.
	if got := result.ReplaceIDMap["parent"]; got != "vpc-new-uuid-after-replace" {
		t.Errorf("ReplaceIDMap[parent]: got %q want %q (full map: %+v)",
			got, "vpc-new-uuid-after-replace", result.ReplaceIDMap)
	}
	// Dependent's Config must have been substituted before the driver
	// saw it. A failure here indicates JIT did not run for the dependent
	// action — either ReplaceIDMap wasn't populated yet or the loop
	// failed to call ResolveSpec.
	depConfig := fp.driver.seenConfigs["dependent"]
	if depConfig == nil {
		t.Fatalf("driver did not receive dependent's Config; seenConfigs=%+v", fp.driver.seenConfigs)
	}
	if got, want := depConfig["vpc_ref"], "vpc-new-uuid-after-replace"; got != want {
		t.Errorf("dependent.Config[vpc_ref]: got %q want %q", got, want)
	}
}

// TestApplyPlan_ReplaceCascade_DependentReplaceGetsNewParentID extends
// the cascade scenario to a Replace-on-Replace shape: the dependent is
// also being Replaced (e.g., it carries a forced field change). The
// post-Delete Create for the dependent must STILL see the freshly-
// resolved ${parent.id} — Delete uses the OLD ProviderID via
// action.Current (unchanged by JIT), Create uses the new resolved spec.
//
// This isolates the "doReplace's Create receives the JIT-resolved spec"
// contract: an early version that built ref-from-action AFTER Delete
// could conceivably stale-reference the un-resolved spec for Create.
func TestApplyPlan_ReplaceCascade_DependentReplaceGetsNewParentID(t *testing.T) {
	fp := newJITRecordingProvider()
	fp.driver.createReturns = map[string]*interfaces.ResourceOutput{
		"parent":    {Name: "parent", Type: "infra.vpc", ProviderID: "vpc-new-uuid"},
		"dependent": {Name: "dependent", Type: "infra.droplet", ProviderID: "droplet-new-uuid"},
	}
	plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{
		{
			Action: "replace",
			Resource: interfaces.ResourceSpec{
				Name: "parent", Type: "infra.vpc",
				Config: map[string]any{"cidr": "10.0.0.0/16"},
			},
			Current: &interfaces.ResourceState{Name: "parent", ProviderID: "vpc-old-uuid"},
		},
		{
			Action: "replace",
			Resource: specWithConfig("dependent", "infra.droplet", map[string]any{
				"vpc_ref": "${parent.id}",
			}),
			Current: &interfaces.ResourceState{Name: "dependent", ProviderID: "droplet-old-uuid"},
		},
	}}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected per-action errors: %+v", result.Errors)
	}
	depConfig := fp.driver.seenConfigs["dependent"]
	if depConfig == nil {
		t.Fatalf("driver did not receive dependent's Config; seenConfigs=%+v", fp.driver.seenConfigs)
	}
	if got, want := depConfig["vpc_ref"], "vpc-new-uuid"; got != want {
		t.Errorf("dependent.Config[vpc_ref] (cascade Replace): got %q want %q", got, want)
	}
	// Both Replaces should have populated ReplaceIDMap.
	if got := result.ReplaceIDMap["parent"]; got != "vpc-new-uuid" {
		t.Errorf("ReplaceIDMap[parent]: got %q", got)
	}
	if got := result.ReplaceIDMap["dependent"]; got != "droplet-new-uuid" {
		t.Errorf("ReplaceIDMap[dependent]: got %q", got)
	}
}
