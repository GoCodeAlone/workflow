package mock

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

// Compile-time interface satisfaction checks.
var (
	_ platform.Provider         = (*MockProvider)(nil)
	_ platform.ResourceDriver   = (*MockResourceDriver)(nil)
	_ platform.CapabilityMapper = (*MockCapabilityMapper)(nil)
	_ platform.CredentialBroker = (*MockCredentialBroker)(nil)
	_ platform.StateStore       = (*MockStateStore)(nil)
	_ platform.LockHandle       = (*MockLockHandle)(nil)
)

// --- MockProvider tests ---

func TestMockProvider_Defaults(t *testing.T) {
	p := NewMockProvider()
	ctx := context.Background()

	if name := p.Name(); name != "mock" {
		t.Fatalf("expected Name() = %q, got %q", "mock", name)
	}
	if ver := p.Version(); ver != "0.0.0-mock" {
		t.Fatalf("expected Version() = %q, got %q", "0.0.0-mock", ver)
	}
	if err := p.Initialize(ctx, nil); err != nil {
		t.Fatalf("expected Initialize to return nil, got %v", err)
	}
	if caps := p.Capabilities(); caps != nil {
		t.Fatalf("expected Capabilities() = nil, got %v", caps)
	}
	plans, err := p.MapCapability(ctx, platform.CapabilityDeclaration{}, nil)
	if err != nil || plans != nil {
		t.Fatalf("expected MapCapability to return (nil, nil), got (%v, %v)", plans, err)
	}
	_, driverErr := p.ResourceDriver("anything")
	if driverErr == nil {
		t.Fatal("expected ResourceDriver to return error for unknown type")
	}
	var rdnf *platform.ResourceDriverNotFoundError
	if !errors.As(driverErr, &rdnf) {
		t.Fatalf("expected ResourceDriverNotFoundError, got %T", driverErr)
	}
	if cb := p.CredentialBroker(); cb != nil {
		t.Fatal("expected CredentialBroker() = nil")
	}
	if ss := p.StateStore(); ss != nil {
		t.Fatal("expected StateStore() = nil")
	}
	if err := p.Healthy(ctx); err != nil {
		t.Fatalf("expected Healthy to return nil, got %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("expected Close to return nil, got %v", err)
	}
}

func TestMockProvider_CustomFunctions(t *testing.T) {
	p := NewMockProvider()
	ctx := context.Background()

	p.NameFn = func() string { return "custom" }
	p.VersionFn = func() string { return "1.0.0" }
	p.HealthyFn = func(context.Context) error { return errors.New("unhealthy") }

	if name := p.Name(); name != "custom" {
		t.Fatalf("expected custom Name(), got %q", name)
	}
	if ver := p.Version(); ver != "1.0.0" {
		t.Fatalf("expected custom Version(), got %q", ver)
	}
	if err := p.Healthy(ctx); err == nil || err.Error() != "unhealthy" {
		t.Fatalf("expected custom Healthy error, got %v", err)
	}
}

func TestMockProvider_CallTracking(t *testing.T) {
	p := NewMockProvider()
	ctx := context.Background()

	p.Name()
	p.Version()
	_ = p.Initialize(ctx, map[string]any{"key": "val"})
	_ = p.Healthy(ctx)

	calls := p.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}
	expected := []string{"Name", "Version", "Initialize", "Healthy"}
	for i, name := range expected {
		if calls[i].Method != name {
			t.Errorf("call[%d]: expected method %q, got %q", i, name, calls[i].Method)
		}
	}
}

// --- MockResourceDriver tests ---

