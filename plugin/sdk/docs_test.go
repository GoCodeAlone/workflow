package sdk

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/plugin"
)

func TestDocGeneratorGeneratePluginDoc(t *testing.T) {
	gen := NewDocGenerator()
	m := &plugin.PluginManifest{
		Name:        "test-plugin",
		Version:     "1.2.3",
		Author:      "Test Author",
		Description: "A plugin for testing documentation generation.",
		License:     "MIT",
		Repository:  "https://github.com/example/test-plugin",
		Tags:        []string{"test", "example"},
		Dependencies: []plugin.Dependency{
			{Name: "dep-plugin", Constraint: ">=1.0.0"},
		},
		Contract: &dynamic.FieldContract{
			RequiredInputs: map[string]dynamic.FieldSpec{
				"message": {Type: dynamic.FieldTypeString, Description: "The message to process"},
			},
			OptionalInputs: map[string]dynamic.FieldSpec{
				"verbose": {Type: dynamic.FieldTypeBool, Description: "Enable verbose output", Default: false},
			},
			Outputs: map[string]dynamic.FieldSpec{
				"result": {Type: dynamic.FieldTypeString, Description: "The processed result"},
			},
		},
	}

	doc := gen.GeneratePluginDoc(m)

	expectedParts := []string{
		"# test-plugin",
		"**Version:** 1.2.3",
		"**Author:** Test Author",
		"**License:** MIT",
		"**Repository:** https://github.com/example/test-plugin",
		"A plugin for testing documentation generation.",
		"test, example",
		"## Dependencies",
		"dep-plugin",
		">=1.0.0",
		"## Required Inputs",
		"message",
		"string",
		"## Optional Inputs",
		"verbose",
		"bool",
		"## Outputs",
		"result",
	}

	for _, part := range expectedParts {
		if !strings.Contains(doc, part) {
			t.Errorf("doc missing expected content %q\n\nFull doc:\n%s", part, doc)
		}
	}
}

func TestDocGeneratorMinimalManifest(t *testing.T) {
	gen := NewDocGenerator()
	m := &plugin.PluginManifest{
		Name:        "minimal",
		Version:     "0.1.0",
		Author:      "Author",
		Description: "Minimal plugin",
	}

	doc := gen.GeneratePluginDoc(m)

	if !strings.Contains(doc, "# minimal") {
		t.Error("expected header")
	}
	if strings.Contains(doc, "## Dependencies") {
		t.Error("should not contain dependencies section for empty deps")
	}
	if strings.Contains(doc, "## Required Inputs") {
		t.Error("should not contain contract sections for nil contract")
	}
}

func TestDocGeneratorGenerateContractDoc(t *testing.T) {
	gen := NewDocGenerator()
	contract := &dynamic.FieldContract{
		RequiredInputs: map[string]dynamic.FieldSpec{
			"input": {Type: dynamic.FieldTypeString, Description: "input field"},
		},
		Outputs: map[string]dynamic.FieldSpec{
			"output": {Type: dynamic.FieldTypeString, Description: "output field"},
		},
	}

	doc := gen.GenerateContractDoc(contract)
	if !strings.Contains(doc, "## Required Inputs") {
		t.Error("missing required inputs section")
	}
	if !strings.Contains(doc, "## Outputs") {
		t.Error("missing outputs section")
	}
}

func TestDocGeneratorNilContract(t *testing.T) {
	gen := NewDocGenerator()
	doc := gen.GenerateContractDoc(nil)
	if doc != "" {
		t.Errorf("expected empty string for nil contract, got %q", doc)
	}
}
