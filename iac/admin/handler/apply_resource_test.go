package handler_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// stubEnforcer is a minimal handler.Enforcer for apply tests.
type testEnforcer struct {
	allow map[string]bool
}

func (e *testEnforcer) Enforce(sub, obj, act string, _ ...string) (bool, error) {
	if e.allow == nil {
		return true, nil // default: allow all
	}
	return e.allow[sub+":"+obj], nil
}

// TestApplyResource_DefaultDeny asserts that evidence with checked=false
// returns a non-empty error (default-deny).
func TestApplyResource_DefaultDeny(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc"},
	}
	// Get a valid plan first.
	planOut, _ := handler.PlanResource(context.Background(), nil, providers, nil, desired,
		&adminpb.AdminPlanInput{Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true}})

	in := &adminpb.AdminApplyInput{
		PlanId:      planOut.PlanId,
		DesiredHash: planOut.DesiredHash,
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: false},
	}
	out, err := handler.ApplyResource(context.Background(), nil, providers, nil, "subject", nil, desired, in)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("ApplyResource with evidence.checked=false should return non-empty error")
	}
}

// TestApplyResource_AuthzDenies asserts that a subject the enforcer
// denies infra:apply → 403 even if the client body has valid evidence.
func TestApplyResource_AuthzDenies(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{{Name: "vpc1", Type: "infra.vpc"}}

	planOut, _ := handler.PlanResource(context.Background(), nil, providers, nil, desired,
		&adminpb.AdminPlanInput{Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true}})

	enforcer := &testEnforcer{allow: map[string]bool{
		// "viewer" is NOT granted infra:apply
	}}
	in := &adminpb.AdminApplyInput{
		PlanId:      planOut.PlanId,
		DesiredHash: planOut.DesiredHash,
		Evidence: &adminpb.AdminAuthzEvidence{
			AuthzChecked: true,
			AuthzAllowed: true,
			// client claims granted_permissions:infra:apply — IGNORED by server
		},
	}
	out, err := handler.ApplyResource(context.Background(), nil, providers, enforcer, "viewer", nil, desired, in)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("ApplyResource should reject subject denied infra:apply by server-side Enforcer")
	}
}

// TestApplyResource_HappyPath asserts that a valid evidence + hash + allowed
// subject returns applied[] with no errors.
func TestApplyResource_HappyPath(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
	}

	planOut, err := handler.PlanResource(context.Background(), nil, providers, nil, desired,
		&adminpb.AdminPlanInput{Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true}})
	if err != nil || planOut.Error != "" {
		t.Fatalf("PlanResource: %v / %s", err, planOut.Error)
	}

	in := &adminpb.AdminApplyInput{
		PlanId:      planOut.PlanId,
		DesiredHash: planOut.DesiredHash,
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.ApplyResource(context.Background(), nil, providers, nil, "operator", nil, desired, in)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected Go error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("ApplyResource: output error: %s", out.Error)
	}
	if len(out.Errors) != 0 {
		t.Errorf("ApplyResource: expected no per-resource errors, got: %v", out.Errors)
	}
}

// TestApplyResource_StalePlanHash asserts that a mismatched desired_hash
// → "plan is stale" error and no apply.
func TestApplyResource_StalePlanHash(t *testing.T) {
	prov := stubprovider.New()
	providers := map[string]interfaces.IaCProvider{"stub": prov}
	desired := []interfaces.ResourceSpec{{Name: "vpc1", Type: "infra.vpc"}}

	planOut, _ := handler.PlanResource(context.Background(), nil, providers, nil, desired,
		&adminpb.AdminPlanInput{Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true}})

	in := &adminpb.AdminApplyInput{
		PlanId:      planOut.PlanId,
		DesiredHash: "stale-hash-does-not-match",
		Evidence:    &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.ApplyResource(context.Background(), nil, providers, nil, "operator", nil, desired, in)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("ApplyResource with stale hash should return non-empty error")
	}
	if out.Error != "apply: plan is stale (desired_hash mismatch)" {
		t.Errorf("stale hash error = %q, want exact literal", out.Error)
	}
}
