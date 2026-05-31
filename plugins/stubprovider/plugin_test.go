package stubprovider_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
	pluginstub "github.com/GoCodeAlone/workflow/plugins/stubprovider"
)

// TestPlugin_ModuleFactories asserts the plugin registers "iac.provider".
func TestPlugin_ModuleFactories(t *testing.T) {
	p := pluginstub.New()
	factories := p.ModuleFactories()
	if factories == nil {
		t.Fatal("ModuleFactories returned nil")
	}
	factory, ok := factories["iac.provider"]
	if !ok {
		t.Fatalf("expected 'iac.provider' in ModuleFactories, got keys: %v", keys(factories))
	}
	if factory == nil {
		t.Fatal("factory for 'iac.provider' is nil")
	}
}

// TestPlugin_Module_ProvidesIaCProvider creates an iac.provider module via the
// factory, calls ProvidesServices(), and asserts one of the entries satisfies
// interfaces.IaCProvider.
func TestPlugin_Module_ProvidesIaCProvider(t *testing.T) {
	p := pluginstub.New()
	factory := p.ModuleFactories()["iac.provider"]

	mod := factory("stub-provider", map[string]any{"provider": "stub"})
	if mod == nil {
		t.Fatal("factory returned nil module")
	}
	if mod.Name() != "stub-provider" {
		t.Errorf("module Name = %q, want 'stub-provider'", mod.Name())
	}

	sa, ok := mod.(modular.ServiceAware)
	if !ok {
		t.Fatalf("module does not implement modular.ServiceAware; got %T", mod)
	}
	services := sa.ProvidesServices()
	if len(services) == 0 {
		t.Fatal("ProvidesServices returned empty slice")
	}

	var foundProvider interfaces.IaCProvider
	for _, svc := range services {
		if p, ok := svc.Instance.(interfaces.IaCProvider); ok {
			foundProvider = p
			break
		}
	}
	if foundProvider == nil {
		t.Fatal("none of ProvidesServices entries implements interfaces.IaCProvider")
	}
}

// TestPlugin_Module_NonStubProviderErrors asserts that a module configured
// with provider != "stub" returns an error from Init.
func TestPlugin_Module_NonStubProviderErrors(t *testing.T) {
	p := pluginstub.New()
	factory := p.ModuleFactories()["iac.provider"]
	mod := factory("bad-provider", map[string]any{"provider": "digitalocean"})

	app := modular.NewStdApplication(nil, nopLogger{})
	err := mod.Init(app)
	if err == nil {
		t.Fatal("Init with provider=digitalocean should return an error, got nil")
	}
}

// TestPlugin_Module_StubProviderInits asserts that a module configured
// with provider=stub initialises without error.
func TestPlugin_Module_StubProviderInits(t *testing.T) {
	p := pluginstub.New()
	factory := p.ModuleFactories()["iac.provider"]
	mod := factory("my-stub", map[string]any{"provider": "stub"})

	app := modular.NewStdApplication(nil, nopLogger{})
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init with provider=stub should not error: %v", err)
	}
}

// TestPlugin_Module_StubProviderApply exercises the resolved IaCProvider
// end-to-end via a Plan call to prove the service wire-up actually works.
func TestPlugin_Module_StubProviderApply(t *testing.T) {
	p := pluginstub.New()
	factory := p.ModuleFactories()["iac.provider"]
	mod := factory("my-stub", map[string]any{"provider": "stub"})

	app := modular.NewStdApplication(nil, nopLogger{})
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	sa := mod.(modular.ServiceAware)
	var prov interfaces.IaCProvider
	for _, svc := range sa.ProvidesServices() {
		if ip, ok := svc.Instance.(interfaces.IaCProvider); ok {
			prov = ip
			break
		}
	}
	if prov == nil {
		t.Fatal("no IaCProvider service after Init")
	}

	plan, err := prov.Plan(context.Background(), []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc"},
	}, nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 || plan.Actions[0].Action != "create" {
		t.Errorf("expected plan with create action, got %+v", plan.Actions)
	}
}

// keys is a test helper that returns the map keys as a slice for diagnostics.
func keys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

// nopLogger satisfies modular.Logger for tests.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
