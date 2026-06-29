package scaffold

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestScaffold_GrammarMergeContent (D11 + Task 11 dogfood): a plugin's Grammar
// (sibling PluginManifest.Grammar, loaded via the inventory retention path)
// reaches the MERGED grammar with its fields intact, and a grammar-driven wire
// of an api.handler emits the crud-route routes (5 methods, each tagged with the
// auth middleware) with dependsOn wired. This closes the v0.82.0
// requires.plugins-content gap: it asserts merge CONTENT, not just "validates".
//
// The dogfood plugin ships its Grammar in a test-fixture plugin.json (design P7:
// the MC-functional test loads it via the sibling-checkout path WITHOUT a real
// sibling repo).
func TestScaffold_GrammarMergeContent(t *testing.T) {
	// Fixture: a sibling workflow-plugin-authz/plugin.json carrying Grammar for
	// its authz.rbac module type (attaches to the router, requires auth.jwt).
	root := t.TempDir()
	pluginDir := filepath.Join(root, "workflow-plugin-authz")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pj := []byte(`{
		"name": "authz", "version": "0.1.0", "author": "t", "description": "d",
		"moduleTypes": ["authz.rbac"],
		"grammar": {"authz.rbac": {"provides": ["authz.Rbac"], "requires": ["auth.jwt"], "attaches": {"to": "http.router"}}}
	}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), pj, 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := inventory.CollectEcosystem(inventory.EcosystemOptions{
		RepoRoot:     root,
		TaxonomyPath: "../inventory/testdata/taxonomy.yaml",
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	reg := schema.NewModuleSchemaRegistry()
	merged, hooks, err := MergeGrammar(reg, inv)
	if err != nil {
		t.Fatalf("MergeGrammar: %v", err)
	}

	// D11: the dogfood plugin's grammar fields survive the merge (CONTENT).
	rb, ok := merged["authz.rbac"]
	if !ok {
		t.Fatal("dogfood authz.rbac grammar not merged (D11)")
	}
	if len(rb.Provides) != 1 || rb.Provides[0] != "authz.Rbac" {
		t.Errorf("authz.rbac Provides not merged: %+v", rb.Provides)
	}
	if rb.Attaches == nil || rb.Attaches.To != "http.router" {
		t.Errorf("authz.rbac Attaches not merged: %+v", rb.Attaches)
	}
	if len(rb.Requires) != 1 || rb.Requires[0] != "auth.jwt" {
		t.Errorf("authz.rbac Requires not merged: %+v", rb.Requires)
	}
	// Engine auth.jwt Category-B hook collected from the real registry.
	sort.Strings(hooks)
	wantHook := false
	for _, h := range hooks {
		if h == "auth-provider-registration" {
			wantHook = true
		}
	}
	if !wantHook {
		t.Errorf("auth-provider-registration RuntimeHook not collected: %+v", hooks)
	}

	// D4/D18: a grammar-driven api.handler wire emits 5 crud routes, each tagged
	// with the auth middleware instance name, and api.handler dependsOn the router.
	mods := []config.ModuleConfig{{Name: "api", Type: "api.handler", Config: map[string]any{"resourceName": "orders"}}}
	res := GrammarWire(mods, merged, reg, []string{"http.router", "health.checker"})

	api := findModule(res.Modules, "api.handler")
	if api == nil {
		t.Fatal("api.handler missing from wired modules")
	}
	router := findModule(res.Modules, "http.router")
	if router == nil {
		t.Fatal("http.router missing (must be ensured for a functional scaffold)")
	}
	if !containsString(api.DependsOn, router.Name) {
		t.Errorf("api.handler must dependOn the router, got DependsOn=%+v", api.DependsOn)
	}

	section, _ := res.Workflows["http"].(map[string]any)
	if section == nil {
		t.Fatal("workflows.http section not emitted")
	}
	routes, _ := section["routes"].([]map[string]any)
	if len(routes) != 5 {
		t.Fatalf("crud-route want 5 routes, got %d: %+v", len(routes), routes)
	}
	methods := map[string]int{}
	for _, r := range routes {
		mw, _ := r["middlewares"].([]string)
		if len(mw) != 1 || mw[0] != "auth" {
			t.Errorf("crud route missing auth middleware (D18): %+v", r)
		}
		if m, ok := r["method"].(string); ok {
			methods[m]++
		}
	}
	if methods["GET"] != 2 || methods["POST"] != 1 || methods["PUT"] != 1 || methods["DELETE"] != 1 {
		t.Errorf("crud-route method distribution wrong: %+v", methods)
	}
}

// findModule returns the first module of the given type, or nil.
func findModule(mods []config.ModuleConfig, typ string) *config.ModuleConfig {
	for i := range mods {
		if mods[i].Type == typ {
			return &mods[i]
		}
	}
	return nil
}

func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
