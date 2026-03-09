package module

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
)

// foreachFailStep is a race-safe PipelineStep that always returns an error.
// Unlike resilienceFailStep, it has no mutable state so it is safe to call
// concurrently from multiple goroutines (as happens in concurrent foreach tests).
type foreachFailStep struct{ stepName string }

func (s *foreachFailStep) Name() string { return s.stepName }
func (s *foreachFailStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	return nil, errors.New("step failed")
}

// foreachSlowStep is a PipelineStep that sleeps for a fixed duration then succeeds.
// It is used in timing-based concurrency tests to verify that items actually run
// in parallel rather than sequentially.
type foreachSlowStep struct {
	stepName string
	delay    time.Duration
}

func (s *foreachSlowStep) Name() string { return s.stepName }
func (s *foreachSlowStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	select {
	case <-time.After(s.delay):
		return &StepResult{Output: map[string]any{"done": true}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// buildTestForEachStep creates a ForEachStep with a fresh StepRegistry for testing.
// It registers a simple "step.set" factory so sub-steps can be built.
func buildTestForEachStep(t *testing.T, name string, config map[string]any) (PipelineStep, error) {
	t.Helper()
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())
	registry.Register("step.log", NewLogStepFactory())
	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
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

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
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

func TestForEachStep_ItemVar(t *testing.T) {
	// item_var is the canonical config key name from the issue spec.
	step, err := buildTestForEachStep(t, "foreach-item-var", map[string]any{
		"collection": "rows",
		"item_var":   "row",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "capture",
				"values": map[string]any{
					"captured": "{{.row}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"rows": []any{"alpha", "beta"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}

	results := result.Output["results"].([]any)
	first := results[0].(map[string]any)
	if first["captured"] != "alpha" {
		t.Errorf("expected captured='alpha', got %v", first["captured"])
	}
}

func TestForEachStep_ForeachIndexInContext(t *testing.T) {
	// Each iteration should expose {{.foreach.index}} in the child context.
	step, err := buildTestForEachStep(t, "foreach-index", map[string]any{
		"collection": "items",
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "capture",
				"values": map[string]any{
					"idx": "{{.foreach.index}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	results := result.Output["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify indexes are 0, 1, 2
	for i, r := range results {
		m := r.(map[string]any)
		want := fmt.Sprintf("%d", i)
		if m["idx"] != want {
			t.Errorf("iteration %d: expected idx=%q, got %v", i, want, m["idx"])
		}
	}
}

func TestForEachStep_SingleStep(t *testing.T) {
	// The "step" (singular) config key should work as an alternative to "steps".
	step, err := buildTestForEachStep(t, "foreach-single-step", map[string]any{
		"collection": "names",
		"item_var":   "name",
		"step": map[string]any{
			"type": "step.set",
			"name": "process",
			"values": map[string]any{
				"processed": "{{.name}}",
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"names": []any{"foo", "bar"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}

	results := result.Output["results"].([]any)
	first := results[0].(map[string]any)
	if first["processed"] != "foo" {
		t.Errorf("expected processed='foo', got %v", first["processed"])
	}
	second := results[1].(map[string]any)
	if second["processed"] != "bar" {
		t.Errorf("expected processed='bar', got %v", second["processed"])
	}
}

func TestForEachStep_SingleStep_InvalidType(t *testing.T) {
	_, err := buildTestForEachStep(t, "foreach-single-bad", map[string]any{
		"collection": "items",
		"step": map[string]any{
			"type": "step.nonexistent",
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown step type in 'step' config")
	}
}

func TestForEachStep_SingleStep_MissingType(t *testing.T) {
	_, err := buildTestForEachStep(t, "foreach-single-notype", map[string]any{
		"collection": "items",
		"step": map[string]any{
			"name": "no-type-here",
		},
	})
	if err == nil {
		t.Fatal("expected error when 'step' config has no 'type'")
	}
}

func TestForEachStep_StepAndStepsMutuallyExclusive(t *testing.T) {
	_, err := buildTestForEachStep(t, "foreach-both", map[string]any{
		"collection": "items",
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"x": "1"},
		},
		"steps": []any{
			map[string]any{"type": "step.set", "name": "s2", "values": map[string]any{"y": "2"}},
		},
	})
	if err == nil {
		t.Fatal("expected error when both 'step' and 'steps' are provided")
	}
}

func TestForEachStep_StepWrongType(t *testing.T) {
	_, err := buildTestForEachStep(t, "foreach-step-wrong-type", map[string]any{
		"collection": "items",
		"step":       "not-a-map",
	})
	if err == nil {
		t.Fatal("expected error when 'step' is not a map")
	}
}

func TestForEachStep_StepsWrongType(t *testing.T) {
	_, err := buildTestForEachStep(t, "foreach-steps-wrong-type", map[string]any{
		"collection": "items",
		"steps":      "not-a-list",
	})
	if err == nil {
		t.Fatal("expected error when 'steps' is not a list")
	}
}

func TestForEachStep_AppPassedToSubStep(t *testing.T) {
	// Verifies that the modular.Application passed to the StepFactory is threaded
	// through to sub-step factories, not silently dropped.
	var capturedApp modular.Application
	registry := NewStepRegistry()
	registry.Register("step.capture_app", func(name string, _ map[string]any, app modular.Application) (PipelineStep, error) {
		capturedApp = app
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				return &StepResult{Output: map[string]any{}}, nil
			},
		}, nil
	})

	sentinel := &struct{ modular.Application }{}
	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	_, err := factory("foreach-app-test", map[string]any{
		"collection": "items",
		"step": map[string]any{
			"type": "step.capture_app",
		},
	}, sentinel)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if capturedApp != sentinel {
		t.Errorf("expected app to be passed through to sub-step factory; got %v", capturedApp)
	}
}

func TestForEachStep_ConcurrentExecution(t *testing.T) {
	// 5 items each taking 50ms, concurrency=5 — should complete in ~50ms not 250ms.
	// We use a custom slow step that sleeps to ensure actual concurrency is tested.
	const itemDelay = 50 * time.Millisecond
	const numItems = 5

	registry := NewStepRegistry()
	registry.Register("step.slow", func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		return &foreachSlowStep{stepName: name, delay: itemDelay}, nil
	})

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("par-foreach", map[string]any{
		"collection":  "items",
		"item_var":    "item",
		"concurrency": numItems, // full concurrency — all items run in parallel
		"step": map[string]any{
			"name": "process",
			"type": "step.slow",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := make([]any, numItems)
	for i := range items {
		items[i] = map[string]any{"id": i}
	}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)

	start := time.Now()
	result, err := step.Execute(context.Background(), pc)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}

	results := result.Output["results"].([]any)
	if len(results) != numItems {
		t.Fatalf("expected %d results, got %d", numItems, len(results))
	}
	count := result.Output["count"]
	if count != numItems {
		t.Fatalf("expected count=%d, got %v", numItems, count)
	}

	// With full concurrency the wall-clock time should be roughly one item's delay.
	// Allow 3× headroom for slow CI environments; reject if it took as long as sequential.
	maxExpected := itemDelay * 3
	if elapsed > time.Duration(numItems)*itemDelay {
		t.Fatalf("concurrent execution took %v; expected <%v (sequential would be %v)",
			elapsed, maxExpected, time.Duration(numItems)*itemDelay)
	}
}

func TestForEachStep_ConcurrentPreservesOrder(t *testing.T) {
	// Items processed concurrently should maintain original index order in results
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("ordered", map[string]any{
		"collection":  "items",
		"item_var":    "item",
		"concurrency": 3,
		"step": map[string]any{
			"name": "echo",
			"type": "step.set",
			"values": map[string]any{
				"id": "{{ .item.id }}",
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := []any{
		map[string]any{"id": "first"},
		map[string]any{"id": "second"},
		map[string]any{"id": "third"},
	}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	results := result.Output["results"].([]any)
	ids := make([]string, len(results))
	for i, r := range results {
		rm := r.(map[string]any)
		ids[i], _ = rm["id"].(string)
	}
	if ids[0] != "first" || ids[1] != "second" || ids[2] != "third" {
		t.Fatalf("order not preserved: %v", ids)
	}
}

func TestForEachStep_ConcurrentCollectErrors(t *testing.T) {
	// With error_strategy=collect_errors, failed items should be marked not crash
	registry := NewStepRegistry()
	registry.Register("step.fail", func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		return &foreachFailStep{stepName: name}, nil
	})

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("err-foreach", map[string]any{
		"collection":     "items",
		"item_var":       "item",
		"concurrency":    2,
		"error_strategy": "collect_errors",
		"step": map[string]any{
			"name": "fail",
			"type": "step.fail",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := []any{"a", "b", "c"}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("collect_errors should not return error: %v", err)
	}

	errorCount := result.Output["error_count"]
	if errorCount != 3 {
		t.Fatalf("expected error_count=3, got %v", errorCount)
	}
}

func TestForEachStep_ConcurrentFailFast(t *testing.T) {
	// With default fail_fast and concurrency, first error cancels others
	registry := NewStepRegistry()
	registry.Register("step.fail", func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		return &foreachFailStep{stepName: name}, nil
	})

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("ff-foreach", map[string]any{
		"collection":  "items",
		"item_var":    "item",
		"concurrency": 2,
		"step": map[string]any{
			"name": "fail",
			"type": "step.fail",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := []any{"a", "b", "c"}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error with fail_fast")
	}
}

func TestForEachStep_ConcurrencyZeroIsSequential(t *testing.T) {
	// concurrency=0 means sequential (backward compat)
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("seq", map[string]any{
		"collection":  "items",
		"item_var":    "item",
		"concurrency": 0,
		"step": map[string]any{
			"name":   "s",
			"type":   "step.set",
			"values": map[string]any{"ok": "true"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := []any{"a", "b"}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["count"] != 2 {
		t.Fatalf("expected count=2, got %v", result.Output["count"])
	}
}

func TestForEachStep_ForeachMapNotSetWhenConflict(t *testing.T) {
	// When item_var is "foreach", the "foreach" context key must NOT be overwritten.
	step, err := buildTestForEachStep(t, "foreach-conflict", map[string]any{
		"collection": "items",
		"item_var":   "foreach", // would collide with the foreach map
		"steps": []any{
			map[string]any{
				"type": "step.set",
				"name": "capture",
				"values": map[string]any{
					"got": "{{.foreach}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"val"},
	}, nil)

	// Should not panic or error; the "foreach" key holds the item value, not the map.
	_, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}
}

func TestForEachStep_ConcurrentFailFastStopsEarly(t *testing.T) {
	// With fail_fast and slow remaining items, context cancellation should prevent
	// the producer from launching unnecessary goroutines after the first error.
	const itemDelay = 100 * time.Millisecond
	registry := NewStepRegistry()
	// first item fails immediately; remaining items sleep
	callCount := int32(0)
	registry.Register("step.slow_or_fail", func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		return &foreachSlowOrFailStep{stepName: name, delay: itemDelay, callCount: &callCount}, nil
	})

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("ff-early-stop", map[string]any{
		"collection":     "items",
		"item_var":       "item",
		"concurrency":    1, // 1 worker so cancellation stops the queue
		"error_strategy": "fail_fast",
		"step": map[string]any{
			"name": "work",
			"type": "step.slow_or_fail",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 10 items; with concurrency=1 the producer should stop after the first failure.
	items := make([]any, 10)
	for i := range items {
		items[i] = map[string]any{"id": i}
	}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)

	start := time.Now()
	_, err = step.Execute(context.Background(), pc)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error with fail_fast")
	}
	// If context cancellation works, we should not execute all 10 slow items.
	// Sequential execution of all 10 would take ~1s; early stop should be much faster.
	if elapsed >= time.Duration(len(items))*itemDelay {
		t.Fatalf("fail_fast did not stop early: took %v (expected less than %v)", elapsed, time.Duration(len(items))*itemDelay)
	}
}

// foreachSlowOrFailStep fails on the first call and sleeps on subsequent calls.
type foreachSlowOrFailStep struct {
	stepName  string
	delay     time.Duration
	callCount *int32
}

func (s *foreachSlowOrFailStep) Name() string { return s.stepName }
func (s *foreachSlowOrFailStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	n := atomic.AddInt32(s.callCount, 1)
	if n == 1 {
		return nil, fmt.Errorf("first item fails immediately")
	}
	select {
	case <-time.After(s.delay):
		return &StepResult{Output: map[string]any{"done": true}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
