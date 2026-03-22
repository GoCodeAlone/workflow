package module

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// infraMockApp is a minimal modular.Application for InfraModule tests.
type infraMockApp struct {
	services map[string]any
}

func newInfraMockApp() *infraMockApp {
	return &infraMockApp{services: make(map[string]any)}
}

func (a *infraMockApp) RegisterConfigSection(string, modular.ConfigProvider)  {}
func (a *infraMockApp) GetConfigSection(string) (modular.ConfigProvider, error) { return nil, nil }
func (a *infraMockApp) ConfigSections() map[string]modular.ConfigProvider {
	return nil
}
func (a *infraMockApp) Logger() modular.Logger                    { return &noopLogger{} }
func (a *infraMockApp) SetLogger(modular.Logger)                  {}
func (a *infraMockApp) ConfigProvider() modular.ConfigProvider    { return nil }
func (a *infraMockApp) SvcRegistry() modular.ServiceRegistry      { return a.services }
func (a *infraMockApp) RegisterModule(modular.Module)             {}
func (a *infraMockApp) RegisterService(name string, svc any) error {
	a.services[name] = svc
	return nil
}
func (a *infraMockApp) GetService(name string, target any) error {
	svc, ok := a.services[name]
	if !ok {
		return errors.New("service not found: " + name)
	}
	// Use reflection to set the target pointer.
	rv := reflect.ValueOf(target)
	if rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv.Elem().Set(reflect.ValueOf(svc))
	}
	return nil
}
func (a *infraMockApp) Init() error                                            { return nil }
func (a *infraMockApp) Start() error                                           { return nil }
func (a *infraMockApp) Stop() error                                            { return nil }
func (a *infraMockApp) Run() error                                             { return nil }
func (a *infraMockApp) IsVerboseConfig() bool                                  { return false }
func (a *infraMockApp) SetVerboseConfig(bool)                                  {}
func (a *infraMockApp) Context() context.Context                               { return context.Background() }
func (a *infraMockApp) GetServicesByModule(string) []string                    { return nil }
func (a *infraMockApp) GetServiceEntry(string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}
func (a *infraMockApp) GetServicesByInterface(reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}
func (a *infraMockApp) GetModule(string) modular.Module              { return nil }
func (a *infraMockApp) GetAllModules() map[string]modular.Module     { return nil }
func (a *infraMockApp) StartTime() time.Time                         { return time.Time{} }
func (a *infraMockApp) OnConfigLoaded(func(modular.Application) error) {}

// infraMockProvider implements interfaces.IaCProvider for tests.
type infraMockProvider struct {
	name    string
	drivers map[string]interfaces.ResourceDriver
}

func (p *infraMockProvider) Name() string    { return p.name }
func (p *infraMockProvider) Version() string { return "0.0.1" }
func (p *infraMockProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *infraMockProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }
func (p *infraMockProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (p *infraMockProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return nil, nil
}
func (p *infraMockProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (p *infraMockProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (p *infraMockProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (p *infraMockProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (p *infraMockProvider) ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return &interfaces.ProviderSizing{InstanceType: string(size)}, nil
}
func (p *infraMockProvider) ResourceDriver(resourceType string) (interfaces.ResourceDriver, error) {
	if d, ok := p.drivers[resourceType]; ok {
		return d, nil
	}
	return nil, errors.New("no driver for " + resourceType)
}
func (p *infraMockProvider) Close() error { return nil }

// infraMockDriver records calls made to it.
type infraMockDriver struct {
	created []interfaces.ResourceSpec
	read    []interfaces.ResourceRef
	updated []interfaces.ResourceRef
	deleted []interfaces.ResourceRef
}

func (d *infraMockDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.created = append(d.created, spec)
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, Status: "created"}, nil
}
func (d *infraMockDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	d.read = append(d.read, ref)
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, Status: "running"}, nil
}
func (d *infraMockDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.updated = append(d.updated, ref)
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, Status: "updated"}, nil
}
func (d *infraMockDriver) Delete(_ context.Context, ref interfaces.ResourceRef) error {
	d.deleted = append(d.deleted, ref)
	return nil
}
func (d *infraMockDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{}, nil
}
func (d *infraMockDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return &interfaces.HealthResult{Healthy: true}, nil
}
func (d *infraMockDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// helpers

func newTestProvider(name string, types ...string) (*infraMockProvider, map[string]*infraMockDriver) {
	drivers := make(map[string]*infraMockDriver, len(types))
	driverIface := make(map[string]interfaces.ResourceDriver, len(types))
	for _, t := range types {
		d := &infraMockDriver{}
		drivers[t] = d
		driverIface[t] = d
	}
	return &infraMockProvider{name: name, drivers: driverIface}, drivers
}

func initWithProvider(t *testing.T, m *InfraModule, providerName string, provider interfaces.IaCProvider) *infraMockApp {
	t.Helper()
	app := newInfraMockApp()
	app.services[providerName] = provider
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return app
}

// Tests

func TestInfraModule_Name(t *testing.T) {
	m := NewInfraModule("my-db", "infra.database", map[string]any{"provider": "aws"})
	if m.Name() != "my-db" {
		t.Errorf("Name() = %q, want %q", m.Name(), "my-db")
	}
}

func TestInfraModule_InfraType(t *testing.T) {
	m := NewInfraModule("my-db", "infra.database", map[string]any{"provider": "aws"})
	if m.InfraType() != "infra.database" {
		t.Errorf("InfraType() = %q, want %q", m.InfraType(), "infra.database")
	}
}

func TestInfraModule_Factory(t *testing.T) {
	factory := NewInfraModuleFactory("infra.vpc")
	m := factory("my-vpc", map[string]any{"provider": "aws"})
	im, ok := m.(*InfraModule)
	if !ok {
		t.Fatalf("factory returned %T, want *InfraModule", m)
	}
	if im.InfraType() != "infra.vpc" {
		t.Errorf("InfraType() = %q, want %q", im.InfraType(), "infra.vpc")
	}
}

func TestInfraModule_Init_SizeDefault(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.database")
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "aws"})
	initWithProvider(t, m, "aws", provider)

	if m.Size() != interfaces.SizeS {
		t.Errorf("Size() = %q, want %q", m.Size(), interfaces.SizeS)
	}
}

