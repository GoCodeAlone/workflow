package recommend_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/recommend"
)

func TestRecommend_GroupsProvidersPerCapability(t *testing.T) {
	inv, err := inventory.CollectEcosystem(inventory.EcosystemOptions{
		RegistryDir:  "../inventory/testdata/ecosystem/registry",
		RepoRoot:     "../inventory/testdata/ecosystem/repos",
		TaxonomyPath: "../inventory/testdata/taxonomy.yaml",
		GeneratedAt:  time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}
	rec := recommend.Recommend(inv, recommend.Options{Capabilities: []string{"auth.authz"}})
	if len(rec.Requested) != 1 || rec.Requested[0] != "auth.authz" {
		t.Fatalf("Requested = %#v, want [auth.authz]", rec.Requested)
	}
	hit := findHit(t, rec, "auth.authz")
	if len(hit.Providers) < 1 {
		t.Fatalf("auth.authz has no providers: %#v", hit)
	}
	if !hasProvider(hit, "auth") {
		t.Fatalf("auth.authz missing provider %q: %#v", "auth", hit.Providers)
	}
	if len(rec.Unmatched) != 0 {
		t.Fatalf("Unmatched = %#v, want empty", rec.Unmatched)
	}
}

func TestRecommend_UnmatchedRequestedCapability(t *testing.T) {
	inv := smallInventory(t)
	rec := recommend.Recommend(inv, recommend.Options{Capabilities: []string{"does.not.exist"}})
	if len(rec.Unmatched) != 1 || rec.Unmatched[0] != "does.not.exist" {
		t.Fatalf("Unmatched = %#v, want [does.not.exist]", rec.Unmatched)
	}
	if len(rec.Capabilities) != 0 {
		t.Fatalf("expected zero hits, got %#v", rec.Capabilities)
	}
}

func TestRecommend_Deterministic(t *testing.T) {
	inv := smallInventory(t)
	opts := recommend.Options{Categories: []string{"auth"}}
	a := recommend.Recommend(inv, opts)
	b := recommend.Recommend(inv, opts)
	if !equalRec(a, b) {
		t.Fatalf("non-deterministic: %#v != %#v", a, b)
	}
}

func TestRecommend_ExcludesUncategorizedByDefault(t *testing.T) {
	inv := uncategorizedInventory(t)
	if rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"uncategorized"}}); len(rec.Capabilities) != 0 {
		t.Fatalf("uncategorized leaked: %#v", rec.Capabilities)
	}
	if rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"uncategorized"}, IncludeUncategorized: true}); len(rec.Capabilities) != 1 {
		t.Fatalf("IncludeUncategorized dropped row: %#v", rec.Capabilities)
	}
}

func TestRecommend_ByTagNotUnmatched(t *testing.T) {
	// Requesting by a TAG (not id/name) must still match and NOT report Unmatched.
	inv := smallInventory(t) // auth.authz carries tag "cross-cutting"
	rec := recommend.Recommend(inv, recommend.Options{Capabilities: []string{"cross-cutting"}})
	if len(rec.Unmatched) != 0 {
		t.Fatalf("tag request falsely unmatched: %#v", rec.Unmatched)
	}
	if len(rec.Capabilities) != 1 || rec.Capabilities[0].ID != "auth.authz" {
		t.Fatalf("tag request should match auth.authz: %#v", rec.Capabilities)
	}
}

func TestRecommend_ByDescriptionNotUnmatched(t *testing.T) {
	// Requesting by a description substring must match and NOT report Unmatched.
	inv := smallInventory(t) // auth.authz description "Enforces permissions and roles"
	rec := recommend.Recommend(inv, recommend.Options{Capabilities: []string{"permissions"}})
	if len(rec.Unmatched) != 0 {
		t.Fatalf("description request falsely unmatched: %#v", rec.Unmatched)
	}
	if len(rec.Capabilities) != 1 {
		t.Fatalf("description request should match auth.authz: %#v", rec.Capabilities)
	}
}

func TestRecommend_PreservesSameNameDiffStatus(t *testing.T) {
	// A capability provided by the same plugin name under different release
	// statuses (registry vs local) must surface BOTH provider rows, not dedupe
	// them away by name.
	inv := &inventory.Inventory{
		Capabilities: []inventory.Capability{
			{
				ID:       "auth.authz",
				Category: "auth",
				Name:     "Authorization",
				Providers: []inventory.Provider{
					{Name: "auth", Kind: "external", ReleaseStatus: "released"},
					{Name: "auth", Kind: "local-plugin", ReleaseStatus: "local-only"},
				},
			},
		},
	}
	rec := recommend.Recommend(inv, recommend.Options{Categories: []string{"auth"}})
	hit := findHit(t, rec, "auth.authz")
	if len(hit.Providers) != 2 {
		t.Fatalf("expected 2 providers (registry+local), got %#v", hit.Providers)
	}
}

// findHit returns the CapabilityHit with the given ID, failing the test if absent.
func findHit(t *testing.T, rec *recommend.Recommendation, id string) recommend.CapabilityHit {
	t.Helper()
	for _, h := range rec.Capabilities {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no CapabilityHit with id %q in recommendation: %#v", id, rec)
	return recommend.CapabilityHit{}
}

// hasProvider reports whether hit has a provider with the given name.
func hasProvider(hit recommend.CapabilityHit, name string) bool {
	for _, p := range hit.Providers {
		if p.Name == name {
			return true
		}
	}
	return false
}

// smallInventory builds a minimal in-memory inventory with one capability
// (id auth.authz, category auth) provided by one local plugin.
func smallInventory(t *testing.T) *inventory.Inventory {
	t.Helper()
	return &inventory.Inventory{
		Capabilities: []inventory.Capability{
			{
				ID:          "auth.authz",
				Category:    "auth",
				Name:        "Authorization",
				Description: "Enforces permissions and roles",
				Tags:        []string{"cross-cutting", "authz-sensitive"},
				Providers: []inventory.Provider{
					{Name: "auth", Kind: "local-plugin", ReleaseStatus: "local-only"},
				},
			},
		},
	}
}

// uncategorizedInventory builds a minimal inventory with one uncategorized capability.
func uncategorizedInventory(t *testing.T) *inventory.Inventory {
	t.Helper()
	return &inventory.Inventory{
		Capabilities: []inventory.Capability{
			{
				ID:          "uncategorized:module:custom.thing",
				Category:    "uncategorized",
				Name:        "custom.thing",
				Description: "Raw capability declaration with no taxonomy mapping",
				Providers: []inventory.Provider{
					{Name: "custom", Kind: "local-plugin", ReleaseStatus: "local-only"},
				},
			},
		},
	}
}

// equalRec reports deep equality between two Recommendations.
func equalRec(a, b *recommend.Recommendation) bool {
	return reflect.DeepEqual(a, b)
}
