// Package stubprovider_test exercises the stub IaCProvider used by
// scenario 92 and integration tests. The stub must be loadable without
// any external plugin subprocess — it runs entirely in-process.
package stubprovider_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestStub_InterfaceConformance asserts that stubprovider.New() satisfies
// interfaces.IaCProvider at compile time (the var _ line in the package
// guards this, but an explicit test surfaces a clear failure message when
// the interface drifts).
func TestStub_InterfaceConformance(t *testing.T) {
	var _ interfaces.IaCProvider = stubprovider.New()
}

// TestStub_Plan_CreateAction asserts that Plan on a 1-spec desired set
// with no current state returns a plan with 1 "create" action.
func TestStub_Plan_CreateAction(t *testing.T) {
	p := stubprovider.New()
	desired := []interfaces.ResourceSpec{
		{Name: "my-vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
	}
	plan, err := p.Plan(context.Background(), desired, nil)
	if err != nil {
		t.Fatalf("Plan: unexpected error: %v", err)
	}
	if plan == nil {
		t.Fatal("Plan: returned nil plan")
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("Plan: expected 1 action, got %d", len(plan.Actions))
	}
	if plan.Actions[0].Action != "create" {
		t.Errorf("Plan: expected action 'create', got %q", plan.Actions[0].Action)
	}
	if plan.Actions[0].Resource.Name != "my-vpc" {
		t.Errorf("Plan: expected resource name 'my-vpc', got %q", plan.Actions[0].Resource.Name)
	}
}

// TestStub_Apply_NoErrors asserts that driving ApplyPlanWithHooks with the
// stub provider on a plan with a create action returns an ApplyResult with
// no errors.
func TestStub_Apply_NoErrors(t *testing.T) {
	p := stubprovider.New()
	plan := &interfaces.IaCPlan{
		ID: "test-plan",
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: interfaces.ResourceSpec{Name: "my-vpc", Type: "infra.vpc"}},
		},
	}
	result, err := wfctlhelpers.ApplyPlanWithHooks(context.Background(), p, plan, wfctlhelpers.ApplyPlanHooks{})
	if err != nil {
		t.Fatalf("ApplyPlanWithHooks: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("ApplyPlanWithHooks: returned nil result")
	}
	if len(result.Errors) != 0 {
		t.Errorf("ApplyPlanWithHooks: expected no errors, got: %v", result.Errors)
	}
}

// TestStub_Destroy_ReturnsRefs asserts that Destroy returns the refs as
// Destroyed names.
func TestStub_Destroy_ReturnsRefs(t *testing.T) {
	p := stubprovider.New()
	refs := []interfaces.ResourceRef{
		{Name: "my-vpc", Type: "infra.vpc", ProviderID: "do-vpc-123"},
		{Name: "my-db", Type: "infra.database", ProviderID: "do-db-456"},
	}
	result, err := p.Destroy(context.Background(), refs)
	if err != nil {
		t.Fatalf("Destroy: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Destroy: returned nil result")
	}
	if len(result.Destroyed) != 2 {
		t.Fatalf("Destroy: expected 2 destroyed, got %d", len(result.Destroyed))
	}
	names := map[string]bool{}
	for _, n := range result.Destroyed {
		names[n] = true
	}
	if !names["my-vpc"] || !names["my-db"] {
		t.Errorf("Destroy: expected 'my-vpc' and 'my-db' in destroyed, got %v", result.Destroyed)
	}
}

// TestStub_DetectDrift_NotDrifted asserts that DetectDrift returns results
// with Drifted:false for all refs.
func TestStub_DetectDrift_NotDrifted(t *testing.T) {
	p := stubprovider.New()
	refs := []interfaces.ResourceRef{
		{Name: "my-vpc", Type: "infra.vpc"},
		{Name: "my-db", Type: "infra.database"},
	}
	results, err := p.DetectDrift(context.Background(), refs)
	if err != nil {
		t.Fatalf("DetectDrift: unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("DetectDrift: expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Drifted {
			t.Errorf("DetectDrift: expected Drifted:false for %q, got true", r.Name)
		}
	}
}
