package module

import (
	"testing"
)

// TestTemplateFuncDescriptionsCoversFuncMap verifies that every key in templateFuncMap()
// has a matching TemplateFuncDef entry, and vice versa (with exception for step/trigger
// which are context-bound and not in templateFuncMap).
func TestTemplateFuncDescriptionsCoversFuncMap(t *testing.T) {
	funcMap := templateFuncMap()
	defs := TemplateFuncDescriptions()

	// Build a set of def names (excluding context-bound step/trigger).
	contextBound := map[string]bool{"step": true, "trigger": true}
	defNames := make(map[string]bool, len(defs))
	for _, d := range defs {
		if !contextBound[d.Name] {
			defNames[d.Name] = true
		}
	}

	// Every function in templateFuncMap must have a def.
	for name := range funcMap {
		if !defNames[name] {
			t.Errorf("templateFuncMap has %q but TemplateFuncDescriptions does not", name)
		}
	}

	// Every def (except context-bound) must be in templateFuncMap.
	for name := range defNames {
		if _, ok := funcMap[name]; !ok {
			t.Errorf("TemplateFuncDescriptions has %q but templateFuncMap does not", name)
		}
	}

	// context-bound functions must be in defs.
	for name := range contextBound {
		found := false
		for _, d := range defs {
			if d.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected context-bound function %q in TemplateFuncDescriptions", name)
		}
	}
}

// TestTemplateFuncDescriptionsComplete verifies no empty Name/Signature/Description/Example fields.
func TestTemplateFuncDescriptionsComplete(t *testing.T) {
	defs := TemplateFuncDescriptions()
	if len(defs) == 0 {
		t.Fatal("expected non-empty TemplateFuncDescriptions")
	}
	for _, d := range defs {
		if d.Name == "" {
			t.Error("TemplateFuncDef has empty Name")
		}
		if d.Signature == "" {
			t.Errorf("function %q has empty Signature", d.Name)
		}
		if d.Description == "" {
			t.Errorf("function %q has empty Description", d.Name)
		}
		if d.Example == "" {
			t.Errorf("function %q has empty Example", d.Name)
		}
	}
}
