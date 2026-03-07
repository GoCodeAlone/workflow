package module

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
)

// parallelSuccessStep returns fixed output after an optional delay.
type parallelSuccessStep struct {
	name   string
	output map[string]any
	delay  time.Duration
}

func (s *parallelSuccessStep) Name() string { return s.name }
func (s *parallelSuccessStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &StepResult{Output: s.output}, nil
}

// parallelFailStep always fails.
type parallelFailStep struct {
	name  string
	delay time.Duration
}

func (s *parallelFailStep) Name() string { return s.name }
func (s *parallelFailStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("step %q failed", s.name)
}

// parallelContextCheckStep records whether context was cancelled.
type parallelContextCheckStep struct {
	name      string
	delay     time.Duration
	cancelled atomic.Bool
}

func (s *parallelContextCheckStep) Name() string { return s.name }
func (s *parallelContextCheckStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	select {
	case <-time.After(s.delay):
		return &StepResult{Output: map[string]any{"done": true}}, nil
	case <-ctx.Done():
		s.cancelled.Store(true)
		return nil, ctx.Err()
	}
}

func buildParallelRegistry(steps map[string]PipelineStep) func() *StepRegistry {
	return func() *StepRegistry {
		reg := NewStepRegistry()
		for name, step := range steps {
			s := step // capture
			reg.Register(name, func(n string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
				return s, nil
			})
		}
		return reg
	}
}

func TestParallelStep_RequiresSteps(t *testing.T) {
	factory := NewParallelStepFactory(func() *StepRegistry { return NewStepRegistry() })
	_, err := factory("par", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing steps config")
	}
}