func TestInfraModule_Init_SizeFromConfig(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.cache")
	m := NewInfraModule("cache", "infra.cache", map[string]any{
		"provider": "aws",
		"size":     "xl",
	})
	initWithProvider(t, m, "aws", provider)

	if m.Size() != interfaces.SizeXL {
		t.Errorf("Size() = %q, want %q", m.Size(), interfaces.SizeXL)
	}
}

func TestInfraModule_Init_ResourceHints(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.database")
	m := NewInfraModule("db", "infra.database", map[string]any{
		"provider": "aws",
		"resources": map[string]any{
			"cpu":    "2",
			"memory": "4Gi",
		},
	})
	initWithProvider(t, m, "aws", provider)

	if m.Hints() == nil {
		t.Fatal("Hints() is nil, expected non-nil")
	}
	if m.Hints().CPU != "2" {
		t.Errorf("CPU = %q, want %q", m.Hints().CPU, "2")
	}
	if m.Hints().Memory != "4Gi" {
		t.Errorf("Memory = %q, want %q", m.Hints().Memory, "4Gi")
	}
}

func TestInfraModule_Init_RegistersDriver(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.vpc")
	m := NewInfraModule("vpc", "infra.vpc", map[string]any{"provider": "aws"})
	app := initWithProvider(t, m, "aws", provider)

	svc, ok := app.services["vpc.driver"]
	if !ok {
		t.Fatal("expected 'vpc.driver' to be registered in SvcRegistry")
	}
	if _, ok := svc.(*InfraModule); !ok {
		t.Errorf("registered service is %T, want *InfraModule", svc)
	}
}

func TestInfraModule_Init_MissingProvider(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "aws"})
	app := newInfraMockApp() // aws not registered
	err := m.Init(app)
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
}

func TestInfraModule_Init_EmptyProvider(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{})
	app := newInfraMockApp()
	err := m.Init(app)
	if err == nil {
		t.Fatal("expected error when 'provider' config missing, got nil")
	}
}

func TestInfraModule_Init_ServiceNotIaCProvider(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "aws"})
	app := newInfraMockApp()
	app.services["aws"] = "not-a-provider"
	err := m.Init(app)
	if err == nil {
		t.Fatal("expected error when service is not IaCProvider, got nil")
	}
}

func TestInfraModule_Init_UnknownResourceType(t *testing.T) {
	// Provider supports only "infra.vpc", not "infra.database"
	provider, _ := newTestProvider("aws", "infra.vpc")
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "aws"})
	app := newInfraMockApp()
	app.services["aws"] = provider
	err := m.Init(app)
	if err == nil {
		t.Fatal("expected error for unsupported resource type, got nil")
	}
}

func TestInfraModule_Create_DelegatestoDriver(t *testing.T) {
	provider, drivers := newTestProvider("aws", "infra.database")
	m := NewInfraModule("my-db", "infra.database", map[string]any{
		"provider": "aws",
		"engine":   "postgres",
	})
	initWithProvider(t, m, "aws", provider)

	out, err := m.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if out.Status != "created" {
		t.Errorf("Create() status = %q, want %q", out.Status, "created")
	}
	if len(drivers["infra.database"].created) != 1 {
		t.Errorf("driver.Create called %d times, want 1", len(drivers["infra.database"].created))
	}
	spec := drivers["infra.database"].created[0]
	if spec.Name != "my-db" {
		t.Errorf("spec.Name = %q, want %q", spec.Name, "my-db")
	}
	// engine field should be forwarded, standard fields stripped
	if spec.Config["engine"] != "postgres" {
		t.Errorf("spec.Config[engine] = %v, want %q", spec.Config["engine"], "postgres")
	}
	if _, ok := spec.Config["provider"]; ok {
		t.Error("spec.Config should not contain 'provider' key")
	}
}