func TestMockResourceDriver_Defaults(t *testing.T) {
	d := NewMockResourceDriver("mock.compute")
	ctx := context.Background()

	if rt := d.ResourceType(); rt != "mock.compute" {
		t.Fatalf("expected ResourceType() = %q, got %q", "mock.compute", rt)
	}

	out, err := d.Create(ctx, "test-resource", map[string]any{"cpu": 2})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if out.Name != "test-resource" || out.Status != platform.ResourceStatusActive {
		t.Fatalf("unexpected Create output: %+v", out)
	}

	_, readErr := d.Read(ctx, "missing")
	var rnf *platform.ResourceNotFoundError
	if !errors.As(readErr, &rnf) {
		t.Fatalf("expected ResourceNotFoundError, got %T", readErr)
	}

	out, err = d.Update(ctx, "test-resource", map[string]any{"cpu": 2}, map[string]any{"cpu": 4})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if out.Properties["cpu"] != 4 {
		t.Fatalf("expected updated property cpu=4, got %v", out.Properties["cpu"])
	}

	if err := d.Delete(ctx, "test-resource"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	hs, err := d.HealthCheck(ctx, "test-resource")
	if err != nil {
		t.Fatalf("HealthCheck returned error: %v", err)
	}
	if hs.Status != "healthy" {
		t.Fatalf("expected healthy status, got %q", hs.Status)
	}

	_, scaleErr := d.Scale(ctx, "test-resource", nil)
	var ns *platform.NotScalableError
	if !errors.As(scaleErr, &ns) {
		t.Fatalf("expected NotScalableError, got %T", scaleErr)
	}

	diff, err := d.Diff(ctx, "test-resource", nil)
	if err != nil || diff != nil {
		t.Fatalf("expected Diff to return (nil, nil), got (%v, %v)", diff, err)
	}
}

func TestMockResourceDriver_CustomFunctions(t *testing.T) {
	d := NewMockResourceDriver("mock.compute")
	ctx := context.Background()

	d.CreateFn = func(_ context.Context, name string, _ map[string]any) (*platform.ResourceOutput, error) {
		return &platform.ResourceOutput{Name: name, Status: platform.ResourceStatusCreating}, nil
	}

	out, err := d.Create(ctx, "custom-res", nil)
	if err != nil {
		t.Fatalf("custom Create returned error: %v", err)
	}
	if out.Status != platform.ResourceStatusCreating {
		t.Fatalf("expected custom status, got %q", out.Status)
	}
}

func TestMockResourceDriver_CallTracking(t *testing.T) {
	d := NewMockResourceDriver("mock.compute")
	ctx := context.Background()

	d.ResourceType()
	_, _ = d.Create(ctx, "r1", nil)
	_, _ = d.Read(ctx, "r1")
	_ = d.Delete(ctx, "r1")

	calls := d.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}
	expected := []string{"ResourceType", "Create", "Read", "Delete"}
	for i, name := range expected {
		if calls[i].Method != name {
			t.Errorf("call[%d]: expected %q, got %q", i, name, calls[i].Method)
		}
	}
}

// --- MockCapabilityMapper tests ---

func TestMockCapabilityMapper_Defaults(t *testing.T) {
	cm := NewMockCapabilityMapper()

	if cm.CanMap("anything") {
		t.Fatal("expected CanMap to return false by default")
	}
	plans, err := cm.Map(platform.CapabilityDeclaration{}, nil)
	if err != nil || plans != nil {
		t.Fatalf("expected Map to return (nil, nil), got (%v, %v)", plans, err)
	}
	violations := cm.ValidateConstraints(platform.CapabilityDeclaration{}, nil)
	if violations != nil {
		t.Fatalf("expected ValidateConstraints to return nil, got %v", violations)
	}
}

func TestMockCapabilityMapper_CustomFunctions(t *testing.T) {
	cm := NewMockCapabilityMapper()
	cm.CanMapFn = func(ct string) bool { return ct == "database" }

	if !cm.CanMap("database") {
		t.Fatal("expected CanMap(database) = true")
	}
	if cm.CanMap("cache") {
		t.Fatal("expected CanMap(cache) = false")
	}
}

func TestMockCapabilityMapper_CallTracking(t *testing.T) {
	cm := NewMockCapabilityMapper()

	cm.CanMap("x")
	cm.Map(platform.CapabilityDeclaration{Name: "db"}, nil)
	cm.ValidateConstraints(platform.CapabilityDeclaration{}, nil)

	calls := cm.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
}

// --- MockCredentialBroker tests ---

