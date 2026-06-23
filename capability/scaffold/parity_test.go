package scaffold

import (
	"sort"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/assembler"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// engineGrammarForParity encodes the EXACT v0.82.0 in-code wire() rules as a
// MergedGrammar, so MC-parity is meaningful (the grammar reproduces what wire()
// hard-codes): http.router requires + attaches to http.server (emitting
// workflows.http); http.middleware.* attaches to the router; health.checker is
// always-selected (the /healthz binding precondition, D15). http.server is the
// entry point pulled in by http.router.Requires.
func engineGrammarForParity() MergedGrammar {
	return MergedGrammar{
		"http.server": {Provides: []string{"http.Server"}},
		"http.router": {
			Requires: []string{"http.server"},
			Attaches: &schema.AttachSpec{To: "http.server", Emit: "workflows.http"},
		},
		"http.middleware.auth":    {Attaches: &schema.AttachSpec{To: "http.router"}},
		"http.middleware.logging": {Attaches: &schema.AttachSpec{To: "http.router"}},
		// health.checker is always-select (P2), not grammar-bearing here.
	}
}

// TestWire_ParityWithV082 is the MC-parity regression guard (D6): the
// grammar-driven wire must reproduce v0.82.0's in-code wire() WIRING — same set
// of module types, same DependsOn (compared as types), and the same
// workflows.http entry-point section — across several fixed input sets.
// Instance names are intentionally NOT compared (runtime resolves by type; the
// grammar's defaultName differs from wire()'s hand-picked names).
//
// Config bytes are NOT asserted equal: GrammarWire intentionally enriches
// materialized modules with the registry's DefaultConfig (P6) where v0.82.0's
// wire() appended bare modules; that enrichment is an accepted delta (the
// regression risk is the wiring graph, not config cosmetics).
func TestWire_ParityWithV082(t *testing.T) {
	eng := engineGrammarForParity()
	reg := schema.NewModuleSchemaRegistry()
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
		{"router-present", []config.ModuleConfig{
			{Name: "my-router", Type: "http.router"},
			{Name: "db", Type: "database.workflow"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			oldMods := assembler.WirePublic(append([]config.ModuleConfig{}, tc.mods...))
			oldWF := assembler.HttpWorkflowPublic(oldMods)

			res := GrammarWire(append([]config.ModuleConfig{}, tc.mods...), eng, reg, alwaysSelect)

			if !moduleGraphsEqual(res.Modules, oldMods) {
				t.Fatalf("%s: module graph drift (types/names/dependsOn)\ngrammar=%s\nv082=%s",
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

// moduleGraphsEqual compares two module lists by their TYPE graph: same set of
// types, and for each type the same set of depended-on TYPES (each DependsOn
// NAME resolved to its module type via a name→type index). Instance names are an
// implementation detail — runtime resolves services by type — so parity guards
// the type graph, not names (wire() names health.checker "health" while the
// grammar defaultName yields "checker"; both are the same type). Order-
// independent. Config excluded (P6 enrichment delta).
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

// typeGraph maps each module type → sorted list of depended-on types (resolving
// DependsOn names to types). Assumes one instance per type (true for scaffolds:
// user modules are unique types; materialized modules are one-per-type).
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

// workflowsEqual compares the workflows.http entry-point section (server/router
// names) — order-independent map comparison.
func workflowsEqual(a, b map[string]any) bool {
	return deepEqual(a, b)
}

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

// deepEqual is a small any-comparator for the workflow maps: maps compare
// order-independent (key set + values); slices ([]map[string]any, []string)
// compare IN ORDER — route order is significant for crud-route fragments.
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
