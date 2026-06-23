package scaffold

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability/assembler"
	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestWire_RealRegistryParity (plan Task 6 verification): the grammar-driven
// wire must reproduce v0.82.0 wire() wiring when the merged grammar comes from
// the REAL engine registry (post glue-gap sweep), not a hand-built synthetic.
// This is the proof that the sweep encoded the v0.82.0 rules faithfully — the
// same MC-parity guard as TestWire_ParityWithV082 but against real builtins.
func TestWire_RealRegistryParity(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	merged, _, err := MergeGrammar(reg, &inventory.Inventory{})
	if err != nil {
		t.Fatalf("MergeGrammar(real registry): %v", err)
	}
	alwaysSelect := []string{"http.router", "health.checker"} // P2: entry-point + /healthz

	cases := []struct {
		name string
		mods []config.ModuleConfig
	}{
		{"db-only", []config.ModuleConfig{{Name: "db", Type: "database.workflow"}}},
		{"with-middleware", []config.ModuleConfig{
			{Name: "db", Type: "database.workflow"},
			{Name: "logging", Type: "http.middleware.logging"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldMods := assembler.WirePublic(append([]config.ModuleConfig{}, tc.mods...))
			oldWF := assembler.HttpWorkflowPublic(oldMods)

			res := GrammarWire(append([]config.ModuleConfig{}, tc.mods...), merged, reg, alwaysSelect)

			if !moduleGraphsEqual(res.Modules, oldMods) {
				t.Fatalf("%s: real-registry module graph drift\ngrammar=%s\nv082=%s",
					tc.name, dumpGraph(res.Modules), dumpGraph(oldMods))
			}
			if !workflowsEqual(res.Workflows, oldWF) {
				t.Fatalf("%s: real-registry workflows.http drift\ngrammar=%#v\nv082=%#v", tc.name, res.Workflows, oldWF)
			}
		})
	}
}

// TestWire_RealRegistryAuthedCRUDRoutes (D4): a real-registry api.handler
// scaffold emits the crud-route routes attached to the router with the auth
// middleware. (The full boot + 401 proof is Task 12; this asserts the grammar
// produces the correct route shape from real builtins.)
func TestWire_RealRegistryAuthedCRUDRoutes(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	merged, _, err := MergeGrammar(reg, &inventory.Inventory{})
	if err != nil {
		t.Fatalf("MergeGrammar: %v", err)
	}
	mods := []config.ModuleConfig{{Name: "api", Type: "api.handler", Config: map[string]any{"resourceName": "orders"}}}
	res := GrammarWire(mods, merged, reg, []string{"http.router", "health.checker"})

	if !hasType(res.Modules, "http.router") || !hasType(res.Modules, "http.middleware.auth") {
		t.Fatalf("api.handler scaffold must ensure router + auth middleware: %s", dumpGraph(res.Modules))
	}
	routes := wfRoutes(res.Workflows)
	if len(routes) != 5 {
		t.Fatalf("want 5 crud routes, got %d: %+v", len(routes), routes)
	}
	for _, r := range routes {
		mw, _ := r["middlewares"].([]string)
		if len(mw) != 1 || mw[0] != "auth" {
			t.Fatalf("real-registry crud route must carry auth middleware: %+v", r)
		}
	}
}
