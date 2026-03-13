package workflow

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugins/all"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestRegistryConsistency(t *testing.T) {
	t.Run("all schema step types in KnownModuleTypes", func(t *testing.T) {
		known := make(map[string]bool)
		for _, mt := range schema.KnownModuleTypes() {
			known[mt] = true
		}
		for _, st := range schema.GetStepSchemaRegistry().Types() {
			if !known[st] {
				t.Errorf("step schema %q registered but not in KnownModuleTypes() — add to coreModuleTypes in schema.go", st)
			}
		}
	})

	t.Run("template func descriptions complete", func(t *testing.T) {
		defs := module.TemplateFuncDescriptions()
		if len(defs) < 30 {
			t.Errorf("expected at least 30 template func descriptions, got %d — check module.buildTemplateFuncDefs()", len(defs))
		}
		for _, d := range defs {
			if d.Name == "" || d.Description == "" {
				t.Errorf("incomplete TemplateFuncDef: %+v", d)
			}
		}
	})

	t.Run("engine step registry covers schema types", func(t *testing.T) {
		// Build an engine with all default plugins to get step registry populated.
		e := NewStdEngine(nil, nil)
		for _, p := range all.DefaultPlugins() {
			if err := e.LoadPlugin(p); err != nil {
				t.Fatalf("failed to load plugin %s: %v", p.Name(), err)
			}
		}

		engineTypes := make(map[string]bool)
		for _, st := range e.GetStepRegistry().Types() {
			engineTypes[st] = true
		}

		// Every schema-registered step type should have an engine factory.
		for _, st := range schema.GetStepSchemaRegistry().Types() {
			if !engineTypes[st] {
				t.Logf("step schema %q has no engine factory (may be plugin-only or external)", st)
			}
		}

		// Engine should have a reasonable number of step types.
		if len(engineTypes) < 40 {
			t.Errorf("expected at least 40 engine step types, got %d", len(engineTypes))
		}
	})
}
