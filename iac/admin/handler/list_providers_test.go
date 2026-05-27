package handler_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// nameableProvider is the minimal interfaces.IaCProvider that
// list_providers_test needs. Only Name() returns a useful value — the
// rest exist to satisfy the interface. Distinct from stubProvider in
// provider_test.go so the two test files don't collide on type names.
type nameableProvider struct{ name string }

func (p *nameableProvider) Name() string                                         { return p.name }
func (p *nameableProvider) Version() string                                      { return "test" }
func (p *nameableProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (p *nameableProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (p *nameableProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) Import(_ context.Context, _, _ string) (*interfaces.ResourceState, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, errors.New("stub")
}
func (p *nameableProvider) SupportedCanonicalKeys() []string { return nil }
func (p *nameableProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (p *nameableProvider) Close() error { return nil }

func providersFixture() map[string]interfaces.IaCProvider {
	return map[string]interfaces.IaCProvider{
		"do-prod":     &nameableProvider{name: "digitalocean"},
		"aws-prod":    &nameableProvider{name: "aws"},
		"stub-tester": &nameableProvider{name: "stub"},
	}
}

func TestListProviders_HappyPath(t *testing.T) {
	providers := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, err := handler.ListProviders(
		context.Background(),
		providers,
		catalog.New(),
		catalog.NewRegionCatalog(),
		catalog.NewEngineCatalog(),
		in,
	)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if out.Error != "" {
		t.Errorf("unexpected error: %q", out.Error)
	}
	if len(out.Providers) != len(providers) {
		t.Fatalf("got %d providers, want %d", len(out.Providers), len(providers))
	}
}

func TestListProviders_PopulatesRegionsAndEnginesAndTypes(t *testing.T) {
	providers := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	var doProv *adminpb.AdminProviderSummary
	for _, p := range out.Providers {
		if p.ModuleName == "do-prod" {
			doProv = p
		}
	}
	if doProv == nil {
		t.Fatal("do-prod missing from result")
	}
	if doProv.ProviderType != "digitalocean" {
		t.Errorf("ProviderType = %q, want digitalocean (from provider.Name())", doProv.ProviderType)
	}
	if doProv.RegionsSource != "local-catalog" {
		t.Errorf("RegionsSource = %q, want local-catalog (v1 per design)", doProv.RegionsSource)
	}
	if len(doProv.SupportedRegions) == 0 {
		t.Error("SupportedRegions empty for digitalocean — region catalog lookup failed")
	}
	if !contains(doProv.SupportedRegions, "nyc3") {
		t.Errorf("SupportedRegions missing nyc3: %v", doProv.SupportedRegions)
	}
	if len(doProv.SupportedEngines) == 0 {
		t.Error("SupportedEngines empty for digitalocean — engine catalog lookup failed")
	}
	if len(doProv.SupportedTypes) == 0 {
		t.Error("SupportedTypes empty — fieldCat reverse-index produced no types")
	}
	// All 13 catalog types should be present in supported_types since v1
	// of the catalog treats every type as cross-provider.
	if len(doProv.SupportedTypes) < 13 {
		t.Errorf("SupportedTypes count = %d, want >= 13 (full catalog)", len(doProv.SupportedTypes))
	}
}

func TestListProviders_SortedByModuleName(t *testing.T) {
	providers := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	gotNames := make([]string, 0, len(out.Providers))
	for _, p := range out.Providers {
		gotNames = append(gotNames, p.ModuleName)
	}
	if !sort.StringsAreSorted(gotNames) {
		t.Errorf("ListProviders not sorted by module_name: %v", gotNames)
	}
}

func TestListProviders_DefaultDeny(t *testing.T) {
	providers := providersFixture()
	cases := []struct {
		name string
		ev   *adminpb.AdminAuthzEvidence
	}{
		{"nil", nil},
		{"checked=false", &adminpb.AdminAuthzEvidence{AuthzChecked: false, AuthzAllowed: true}},
		{"allowed=false", &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: false}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := &adminpb.AdminListProvidersInput{Evidence: c.ev}
			out, _ := handler.ListProviders(
				context.Background(), providers,
				catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
			)
			if out.Error == "" {
				t.Error("expected non-empty Error on default-deny")
			}
			if len(out.Providers) != 0 {
				t.Errorf("expected empty Providers on refusal, got %d", len(out.Providers))
			}
		})
	}
}

func TestListProviders_UnknownProviderTypeStillSurfaces(t *testing.T) {
	// A provider with a Name() that's not in the region/engine catalog
	// should still appear in the listing, but with empty
	// supported_regions / supported_engines (so the UI can render a
	// "no regions available" placeholder rather than dropping the
	// provider).
	providers := map[string]interfaces.IaCProvider{
		"unknown-mod": &nameableProvider{name: "mystery-cloud"},
	}
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	if len(out.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(out.Providers))
	}
	p := out.Providers[0]
	if p.ProviderType != "mystery-cloud" {
		t.Errorf("ProviderType = %q, want mystery-cloud", p.ProviderType)
	}
	if len(p.SupportedRegions) != 0 {
		t.Errorf("SupportedRegions = %v, want empty (mystery-cloud not in catalog)", p.SupportedRegions)
	}
	if len(p.SupportedEngines) != 0 {
		t.Errorf("SupportedEngines = %v, want empty (mystery-cloud not in catalog)", p.SupportedEngines)
	}
	// supported_types is catalog-derived (NOT per-provider) in v1 so
	// it stays populated even for uncatalogued provider types.
	if len(p.SupportedTypes) == 0 {
		t.Error("SupportedTypes empty — should still list the full catalog regardless of provider")
	}
}

func contains(slice []string, v string) bool {
	for _, s := range slice {
		if s == v {
			return true
		}
	}
	return false
}
