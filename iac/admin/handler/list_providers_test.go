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

// providersFixture returns the providers map + a parallel
// providerTypeByModule map captured "as if" at module Init from the
// host YAML config. Per spec-reviewer T6 F1: the test fixture must
// separate the two so a regression that mistakenly uses provider.Name()
// can't be masked by a fake that happens to return the YAML-config
// string. We deliberately set the fake Names to DIFFERENT,
// DISPLAY-style strings so the bug would surface as wrong
// provider_type / empty region+engine lists.
func providersFixture() (map[string]interfaces.IaCProvider, map[string]string) {
	providers := map[string]interfaces.IaCProvider{
		"do-prod":     &nameableProvider{name: "DigitalOcean Provider"}, // display name
		"aws-prod":    &nameableProvider{name: "AWS Provider Plugin"},   // display name
		"stub-tester": &nameableProvider{name: "Stub IaC Provider"},     // display name
	}
	providerTypeByModule := map[string]string{
		"do-prod":     "digitalocean", // YAML-config string (stable identifier)
		"aws-prod":    "aws",
		"stub-tester": "stub",
	}
	return providers, providerTypeByModule
}

func TestListProviders_HappyPath(t *testing.T) {
	providers, providerTypeByModule := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, err := handler.ListProviders(
		context.Background(),
		providers,
		providerTypeByModule,
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
	providers, providerTypeByModule := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers, providerTypeByModule,
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
		t.Errorf("ProviderType = %q, want digitalocean (from providerTypeByModule, NOT provider.Name())", doProv.ProviderType)
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

// TestListProviders_UsesCapturedConfigStringNotProviderName is the
// regression guard for spec-reviewer T6 F1 (commit 1ea231fdd):
// provider.Name() returns the plugin's display name in production;
// the YAML-config provider: string (captured at module Init via
// providerTypeByModule) is what the catalogs key against. If the
// handler ever reverts to p.Name(), this test fails because the
// fake's Name() returns "DigitalOcean Provider" — not in any
// catalog — so SupportedRegions + SupportedEngines come back empty.
func TestListProviders_UsesCapturedConfigStringNotProviderName(t *testing.T) {
	providers, providerTypeByModule := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers, providerTypeByModule,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	var doProv *adminpb.AdminProviderSummary
	for _, p := range out.Providers {
		if p.ModuleName == "do-prod" {
			doProv = p
		}
	}
	if doProv == nil {
		t.Fatal("do-prod missing")
	}
	// Bug-class assertion: handler MUST NOT carry the display name
	// from provider.Name() into provider_type. The fixture's fake
	// Name() returns "DigitalOcean Provider" — if that leaks
	// through, the catalog lookup downstream will fail and
	// SupportedRegions will be empty.
	if doProv.ProviderType == "DigitalOcean Provider" {
		t.Fatal("BUG: provider_type carries provider.Name() (display name) instead of providerTypeByModule (YAML config string)")
	}
	if doProv.ProviderType != "digitalocean" {
		t.Errorf("ProviderType = %q, want exactly 'digitalocean'", doProv.ProviderType)
	}
	if len(doProv.SupportedRegions) == 0 {
		t.Error("SupportedRegions empty — provider_type isn't a catalog key (the F1 bug symptom)")
	}
}

// TestListProviders_MissingProviderTypeByModule_DegradesGracefully
// guards the F1 fix's degradation path: when providerTypeByModule
// doesn't include a key for a registered iac.provider module (e.g.
// stale Init, hot-reload race), the handler still emits a summary
// entry with empty provider_type + empty regions/engines so the UI
// renders a graceful empty-dropdown affordance instead of crashing
// or dropping the provider entirely.
func TestListProviders_MissingProviderTypeByModule_DegradesGracefully(t *testing.T) {
	providers, _ := providersFixture()
	// Pass an EMPTY map — simulates "Init never populated for these modules".
	emptyTypeMap := map[string]string{}
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers, emptyTypeMap,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	if len(out.Providers) != len(providers) {
		t.Fatalf("got %d providers, want %d (handler dropped entries instead of degrading)", len(out.Providers), len(providers))
	}
	for _, p := range out.Providers {
		if p.ProviderType != "" {
			t.Errorf("module %q ProviderType = %q, want empty when providerTypeByModule key is missing", p.ModuleName, p.ProviderType)
		}
		if len(p.SupportedRegions) != 0 {
			t.Errorf("module %q SupportedRegions = %v, want empty (regions keyed by empty string)", p.ModuleName, p.SupportedRegions)
		}
	}
}

func TestListProviders_SortedByModuleName(t *testing.T) {
	providers, providerTypeByModule := providersFixture()
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers, providerTypeByModule,
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
	providers, providerTypeByModule := providersFixture()
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
				context.Background(), providers, providerTypeByModule,
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
	// A registered iac.provider module whose YAML-config provider:
	// string isn't in the region/engine catalog should still appear
	// in the listing, but with empty supported_regions /
	// supported_engines (so the UI can render a "no regions available"
	// placeholder rather than dropping the provider).
	providers := map[string]interfaces.IaCProvider{
		"unknown-mod": &nameableProvider{name: "Mystery Cloud Provider"}, // display name; irrelevant
	}
	providerTypeByModule := map[string]string{
		"unknown-mod": "mystery-cloud", // YAML config string; not in any catalog
	}
	in := &adminpb.AdminListProvidersInput{Evidence: authzOK()}
	out, _ := handler.ListProviders(
		context.Background(), providers, providerTypeByModule,
		catalog.New(), catalog.NewRegionCatalog(), catalog.NewEngineCatalog(), in,
	)
	if len(out.Providers) != 1 {
		t.Fatalf("got %d providers, want 1", len(out.Providers))
	}
	p := out.Providers[0]
	if p.ProviderType != "mystery-cloud" {
		t.Errorf("ProviderType = %q, want mystery-cloud (from providerTypeByModule)", p.ProviderType)
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