func TestMockCredentialBroker_Defaults(t *testing.T) {
	cb := NewMockCredentialBroker()
	ctx := context.Background()
	pctx := &platform.PlatformContext{Org: "acme", Environment: "prod", Tier: platform.TierApplication}

	ref, err := cb.IssueCredential(ctx, pctx, platform.CredentialRequest{Name: "db-pass"})
	if err != nil {
		t.Fatalf("IssueCredential returned error: %v", err)
	}
	if ref.ID != "mock-cred-db-pass" {
		t.Fatalf("unexpected credential ID: %q", ref.ID)
	}
	if ref.ContextPath != "acme/prod" {
		t.Fatalf("unexpected context path: %q", ref.ContextPath)
	}

	if err := cb.RevokeCredential(ctx, ref); err != nil {
		t.Fatalf("RevokeCredential returned error: %v", err)
	}

	val, err := cb.ResolveCredential(ctx, ref)
	if err != nil || val != "mock-secret-value" {
		t.Fatalf("expected ResolveCredential = %q, got (%q, %v)", "mock-secret-value", val, err)
	}

	rotated, err := cb.RotateCredential(ctx, ref)
	if err != nil {
		t.Fatalf("RotateCredential returned error: %v", err)
	}
	if rotated.ID != ref.ID+"-rotated" {
		t.Fatalf("unexpected rotated ID: %q", rotated.ID)
	}

	creds, err := cb.ListCredentials(ctx, pctx)
	if err != nil || creds != nil {
		t.Fatalf("expected ListCredentials = (nil, nil), got (%v, %v)", creds, err)
	}
}

func TestMockCredentialBroker_CustomFunctions(t *testing.T) {
	cb := NewMockCredentialBroker()
	ctx := context.Background()

	cb.ResolveCredentialFn = func(_ context.Context, ref *platform.CredentialRef) (string, error) {
		return "super-secret-" + ref.Name, nil
	}

	val, err := cb.ResolveCredential(ctx, &platform.CredentialRef{Name: "api-key"})
	if err != nil || val != "super-secret-api-key" {
		t.Fatalf("expected custom resolve, got (%q, %v)", val, err)
	}
}

func TestMockCredentialBroker_CallTracking(t *testing.T) {
	cb := NewMockCredentialBroker()
	ctx := context.Background()
	pctx := &platform.PlatformContext{Org: "o", Environment: "e"}

	_, _ = cb.IssueCredential(ctx, pctx, platform.CredentialRequest{Name: "x"})
	_ = cb.RevokeCredential(ctx, &platform.CredentialRef{})
	_, _ = cb.ResolveCredential(ctx, &platform.CredentialRef{})

	calls := cb.GetCalls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls, got %d", len(calls))
	}
	expected := []string{"IssueCredential", "RevokeCredential", "ResolveCredential"}
	for i, name := range expected {
		if calls[i].Method != name {
			t.Errorf("call[%d]: expected %q, got %q", i, name, calls[i].Method)
		}
	}
}

// --- MockStateStore tests ---

func TestMockStateStore_InMemoryResources(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()
	ctxPath := "acme/prod/api"

	// Save a resource.
	res := &platform.ResourceOutput{
		Name:       "web-server",
		Type:       "container_runtime",
		Status:     platform.ResourceStatusActive,
		Properties: map[string]any{"replicas": 3},
		LastSynced: time.Now(),
	}
	if err := ss.SaveResource(ctx, ctxPath, res); err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}

	// Retrieve it.
	got, err := ss.GetResource(ctx, ctxPath, "web-server")
	if err != nil {
		t.Fatalf("GetResource failed: %v", err)
	}
	if got.Name != "web-server" || got.Properties["replicas"] != 3 {
		t.Fatalf("unexpected resource: %+v", got)
	}

	// List resources.
	list, err := ss.ListResources(ctx, ctxPath)
	if err != nil {
		t.Fatalf("ListResources failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(list))
	}

	// Resource not found.
	_, err = ss.GetResource(ctx, ctxPath, "missing")
	var rnf *platform.ResourceNotFoundError
	if !errors.As(err, &rnf) {
		t.Fatalf("expected ResourceNotFoundError, got %T", err)
	}

	// Delete and verify.
	if err := ss.DeleteResource(ctx, ctxPath, "web-server"); err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}
	list, err = ss.ListResources(ctx, ctxPath)
	if err != nil {
		t.Fatalf("ListResources after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 resources after delete, got %d", len(list))
	}
}

