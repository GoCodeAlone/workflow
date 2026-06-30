package assembler

import (
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/capability/scaffold"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// engineGrammarForParity encodes the EXACT v0.82.0 in-code wire() rules as a
// scaffold.MergedGrammar, so MC-parity is meaningful (the grammar reproduces what
// wire() hard-codes). The real registry carries the same rules after the glue
// sweep (Task 6), so TestWire_RealRegistryParity uses the real registry instead.
func engineGrammarForParity() scaffold.MergedGrammar {
	return scaffold.MergedGrammar{
		"http.server": {Provides: []string{"http.Server"}},
		"http.router": {
			Requires: []string{"http.server"},
			Attaches: &schema.AttachSpec{To: "http.server", Emit: "workflows.http"},
		},
		"http.middleware.auth":    {Attaches: &schema.AttachSpec{To: "http.router"}},
		"http.middleware.logging": {Attaches: &schema.AttachSpec{To: "http.router"}},
	}
}

// TestWire_ParityWithV082 is the MC-parity regression guard (D6): the
// grammar-driven wire must reproduce v0.82.0's in-code wire() WIRING — same set
// of module types, same DependsOn (compared as types), and the same
// workflows.http entry-point section — across several input sets. Instance names
// are intentionally NOT compared (runtime resolves by type; the grammar's
// defaultName differs from wire()'s hand-picked names). Config bytes are NOT
// compared: GrammarWire enriches materialized modules with DefaultConfig (P6)
// where wire() appended bare modules — the regression risk is the wiring graph.
//
// This test lives in package assembler (not scaffold) because it compares
// scaffold.GrammarWire against the v0.82.0 wire()/httpWorkflow() parity
// reference, avoiding an assembler<->scaffold test import cycle now that the
// assembler routes prod through scaffold.GrammarWire.
func TestWire_ParityWithV082(t *testing.T) {
	eng := engineGrammarForParity()
	reg := schema.NewModuleSchemaRegistry()
	alwaysSelect := []string{"http.router", "health.checker"}

	cases := []struct {
		name string
		mods []config.ModuleConfig
	}{
		{"db-only", []config.ModuleConfig{{Name: "db", Type: "database.workflow"}}},
		{"with-middleware", []config.ModuleConfig{
			{Name: "db", Type: "database.workflow"},
			{Name: "logging", Type: "http.middleware.logging"},
		}},
		{"router-present", []config.ModuleConfig{
			{Name: "my-router", Type: "http.router"},
			{Name: "db", Type: "database.workflow"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldMods := wire(append([]config.ModuleConfig{}, tc.mods...))
			oldWF := httpWorkflow(oldMods)

			res := scaffold.GrammarWire(append([]config.ModuleConfig{}, tc.mods...), eng, reg, alwaysSelect)

			if !moduleGraphsEqual(res.Modules, oldMods) {
				t.Fatalf("%s: module graph drift\ngrammar=%s\nv082=%s",
					tc.name, dumpGraph(res.Modules), dumpGraph(oldMods))
			}
			if !workflowsEqual(res.Workflows, oldWF) {
				t.Fatalf("%s: workflows.http drift\ngrammar=%#v\nv082=%#v", tc.name, res.Workflows, oldWF)
			}
			if len(res.GlueGaps) != 0 {
				t.Fatalf("%s: unexpected glue-gaps: %+v", tc.name, res.GlueGaps)
			}
		})
	}
}

// TestWire_RealRegistryParity (Task 6 verification): the grammar wire reproduces
// v0.82.0 wiring when the merged grammar comes from the REAL registry (post glue
// sweep), not a synthetic map.
func TestWire_RealRegistryParity(t *testing.T) {
	reg := schema.NewModuleSchemaRegistry()
	merged, _, err := scaffold.MergeGrammar(reg, &inventory.Inventory{})
	if err != nil {
		t.Fatalf("MergeGrammar(real registry): %v", err)
	}
	alwaysSelect := []string{"http.router", "health.checker"}

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
			oldMods := wire(append([]config.ModuleConfig{}, tc.mods...))
			oldWF := httpWorkflow(oldMods)
			res := scaffold.GrammarWire(append([]config.ModuleConfig{}, tc.mods...), merged, reg, alwaysSelect)
			if !moduleGraphsEqual(res.Modules, oldMods) {
				t.Fatalf("%s: real-registry graph drift\ngrammar=%s\nv082=%s", tc.name, dumpGraph(res.Modules), dumpGraph(oldMods))
			}
			if !workflowsEqual(res.Workflows, oldWF) {
				t.Fatalf("%s: real-registry workflows.http drift\ngrammar=%#v\nv082=%#v", tc.name, res.Workflows, oldWF)
			}
		})
	}
}

// --- comparison helpers (type-graph based; instance names are an impl detail) ---

func moduleGraphsEqual(a, b []config.ModuleConfig) bool {
	ga := typeGraph(a)
	gb := typeGraph(b)
	if len(ga) != len(gb) {
		return false
	}
	for t, depsA := range ga {
		depsB, ok := gb[t]
		if !ok || !depSetsEqual(depsA, depsB) {
			return false
		}
	}
	return true
}

func typeGraph(mods []config.ModuleConfig) map[string][]string {
	nameToType := make(map[string]string, len(mods))
	for _, m := range mods {
		nameToType[m.Name] = m.Type
	}
	out := make(map[string][]string, len(mods))
	for _, m := range mods {
		var deps []string
		for _, d := range m.DependsOn {
			if t, ok := nameToType[d]; ok {
				deps = append(deps, t)
			}
		}
		sort.Strings(deps)
		out[m.Type] = deps
	}
	return out
}

func depSetsEqual(a, b []string) bool {
	sa := append([]string{}, a...)
	sb := append([]string{}, b...)
	sort.Strings(sa)
	sort.Strings(sb)
	if len(sa) != len(sb) {
		return false
	}
	for i := range sa {
		if sa[i] != sb[i] {
			return false
		}
	}
	return true
}

func workflowsEqual(a, b map[string]any) bool { return deepEqual(a, b) }

func dumpGraph(mods []config.ModuleConfig) string {
	out := ""
	for _, m := range mods {
		deps := append([]string{}, m.DependsOn...)
		sort.Strings(deps)
		out += m.Name + "(" + m.Type + ")<-[" + joinDeps(deps) + "] "
	}
	return out
}

func joinDeps(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

func deepEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			if !deepEqual(v, bv[k]) {
				return false
			}
		}
		return true
	case []map[string]any:
		bv, ok := b.([]map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !deepEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case []string:
		bv, ok := b.([]string)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if av[i] != bv[i] {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
