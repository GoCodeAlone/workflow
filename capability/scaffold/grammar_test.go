package scaffold

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/capability/inventory"
	"github.com/GoCodeAlone/workflow/schema"
)

// TestMerge_DeterministicAcrossMapOrder asserts V10: the same {set, grammar
// sources} produce byte-identical merged-grammar JSON across runs regardless of
// Go map iteration order. Engine-first seeding + (provider, type) sort = stable.
func TestMerge_DeterministicAcrossMapOrder(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "http.router", Requires: []string{"http.server"}})
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p1", Grammar: map[string]schema.GrammarDecl{"a": {Provides: []string{"a.Svc"}}}},
			{Name: "p2", Grammar: map[string]schema.GrammarDecl{"b": {Provides: []string{"b.Svc"}}}},
		}},
	}}
	g1, _, err := MergeGrammar(r, inv)
	if err != nil {
		t.Fatal(err)
	}
	g2, _, err := MergeGrammar(r, inv)
	if err != nil {
		t.Fatal(err)
	}
	if !equalMerged(t, g1, g2) {
		t.Fatal("non-deterministic merge (V10)")
	}
}

// TestMerge_CycleDetected asserts D17: A.requires B, B.requires A → Tarjan SCC
// cycle → named-offender error.
func TestMerge_CycleDetected(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "A", Requires: []string{"B"}})
	r.Register(&schema.ModuleSchema{Type: "B", Requires: []string{"A"}})
	_, _, err := MergeGrammar(r, &inventory.Inventory{})
	if err == nil {
		t.Fatal("want cycle error (D17)")
	}
}

// TestMerge_ConflictingAttachesIsError asserts D22: two plugins declare grammar
// for the same type with different Attaches.To → scalar per-field conflict error.
func TestMerge_ConflictingAttachesIsError(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p1", Grammar: map[string]schema.GrammarDecl{"m": {Attaches: &schema.AttachSpec{To: "a"}}}},
			{Name: "p2", Grammar: map[string]schema.GrammarDecl{"m": {Attaches: &schema.AttachSpec{To: "b"}}}},
		}},
	}}
	_, _, err := MergeGrammar(r, inv)
	if err == nil {
		t.Fatal("want scalar conflict error (D22)")
	}
}

// TestMerge_IdenticalReDeclarationIsNoOp asserts D22's no-op half: two plugins
// declare IDENTICAL grammar for the same type → union/merge, no conflict.
func TestMerge_IdenticalReDeclarationIsNoOp(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "a"}) // Attaches.To target must resolve (D23)
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p1", Grammar: map[string]schema.GrammarDecl{"m": {Attaches: &schema.AttachSpec{To: "a"}, Provides: []string{"m.Svc"}}}},
			{Name: "p2", Grammar: map[string]schema.GrammarDecl{"m": {Attaches: &schema.AttachSpec{To: "a"}, Provides: []string{"m.Other"}}}},
		}},
	}}
	merged, _, err := MergeGrammar(r, inv)
	if err != nil {
		t.Fatalf("identical re-declaration must be a no-op (D22): %v", err)
	}
	g := merged["m"]
	if g.Attaches == nil || g.Attaches.To != "a" {
		t.Fatalf("Attaches not preserved on identical re-decl: %+v", g)
	}
	// Provides list-union (sorted): both contributes merged.
	if len(g.Provides) != 2 {
		t.Fatalf("Provides not union-merged: %+v", g.Provides)
	}
}

// TestMerge_PhantomAttachesIsLoadError asserts D23: Attaches.To references a type
// not present in the merged grammar (or engine registry) → grammar-load error.
func TestMerge_PhantomAttachesIsLoadError(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p", Grammar: map[string]schema.GrammarDecl{"m": {Attaches: &schema.AttachSpec{To: "ghost"}}}},
		}},
	}}
	_, _, err := MergeGrammar(r, inv)
	if err == nil {
		t.Fatal("want phantom-target load error (D23)")
	}
}

// TestMerge_PhantomRequiresIsLoadError asserts D23 for Requires edges too.
func TestMerge_PhantomRequiresIsLoadError(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p", Grammar: map[string]schema.GrammarDecl{"m": {Requires: []string{"nope"}}}},
		}},
	}}
	_, _, err := MergeGrammar(r, inv)
	if err == nil {
		t.Fatal("want phantom-target load error for Requires (D23)")
	}
}

// TestMerge_EmptyGrammarNoOp asserts D8: types/providers with no grammar
// contribute nothing and produce no error. A bare (no-grammar) builtin is
// excluded from merged; a no-grammar plugin adds nothing. merged is empty.
// (GrammarWire receives the registry separately for type lookups, so merged
// carries only grammar-bearing types.)
func TestMerge_EmptyGrammarNoOp(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "http.server"}) // bare: no grammar
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{{Name: "p"}}}, // no Grammar
	}}
	merged, hooks, err := MergeGrammar(r, inv)
	if err != nil || len(merged) != 0 || len(hooks) != 0 {
		t.Fatalf("empty-grammar no-op failed: merged=%+v hooks=%v err=%v", merged, hooks, err)
	}
}

// TestMerge_EngineWinsDropsPluginGrammar asserts D25: a plugin's grammar for an
// engine-registered type is ignored (engine wins), NOT a conflict. The engine's
// grammar for that type is what survives.
func TestMerge_EngineWinsDropsPluginGrammar(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "engine.type", Provides: []string{"engine.Svc"}})
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p", Grammar: map[string]schema.GrammarDecl{"engine.type": {Provides: []string{"plugin.Svc"}}}},
		}},
	}}
	merged, _, err := MergeGrammar(r, inv)
	if err != nil {
		t.Fatalf("engine-wins must not error (D25): %v", err)
	}
	g, ok := merged["engine.type"]
	if !ok {
		t.Fatal("engine grammar for engine.type must survive (D25)")
	}
	// Engine Provides wins; plugin Provides ignored (not unioned).
	if len(g.Provides) != 1 || g.Provides[0] != "engine.Svc" {
		t.Fatalf("engine-wins failed: %+v want [engine.Svc]", g.Provides)
	}
}

// TestMerge_CollectsRuntimeHooks asserts Category B RuntimeHooks are collected
// (sorted, de-duplicated) for NEXT_STEPS documentation (D3).
func TestMerge_CollectsRuntimeHooks(t *testing.T) {
	r := schema.NewModuleSchemaRegistry()
	r.Register(&schema.ModuleSchema{Type: "auth.jwt", RuntimeHooks: []string{"auth-provider-registration"}})
	inv := &inventory.Inventory{Capabilities: []inventory.Capability{
		{ID: "x", Providers: []inventory.Provider{
			{Name: "p", Grammar: map[string]schema.GrammarDecl{"health.checker": {RuntimeHooks: []string{"health-endpoints"}}}},
		}},
	}}
	_, hooks, err := MergeGrammar(r, inv)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"auth-provider-registration": true, "health-endpoints": true}
	if len(hooks) != 2 || !want[hooks[0]] || !want[hooks[1]] {
		t.Fatalf("RuntimeHooks not collected/sorted: %+v", hooks)
	}
}

// equalMerged proves determinism: marshal both merged grammars to JSON (map keys
// sorted deterministically by encoding/json) and byte-compare.
func equalMerged(t *testing.T, a, b MergedGrammar) bool {
	t.Helper()
	aj, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	bj, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(aj, bj)
}