func TestMockStateStore_InMemoryPlans(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()
	ctxPath := "acme/staging"

	plan1 := &platform.Plan{ID: "plan-1", Context: ctxPath, Status: "pending", CreatedAt: time.Now()}
	plan2 := &platform.Plan{ID: "plan-2", Context: ctxPath, Status: "approved", CreatedAt: time.Now()}

	if err := ss.SavePlan(ctx, plan1); err != nil {
		t.Fatalf("SavePlan 1 failed: %v", err)
	}
	if err := ss.SavePlan(ctx, plan2); err != nil {
		t.Fatalf("SavePlan 2 failed: %v", err)
	}

	// Get by ID.
	got, err := ss.GetPlan(ctx, "plan-1")
	if err != nil || got.ID != "plan-1" {
		t.Fatalf("GetPlan failed: got (%v, %v)", got, err)
	}

	// Plan not found.
	_, err = ss.GetPlan(ctx, "missing")
	if err == nil {
		t.Fatal("expected error for missing plan")
	}

	// List plans (newest first).
	plans, err := ss.ListPlans(ctx, ctxPath, 10)
	if err != nil {
		t.Fatalf("ListPlans failed: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}
	if plans[0].ID != "plan-2" {
		t.Fatalf("expected newest plan first, got %q", plans[0].ID)
	}

	// List with limit.
	plans, err = ss.ListPlans(ctx, ctxPath, 1)
	if err != nil {
		t.Fatalf("ListPlans with limit failed: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan with limit, got %d", len(plans))
	}
}

func TestMockStateStore_LockingBehavior(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()
	ctxPath := "acme/prod"

	// Acquire lock.
	handle, err := ss.Lock(ctx, ctxPath, 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock failed: %v", err)
	}

	// Second lock on same path should fail.
	_, err = ss.Lock(ctx, ctxPath, 5*time.Minute)
	var lc *platform.LockConflictError
	if !errors.As(err, &lc) {
		t.Fatalf("expected LockConflictError, got %T: %v", err, err)
	}

	// Refresh should succeed.
	if err := handle.Refresh(ctx, 10*time.Minute); err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}

	// Unlock and re-acquire.
	if err := handle.Unlock(ctx); err != nil {
		t.Fatalf("Unlock failed: %v", err)
	}
	_, err = ss.Lock(ctx, ctxPath, 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock after unlock failed: %v", err)
	}
}

func TestMockStateStore_Dependencies(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()

	dep := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "vpc",
		TargetContext:  "acme/prod",
		TargetResource: "eks-cluster",
		Type:           "hard",
	}
	if err := ss.AddDependency(ctx, dep); err != nil {
		t.Fatalf("AddDependency failed: %v", err)
	}

	deps, err := ss.Dependencies(ctx, "acme/prod", "vpc")
	if err != nil {
		t.Fatalf("Dependencies failed: %v", err)
	}
	if len(deps) != 1 || deps[0].TargetResource != "eks-cluster" {
		t.Fatalf("unexpected dependencies: %+v", deps)
	}

	// No dependencies for unrelated resource.
	deps, err = ss.Dependencies(ctx, "acme/prod", "other")
	if err != nil {
		t.Fatalf("Dependencies for unrelated resource failed: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected 0 dependencies, got %d", len(deps))
	}
}

func TestMockStateStore_CustomFunctions(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()

	customErr := errors.New("custom save error")
	ss.SaveResourceFn = func(context.Context, string, *platform.ResourceOutput) error {
		return customErr
	}

	err := ss.SaveResource(ctx, "p", &platform.ResourceOutput{Name: "x"})
	if !errors.Is(err, customErr) {
		t.Fatalf("expected custom error, got %v", err)
	}
}

func TestMockStateStore_CallTracking(t *testing.T) {
	ss := NewMockStateStore()
	ctx := context.Background()

	_ = ss.SaveResource(ctx, "p", &platform.ResourceOutput{Name: "r"})
	_, _ = ss.GetResource(ctx, "p", "r")
	_, _ = ss.ListResources(ctx, "p")
	_ = ss.DeleteResource(ctx, "p", "r")

	calls := ss.GetCalls()
	if len(calls) != 4 {
		t.Fatalf("expected 4 calls, got %d", len(calls))
	}
	expected := []string{"SaveResource", "GetResource", "ListResources", "DeleteResource"}
	for i, name := range expected {
		if calls[i].Method != name {
			t.Errorf("call[%d]: expected %q, got %q", i, name, calls[i].Method)
		}
	}
}
