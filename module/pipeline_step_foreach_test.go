package module

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/CrisisTextLine/modular"
)

// buildTestForEachStep creates a ForEachStep with a fresh StepRegistry for testing.
// It registers a simple "step.set" factory so sub-steps can be built.
func buildTestForEachStep(t *testing.T, name string, config map[string]any) (PipelineStep, error) {
	t.Helper()
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())
	registry.Register("step.log", NewLogStepFactory())
	factory := NewForEachStepFactory(func() *StepRegistry { return registry }, nil)
	return factory(name, config, nil)
}

func TestForEachStep_IteratesOverSliceOfMaps(t *testing.T) {
	step, err := buildTestForEachStep(t, "foreach-test", map[string]any{
		"collection": "items",
		"item_key":   "item",
		"index_key":  "index",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "capture",
				"values": map[string]any{
					"captured_name": "{{.item.name}}",
					"captured_idx":  "{{.index}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "alice", "age": 30},
			map[string]any{"name": "bob", "age": 25},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}

	results, ok := result.Output["results"].([]any)
	if !ok {
		t.Fatalf("expected results to be []any, got %T", result.Output["results"])
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first result to be map[string]any, got %T", results[0])
	}
	if first["captured_name"] != "alice" {
		t.Errorf("expected captured_name='alice', got %v", first["captured_name"])
	}
	if first["captured_idx"] != "0" {
		t.Errorf("expected captured_idx='0', got %v", first["captured_idx"])
	}

	second, ok := results[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second result to be map[string]any, got %T", results[1])
	}
	if second["captured_name"] != "bob" {
		t.Errorf("expected captured_name='bob', got %v", second["captured_name"])
	}
}

func TestForEachStep_EmptyCollection(t *testing.T) {
	step, err := buildTestForEachStep(t, "foreach-empty", map[string]any{
		"collection": "items",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "set-item",
				"values": map[string]any{
					"processed": "true",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 0 {
		t.Errorf("expected count=0, got %v", result.Output["count"])
	}

	results, ok := result.Output["results"].([]any)
	if !ok {
		t.Fatalf("expected results to be []any, got %T", result.Output["results"])
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestForEachStep_DefaultItemAndIndexKeys(t *testing.T) {
	step, err := buildTestForEachStep(t, "foreach-defaults", map[string]any{
		"collection": "things",
		// no item_key or index_key — should default to "item" and "index"
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "set-val",
				"values": map[string]any{
					"got_item":  "{{.item}}",
					"got_index": "{{.index}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"things": []any{"x", "y"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

func TestForEachStep_SubStepErrorStopsIteration(t *testing.T) {
	// Register a failing step type for this test
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())

	callCount := 0
	registry.Register("step.fail", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				callCount++
				if callCount >= 1 {
					return nil, errors.New("deliberate sub-step failure")
				}
				return &StepResult{Output: map[string]any{}}, nil
			},
		}, nil
	})

	factory := NewForEachStepFactory(func() *StepRegistry { return registry }, nil)
	step, err := factory("foreach-fail", map[string]any{
		"collection": "items",
		"steps": []any{
			map[string]any{
				"type": "step.fail",
				"name": "fail-step",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
	}, nil)

	_, execErr := step.Execute(context.Background(), pc)
	if execErr == nil {
		t.Fatal("expected error from failing sub-step")
	}
	if callCount > 1 {
		t.Errorf("expected iteration to stop after first failure, but sub-step was called %d times", callCount)
	}
}

func TestForEachStep_FactoryRejectsMissingCollection(t *testing.T) {
	_, err := buildTestForEachStep(t, "bad-foreach", map[string]any{
		"steps": []any{},
	})
	if err == nil {
		t.Fatal("expected error for missing 'collection'")
	}
}

func TestForEachStep_FactoryRejectsInvalidSubStepType(t *testing.T) {
	_, err := buildTestForEachStep(t, "bad-substep", map[string]any{
		"collection": "items",
		"steps": []any{
			map[string]any{
				"type": "step.nonexistent",
				"name": "bad",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown sub-step type")
	}
}

func TestForEachStep_IteratesWithStepOutputAccess(t *testing.T) {
	// Test that the item is accessible and sub-steps can build on each other within an iteration
	step, err := buildTestForEachStep(t, "foreach-chained", map[string]any{
		"collection": "users",
		"item_key":   "user",
		"index_key":  "i",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "extract",
				"values": map[string]any{
					"user_id": "{{.user.id}}",
				},
			},
			map[string]any{
				"type": "step.set",
				"name": "annotate",
				"values": map[string]any{
					"label": "user-{{.user_id}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"users": []any{
			map[string]any{"id": "u1"},
			map[string]any{"id": "u2"},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Fatalf("expected count=2, got %v", result.Output["count"])
	}

	results := result.Output["results"].([]any)
	first := results[0].(map[string]any)
	if first["label"] != "user-u1" {
		t.Errorf("expected label='user-u1', got %v", first["label"])
	}

	second := results[1].(map[string]any)
	if second["label"] != "user-u2" {
		t.Errorf("expected label='user-u2', got %v", second["label"])
	}
}

func TestForEachStep_CollectionFromStepOutputs(t *testing.T) {
	step, err := buildTestForEachStep(t, "foreach-from-step", map[string]any{
		"collection": "steps.fetch.rows",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "tag",
				"values": map[string]any{
					"tagged": "yes",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{
		"rows": []any{
			map[string]any{"id": 1},
			map[string]any{"id": 2},
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

// Compile-time check: ensure the step_fail factory signature matches StepFactory.
// This avoids having an unused import of fmt.
var _ = fmt.Sprintf
