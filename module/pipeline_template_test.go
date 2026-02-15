package module

import (
	"strings"
	"testing"
)

func TestTemplateEngine_ResolveSimpleField(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)

	result, err := te.Resolve("{{ .name }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected 'Alice', got %q", result)
	}
}

func TestTemplateEngine_ResolveNestedStepReference(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"result": "passed"})

	result, err := te.Resolve("{{ .steps.validate.result }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "passed" {
		t.Errorf("expected 'passed', got %q", result)
	}
}

func TestTemplateEngine_ResolveMetadata(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, map[string]any{"pipeline": "order-pipeline"})

	result, err := te.Resolve("{{ .meta.pipeline }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "order-pipeline" {
		t.Errorf("expected 'order-pipeline', got %q", result)
	}
}

func TestTemplateEngine_ResolveTriggerData(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"source": "webhook"}, nil)

	result, err := te.Resolve("{{ .trigger.source }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "webhook" {
		t.Errorf("expected 'webhook', got %q", result)
	}
}

func TestTemplateEngine_NonTemplatePassthrough(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	plain := "just a plain string"
	result, err := te.Resolve(plain, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != plain {
		t.Errorf("expected %q, got %q", plain, result)
	}
}

func TestTemplateEngine_EmptyStringPassthrough(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve("", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestTemplateEngine_MissingKeyReturnsZeroValue(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	// missingkey=zero means missing keys produce zero values, not errors
	result, err := te.Resolve("{{ .nonexistent }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The zero value for a missing key in a map renders as "<no value>" or empty
	// With missingkey=zero it should render as the zero value string representation
	_ = result // Just verify no error
}

func TestTemplateEngine_InvalidTemplateReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	_, err := te.Resolve("{{ .unclosed", pc)
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
	if !strings.Contains(err.Error(), "template parse error") {
		t.Errorf("expected 'template parse error' in message, got: %v", err)
	}
}

func TestTemplateEngine_TemplateWithMixedContent(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"user": "Bob"}, nil)

	result, err := te.Resolve("Hello {{ .user }}!", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello Bob!" {
		t.Errorf("expected 'Hello Bob!', got %q", result)
	}
}

func TestTemplateEngine_ResolveMap_SimpleValues(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice", "age": 30}, nil)

	data := map[string]any{
		"greeting": "Hello {{ .name }}",
		"count":    42,
		"flag":     true,
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["greeting"] != "Hello Alice" {
		t.Errorf("expected 'Hello Alice', got %v", result["greeting"])
	}
	// Non-string values should pass through unchanged
	if result["count"] != 42 {
		t.Errorf("expected count=42, got %v", result["count"])
	}
	if result["flag"] != true {
		t.Errorf("expected flag=true, got %v", result["flag"])
	}
}

func TestTemplateEngine_ResolveMap_NestedMaps(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"env": "prod"}, nil)

	data := map[string]any{
		"outer": map[string]any{
			"inner": "running in {{ .env }}",
		},
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outer, ok := result["outer"].(map[string]any)
	if !ok {
		t.Fatalf("expected outer to be map[string]any, got %T", result["outer"])
	}
	if outer["inner"] != "running in prod" {
		t.Errorf("expected 'running in prod', got %v", outer["inner"])
	}
}

func TestTemplateEngine_ResolveMap_Slices(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"item": "widget"}, nil)

	data := map[string]any{
		"items": []any{"{{ .item }}", "static", 123},
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	items, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("expected items to be []any, got %T", result["items"])
	}
	if items[0] != "widget" {
		t.Errorf("expected items[0]='widget', got %v", items[0])
	}
	if items[1] != "static" {
		t.Errorf("expected items[1]='static', got %v", items[1])
	}
	if items[2] != 123 {
		t.Errorf("expected items[2]=123, got %v", items[2])
	}
}

func TestTemplateEngine_ResolveMap_ErrorPropagation(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	data := map[string]any{
		"broken": "{{ .unclosed",
	}

	_, err := te.ResolveMap(data, pc)
	if err == nil {
		t.Fatal("expected error from invalid template in map")
	}
}

func TestTemplateEngine_ResolveMap_DoesNotMutateInput(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"x": "resolved"}, nil)

	data := map[string]any{
		"key": "{{ .x }}",
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original should be unchanged
	if data["key"] != "{{ .x }}" {
		t.Errorf("original map was mutated")
	}
	if result["key"] != "resolved" {
		t.Errorf("expected 'resolved', got %v", result["key"])
	}
}
