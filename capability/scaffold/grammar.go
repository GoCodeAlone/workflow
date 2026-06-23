// Package scaffold composes functional workflow scaffolds via Assembly Grammar.
package scaffold

import (
	"fmt"
	"os"
	"slices"
	"sort"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
)

// MergedGrammar is the per-module-type resolved grammar (engine ∪ plugins).
type MergedGrammar map[string]schema.GrammarDecl

// MergeGrammar merges engine ModuleSchemaRegistry grammar ∪ plugin Provider.Grammar.
// Order: engine-first, then plugins by (provider-name, module-type). Conflict
// policy (D2/D16/D17/D22/D23):
//   - scalar-field conflict = error (per-field across structs); list-field = union;
//   - engine wins for engine-registered types (D25);
//   - phantom Attaches/Requires target = load-error (D23);
//   - circular Requires = error (D17, via Tarjan SCC).
//
// Returns the merged grammar + the sorted, de-duplicated union of Category-B
// RuntimeHooks (for NEXT_STEPS documentation, D3).
func MergeGrammar(reg *schema.ModuleSchemaRegistry, inv *inventory.Inventory) (MergedGrammar, []string, error) {
	merged := MergedGrammar{}

	// 1. Engine builtins (D25: engine wins — seeded first; a plugin re-declaring
	//    grammar for an engine-registered type is ignored, NOT a conflict).
	engineTypes := map[string]bool{}
	for _, t := range reg.Types() { // reg.Types() is []string (retro #1: range two-value)
		s := reg.Get(t)
		if s == nil || !hasGrammar(s) {
			continue
		}
		merged[t] = fromSchema(s)
		engineTypes[t] = true
	}

	// 2. Plugin grammar, sorted by (provider-name, module-type) for V10 determinism.
	type pluginEntry struct {
		provider string
		typ      string
		decl     schema.GrammarDecl
	}
	var entries []pluginEntry
	for i := range inv.Capabilities {
		for j := range inv.Capabilities[i].Providers {
			p := &inv.Capabilities[i].Providers[j]
			if p.Grammar == nil {
				continue
			}
			for typ, decl := range p.Grammar {
				entries = append(entries, pluginEntry{p.Name, typ, decl})
			}
		}
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].provider != entries[b].provider {
			return entries[a].provider < entries[b].provider
		}
		return entries[a].typ < entries[b].typ
	})
	for i := range entries {
		e := &entries[i]
		if engineTypes[e.typ] {
			// P9/P15: surface the drop (⊥ silent field-loss) so plugin authors see it.
			fmt.Fprintf(os.Stderr, "scaffold: plugin %q grammar for engine type %q ignored (engine wins, D25)\n", e.provider, e.typ)
			continue
		}
		existing, ok := merged[e.typ]
		if !ok {
			merged[e.typ] = e.decl
			continue
		}
		// merge field-by-field; scalar conflict = error (D22)
		merged2, err := mergeDecl(existing, e.decl, e.typ)
		if err != nil {
			return nil, nil, err
		}
		merged[e.typ] = merged2
	}

	// 3. Validate references (D23: phantom target = load-error) + collect RuntimeHooks.
	allTypes := map[string]bool{}
	for t := range merged {
		allTypes[t] = true
	}
	for _, t := range reg.Types() { // a grammar target may be a non-grammar type
		allTypes[t] = true
	}
	seenHooks := map[string]bool{}
	var hooks []string
	for typ, decl := range merged {
		for _, tgt := range refTargets(decl) {
			if !allTypes[tgt] {
				return nil, nil, fmt.Errorf("grammar: %q references unknown type %q (D23 phantom target)", typ, tgt)
			}
		}
		for _, h := range decl.RuntimeHooks {
			if !seenHooks[h] {
				seenHooks[h] = true
				hooks = append(hooks, h)
			}
		}
	}
	sort.Strings(hooks)

	// 4. Cycle detection (D17): Tarjan SCC over Requires-edges at merge time
	//    (selection-independent). Recursion bounded by the visited indices set.
	if err := detectCycles(merged); err != nil {
		return nil, nil, err
	}
	return merged, hooks, nil
}