func TestParallelStep_AllSucceed(t *testing.T) {
	// 3 branches, all succeed — verify results map contains all 3 outputs
	stepA := &parallelSuccessStep{name: "a", output: map[string]any{"val": "alpha"}}
	stepB := &parallelSuccessStep{name: "b", output: map[string]any{"val": "beta"}}
	stepC := &parallelSuccessStep{name: "c", output: map[string]any{"val": "gamma"}}

	regFn := buildParallelRegistry(map[string]PipelineStep{
		"mock.a": stepA, "mock.b": stepB, "mock.c": stepC,
	})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"steps": []any{
			map[string]any{"name": "a", "type": "mock.a"},
			map[string]any{"name": "b", "type": "mock.b"},
			map[string]any{"name": "c", "type": "mock.c"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	results := result.Output["results"].(map[string]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	completed := result.Output["completed"]
	if completed != 3 {
		t.Fatalf("expected completed=3, got %v", completed)
	}
	failed := result.Output["failed"]
	if failed != 0 {
		t.Fatalf("expected failed=0, got %v", failed)
	}
}

func TestParallelStep_FailFast_CancelsOthers(t *testing.T) {
	// Branch "fast-fail" fails immediately; "slow" should get cancelled
	slow := &parallelContextCheckStep{name: "slow", delay: 5 * time.Second}
	fail := &parallelFailStep{name: "fast-fail", delay: 0}

	regFn := buildParallelRegistry(map[string]PipelineStep{
		"mock.slow": slow, "mock.fail": fail,
	})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"error_strategy": "fail_fast",
		"steps": []any{
			map[string]any{"name": "slow", "type": "mock.slow"},
			map[string]any{"name": "fast-fail", "type": "mock.fail"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error from fail_fast")
	}

	// Give goroutines time to observe cancellation
	time.Sleep(50 * time.Millisecond)
	if !slow.cancelled.Load() {
		t.Fatal("expected slow step to be cancelled")
	}
}

func TestParallelStep_CollectErrors_PartialSuccess(t *testing.T) {
	// 2 succeed, 1 fails — collect_errors should return partial results
	stepA := &parallelSuccessStep{name: "a", output: map[string]any{"val": 1}}
	stepB := &parallelFailStep{name: "b"}
	stepC := &parallelSuccessStep{name: "c", output: map[string]any{"val": 3}}

	regFn := buildParallelRegistry(map[string]PipelineStep{
		"mock.a": stepA, "mock.b": stepB, "mock.c": stepC,
	})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"error_strategy": "collect_errors",
		"steps": []any{
			map[string]any{"name": "a", "type": "mock.a"},
			map[string]any{"name": "b", "type": "mock.b"},
			map[string]any{"name": "c", "type": "mock.c"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(context.Background(), pc)
	// Should NOT error since not ALL branches failed
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	results := result.Output["results"].(map[string]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	errors := result.Output["errors"].(map[string]any)
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if result.Output["completed"] != 2 {
		t.Fatalf("expected completed=2, got %v", result.Output["completed"])
	}
	if result.Output["failed"] != 1 {
		t.Fatalf("expected failed=1, got %v", result.Output["failed"])
	}
}

func TestParallelStep_CollectErrors_AllFail(t *testing.T) {
	// All branches fail — collect_errors returns error
	stepA := &parallelFailStep{name: "a"}
	stepB := &parallelFailStep{name: "b"}

	regFn := buildParallelRegistry(map[string]PipelineStep{
		"mock.a": stepA, "mock.b": stepB,
	})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"error_strategy": "collect_errors",
		"steps": []any{
			map[string]any{"name": "a", "type": "mock.a"},
			map[string]any{"name": "b", "type": "mock.b"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when all branches fail")
	}
}

func TestParallelStep_RunsConcurrently(t *testing.T) {
	// 3 branches each take 100ms — should complete in ~100ms, not 300ms
	stepA := &parallelSuccessStep{name: "a", output: map[string]any{"val": 1}, delay: 100 * time.Millisecond}
	stepB := &parallelSuccessStep{name: "b", output: map[string]any{"val": 2}, delay: 100 * time.Millisecond}
	stepC := &parallelSuccessStep{name: "c", output: map[string]any{"val": 3}, delay: 100 * time.Millisecond}

	regFn := buildParallelRegistry(map[string]PipelineStep{
		"mock.a": stepA, "mock.b": stepB, "mock.c": stepC,
	})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"steps": []any{
			map[string]any{"name": "a", "type": "mock.a"},
			map[string]any{"name": "b", "type": "mock.b"},
			map[string]any{"name": "c", "type": "mock.c"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	start := time.Now()
	result, err := step.Execute(context.Background(), pc)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("took %v — branches should run concurrently (expected ~100ms)", elapsed)
	}
	results := result.Output["results"].(map[string]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
}

func TestParallelStep_ChildContextIsolation(t *testing.T) {
	// Verify each branch gets isolated context — mutations don't leak
	step, err := buildParallelStepDirect("par", []PipelineStep{
		&parallelSuccessStep{name: "a", output: map[string]any{"shared_key": "from_a"}},
		&parallelSuccessStep{name: "b", output: map[string]any{"shared_key": "from_b"}},
	}, "fail_fast")
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{"input": "original"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	// Parent context's Current should NOT have "shared_key" from either branch
	if _, exists := pc.Current["shared_key"]; exists {
		t.Fatal("branch output leaked into parent context")
	}

	results := result.Output["results"].(map[string]any)
	aOut := results["a"].(map[string]any)
	bOut := results["b"].(map[string]any)
	if aOut["shared_key"] != "from_a" || bOut["shared_key"] != "from_b" {
		t.Fatalf("branch outputs mixed up: a=%v, b=%v", aOut, bOut)
	}
}

func TestParallelStep_DefaultErrorStrategy(t *testing.T) {
	// No error_strategy specified — default is fail_fast
	fail := &parallelFailStep{name: "fail"}
	regFn := buildParallelRegistry(map[string]PipelineStep{"mock.fail": fail})
	factory := NewParallelStepFactory(regFn)

	step, err := factory("par", map[string]any{
		"steps": []any{
			map[string]any{"name": "fail", "type": "mock.fail"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error with default fail_fast strategy")
	}
}

func TestBuildParallelChildContext_DeepCopy(t *testing.T) {
	// Verify that buildParallelChildContext performs a true deep copy so that
	// mutations to nested maps/slices in a child context don't affect the parent.
	nested := map[string]any{"inner": "original"}
	slice := []any{"a", "b"}
	parent := &PipelineContext{
		TriggerData: map[string]any{"data": nested},
		Current:     map[string]any{"list": slice},
		Metadata:    map[string]any{},
		StepOutputs: map[string]map[string]any{},
	}

	child := buildParallelChildContext(parent)

	// Mutate the child's nested map — parent should be unaffected.
	child.TriggerData["data"].(map[string]any)["inner"] = "mutated"
	if parent.TriggerData["data"].(map[string]any)["inner"] != "original" {
		t.Fatal("deep copy failed: mutation of child TriggerData nested map affected parent")
	}

	// Mutate the child's slice — parent should be unaffected.
	child.Current["list"].([]any)[0] = "z"
	if parent.Current["list"].([]any)[0] != "a" {
		t.Fatal("deep copy failed: mutation of child Current slice affected parent")
	}
}
