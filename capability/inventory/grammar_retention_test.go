package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocalPluginGrammarRetained asserts a local plugin.json's sibling Grammar
// field reaches Provider.Grammar via the sibling-checkout path (design D1/D26 —
// grammar is a NEW sibling top-level PluginManifest field, ⊥ nesting under
// moduleTypes which is []string).
func TestLocalPluginGrammarRetained(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "workflow-plugin-x")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pj := []byte(`{
		"name": "x",
		"version": "0.1.0",
		"author": "test",
		"description": "test plugin",
		"moduleTypes": ["x.foo"],
		"grammar": {
			"x.foo": {
				"provides": ["x.Foo"],
				"requires": ["http.router"]
			}
		}
	}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), pj, 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := CollectEcosystem(EcosystemOptions{
		RepoRoot:     root,
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	// The grammar must be carried onto the declaring provider. Selection-filter
	// (only-for-selected-set) is the assembler's job (Task 3); retention plumbing
	// serves all providers here (D26).
	var found bool
	for i := range inv.Capabilities {
		for j := range inv.Capabilities[i].Providers {
			p := &inv.Capabilities[i].Providers[j]
			if p.Name != "x" || p.Grammar == nil {
				continue
			}
			g, ok := p.Grammar["x.foo"]
			if !ok {
				continue
			}
			if len(g.Requires) == 1 && g.Requires[0] == "http.router" && len(g.Provides) == 1 && g.Provides[0] == "x.Foo" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("plugin Grammar not retained on Provider (D1): %+v", inv.Capabilities)
	}
}

// TestLocalPluginGrammarSingleSource asserts P5/D26 single-source semantics:
// Grammar is set on first provider insert and not overwritten by a later
// mergeProvider for the same (Name, ReleaseStatus). (A single manifest's grammar
// is constant across its raw capabilities, so the first-insert wins and is stable.)
func TestLocalPluginGrammarSingleSource(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "workflow-plugin-multi")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two module types, one shared grammar map → provider carries grammar once.
	pj := []byte(`{
		"name": "multi",
		"version": "0.1.0",
		"author": "test",
		"description": "test plugin",
		"moduleTypes": ["multi.a", "multi.b"],
		"grammar": {
			"multi.a": {"provides": ["multi.A"]},
			"multi.b": {"requires": ["multi.a"]}
		}
	}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), pj, 0o644); err != nil {
		t.Fatal(err)
	}

	inv, err := CollectEcosystem(EcosystemOptions{
		RepoRoot:     root,
		TaxonomyPath: "testdata/taxonomy.yaml",
		GeneratedAt:  fixedTime,
	})
	if err != nil {
		t.Fatalf("CollectEcosystem: %v", err)
	}

	// Every provider row named "multi" must carry the SAME grammar map (both keys).
	for i := range inv.Capabilities {
		for j := range inv.Capabilities[i].Providers {
			p := &inv.Capabilities[i].Providers[j]
			if p.Name != "multi" {
				continue
			}
			if p.Grammar == nil {
				t.Fatalf("provider %q missing Grammar (P5 single-source): %+v", p.Name, p)
			}
			if _, ok := p.Grammar["multi.a"]; !ok {
				t.Fatalf("provider %q Grammar missing multi.a: %+v", p.Name, p.Grammar)
			}
			if _, ok := p.Grammar["multi.b"]; !ok {
				t.Fatalf("provider %q Grammar missing multi.b: %+v", p.Name, p.Grammar)
			}
		}
	}
}