// mergeDecl merges two GrammarDecls for the same type (D22): list-fields union;
// scalar/struct fields conflict-if-differ (per-field); identical re-decl = no-op.
func mergeDecl(a, b schema.GrammarDecl, typ string) (schema.GrammarDecl, error) {
	out := a
	out.Provides = union(out.Provides, b.Provides)
	out.Requires = union(out.Requires, b.Requires)
	out.RouteMiddlewares = union(out.RouteMiddlewares, b.RouteMiddlewares)
	out.RuntimeHooks = union(out.RuntimeHooks, b.RuntimeHooks)
	// Attaches (struct): conflict if both set and differ.
	if b.Attaches != nil {
		if out.Attaches == nil {
			out.Attaches = b.Attaches
		} else if *out.Attaches != *b.Attaches {
			return out, fmt.Errorf("grammar: conflicting Attaches for %q (D22)", typ)
		}
	}
	// Fragment (struct): conflict if both set and differ. FragmentSpec contains
	// a []string (Fields), so it is not comparable with != — compare by value.
	if b.Fragment != nil {
		if out.Fragment == nil {
			out.Fragment = b.Fragment
		} else if !fragmentEqual(out.Fragment, b.Fragment) {
			return out, fmt.Errorf("grammar: conflicting Fragment for %q (D22)", typ)
		}
	}
	return out, nil
}

// fragmentEqual compares two FragmentSpecs by value (Kind + Fields). nil-safe.
func fragmentEqual(a, b *schema.FragmentSpec) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	return stringSliceEqual(a.Fields, b.Fields)
}

// stringSliceEqual is an order-sensitive []string comparison.
func stringSliceEqual(a, b []string) bool {
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

// refTargets returns all type-references in a decl (Requires + Attaches.To) for
// the phantom-check (D23).
func refTargets(d schema.GrammarDecl) []string {
	out := append([]string{}, d.Requires...)
	if d.Attaches != nil {
		out = append(out, d.Attaches.To)
	}
	return out
}

// detectCycles runs Tarjan SCC over Requires-edges; any SCC of size>1 (or a
// self-loop) is a cycle → named-offender error (D17).
func detectCycles(g MergedGrammar) error {
	index := 0
	stack := []string{}
	onStack := map[string]bool{}
	indices := map[string]int{}
	low := map[string]int{}
	var cycle []string

	var sc func(v string)
	sc = func(v string) {
		indices[v], low[v] = index, index
		index++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range g[v].Requires {
			if w == v {
				// self-loop = a cycle (P11: set + continue, ⊥ return which would
				// skip SCC finalization; safe because the outer loop breaks on cycle).
				if !slices.Contains(cycle, v) {
					cycle = append(cycle, v)
				}
				continue
			}
			if _, seen := indices[w]; !seen {
				if _, ok := g[w]; ok { // only recurse into grammar-bearing nodes
					sc(w)
					low[v] = minI(low[v], low[w])
				}
			} else if onStack[w] {
				low[v] = minI(low[v], indices[w])
			}
		}
		if low[v] == indices[v] {
			var comp []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				comp = append(comp, w)
				if w == v {
					break
				}
			}
			if len(comp) > 1 { // SCC >1 node = cycle
				for _, n := range comp {
					if !slices.Contains(cycle, n) {
						cycle = append(cycle, n)
					}
				}
			}
		}
	}

	// iterate types in sorted order for deterministic offender-naming
	types := make([]string, 0, len(g))
	for t := range g {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		if _, seen := indices[t]; !seen {
			sc(t)
		}
		if len(cycle) > 0 {
			break
		}
	}
	if len(cycle) > 0 {
		sort.Strings(cycle)
		return fmt.Errorf("grammar: circular Requires detected among %v (D17)", cycle)
	}
	return nil
}

// --- helpers ---

func hasGrammar(s *schema.ModuleSchema) bool {
	d := fromSchema(s)
	return len(d.Provides) > 0 || len(d.Requires) > 0 || d.Attaches != nil ||
		d.Fragment != nil || len(d.RouteMiddlewares) > 0 || len(d.RuntimeHooks) > 0
}

func fromSchema(s *schema.ModuleSchema) schema.GrammarDecl {
	return schema.GrammarDecl{
		Provides:         s.Provides,
		Requires:         s.Requires,
		Attaches:         s.Attaches,
		RouteMiddlewares: s.RouteMiddlewares,
		Fragment:         s.Fragment,
		RuntimeHooks:     s.RuntimeHooks,
	}
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range append(a, b...) {
		if x == "" || seen[x] {
			continue
		}
		seen[x] = true
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}
