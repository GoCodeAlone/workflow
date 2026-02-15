package module

import (
	"context"
	"testing"
)

func TestSetStep_SetsValuesInContext(t *testing.T) {
	factory := NewSetStepFactory()
	step, err := factory("set-data", map[string]any{
		"values": map[string]any{
			"status":  "approved",
			"score":   100,
			"enabled": true,
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if step.Name() != "set-data" {
		t.Errorf("expected step name 'set-data', got %q", step.Name())
	}

	pc := NewPipelineContext(map[string]any{"order_id": "ORD-1"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["status"] != "approved" {
		t.Errorf("expected output status='approved', got %v", result.Output["status"])
	}
	if result.Output["score"] != 100 {
		t.Errorf("expected output score=100, got %v", result.Output["score"])
	}
	if result.Output["enabled"] != true {
		t.Errorf("expected output enabled=true, got %v", result.Output["enabled"])
	}
}

func TestSetStep_TemplateResolvesInValues(t *testing.T) {
	factory := NewSetStepFactory()
	step, err := factory("set-templated", map[string]any{
		"values": map[string]any{
			"greeting": "Hello {{ .user }}",
			"summary":  "Order {{ .order_id }} for {{ .user }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"user":     "Alice",
		"order_id": "ORD-42",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["greeting"] != "Hello Alice" {
		t.Errorf("expected 'Hello Alice', got %v", result.Output["greeting"])
	}
	if result.Output["summary"] != "Order ORD-42 for Alice" {
		t.Errorf("expected 'Order ORD-42 for Alice', got %v", result.Output["summary"])
	}
}

func TestSetStep_TemplateResolvesPreviousStepOutput(t *testing.T) {
	factory := NewSetStepFactory()
	step, err := factory("set-from-steps", map[string]any{
		"values": map[string]any{
			"result": "{{ .steps.validate.status }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"status": "passed"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["result"] != "passed" {
		t.Errorf("expected 'passed', got %v", result.Output["result"])
	}
}

func TestSetStep_FactoryRejectsEmptyValues(t *testing.T) {
	factory := NewSetStepFactory()

	_, err := factory("bad-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for empty config without values")
	}

	_, err = factory("bad-step2", map[string]any{"values": map[string]any{}}, nil)
	if err == nil {
		t.Fatal("expected error for empty values map")
	}
}

func TestSetStep_NonStringValuesPassThrough(t *testing.T) {
	factory := NewSetStepFactory()
	step, err := factory("set-mixed", map[string]any{
		"values": map[string]any{
			"count":   42,
			"flag":    true,
			"ratio":   3.14,
			"message": "static text",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 42 {
		t.Errorf("expected count=42, got %v", result.Output["count"])
	}
	if result.Output["flag"] != true {
		t.Errorf("expected flag=true, got %v", result.Output["flag"])
	}
	if result.Output["ratio"] != 3.14 {
		t.Errorf("expected ratio=3.14, got %v", result.Output["ratio"])
	}
	if result.Output["message"] != "static text" {
		t.Errorf("expected message='static text', got %v", result.Output["message"])
	}
}
