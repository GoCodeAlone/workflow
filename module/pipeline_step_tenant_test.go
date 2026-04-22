package module

import (
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeTenantRegistry is a test double for interfaces.TenantRegistry.
type fakeTenantRegistry struct {
	ensureCalled      *interfaces.TenantSpec
	ensureResult      interfaces.Tenant
	ensureErr         error
	listCalled        *interfaces.TenantFilter
	listResult        []interfaces.Tenant
	listErr           error
	getByDomainCalled string
	getByDomainResult interfaces.Tenant
	getByDomainErr    error
	updateCalled      *tenantUpdateCall
	updateResult      interfaces.Tenant
	updateErr         error
	disableCalled     string
	disableErr        error
	getByIDCalled     string
	getByIDResult     interfaces.Tenant
	getByIDErr        error
	getBySlugCalled   string
	getBySlugResult   interfaces.Tenant
	getBySlugErr      error
}

type tenantUpdateCall struct {
	id    string
	patch interfaces.TenantPatch
}

func (f *fakeTenantRegistry) Ensure(spec interfaces.TenantSpec) (interfaces.Tenant, error) {
	f.ensureCalled = &spec
	return f.ensureResult, f.ensureErr
}
func (f *fakeTenantRegistry) GetByID(id string) (interfaces.Tenant, error) {
	f.getByIDCalled = id
	return f.getByIDResult, f.getByIDErr
}
func (f *fakeTenantRegistry) GetByDomain(domain string) (interfaces.Tenant, error) {
	f.getByDomainCalled = domain
	return f.getByDomainResult, f.getByDomainErr
}
func (f *fakeTenantRegistry) GetBySlug(slug string) (interfaces.Tenant, error) {
	f.getBySlugCalled = slug
	return f.getBySlugResult, f.getBySlugErr
}
func (f *fakeTenantRegistry) List(filter interfaces.TenantFilter) ([]interfaces.Tenant, error) {
	f.listCalled = &filter
	return f.listResult, f.listErr
}
func (f *fakeTenantRegistry) Update(id string, patch interfaces.TenantPatch) (interfaces.Tenant, error) {
	f.updateCalled = &tenantUpdateCall{id: id, patch: patch}
	return f.updateResult, f.updateErr
}
func (f *fakeTenantRegistry) Disable(id string) error {
	f.disableCalled = id
	return f.disableErr
}

// newTenantStepApp builds a MockApplication with a fake TenantRegistry registered.
func newTenantStepApp(reg interfaces.TenantRegistry) *MockApplication {
	app := NewMockApplication()
	app.Services[TenantRegistryServiceName] = reg
	return app
}

// newTenantStepPC builds a minimal PipelineContext for tenant step tests.
func newTenantStepPC(fields map[string]any) *PipelineContext {
	current := make(map[string]any)
	for k, v := range fields {
		current[k] = v
	}
	return &PipelineContext{
		TriggerData: current,
		Current:     current,
		StepOutputs: make(map[string]map[string]any),
	}
}

// ---- tenant_ensure ----

func TestTenantEnsureStep_Execute(t *testing.T) {
	reg := &fakeTenantRegistry{
		ensureResult: interfaces.Tenant{ID: "t1", Name: "Acme", Slug: "acme", IsActive: true},
	}
	app := newTenantStepApp(reg)

	factory := NewTenantEnsureStepFactory()
	step, err := factory("ensure-tenant", map[string]any{
		"name_key": "tenant_name",
		"slug_key": "tenant_slug",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(map[string]any{
		"tenant_name": "Acme",
		"tenant_slug": "acme",
	})
	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Output == nil {
		t.Fatal("expected non-nil output")
	}
	if reg.ensureCalled == nil {
		t.Fatal("Ensure not called")
	}
	if reg.ensureCalled.Name != "Acme" {
		t.Errorf("expected Name=Acme got %q", reg.ensureCalled.Name)
	}
	if reg.ensureCalled.Slug != "acme" {
		t.Errorf("expected Slug=acme got %q", reg.ensureCalled.Slug)
	}
	if out, _ := result.Output["tenant"].(map[string]any); out["id"] != "t1" {
		t.Errorf("expected output tenant.id=t1, got %v", result.Output["tenant"])
	}
}

func TestTenantEnsureStep_MissingRegistry(t *testing.T) {
	app := NewMockApplication() // no registry registered
	factory := NewTenantEnsureStepFactory()
	step, err := factory("ensure-tenant", map[string]any{
		"name_key": "n",
		"slug_key": "s",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	pc := newTenantStepPC(map[string]any{"n": "x", "s": "y"})
	_, err = step.Execute(t.Context(), pc)
	if err == nil {
		t.Fatal("expected error when registry missing")
	}
}

// ---- tenant_list ----

func TestTenantListStep_Execute(t *testing.T) {
	tenants := []interfaces.Tenant{
		{ID: "t1", Slug: "acme", IsActive: true},
		{ID: "t2", Slug: "beta", IsActive: true},
	}
	reg := &fakeTenantRegistry{listResult: tenants}
	app := newTenantStepApp(reg)

	factory := NewTenantListStepFactory()
	step, err := factory("list-tenants", map[string]any{
		"active_only": true,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(nil)
	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.listCalled == nil {
		t.Fatal("List not called")
	}
	if !reg.listCalled.ActiveOnly {
		t.Error("expected ActiveOnly=true")
	}
	list, _ := result.Output["tenants"].([]interfaces.Tenant)
	if len(list) != 2 {
		t.Errorf("expected 2 tenants, got %d", len(list))
	}
}

// ---- tenant_get_by_domain ----

func TestTenantGetByDomainStep_Execute(t *testing.T) {
	reg := &fakeTenantRegistry{
		getByDomainResult: interfaces.Tenant{ID: "t1", Slug: "acme"},
	}
	app := newTenantStepApp(reg)

	factory := NewTenantGetByDomainStepFactory()
	step, err := factory("get-by-domain", map[string]any{
		"domain_key": "host",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(map[string]any{"host": "acme.example.com"})
	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.getByDomainCalled != "acme.example.com" {
		t.Errorf("expected GetByDomain(%q) got %q", "acme.example.com", reg.getByDomainCalled)
	}
	if out, _ := result.Output["tenant"].(map[string]any); out["id"] != "t1" {
		t.Errorf("expected tenant.id=t1, got %v", result.Output["tenant"])
	}
}

// ---- tenant_update ----

func TestTenantUpdateStep_Execute(t *testing.T) {
	newName := "Acme Corp"
	reg := &fakeTenantRegistry{
		updateResult: interfaces.Tenant{ID: "t1", Name: newName, Slug: "acme"},
	}
	app := newTenantStepApp(reg)

	factory := NewTenantUpdateStepFactory()
	step, err := factory("update-tenant", map[string]any{
		"id_key":   "tenant_id",
		"name_key": "new_name",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(map[string]any{
		"tenant_id": "t1",
		"new_name":  newName,
	})
	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.updateCalled == nil {
		t.Fatal("Update not called")
	}
	if reg.updateCalled.id != "t1" {
		t.Errorf("expected id=t1 got %q", reg.updateCalled.id)
	}
	if reg.updateCalled.patch.Name == nil || *reg.updateCalled.patch.Name != newName {
		t.Errorf("expected patch.Name=%q, got %v", newName, reg.updateCalled.patch.Name)
	}
	if out, _ := result.Output["tenant"].(map[string]any); out["id"] != "t1" {
		t.Errorf("expected tenant.id=t1, got %v", result.Output["tenant"])
	}
}

// ---- tenant_disable ----

func TestTenantDisableStep_Execute(t *testing.T) {
	reg := &fakeTenantRegistry{}
	app := newTenantStepApp(reg)

	factory := NewTenantDisableStepFactory()
	step, err := factory("disable-tenant", map[string]any{
		"id_key": "tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(map[string]any{"tenant_id": "t1"})
	result, err := step.Execute(t.Context(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reg.disableCalled != "t1" {
		t.Errorf("expected Disable(%q) got %q", "t1", reg.disableCalled)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestTenantDisableStep_MissingID(t *testing.T) {
	reg := &fakeTenantRegistry{}
	app := newTenantStepApp(reg)

	factory := NewTenantDisableStepFactory()
	step, err := factory("disable-tenant", map[string]any{
		"id_key": "tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := newTenantStepPC(nil) // no tenant_id in context
	_, err = step.Execute(t.Context(), pc)
	if err == nil {
		t.Fatal("expected error when id missing")
	}
}
