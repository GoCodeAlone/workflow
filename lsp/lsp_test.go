package lsp

import (
	"fmt"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

const testYAML = `modules:
  - name: server
    type: http.server
    config:
      address: :8080
  - name: router
    type: http.router
    dependsOn:
      - server
  - name: mymod
    type: nonexistent.module

triggers:
  http:
    port: 8080
  badtrigger:
    foo: bar
`

// TestRegistry_ModuleTypes checks that the registry loads module types.
func TestRegistry_ModuleTypes(t *testing.T) {
	reg := NewRegistry()
	if len(reg.ModuleTypes) == 0 {
		t.Fatal("registry has no module types")
	}
	// http.server must be registered.
	info, ok := reg.ModuleTypes["http.server"]
	if !ok {
		t.Fatal("http.server not in registry")
	}
	if info.Type != "http.server" {
		t.Errorf("unexpected type: %q", info.Type)
	}
	// Should have config keys.
	if len(info.ConfigKeys) == 0 {
		t.Error("http.server should have config keys")
	}
}

// TestRegistry_StepTypes checks step type registry.
func TestRegistry_StepTypes(t *testing.T) {
	reg := NewRegistry()
	if len(reg.StepTypes) == 0 {
		t.Fatal("registry has no step types")
	}
	if _, ok := reg.StepTypes["step.set"]; !ok {
		t.Error("step.set not in step type registry")
	}
}

// TestRegistry_TriggerTypes checks trigger type registry.
func TestRegistry_TriggerTypes(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.TriggerTypes["http"]; !ok {
		t.Error("http trigger not in registry")
	}
	if _, ok := reg.TriggerTypes["schedule"]; !ok {
		t.Error("schedule trigger not in registry")
	}
}

// TestDocumentStore_SetGet checks basic document store operations.
func TestDocumentStore_SetGet(t *testing.T) {
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)
	if doc == nil {
		t.Fatal("Set returned nil")
	}
	got := store.Get("file:///test.yaml")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Content != testYAML {
		t.Error("content mismatch")
	}
}

// TestDocumentStore_ParseYAML checks that YAML is parsed on Set.
func TestDocumentStore_ParseYAML(t *testing.T) {
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)
	if doc.Node == nil {
		t.Fatal("document should have parsed YAML node")
	}
}

// TestDiagnostics_UnknownModuleType checks that unknown module types produce errors.
func TestDiagnostics_UnknownModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	diags := Diagnostics(reg, doc)

	found := false
	for _, d := range diags {
		if containsStr(d.Message, "nonexistent.module") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diagnostic for nonexistent.module, got %d diags: %v", len(diags), diagMessages(diags))
	}
}

// TestDiagnostics_UnknownTriggerType checks that unknown trigger types produce errors.
func TestDiagnostics_UnknownTriggerType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	diags := Diagnostics(reg, doc)

	found := false
	for _, d := range diags {
		if containsStr(d.Message, "badtrigger") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diagnostic for badtrigger, got: %v", diagMessages(diags))
	}
}

// TestDiagnostics_ValidConfig checks no spurious errors on valid config.
func TestDiagnostics_ValidConfig(t *testing.T) {
	validYAML := `modules:
  - name: server
    type: http.server
    config:
      address: :8080

triggers:
  http:
    port: 8080
`
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///valid.yaml", validYAML)
	diags := Diagnostics(reg, doc)

	// Should have no errors (warnings for unknown config keys are ok but
	// there should be no unknown type errors).
	for _, d := range diags {
		if d.Severity != nil && *d.Severity == 1 { // DiagnosticSeverityError
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

// TestCompletions_ModuleType checks that module type completions are returned.
func TestCompletions_ModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:   SectionModules,
		FieldName: "type",
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("no completions for module type")
	}
	found := false
	for _, item := range items {
		if item.Label == "http.server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("http.server not in module type completions")
	}
}

// TestCompletions_TopLevel checks top-level key completions.
func TestCompletions_TopLevel(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", "")

	ctx := PositionContext{Section: SectionTopLevel}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("no top-level completions")
	}

	labels := make(map[string]bool, len(items))
	for _, item := range items {
		labels[item.Label] = true
	}
	for _, expected := range []string{"modules", "workflows", "triggers"} {
		if !labels[expected] {
			t.Errorf("missing top-level key completion: %q", expected)
		}
	}
}

// TestHover_ModuleType checks hover for module types.
func TestHover_ModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:    SectionModules,
		ModuleType: "http.server",
		FieldName:  "type",
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for http.server")
	}
}

// TestContextAt checks basic context detection.
func TestContextAt(t *testing.T) {
	yaml := `modules:
  - name: server
    type: http.server
`
	ctx := ContextAt(yaml, 2, 10)
	// Line 2 is "    type: http.server" — should detect modules section.
	if ctx.Section != SectionModules {
		t.Errorf("expected SectionModules, got %q", ctx.Section)
	}
}

// TestTemplateFunctions checks the template functions list.
func TestTemplateFunctions(t *testing.T) {
	fns := templateFunctions()
	if len(fns) == 0 {
		t.Fatal("no template functions")
	}
	foundUUID := false
	for _, f := range fns {
		if f == "uuidv4" {
			foundUUID = true
			break
		}
	}
	if !foundUUID {
		t.Error("uuidv4 not in template functions")
	}
}

// helpers

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func diagMessages(diags []protocol.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = fmt.Sprintf("[%d] %s", i, d.Message)
	}
	return msgs
}

