package module

import (
	"context"
	"testing"
)

func TestJSONParseStep_StringJSON(t *testing.T) {
	factory := NewJSONParseStepFactory()
	step, err := factory("parse-json", map[string]any{
		"source": "steps.fetch.row.data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{
			"data": `[{"id":1,"type":"follow-ups"}]`,
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	parsed, ok := result.Output["value"].([]any)
	if !ok {
		t.Fatalf("expected []any, got %T: %v", result.Output["value"], result.Output["value"])
	}
	if len(parsed) != 1 {
		t.Fatalf("expected 1 element, got %d", len(parsed))
	}
	obj, ok := parsed[0].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any element, got %T", parsed[0])
	}
	// JSON numbers unmarshal to float64 by default.
	if obj["id"] != float64(1) {
		t.Errorf("expected id=1, got %v", obj["id"])
	}
	if obj["type"] != "follow-ups" {
		t.Errorf("expected type='follow-ups', got %v", obj["type"])
	}
}

func TestJSONParseStep_StringJSONObject(t *testing.T) {
	factory := NewJSONParseStepFactory()
	step, err := factory("parse-obj", map[string]any{
		"source": "steps.fetch.row.meta",
		"target": "parsed_meta",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{
			"meta": `{"total":42,"page":1}`,
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	parsed, ok := result.Output["parsed_meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result.Output["parsed_meta"])
	}
	if parsed["total"] != float64(42) {
		t.Errorf("expected total=42, got %v", parsed["total"])
	}
	if parsed["page"] != float64(1) {
		t.Errorf("expected page=1, got %v", parsed["page"])
	}
}

func TestJSONParseStep_ByteSliceJSON(t *testing.T) {
	factory := NewJSONParseStepFactory()
	step, err := factory("parse-bytes", map[string]any{
		"source": "steps.fetch.row.jsonb_col",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{
			"jsonb_col": []byte(`{"key":"value"}`),
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	parsed, ok := result.Output["value"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result.Output["value"])
	}
	if parsed["key"] != "value" {
		t.Errorf("expected key='value', got %v", parsed["key"])
	}
}

func TestJSONParseStep_AlreadyParsed(t *testing.T) {
	// When the upstream step already returns a structured value (map/slice),
	// json_parse should pass it through unchanged.
	factory := NewJSONParseStepFactory()
	step, err := factory("no-op-parse", map[string]any{
		"source": "steps.fetch.row.data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	original := map[string]any{"id": 1, "name": "test"}
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{
			"data": original,
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	parsed, ok := result.Output["value"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result.Output["value"])
	}
	if parsed["name"] != "test" {
		t.Errorf("expected name='test', got %v", parsed["name"])
	}
}

func TestJSONParseStep_InvalidJSON(t *testing.T) {
	factory := NewJSONParseStepFactory()
	step, err := factory("bad-json", map[string]any{
		"source": "steps.fetch.row.data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{
			"data": "not valid json {{{",
		},
	})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONParseStep_MissingSource(t *testing.T) {
	factory := NewJSONParseStepFactory()
	_, err := factory("no-source", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestJSONParseStep_UnresolvablePath(t *testing.T) {
	// A typo in source should fail fast rather than silently producing nil.
	factory := NewJSONParseStepFactory()
	step, err := factory("bad-path", map[string]any{
		"source": "steps.nonexistent.field",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when source path resolves to nil")
	}
}

func TestJSONParseStep_DefaultTargetKey(t *testing.T) {
	factory := NewJSONParseStepFactory()
	step, err := factory("default-target", map[string]any{
		"source": "steps.fetch.row.data",
		// no "target" config — should default to "value"
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"row": map[string]any{"data": `{"ok":true}`},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if _, hasValue := result.Output["value"]; !hasValue {
		t.Errorf("expected 'value' key in output, got keys: %v", result.Output)
	}
}
