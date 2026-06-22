package assembler

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
)

func regWith(types ...string) *schema.ModuleSchemaRegistry {
	r := schema.NewModuleSchemaRegistry()
	for _, t := range types {
		if r.Get(t) == nil {
			r.Register(&schema.ModuleSchema{Type: t, ConfigFields: nil})
		}
	}
	return r
}

func cap(id string, providers ...inventory.Provider) inventory.Capability {
	return inventory.Capability{ID: id, Providers: providers}
}
func prov(name, kind string, rawCaps ...string) inventory.Provider {
	return inventory.Provider{Name: name, Kind: kind, Capabilities: rawCaps}
}

func TestSelect_GreedyCoversAllRequestedCaps(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("http.routing", prov("http", "builtin", "module:http.server", "module:http.router")),
		cap("observability.health", prov("observability", "builtin", "module:health.checker")),
	}}
	reg := regWith("http.server", "http.router", "health.checker")
	got, unmatched := selectModules(inv, []string{"http.routing", "observability.health"}, reg)
	// one type covers http.routing (set-cover picks http.server OR http.router; both cover only http.routing → 1 pick)
	if len(unmatched) != 0 {
		t.Fatalf("unmatched=%v want none", unmatched)
	}
	if !contains(got, "health.checker") {
		t.Fatalf("got=%v want health.checker", got)
	}
}

func TestSelect_RegistryPreferredTieBreak(t *testing.T) {
	// auth.authn offered by a builtin (auth.jwt, in registry) + a plugin (auth.credential, NOT in registry)
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("auth.authn",
			prov("auth", "builtin", "module:auth.jwt", "module:auth.credential")),
	}}
	reg := regWith("auth.jwt") // auth.credential deliberately absent from registry
	got, unmatched := selectModules(inv, []string{"auth.authn"}, reg)
	if len(unmatched) != 0 || len(got) != 1 || got[0] != "auth.jwt" {
		t.Fatalf("got=%v unmatched=%v want [auth.jwt]", got, unmatched)
	}
}

func TestSelect_UnmatchedWhenNoModuleCandidate(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("auth.sso", prov("sso", "external", "wiringHook:sso")), // no module:* → D17
	}}
	reg := regWith()
	got, unmatched := selectModules(inv, []string{"auth.sso"}, reg)
	if len(got) != 0 || len(unmatched) != 1 || unmatched[0] != "auth.sso" {
		t.Fatalf("got=%v unmatched=%v want unmatched=[auth.sso]", got, unmatched)
	}
}

func TestSelect_DeterministicAcrossMapOrder(t *testing.T) {
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		cap("http.routing", prov("http", "builtin", "module:http.server", "module:http.router")),
	}}
	reg := regWith("http.server", "http.router")
	a, _ := selectModules(inv, []string{"http.routing"}, reg)
	b, _ := selectModules(inv, []string{"http.routing"}, reg)
	if !equalStrings(a, b) {
		t.Fatalf("non-deterministic: %v vs %v", a, b)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
