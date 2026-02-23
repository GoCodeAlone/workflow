package module

import (
	"context"
	"testing"
)

func TestValidatePathParamStepFactory(t *testing.T) {
	factory := NewValidatePathParamStepFactory()

	t.Run("missing params config", func(t *testing.T) {
		_, err := factory("test", map[string]any{}, nil)
		if err == nil {
			t.Fatal("expected error for missing params")
		}
	})

	t.Run("valid factory config", func(t *testing.T) {
		step, err := factory("test", map[string]any{
			"params": []any{"id"},
			"format": "uuid",
			"source": "steps.parse-request.path_params",
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if step.Name() != "test" {
			t.Fatalf("expected name 'test', got %q", step.Name())
		}
	})
}

func TestValidatePathParamStep_Execute(t *testing.T) {
	factory := NewValidatePathParamStepFactory()

	t.Run("valid UUID", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"params": []any{"id"},
			"format": "uuid",
			"source": "steps.parse-request.path_params",
		}, nil)

		pc := &PipelineContext{
			StepOutputs: map[string]map[string]any{
				"parse-request": {
					"path_params": map[string]any{
						"id": "550e8400-e29b-41d4-a716-446655440000",
					},
				},
			},
			Current: map[string]any{},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["valid"] != true {
			t.Fatal("expected valid=true")
		}
	})

	t.Run("invalid UUID", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"params": []any{"id"},
			"format": "uuid",
			"source": "steps.parse-request.path_params",
		}, nil)

		pc := &PipelineContext{
			StepOutputs: map[string]map[string]any{
				"parse-request": {
					"path_params": map[string]any{
						"id": "not-a-uuid",
					},
				},
			},
			Current: map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for invalid UUID")
		}
	})

	t.Run("missing param", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"params": []any{"id"},
			"source": "steps.parse-request.path_params",
		}, nil)

		pc := &PipelineContext{
			StepOutputs: map[string]map[string]any{
				"parse-request": {
					"path_params": map[string]any{},
				},
			},
			Current: map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for missing param")
		}
	})

	t.Run("non-empty only (no format)", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"params": []any{"id"},
		}, nil)

		pc := &PipelineContext{
			Current: map[string]any{
				"id": "some-value",
			},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["valid"] != true {
			t.Fatal("expected valid=true")
		}
	})

	t.Run("empty string param", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"params": []any{"id"},
		}, nil)

		pc := &PipelineContext{
			Current: map[string]any{
				"id": "",
			},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for empty param")
		}
	})
}
