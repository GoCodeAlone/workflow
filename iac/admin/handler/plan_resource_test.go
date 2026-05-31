package handler_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestPlanResource_DefaultDeny asserts that evidence with checked=false
// returns a non-empty error and no plan payload.
func TestPlanResource_DefaultDeny(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc"},
	}
	in := &adminpb.AdminPlanInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: false},
	}
	out, err := handler.PlanResource(context.Background(), nil, providers, nil, desired, in)
	if err != nil {
		t.Fatalf("PlanResource: unexpected error: %v", err)
	}
	if out.Error == "" {
		t.Error("PlanResource with evidence.checked=false should return non-empty error")
	}
	if out.PlanId != "" || len(out.Actions) > 0 {
		t.Error("PlanResource with denial should return no plan payload")
	}
}

// TestPlanResource_ReturnsActions asserts that a valid evidence returns
// a plan_id, non-empty desired_hash, and at least one action.
func TestPlanResource_ReturnsActions(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
	}
	in := &adminpb.AdminPlanInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.PlanResource(context.Background(), nil, providers, nil, desired, in)
	if err != nil {
		t.Fatalf("PlanResource: unexpected error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("PlanResource: unexpected error in output: %s", out.Error)
	}
	if out.PlanId == "" {
		t.Error("PlanResource: plan_id should be non-empty")
	}
	if out.DesiredHash == "" {
		t.Error("PlanResource: desired_hash should be non-empty")
	}
	if len(out.Actions) == 0 {
		t.Error("PlanResource: actions list should be non-empty for 1-spec desired set with no current state")
	}
	if out.Actions[0].ActionType != "create" {
		t.Errorf("PlanResource: expected action_type 'create', got %q", out.Actions[0].ActionType)
	}
}

// TestPlanResource_NoProvidersError asserts that calling PlanResource with
// an empty providers map returns an error indicating no provider is available.
func TestPlanResource_NoProvidersError(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc"},
	}
	in := &adminpb.AdminPlanInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.PlanResource(context.Background(), nil, nil, nil, desired, in)
	if err != nil {
		t.Fatalf("PlanResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("PlanResource with no providers should return non-empty error")
	}
}

// TestPlanResource_WithCurrentState asserts that existing state is reflected
// in the plan (create vs update).
func TestPlanResource_WithCurrentState(t *testing.T) {
	store := &fakeStateStore{
		resources: []interfaces.ResourceState{
			{Name: "vpc1", Type: "infra.vpc", ProviderID: "do-vpc-111"},
		},
	}
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc"},     // already exists → update
		{Name: "db1", Type: "infra.database"}, // new → create
	}
	in := &adminpb.AdminPlanInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.PlanResource(context.Background(), store, providers, nil, desired, in)
	if err != nil {
		t.Fatalf("PlanResource: unexpected error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("PlanResource: output error: %s", out.Error)
	}
	if len(out.Actions) != 2 {
		t.Fatalf("expected 2 actions (1 update + 1 create), got %d", len(out.Actions))
	}
	// Pin the create/update distinction per spec (FAIL-16 fix).
	actionByName := map[string]string{}
	for _, a := range out.Actions {
		actionByName[a.ResourceName] = a.ActionType
	}
	if actionByName["vpc1"] != "update" {
		t.Errorf("vpc1 (existing) should be 'update', got %q", actionByName["vpc1"])
	}
	if actionByName["db1"] != "create" {
		t.Errorf("db1 (new) should be 'create', got %q", actionByName["db1"])
	}
}
