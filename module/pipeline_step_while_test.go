package module

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
)

// buildTestWhileStep creates a WhileStep with a fresh StepRegistry for testing.
// It registers step.set and step.log so sub-steps can be built.
func buildTestWhileStep(t *testing.T, name string, config map[string]any) (PipelineStep, error) {
	t.Helper()
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())
	registry.Register("step.log", NewLogStepFactory())
	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	return factory(name, config, nil)
}

// whileCounterStep increments a counter on each Execute call.
// has_next is true while calls < maxRuns.
type whileCounterStep struct {
	stepName string
	callsPtr *int
	maxRuns  int
}

func (s *whileCounterStep) Name() string { return s.stepName }
func (s *whileCounterStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	*s.callsPtr++
	hasNext := *s.callsPtr < s.maxRuns
	return &StepResult{Output: map[string]any{
		"has_next": hasNext,
		"call":     *s.callsPtr,
	}}, nil
}

// TestWhileStep_ThreeIterationsThenStops runs a while loop that stops after 3 iterations.
// The condition references a sub-step output; the sub-step flips the flag after 3 runs.
func TestWhileStep_ThreeIterationsThenStops(t *testing.T) {
	calls := 0
	registry := NewStepRegistry()
	registry.Register("step.counter", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &whileCounterStep{stepName: name, callsPtr: &calls, maxRuns: 3}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("count-while", map[string]any{
		"condition":      "{{.steps.tick.has_next}}",
		"max_iterations": 100,
		"steps": []any{
			map[string]any{
				"type": "step.counter",
				"name": "tick",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Seed the context so the condition is truthy on the first pass.
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("tick", map[string]any{"has_next": true})

	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	if result.Output["iterations"] != 3 {
		t.Errorf("expected iterations=3, got %v", result.Output["iterations"])
	}
	if calls != 3 {
		t.Errorf("expected sub-step called 3 times, got %d", calls)
	}
}

// TestWhileStep_MaxIterationsExceeded verifies that an always-truthy condition
// with max_iterations=5 returns an error containing "exceeded max_iterations" and "5".
func TestWhileStep_MaxIterationsExceeded(t *testing.T) {
	step, err := buildTestWhileStep(t, "infinite-while", map[string]any{
		"condition":      "true",
		"max_iterations": 5,
		"step": map[string]any{
			"type":   "step.set",
			"name":   "noop",
			"values": map[string]any{"x": "1"},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, execErr := step.Execute(context.Background(), pc)
	if execErr == nil {
		t.Fatal("expected error for exceeded max_iterations")
	}
	if !strings.Contains(execErr.Error(), "exceeded max_iterations") {
		t.Errorf("expected error to contain 'exceeded max_iterations', got: %v", execErr)
	}
	if !strings.Contains(execErr.Error(), "5") {
		t.Errorf("expected error to contain '5', got: %v", execErr)
	}
}

// TestWhileStep_AccumulatesFlattened verifies that when a sub-step produces an
// array each iteration, accumulate.from flattens them all into accumulate.key.
func TestWhileStep_AccumulatesFlattened(t *testing.T) {
	calls := 0
	maxRuns := 3
	registry := NewStepRegistry()
	registry.Register("step.pages", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				calls++
				hasNext := calls < maxRuns
				items := []any{fmt.Sprintf("item-%d-a", calls), fmt.Sprintf("item-%d-b", calls)}
				return &StepResult{Output: map[string]any{
					"items":    items,
					"has_next": hasNext,
				}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("paginate", map[string]any{
		"condition":      "{{.steps.fetch.has_next}}",
		"max_iterations": 100,
		"accumulate": map[string]any{
			"key":  "all",
			"from": "{{.steps.fetch.items}}",
		},
		"step": map[string]any{
			"type": "step.pages",
			"name": "fetch",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{"has_next": true, "items": []any{}})

	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	all, ok := result.Output["all"].([]any)
	if !ok {
		t.Fatalf("expected output['all'] to be []any, got %T: %v", result.Output["all"], result.Output["all"])
	}
	// 3 iterations × 2 items = 6
	if len(all) != 6 {
		t.Errorf("expected 6 accumulated items, got %d: %v", len(all), all)
	}
}

// TestWhileStep_AccumulateScalar verifies that scalar accumulate.from values
// are appended individually (not flattened).
func TestWhileStep_AccumulateScalar(t *testing.T) {
	calls := 0
	maxRuns := 4
	registry := NewStepRegistry()
	registry.Register("step.tick", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				calls++
				return &StepResult{Output: map[string]any{
					"val":      calls,
					"has_next": calls < maxRuns,
				}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("scalar-accum", map[string]any{
		"condition":      "{{.steps.t.has_next}}",
		"max_iterations": 100,
		"accumulate": map[string]any{
			"key":  "all",
			"from": "{{.steps.t.val}}",
		},
		"step": map[string]any{
			"type": "step.tick",
			"name": "t",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("t", map[string]any{"has_next": true, "val": 0})

	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	all, ok := result.Output["all"].([]any)
	if !ok {
		t.Fatalf("expected output['all'] to be []any, got %T", result.Output["all"])
	}
	if len(all) != maxRuns {
		t.Errorf("expected %d accumulated scalars, got %d: %v", maxRuns, len(all), all)
	}
}

// TestWhileStep_ZeroIterationsWhenConditionFalseAtStart checks that when the
// condition is false immediately, zero iterations execute and the output is correct.
func TestWhileStep_ZeroIterationsWhenConditionFalseAtStart(t *testing.T) {
	calls := 0
	registry := NewStepRegistry()
	registry.Register("step.counter", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				calls++
				return &StepResult{Output: map[string]any{"ok": true}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("zero-while", map[string]any{
		"condition":      "false",
		"max_iterations": 100,
		"step": map[string]any{
			"type": "step.counter",
			"name": "c",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	if result.Output["iterations"] != 0 {
		t.Errorf("expected iterations=0, got %v", result.Output["iterations"])
	}
	if calls != 0 {
		t.Errorf("expected sub-step not called, got %d calls", calls)
	}
	// No accumulate configured, so accumulate key should be absent
	if _, exists := result.Output["all"]; exists {
		t.Error("unexpected 'all' key in output when accumulate not configured")
	}
}

// TestWhileStep_ZeroIterations_WithAccumulateEmpty checks that when the condition
// is false at the start with accumulate set, output contains an empty array.
func TestWhileStep_ZeroIterations_WithAccumulateEmpty(t *testing.T) {
	step, err := buildTestWhileStep(t, "zero-accum", map[string]any{
		"condition":      "false",
		"max_iterations": 10,
		"accumulate": map[string]any{
			"key":  "results",
			"from": "{{.steps.s.val}}",
		},
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"val": "x"},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	all, ok := result.Output["results"].([]any)
	if !ok {
		t.Fatalf("expected output['results'] to be []any, got %T", result.Output["results"])
	}
	if len(all) != 0 {
		t.Errorf("expected empty accumulation, got %d items", len(all))
	}
}

// TestWhileStep_FactoryRejectsMissingCondition verifies factory returns error
// when condition is absent.
func TestWhileStep_FactoryRejectsMissingCondition(t *testing.T) {
	_, err := buildTestWhileStep(t, "bad-while", map[string]any{
		"max_iterations": 10,
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"x": "1"},
		},
	})
	if err == nil {
		t.Fatal("expected error for missing 'condition'")
	}
}

// TestWhileStep_FactoryRejectsNegativeMaxIterations verifies factory rejects
// negative max_iterations.
func TestWhileStep_FactoryRejectsNegativeMaxIterations(t *testing.T) {
	_, err := buildTestWhileStep(t, "neg-while", map[string]any{
		"condition":      "true",
		"max_iterations": -1,
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"x": "1"},
		},
	})
	if err == nil {
		t.Fatal("expected error for max_iterations=-1")
	}
}

// TestWhileStep_FactoryRejectsStepAndStepsTogether verifies that providing both
// step and steps is rejected.
func TestWhileStep_FactoryRejectsStepAndStepsTogether(t *testing.T) {
	_, err := buildTestWhileStep(t, "both-while", map[string]any{
		"condition": "true",
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"x": "1"},
		},
		"steps": []any{
			map[string]any{
				"type":   "step.set",
				"name":   "s2",
				"values": map[string]any{"y": "2"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected error when both 'step' and 'steps' are provided")
	}
}

// TestWhileStep_IterationVarExposesIndex checks that iteration_var is set in the
// child context with {index, first} shape on every iteration.
func TestWhileStep_IterationVarExposesIndex(t *testing.T) {
	type iterRecord struct {
		index int
		first bool
	}
	var records []iterRecord

	calls := 0
	maxRuns := 3
	registry := NewStepRegistry()
	registry.Register("step.capture_iter", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, pc *PipelineContext) (*StepResult, error) {
				calls++
				// Read the iteration_var from Current
				iterVal, _ := pc.Current["myiter"].(map[string]any)
				idx, _ := iterVal["index"].(int)
				first, _ := iterVal["first"].(bool)
				records = append(records, iterRecord{index: idx, first: first})
				return &StepResult{Output: map[string]any{
					"has_next": calls < maxRuns,
				}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("iter-var-while", map[string]any{
		"condition":      "{{.steps.cap.has_next}}",
		"iteration_var":  "myiter",
		"max_iterations": 10,
		"step": map[string]any{
			"type": "step.capture_iter",
			"name": "cap",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("cap", map[string]any{"has_next": true})

	_, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	if len(records) != maxRuns {
		t.Fatalf("expected %d iteration records, got %d", maxRuns, len(records))
	}

	for i, rec := range records {
		if rec.index != i {
			t.Errorf("iteration %d: expected index=%d, got %d", i, i, rec.index)
		}
		wantFirst := i == 0
		if rec.first != wantFirst {
			t.Errorf("iteration %d: expected first=%v, got %v", i, wantFirst, rec.first)
		}
	}
}

// TestWhileStep_ContextCancellation verifies that a cancelled context causes
// Execute to return context.Canceled.
func TestWhileStep_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	step, err := buildTestWhileStep(t, "cancel-while", map[string]any{
		"condition": "true",
		"step": map[string]any{
			"type":   "step.set",
			"name":   "s",
			"values": map[string]any{"x": "1"},
		},
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, execErr := step.Execute(ctx, pc)
	if execErr == nil {
		t.Fatal("expected error from cancelled context")
	}
	if execErr != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", execErr)
	}
}

// TestWhileStep_AccumulateFlattensTypedSlice verifies that when a sub-step returns
// a typed slice ([]string rather than []any), the accumulator flattens the elements
// rather than boxing the whole slice as a single scalar.
func TestWhileStep_AccumulateFlattensTypedSlice(t *testing.T) {
	calls := 0
	maxRuns := 3
	registry := NewStepRegistry()
	registry.Register("step.str_pages", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				calls++
				hasNext := calls < maxRuns
				// Return a []string — the accumulator must flatten this, not box it.
				items := []string{
					fmt.Sprintf("item-%d-a", calls),
					fmt.Sprintf("item-%d-b", calls),
				}
				return &StepResult{Output: map[string]any{
					"items":    items,
					"has_next": hasNext,
				}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	step, err := factory("paginate-typed", map[string]any{
		"condition":      "{{.steps.fetch.has_next}}",
		"max_iterations": 100,
		"accumulate": map[string]any{
			"key":  "all",
			"from": "{{.steps.fetch.items}}",
		},
		"step": map[string]any{
			"type": "step.str_pages",
			"name": "fetch",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch", map[string]any{"has_next": true, "items": []string{}})

	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("execute error: %v", execErr)
	}

	all, ok := result.Output["all"].([]any)
	if !ok {
		t.Fatalf("expected output['all'] to be []any, got %T: %v", result.Output["all"], result.Output["all"])
	}
	// 3 iterations × 2 strings = 6 individual string elements (not 3 boxed slices)
	if len(all) != 6 {
		t.Errorf("expected 6 flattened string elements, got %d: %v", len(all), all)
	}
	// Verify the elements are strings, not slices
	for idx, elem := range all {
		if _, isStr := elem.(string); !isStr {
			t.Errorf("element[%d] should be string, got %T: %v", idx, elem, elem)
		}
	}
}

// TestWhileStep_TruthyNoValueSentinel checks that a condition resolving to
// literal "<no value>" is treated as FALSE (loop exits immediately).
func TestWhileStep_TruthyNoValueSentinel(t *testing.T) {
	calls := 0
	registry := NewStepRegistry()
	registry.Register("step.noop", func(name string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return &mockStep{
			name: name,
			execFn: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				calls++
				return &StepResult{Output: map[string]any{}}, nil
			},
		}, nil
	})

	factory := NewWhileStepFactory(func() *StepRegistry { return registry })
	// Reference a step output key that doesn't exist — template will render "<no value>"
	step, err := factory("novalue-while", map[string]any{
		"condition":      "{{.steps.nonexistent.has_next}}",
		"max_iterations": 100,
		"step": map[string]any{
			"type": "step.noop",
			"name": "n",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, execErr := step.Execute(context.Background(), pc)
	if execErr != nil {
		t.Fatalf("expected no error, got: %v", execErr)
	}

	// Condition must have been treated as false — zero iterations, no sub-step calls.
	if result.Output["iterations"] != 0 {
		t.Errorf("expected iterations=0 for <no value> sentinel, got %v", result.Output["iterations"])
	}
	if calls != 0 {
		t.Errorf("expected sub-step not called for <no value> condition, got %d calls", calls)
	}
}
