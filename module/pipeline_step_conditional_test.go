package module

import (
	"context"
	"strings"
	"testing"
)

func TestConditionalStep_RoutesToCorrectStep(t *testing.T) {
	factory := NewConditionalStepFactory()
	step, err := factory("route-by-status", map[string]any{
		"field": "status",
		"routes": map[string]any{
			"approved": "process-payment",
			"rejected": "send-rejection",
			"pending":  "manual-review",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if step.Name() != "route-by-status" {
		t.Errorf("expected step name 'route-by-status', got %q", step.Name())
	}

	tests := []struct {
		status   string
		expected string
	}{
		{"approved", "process-payment"},
		{"rejected", "send-rejection"},
		{"pending", "manual-review"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			pc := NewPipelineContext(map[string]any{"status": tt.status}, nil)
			result, err := step.Execute(context.Background(), pc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.NextStep != tt.expected {
				t.Errorf("expected NextStep=%q, got %q", tt.expected, result.NextStep)
			}
			if result.Output["matched_value"] != tt.status {
				t.Errorf("expected matched_value=%q, got %v", tt.status, result.Output["matched_value"])
			}
			if result.Output["next_step"] != tt.expected {
				t.Errorf("expected output next_step=%q, got %v", tt.expected, result.Output["next_step"])
			}
		})
	}
}

func TestConditionalStep_UsesDefaultWhenNoMatch(t *testing.T) {
	factory := NewConditionalStepFactory()
	step, err := factory("route-default", map[string]any{
		"field": "priority",
		"routes": map[string]any{
			"high": "fast-track",
		},
		"default": "normal-track",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"priority": "low"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NextStep != "normal-track" {
		t.Errorf("expected NextStep='normal-track', got %q", result.NextStep)
	}
	if result.Output["used_default"] != true {
		t.Errorf("expected used_default=true in output, got %v", result.Output["used_default"])
	}
}

func TestConditionalStep_ErrorWhenNoMatchAndNoDefault(t *testing.T) {
	factory := NewConditionalStepFactory()
	step, err := factory("route-strict", map[string]any{
		"field": "status",
		"routes": map[string]any{
			"approved": "next-step",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"status": "unknown"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when no route matches and no default")
	}
	if !strings.Contains(err.Error(), "not found in routes") {
		t.Errorf("expected 'not found in routes' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected the unmatched value 'unknown' in error, got: %v", err)
	}
}

func TestConditionalStep_FieldFromStepOutput(t *testing.T) {
	factory := NewConditionalStepFactory()
	step, err := factory("route-by-result", map[string]any{
		"field": "steps.validate.result",
		"routes": map[string]any{
			"pass": "continue",
			"fail": "abort",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"result": "pass"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.NextStep != "continue" {
		t.Errorf("expected NextStep='continue', got %q", result.NextStep)
	}
}

func TestConditionalStep_FactoryRejectsMissingField(t *testing.T) {
	factory := NewConditionalStepFactory()

	_, err := factory("bad-cond", map[string]any{
		"routes": map[string]any{"a": "b"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing 'field'")
	}
	if !strings.Contains(err.Error(), "'field' is required") {
		t.Errorf("expected 'field is required' in error, got: %v", err)
	}
}

func TestConditionalStep_FactoryRejectsEmptyRoutes(t *testing.T) {
	factory := NewConditionalStepFactory()

	_, err := factory("bad-routes", map[string]any{
		"field":  "status",
		"routes": map[string]any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error for empty routes")
	}
	if !strings.Contains(err.Error(), "'routes' map is required") {
		t.Errorf("expected routes required message, got: %v", err)
	}
}

func TestConditionalStep_FactoryRejectsMissingRoutes(t *testing.T) {
	factory := NewConditionalStepFactory()

	_, err := factory("no-routes", map[string]any{
		"field": "status",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing routes")
	}
}

func TestConditionalStep_DoesNotStop(t *testing.T) {
	factory := NewConditionalStepFactory()
	step, err := factory("check-stop", map[string]any{
		"field": "x",
		"routes": map[string]any{
			"val": "next",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"x": "val"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Stop {
		t.Error("conditional step should not set Stop")
	}
}
