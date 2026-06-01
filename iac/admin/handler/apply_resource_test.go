package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
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
	prov := &planningProvider{}
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
	if !errors.Is(err, handler.ErrAuthzDenied) {
		t.Fatalf("ApplyResource: want ErrAuthzDenied, got %v (out.Error=%s)", err, out.GetError())
	}
	if out.Error == "" {
		t.Error("ApplyResource with evidence.checked=false should return non-empty error")
	}
}

// TestApplyResource_AuthzDenies asserts that a subject the enforcer
// denies infra:apply → 403 even if the client body has valid evidence.
func TestApplyResource_AuthzDenies(t *testing.T) {
	prov := &planningProvider{}
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
	if !errors.Is(err, handler.ErrAuthzDenied) {
		t.Fatalf("ApplyResource: want ErrAuthzDenied, got %v (out.Error=%s)", err, out.GetError())
	}
	if out.Error == "" {
		t.Error("ApplyResource should reject subject denied infra:apply by server-side Enforcer")
	}
}

// TestApplyResource_HappyPath asserts that a valid evidence + hash + allowed
// subject returns applied[] with no errors.
func TestApplyResource_HappyPath(t *testing.T) {
	prov := &planningProvider{}
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
	prov := &planningProvider{}
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

// TestApplyResource_ReplaceWithoutAuthorization asserts Gate 4:
// a plan containing a replace action on a protected:true resource with an
// empty allow_replace list must be rejected before any cloud operation
// (IMPORTANT-2 fix).
func TestApplyResource_ReplaceWithoutAuthorization(t *testing.T) {
	// Build a stub provider that returns a plan with a replace action on a
	// protected resource — we need a provider whose Plan method returns a
	// pre-built plan rather than using the stub's dynamic one.
	prov := &replacePlanProvider{}
	providers := map[string]interfaces.IaCProvider{"stub": prov}

	// desiredSpecs must match the provider's plan output so the hash lines up.
	desired := []interfaces.ResourceSpec{
		{Name: "protected-db", Type: "infra.database", Config: map[string]any{"protected": true, "size": "xl"}},
	}

	// Get the desired_hash for these specs with no current state.
	planIn := &adminpb.AdminPlanInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	planOut, err := handler.PlanResource(context.Background(), nil, providers, nil, desired, planIn)
	if err != nil || planOut.Error != "" {
		t.Fatalf("PlanResource setup: %v / %s", err, planOut.Error)
	}

	in := &adminpb.AdminApplyInput{
		PlanId:       planOut.PlanId,
		DesiredHash:  planOut.DesiredHash,
		AllowReplace: nil, // explicitly empty — no replace authorization
		Evidence:     &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: true},
	}
	out, err := handler.ApplyResource(context.Background(), nil, providers, nil, "operator", nil, desired, in)
	if err != nil {
		t.Fatalf("ApplyResource: unexpected Go error: %v", err)
	}
	if out.Error == "" {
		t.Error("ApplyResource should reject replace on protected resource with empty allow_replace")
	}
	if len(out.Applied) > 0 {
		t.Error("ApplyResource: no resources should be applied when replace is unauthorized")
	}
}

// replacePlanProvider is a stub provider that always returns a single
// replace action on a protected resource, used to test Gate 4.
type replacePlanProvider struct{}

var _ interfaces.IaCProvider = (*replacePlanProvider)(nil)

func (p *replacePlanProvider) Name() string                                         { return "replace-stub" }
func (p *replacePlanProvider) Version() string                                      { return "0.1.0" }
func (p *replacePlanProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *replacePlanProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *replacePlanProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	// Always return a replace action on the first spec, marked as protected.
	if len(desired) == 0 {
		return &interfaces.IaCPlan{}, nil
	}
	spec := desired[0]
	if spec.Config == nil {
		spec.Config = map[string]any{}
	}
	spec.Config["protected"] = true
	return &interfaces.IaCPlan{
		Actions: []interfaces.PlanAction{
			{Action: "replace", Resource: spec},
		},
	}, nil
}
func (p *replacePlanProvider) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return &interfaces.DestroyResult{Destroyed: names}, nil
}
func (p *replacePlanProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *replacePlanProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *replacePlanProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *replacePlanProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (p *replacePlanProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (p *replacePlanProvider) SupportedCanonicalKeys() []string { return nil }
func (p *replacePlanProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *replacePlanProvider) Close() error { return nil }
