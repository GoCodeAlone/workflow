package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// stubRegionLister implements interfaces.IaCProviderRegionLister.
type stubRegionLister struct {
	regions []string
	err     error
}

func (r *stubRegionLister) ListRegions(_ context.Context, _ string) ([]string, error) {
	return r.regions, r.err
}

// compile-time check
var _ interfaces.IaCProviderRegionLister = (*stubRegionLister)(nil)

// stubProviderWithRegionLister extends stubIaCProvider with RegionLister capability.
// It satisfies providerclient.RegionListerProvider via the RegionLister() accessor.
type stubProviderWithRegionLister struct {
	stubIaCProvider
	lister *stubRegionLister // nil → RegionLister() returns nil (unadvertised)
}

// RegionLister satisfies providerclient.RegionListerProvider.
func (p *stubProviderWithRegionLister) RegionLister() interfaces.IaCProviderRegionLister {
	if p.lister == nil {
		return nil
	}
	return p.lister
}

// ─── step.iac_provider_catalog tests ─────────────────────────────────────────

func TestIaCProviderCatalogStep_LiveRegions(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubProviderWithRegionLister{
		stubIaCProvider: stubIaCProvider{
			caps: []interfaces.IaCCapabilityDeclaration{
				{ResourceType: "infra.database", Tier: 1, Operations: []string{"create", "delete"}},
			},
		},
		lister: &stubRegionLister{regions: []string{"us-east-1", "eu-west-1"}},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderCatalogStepFactory()
	step, err := factory("catalog-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Output["source"] != "live" {
		t.Errorf("expected source=live, got %v", result.Output["source"])
	}
	regions, ok := result.Output["regions"].([]string)
	if !ok {
		t.Fatalf("expected []string regions, got %T", result.Output["regions"])
	}
	if len(regions) != 2 {
		t.Errorf("expected 2 regions, got %d", len(regions))
	}

	types, ok := result.Output["types"].([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any types, got %T", result.Output["types"])
	}
	if len(types) != 1 || types[0]["resource_type"] != "infra.database" {
		t.Errorf("unexpected types: %v", types)
	}
}

func TestIaCProviderCatalogStep_StaticFallback_NoRegionLister(t *testing.T) {
	app := module.NewMockApplication()
	// stubIaCProvider does NOT implement RegionListerProvider → static fallback.
	provider := &stubIaCProvider{}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderCatalogStepFactory()
	step, err := factory("catalog-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Output["source"] != "static" {
		t.Errorf("expected source=static, got %v", result.Output["source"])
	}
	regions, ok := result.Output["regions"].([]string)
	if !ok {
		t.Fatalf("expected []string regions, got %T", result.Output["regions"])
	}
	if len(regions) == 0 {
		t.Error("expected non-empty static region list")
	}
}

func TestIaCProviderCatalogStep_StaticFallback_NilRegionLister(t *testing.T) {
	app := module.NewMockApplication()
	// Provider implements RegionListerProvider but RegionLister() returns nil
	// (plugin did not advertise the service).
	provider := &stubProviderWithRegionLister{lister: nil}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderCatalogStepFactory()
	step, err := factory("catalog-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Output["source"] != "static" {
		t.Errorf("expected source=static (nil lister), got %v", result.Output["source"])
	}
}

func TestIaCProviderCatalogStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderCatalogStepFactory()
	_, err := factory("catalog-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing, got nil")
	}
}