func TestInfraModule_Read_DelegatesToDriver(t *testing.T) {
	provider, drivers := newTestProvider("gcp", "infra.vpc")
	m := NewInfraModule("my-vpc", "infra.vpc", map[string]any{"provider": "gcp"})
	initWithProvider(t, m, "gcp", provider)

	_, err := m.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(drivers["infra.vpc"].read) != 1 {
		t.Errorf("driver.Read called %d times, want 1", len(drivers["infra.vpc"].read))
	}
	if drivers["infra.vpc"].read[0].Name != "my-vpc" {
		t.Errorf("ref.Name = %q, want %q", drivers["infra.vpc"].read[0].Name, "my-vpc")
	}
}

func TestInfraModule_Update_DelegatesToDriver(t *testing.T) {
	provider, drivers := newTestProvider("gcp", "infra.cache")
	m := NewInfraModule("cache", "infra.cache", map[string]any{"provider": "gcp"})
	initWithProvider(t, m, "gcp", provider)

	_, err := m.Update(context.Background())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(drivers["infra.cache"].updated) != 1 {
		t.Errorf("driver.Update called %d times, want 1", len(drivers["infra.cache"].updated))
	}
}

func TestInfraModule_Delete_DelegatesToDriver(t *testing.T) {
	provider, drivers := newTestProvider("do", "infra.dns")
	m := NewInfraModule("dns", "infra.dns", map[string]any{"provider": "do"})
	initWithProvider(t, m, "do", provider)

	err := m.Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(drivers["infra.dns"].deleted) != 1 {
		t.Errorf("driver.Delete called %d times, want 1", len(drivers["infra.dns"].deleted))
	}
}

func TestInfraModule_RequiresServices(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "my-aws"})
	deps := m.RequiresServices()
	if len(deps) != 1 {
		t.Fatalf("RequiresServices() returned %d deps, want 1", len(deps))
	}
	if deps[0].Name != "my-aws" {
		t.Errorf("dep Name = %q, want %q", deps[0].Name, "my-aws")
	}
	if !deps[0].Required {
		t.Error("dep.Required should be true")
	}
}

func TestInfraModule_RequiresServices_EmptyProvider(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{})
	deps := m.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("RequiresServices() returned %d deps, want 0", len(deps))
	}
}

func TestInfraModule_ProvidesServices(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{"provider": "aws"})
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("ProvidesServices() returned %d services, want 1", len(svcs))
	}
	if svcs[0].Name != "db.driver" {
		t.Errorf("service Name = %q, want %q", svcs[0].Name, "db.driver")
	}
}

func TestInfraModule_ResourceConfig_StripsStandardKeys(t *testing.T) {
	m := NewInfraModule("db", "infra.database", map[string]any{
		"provider": "aws",
		"size":     "m",
		"resources": map[string]any{"cpu": "2"},
		"engine":   "postgres",
		"version":  "16",
	})
	cfg := m.ResourceConfig()
	if _, ok := cfg["provider"]; ok {
		t.Error("ResourceConfig should strip 'provider'")
	}
	if _, ok := cfg["size"]; ok {
		t.Error("ResourceConfig should strip 'size'")
	}
	if _, ok := cfg["resources"]; ok {
		t.Error("ResourceConfig should strip 'resources'")
	}
	if cfg["engine"] != "postgres" {
		t.Errorf("engine = %v, want %q", cfg["engine"], "postgres")
	}
	if cfg["version"] != "16" {
		t.Errorf("version = %v, want %q", cfg["version"], "16")
	}
}

func TestInfraModule_SizingPassesThroughToProvider(t *testing.T) {
	provider, _ := newTestProvider("aws", "infra.k8s_cluster")
	m := NewInfraModule("k8s", "infra.k8s_cluster", map[string]any{
		"provider": "aws",
		"size":     "l",
	})
	initWithProvider(t, m, "aws", provider)

	sizing, err := m.Provider().ResolveSizing(m.InfraType(), m.Size(), m.Hints())
	if err != nil {
		t.Fatalf("ResolveSizing: %v", err)
	}
	// mock provider returns the size as instance type
	if sizing.InstanceType != "l" {
		t.Errorf("InstanceType = %q, want %q", sizing.InstanceType, "l")
	}
}
