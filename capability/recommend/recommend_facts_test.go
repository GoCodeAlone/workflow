package recommend_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/recommend"
)

// TestRecommend_InstallRequiredFacts (D10/AS5): each ProviderSummary carries a
// selection FACT — installRequired — computed as (Kind=="external" or
// "local-plugin") AND name NOT in the default (built-in) plugin set. Raw
// Kind/ReleaseStatus are also surfaced as-is for the consumer to interpret.
// ⊥ "wiring implications" (M2) and ⊥ quality-rank (D13).
func TestRecommend_InstallRequiredFacts(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "auth.authz", Category: "auth", Name: "Authorization", Providers: []inventory.Provider{
			{Name: "auth", Kind: "external", ReleaseStatus: "released"},         // built-in name → not required
			{Name: "custom", Kind: "local-plugin", ReleaseStatus: "local-only"}, // not built-in + local → required
			{Name: "authz-pro", Kind: "registry", ReleaseStatus: "released"},    // registry kind → not required (formula)
		}},
	}}

	rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"auth"}, DefaultPlugins: []string{"auth"}})
	hit := findHit(t, rec, "auth.authz")

	want := map[string]bool{
		"auth":      false, // "auth" is a default plugin name
		"custom":    true,  // not a default; local-plugin
		"authz-pro": false, // registry kind (not external/local-plugin)
	}
	got := map[string]bool{}
	for _, p := range hit.Providers {
		// Raw Kind/ReleaseStatus must still be surfaced (D10).
		if p.Kind == "" {
			t.Fatalf("provider %q missing raw Kind", p.Name)
		}
		got[p.Name] = p.InstallRequired
	}
	for name, exp := range want {
		if g, ok := got[name]; !ok {
			t.Fatalf("provider %q missing from recommendation", name)
		} else if g != exp {
			t.Fatalf("provider %q installRequired: got %v want %v", name, g, exp)
		}
	}
}

// TestRecommend_InstallRequiredNoDefaults: with no DefaultPlugins provided,
// every external/local-plugin provider is installRequired (nothing is
// pre-considered built-in).
func TestRecommend_InstallRequiredNoDefaults(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x.y", Category: "x", Name: "X", Providers: []inventory.Provider{
			{Name: "ext", Kind: "external", ReleaseStatus: "released"},
		}},
	}}
	rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"x"}})
	hit := findHit(t, rec, "x.y")
	if len(hit.Providers) != 1 || !hit.Providers[0].InstallRequired {
		t.Fatalf("external provider with no defaults must be installRequired: %+v", hit.Providers)
	}
}
