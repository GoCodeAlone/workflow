---
status: implemented
area: runtime
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 3d2eb47
  - repo: workflow
    commit: c4f775a
  - repo: workflow
    commit: 566fb5a
  - repo: workflow
    commit: 50f9c06
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - "rg -n \"step.parallel|concurrency|groupBy\" module plugins cmd"
    - "GOWORK=off go test ./module -run 'Test(ExplicitTraceHeader|TrackPipelineExecution|ParallelStep|ForEachStep|TemplateEngine_Func|Scan)'"
  result: pass
supersedes: []
superseded_by: []
---

# Fan-Out / Fan-In / Map-Reduce Implementation Plan

**Goal:** Add parallel execution capabilities (`step.parallel`, concurrent `step.foreach`) and collection template functions (`sum`, `pluck`, `groupBy`, etc.) to the workflow engine.

**Architecture:** Concurrency is opt-in at the step level — the pipeline executor stays sequential. `step.parallel` spawns goroutines for fixed branches; `step.foreach` gets an optional worker pool. Each goroutine operates on a deep-copied PipelineContext, eliminating shared mutable state. 10 collection template functions are added for inline aggregation.

**Tech Stack:** Go, `sync.WaitGroup`, `context.WithCancel`, channel-based semaphore, `sort.SliceStable`, `reflect`

---

## Context

**Design document:** `docs/plans/2026-03-06-fan-out-fan-in-design.md`

**Key existing patterns to follow:**

- **Sub-step construction:** `buildSubStep()` in `module/pipeline_step_resilience.go:262-287` — extracts type+name+config, calls `registry.Create()`
- **Lazy registry getter:** `func() *StepRegistry` closure passed to factories — see `pipeline_step_foreach.go:26` and `pipelinesteps/plugin.go:147-149`
- **Child context isolation:** `ForEachStep.buildChildContext()` in `pipeline_step_foreach.go:184-225` — deep-copies TriggerData, Current, StepOutputs, Metadata
- **Factory registration:** `plugins/pipelinesteps/plugin.go` — add to `StepTypes` manifest (line 54-99) AND `StepFactories()` method (line 121-176)
- **Template function registration:** `module/pipeline_template.go:346-512` — `templateFuncMap()` returns `template.FuncMap`
- **Test fixtures:** `module/pipeline_step_resilience_test.go:12-59` — mock steps (`resilienceFailStep`, `resilienceSucceedStep`, `resilienceCountingStep`), `buildResilienceRegistry()` helper
- **Step output helper:** `ensureOutput()` in `pipeline_step_resilience.go:290-298`
- **Template test pattern:** `module/pipeline_template_test.go` — `TestTemplateEngine_Func<Name>` naming, PipelineContext setup with StepOutputs

**Numeric conversion:** Use `toFloat64()` from `pipeline_template.go:19-44` for extracting numeric values in sum/min/max functions. Use `isIntType()` from `pipeline_template.go:47-54` to preserve integer types when possible.

---

## Task 1: Create `step.parallel` — Fixed-Branch Fan-Out

**Files:**
- Create: `module/pipeline_step_parallel.go`
- Create: `module/pipeline_step_parallel_test.go`

### Step 1: Write tests for step.parallel

Create `module/pipeline_step_parallel_test.go`:

```go
package module

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
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
			reg.Register(name, func(n string, cfg map[string]any, app any) (PipelineStep, error) {
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
```

**Note:** `buildParallelStepDirect` is a test helper that constructs a ParallelStep directly (bypassing the factory/registry) for context isolation tests. Define it alongside the test fixtures.

### Step 2: Run tests to verify they fail

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestParallel -v`
Expected: Compilation error — `NewParallelStepFactory` and `ParallelStep` not defined.

### Step 3: Implement `step.parallel`

Create `module/pipeline_step_parallel.go`:

```go
package module

import (
	"context"
	"fmt"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// ParallelStep executes multiple named sub-steps concurrently and collects results.
//
// Complexity:
//   - Time:  O(max(branch_duration)) — wall clock bounded by slowest branch
//   - Space: O(branches × context_size) — deep copy of PipelineContext per branch
type ParallelStep struct {
	name          string
	subSteps      []PipelineStep
	errorStrategy string // "fail_fast" or "collect_errors"
}

// NewParallelStepFactory returns a StepFactory that creates ParallelStep instances.
// registryFn is called at step-creation time to obtain the step registry, using the
// same lazy pattern as ForEachStep and RetryWithBackoffStep.
func NewParallelStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		stepsRaw, ok := config["steps"].([]any)
		if !ok || len(stepsRaw) == 0 {
			return nil, fmt.Errorf("parallel step %q: 'steps' list is required", name)
		}

		errorStrategy, _ := config["error_strategy"].(string)
		if errorStrategy == "" {
			errorStrategy = "fail_fast"
		}
		if errorStrategy != "fail_fast" && errorStrategy != "collect_errors" {
			return nil, fmt.Errorf("parallel step %q: error_strategy must be 'fail_fast' or 'collect_errors', got %q", name, errorStrategy)
		}

		subSteps := make([]PipelineStep, 0, len(stepsRaw))
		seen := make(map[string]bool)
		for i, raw := range stepsRaw {
			stepCfg, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("parallel step %q: steps[%d] must be a map", name, i)
			}
			stepName, _ := stepCfg["name"].(string)
			if stepName == "" {
				return nil, fmt.Errorf("parallel step %q: steps[%d] requires a 'name'", name, i)
			}
			if seen[stepName] {
				return nil, fmt.Errorf("parallel step %q: duplicate branch name %q", name, stepName)
			}
			seen[stepName] = true

			step, err := buildSubStep(name, stepName, stepCfg, registryFn, app)
			if err != nil {
				return nil, fmt.Errorf("parallel step %q: %w", name, err)
			}
			subSteps = append(subSteps, step)
		}

		return &ParallelStep{
			name:          name,
			subSteps:      subSteps,
			errorStrategy: errorStrategy,
		}, nil
	}
}

// Name returns the step name.
func (s *ParallelStep) Name() string { return s.name }

// Execute runs all sub-steps concurrently and collects their results.
//
// Output:
//
//	{
//	  "results":   map[string]any  — branch_name → branch output (successful branches)
//	  "errors":    map[string]any  — branch_name → error string (failed branches)
//	  "completed": int             — count of successful branches
//	  "failed":    int             — count of failed branches
//	}
func (s *ParallelStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	n := len(s.subSteps)
	if n == 0 {
		return &StepResult{
			Output: map[string]any{
				"results":   map[string]any{},
				"errors":    map[string]any{},
				"completed": 0,
				"failed":    0,
			},
		}, nil
	}

	type branchResult struct {
		name   string
		output map[string]any
		err    error
	}

	results := make([]branchResult, n)
	var wg sync.WaitGroup
	wg.Add(n)

	// For fail_fast, derive a cancellable context
	branchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var firstErr error
	var errOnce sync.Once

	for i, step := range s.subSteps {
		i, step := i, step
		childPC := buildParallelChildContext(pc)
		go func() {
			defer wg.Done()
			result, err := step.Execute(branchCtx, childPC)
			if err != nil {
				results[i] = branchResult{name: step.Name(), err: err}
				if s.errorStrategy == "fail_fast" {
					errOnce.Do(func() {
						firstErr = fmt.Errorf("parallel step %q: branch %q failed: %w", s.name, step.Name(), err)
						cancel()
					})
				}
				return
			}
			output := make(map[string]any)
			if result != nil && result.Output != nil {
				for k, v := range result.Output {
					output[k] = v
				}
			}
			results[i] = branchResult{name: step.Name(), output: output}
		}()
	}

	wg.Wait()

	// Build output maps
	successMap := make(map[string]any)
	errorMap := make(map[string]any)
	for _, r := range results {
		if r.err != nil {
			errorMap[r.name] = r.err.Error()
		} else {
			successMap[r.name] = r.output
		}
	}

	completed := len(successMap)
	failed := len(errorMap)

	if s.errorStrategy == "fail_fast" && firstErr != nil {
		return nil, firstErr
	}

	if s.errorStrategy == "collect_errors" && failed == n {
		return nil, fmt.Errorf("parallel step %q: all %d branches failed", s.name, n)
	}

	return &StepResult{
		Output: map[string]any{
			"results":   successMap,
			"errors":    errorMap,
			"completed": completed,
			"failed":    failed,
		},
	}, nil
}

// buildParallelChildContext creates a deep copy of the PipelineContext for a parallel branch.
// Each branch gets its own isolated copy so goroutines don't share mutable state.
func buildParallelChildContext(parent *PipelineContext) *PipelineContext {
	childTrigger := make(map[string]any, len(parent.TriggerData))
	for k, v := range parent.TriggerData {
		childTrigger[k] = v
	}

	childMeta := make(map[string]any, len(parent.Metadata))
	for k, v := range parent.Metadata {
		childMeta[k] = v
	}

	childCurrent := make(map[string]any, len(parent.Current))
	for k, v := range parent.Current {
		childCurrent[k] = v
	}

	childOutputs := make(map[string]map[string]any, len(parent.StepOutputs))
	for k, v := range parent.StepOutputs {
		out := make(map[string]any, len(v))
		for k2, v2 := range v {
			out[k2] = v2
		}
		childOutputs[k] = out
	}

	return &PipelineContext{
		TriggerData: childTrigger,
		StepOutputs: childOutputs,
		Current:     childCurrent,
		Metadata:    childMeta,
	}
}

// buildParallelStepDirect constructs a ParallelStep from pre-built PipelineSteps
// without going through the factory/registry. Used for testing context isolation.
func buildParallelStepDirect(name string, steps []PipelineStep, errorStrategy string) (*ParallelStep, error) {
	if errorStrategy == "" {
		errorStrategy = "fail_fast"
	}
	return &ParallelStep{
		name:          name,
		subSteps:      steps,
		errorStrategy: errorStrategy,
	}, nil
}
```

### Step 4: Run tests to verify they pass

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestParallel -v -race`
Expected: All tests PASS. The `-race` flag is critical for concurrent code.

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_parallel.go module/pipeline_step_parallel_test.go
git commit -m "feat: add step.parallel for concurrent fixed-branch fan-out"
```

---

## Task 2: Enhance `step.foreach` with Concurrent Execution

**Files:**
- Modify: `module/pipeline_step_foreach.go`
- Modify: `module/pipeline_step_foreach_test.go`

### Step 1: Write tests for concurrent foreach

Add to `module/pipeline_step_foreach_test.go`:

```go
func TestForEachStep_ConcurrentExecution(t *testing.T) {
	// 5 items each taking 100ms, concurrency=5 — should complete in ~100ms not 500ms
	registry := NewStepRegistry()
	registry.Register("step.set", NewSetStepFactory())

	factory := NewForEachStepFactory(func() *StepRegistry { return registry })
	step, err := factory("par-foreach", map[string]any{
		"collection":  "items",
		"item_var":    "item",
		"concurrency": 5,
		"step": map[string]any{
			"name": "process",
			"type": "step.set",
			"values": map[string]any{
				"processed": "true",
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	items := make([]any, 5)
	for i := range items {
		items[i] = map[string]any{"id": i}
	}
	pc := NewPipelineContext(map[string]any{"items": items}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	results := result.Output["results"].([]any)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
	count := result.Output["count"]
	if count != 5 {
		t.Fatalf("expected count=5, got %v", count)
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
	registry.Register("step.fail", func(name string, cfg map[string]any, app any) (PipelineStep, error) {
		return &resilienceFailStep{name: name}, nil
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
	registry.Register("step.fail", func(name string, cfg map[string]any, app any) (PipelineStep, error) {
		return &resilienceFailStep{name: name}, nil
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
			"name": "s",
			"type": "step.set",
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
```

### Step 2: Run tests to verify they fail

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run "TestForEachStep_Concurrent|TestForEachStep_ConcurrencyZero" -v`
Expected: Tests fail — ForEachStep doesn't recognize `concurrency` or `error_strategy` config keys yet.

### Step 3: Implement concurrent foreach

Modify `module/pipeline_step_foreach.go`:

**Changes to `ForEachStep` struct** (add two fields after line 18):

```go
type ForEachStep struct {
	name          string
	collection    string
	itemKey       string
	indexKey      string
	subSteps      []PipelineStep
	tmpl          *TemplateEngine
	concurrency   int    // 0 = sequential (default)
	errorStrategy string // "fail_fast" (default) or "collect_errors"
}
```

**Changes to factory** (after `indexKey` parsing, around line 45, add):

```go
		concurrency := 0
		if v, ok := config["concurrency"]; ok {
			switch val := v.(type) {
			case int:
				concurrency = val
			case float64:
				concurrency = int(val)
			}
		}
		if concurrency < 0 {
			concurrency = 0
		}

		errorStrategy, _ := config["error_strategy"].(string)
		if errorStrategy == "" {
			errorStrategy = "fail_fast"
		}
```

**Add fields to the return struct** (around line 92):

```go
		return &ForEachStep{
			name:          name,
			collection:    collection,
			itemKey:       itemKey,
			indexKey:       indexKey,
			subSteps:      subSteps,
			tmpl:          NewTemplateEngine(),
			concurrency:   concurrency,
			errorStrategy: errorStrategy,
		}, nil
```

**In `Execute` method** (replace lines 124-147 — the iteration loop):

After the empty collection check (line 122), replace the sequential loop with:

```go
	if s.concurrency > 0 {
		return s.executeConcurrent(ctx, pc, items)
	}
	return s.executeSequential(ctx, pc, items)
```

Extract the existing loop into `executeSequential`:

```go
// executeSequential processes items one at a time. O(n × per_item) time, O(context_size) space.
func (s *ForEachStep) executeSequential(ctx context.Context, pc *PipelineContext, items []any) (*StepResult, error) {
	collected := make([]any, 0, len(items))
	for i, item := range items {
		childPC := s.buildChildContext(pc, item, i)
		iterResult := make(map[string]any)
		for _, step := range s.subSteps {
			result, execErr := step.Execute(ctx, childPC)
			if execErr != nil {
				return nil, fmt.Errorf("foreach step %q: iteration %d, sub-step %q failed: %w",
					s.name, i, step.Name(), execErr)
			}
			if result != nil && result.Output != nil {
				childPC.MergeStepOutput(step.Name(), result.Output)
				maps.Copy(iterResult, result.Output)
			}
			if result != nil && result.Stop {
				break
			}
		}
		collected = append(collected, iterResult)
	}
	return &StepResult{
		Output: map[string]any{
			"results": collected,
			"count":   len(collected),
		},
	}, nil
}
```

Add the concurrent implementation:

```go
// executeConcurrent processes items with a bounded worker pool.
// O(⌈n/c⌉ × per_item) time, O(c × context_size) space where c = concurrency.
func (s *ForEachStep) executeConcurrent(ctx context.Context, pc *PipelineContext, items []any) (*StepResult, error) {
	n := len(items)
	results := make([]any, n)
	errs := make([]error, n)

	sem := make(chan struct{}, s.concurrency)
	branchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for i, item := range items {
		wg.Add(1)
		i, item := i, item

		sem <- struct{}{} // acquire semaphore slot
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // release slot

			childPC := s.buildChildContext(pc, item, i)
			iterResult := make(map[string]any)
			for _, step := range s.subSteps {
				result, execErr := step.Execute(branchCtx, childPC)
				if execErr != nil {
					errs[i] = fmt.Errorf("iteration %d, sub-step %q: %w", i, step.Name(), execErr)
					if s.errorStrategy == "fail_fast" {
						errOnce.Do(func() {
							firstErr = fmt.Errorf("foreach step %q: %w", s.name, errs[i])
							cancel()
						})
					}
					return
				}
				if result != nil && result.Output != nil {
					childPC.MergeStepOutput(step.Name(), result.Output)
					maps.Copy(iterResult, result.Output)
				}
				if result != nil && result.Stop {
					break
				}
			}
			results[i] = iterResult
		}()
	}

	wg.Wait()

	if s.errorStrategy == "fail_fast" && firstErr != nil {
		return nil, firstErr
	}

	// Build output
	collected := make([]any, 0, n)
	errorCount := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			errorCount++
			collected = append(collected, map[string]any{
				"_error": errs[i].Error(),
				"_index": i,
			})
		} else if results[i] != nil {
			collected = append(collected, results[i])
		} else {
			collected = append(collected, map[string]any{})
		}
	}

	output := map[string]any{
		"results": collected,
		"count":   n,
	}
	if errorCount > 0 {
		output["error_count"] = errorCount
	}
	return &StepResult{Output: output}, nil
}
```

Also add `"sync"` to imports.

### Step 4: Run tests to verify they pass

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run "TestForEachStep" -v -race`
Expected: All existing and new tests PASS.

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_foreach.go module/pipeline_step_foreach_test.go
git commit -m "feat: add concurrent execution to step.foreach via concurrency config"
```

---

## Task 3: Add Collection Template Functions

**Files:**
- Modify: `module/pipeline_template.go`
- Modify: `module/pipeline_template_test.go`

### Step 1: Write tests for collection template functions

Add to `module/pipeline_template_test.go`:

```go
func TestTemplateEngine_FuncSum(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nums": []any{10, 20, 30},
		"items": []any{
			map[string]any{"amount": 10.5},
			map[string]any{"amount": 20.0},
			map[string]any{"amount": 5.5},
		},
	}, nil)

	// Sum scalars
	got, err := te.Resolve(`{{ sum .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "60" {
		t.Fatalf("expected 60, got %q", got)
	}

	// Sum with key
	got, err = te.Resolve(`{{ sum .items "amount" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "36" {
		t.Fatalf("expected 36, got %q", got)
	}
}

func TestTemplateEngine_FuncPluck(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"users": []any{
			map[string]any{"name": "Alice", "age": 30},
			map[string]any{"name": "Bob", "age": 25},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (pluck .users "name") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["Alice","Bob"]` {
		t.Fatalf("expected [\"Alice\",\"Bob\"], got %q", got)
	}
}

func TestTemplateEngine_FuncFlatten(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nested": []any{
			[]any{1, 2},
			[]any{3, 4},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (flatten .nested) }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `[1,2,3,4]` {
		t.Fatalf("expected [1,2,3,4], got %q", got)
	}
}

func TestTemplateEngine_FuncUnique(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"tags": []any{"go", "rust", "go", "python", "rust"},
	}, nil)
	got, err := te.Resolve(`{{ json (unique .tags) }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["go","rust","python"]` {
		t.Fatalf("expected deduplicated, got %q", got)
	}
}

func TestTemplateEngine_FuncGroupBy(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"cat": "books", "title": "Go"},
			map[string]any{"cat": "toys", "title": "Ball"},
			map[string]any{"cat": "books", "title": "Rust"},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (groupBy .items "cat") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"books"`) || !strings.Contains(got, `"toys"`) {
		t.Fatalf("expected grouped output, got %q", got)
	}
}

func TestTemplateEngine_FuncSortBy(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "Charlie", "score": 3},
			map[string]any{"name": "Alice", "score": 1},
			map[string]any{"name": "Bob", "score": 2},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (pluck (sortBy .items "score") "name") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["Alice","Bob","Charlie"]` {
		t.Fatalf("expected sorted, got %q", got)
	}
}

func TestTemplateEngine_FuncFirstLast(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
		"empty": []any{},
	}, nil)

	got, err := te.Resolve(`{{ first .items }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a" {
		t.Fatalf("expected a, got %q", got)
	}

	got, err = te.Resolve(`{{ last .items }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "c" {
		t.Fatalf("expected c, got %q", got)
	}

	got, err = te.Resolve(`{{ first .empty }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<nil>" {
		t.Fatalf("expected <nil>, got %q", got)
	}
}

func TestTemplateEngine_FuncMinMax(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nums": []any{5, 2, 8, 1, 9},
		"items": []any{
			map[string]any{"price": 10.5},
			map[string]any{"price": 3.0},
			map[string]any{"price": 7.5},
		},
	}, nil)

	got, err := te.Resolve(`{{ min .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1" {
		t.Fatalf("expected 1, got %q", got)
	}

	got, err = te.Resolve(`{{ max .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "9" {
		t.Fatalf("expected 9, got %q", got)
	}

	got, err = te.Resolve(`{{ min .items "price" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "3" {
		t.Fatalf("expected 3, got %q", got)
	}

	got, err = te.Resolve(`{{ max .items "price" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.5" {
		t.Fatalf("expected 10.5, got %q", got)
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run "TestTemplateEngine_Func(Sum|Pluck|Flatten|Unique|GroupBy|SortBy|FirstLast|MinMax)" -v`
Expected: Template functions not found.

### Step 3: Implement template functions

Add to `module/pipeline_template.go`, inside `templateFuncMap()`, before the closing `}` (after line 510):

```go
		// --- Collection functions ---
		// These functions operate on slices ([]any) with optional KEY for map elements.

		// sum returns the sum of numeric values in a slice. O(n) single pass.
		// Usage: {{ sum .nums }} or {{ sum .items "amount" }}
		"sum": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if items == nil {
				return float64(0)
			}
			total := float64(0)
			allInt := true
			for _, item := range items {
				v := extractField(item, keys)
				if !isIntType(v) {
					allInt = false
				}
				total += toFloat64(v)
			}
			if allInt {
				return int64(total)
			}
			return total
		},
		// pluck extracts a single field from each map in a slice. O(n).
		// Usage: {{ pluck .users "name" }}
		"pluck": func(slice any, key string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			result := make([]any, 0, len(items))
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					result = append(result, m[key])
				}
			}
			return result
		},
		// flatten flattens one level of nested slices. O(n×m).
		// Usage: {{ flatten .nested }}
		"flatten": func(slice any) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			var result []any
			for _, item := range items {
				if inner := toAnySlice(item); inner != nil {
					result = append(result, inner...)
				} else {
					result = append(result, item)
				}
			}
			return result
		},
		// unique deduplicates a slice preserving insertion order. O(n).
		// For maps: {{ unique .items "id" }} deduplicates by key value.
		// For scalars: {{ unique .tags }}
		"unique": func(slice any, keys ...string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			seen := make(map[string]bool)
			var result []any
			for _, item := range items {
				v := extractField(item, keys)
				key := fmt.Sprintf("%v", v)
				if !seen[key] {
					seen[key] = true
					result = append(result, item)
				}
			}
			return result
		},
		// groupBy groups slice elements by a key value. O(n).
		// Usage: {{ groupBy .items "category" }} → map[string][]any
		"groupBy": func(slice any, key string) map[string][]any {
			items := toAnySlice(slice)
			if items == nil {
				return map[string][]any{}
			}
			groups := make(map[string][]any)
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					k := fmt.Sprintf("%v", m[key])
					groups[k] = append(groups[k], item)
				}
			}
			return groups
		},
		// sortBy sorts a slice of maps by a key value ascending. O(n log n) stable sort.
		// Usage: {{ sortBy .items "price" }}
		"sortBy": func(slice any, key string) []any {
			items := toAnySlice(slice)
			if items == nil {
				return []any{}
			}
			sorted := make([]any, len(items))
			copy(sorted, items)
			sort.SliceStable(sorted, func(i, j int) bool {
				vi := extractField(sorted[i], []string{key})
				vj := extractField(sorted[j], []string{key})
				return toFloat64(vi) < toFloat64(vj)
			})
			return sorted
		},
		// first returns the first element of a slice. O(1).
		"first": func(slice any) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			return items[0]
		},
		// last returns the last element of a slice. O(1).
		"last": func(slice any) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			return items[len(items)-1]
		},
		// min returns the minimum numeric value in a slice. O(n) single pass.
		// Usage: {{ min .nums }} or {{ min .items "price" }}
		"min": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			minVal := toFloat64(extractField(items[0], keys))
			allInt := isIntType(extractField(items[0], keys))
			for _, item := range items[1:] {
				v := extractField(item, keys)
				f := toFloat64(v)
				if !isIntType(v) {
					allInt = false
				}
				if f < minVal {
					minVal = f
				}
			}
			if allInt {
				return int64(minVal)
			}
			return minVal
		},
		// max returns the maximum numeric value in a slice. O(n) single pass.
		// Usage: {{ max .nums }} or {{ max .items "price" }}
		"max": func(slice any, keys ...string) any {
			items := toAnySlice(slice)
			if len(items) == 0 {
				return nil
			}
			maxVal := toFloat64(extractField(items[0], keys))
			allInt := isIntType(extractField(items[0], keys))
			for _, item := range items[1:] {
				v := extractField(item, keys)
				f := toFloat64(v)
				if !isIntType(v) {
					allInt = false
				}
				if f > maxVal {
					maxVal = f
				}
			}
			if allInt {
				return int64(maxVal)
			}
			return maxVal
		},
```

Add helper functions before `templateFuncMap()` (around line 344):

```go
// toAnySlice converts any slice type to []any using reflect. Returns nil for non-slices.
func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil
	}
	result := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		result[i] = rv.Index(i).Interface()
	}
	return result
}

// extractField extracts a value from an item. If keys is provided and item is a map,
// returns map[key]. Otherwise returns item itself.
func extractField(item any, keys []string) any {
	if len(keys) > 0 {
		if m, ok := item.(map[string]any); ok {
			return m[keys[0]]
		}
	}
	return item
}
```

Also add `"sort"` to the imports at the top of the file.

### Step 4: Run tests to verify they pass

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run "TestTemplateEngine_Func(Sum|Pluck|Flatten|Unique|GroupBy|SortBy|FirstLast|MinMax)" -v`
Expected: All PASS.

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_template.go module/pipeline_template_test.go
git commit -m "feat: add collection template functions (sum, pluck, flatten, unique, groupBy, sortBy, first, last, min, max)"
```

---

## Task 4: Register step.parallel in Plugin + Update Schemas

**Files:**
- Modify: `plugins/pipelinesteps/plugin.go`
- Modify: `schema/step_schema.go` (add schema for step.parallel)
- Modify: `schema/step_inference.go` (add inference for step.parallel and updated foreach)

### Step 1: Register step.parallel in plugin

In `plugins/pipelinesteps/plugin.go`:

**Add to `StepTypes` manifest** (after `"step.regex_match"` at line 98):

```go
				"step.parallel",
```

**Add to `StepFactories()` method** (after `step.regex_match` at line 174):

```go
		"step.parallel": wrapStepFactory(module.NewParallelStepFactory(func() *module.StepRegistry {
			return p.concreteStepRegistry
		})),
```

### Step 2: Add step.parallel schema

Add to the schema registration (where other step schemas are defined). If step schemas are registered in individual step files, add a `StepSchema` variable. Check the existing pattern — schemas may be registered via `schema.GetStepSchemaRegistry().Register()` in the plugin or in `schema/step_schemas.go`.

Add for `step.parallel`:

```go
&StepSchema{
	Type:        "step.parallel",
	Plugin:      "pipeline-steps",
	Description: "Execute multiple named sub-steps concurrently and collect results. Time: O(max(branch)). Space: O(branches × context_size).",
	ConfigFields: []ConfigFieldDef{
		{Key: "steps", Type: "list", Required: true, Description: "List of sub-steps to run concurrently. Each must have a unique 'name'."},
		{Key: "error_strategy", Type: "string", Required: false, Default: "fail_fast", Options: []string{"fail_fast", "collect_errors"}, Description: "fail_fast: cancel on first error. collect_errors: run all, collect partial results."},
	},
	Outputs: []StepOutputDef{
		{Key: "results", Type: "map", Description: "Map of branch_name → branch output (successful branches)"},
		{Key: "errors", Type: "map", Description: "Map of branch_name → error string (failed branches)"},
		{Key: "completed", Type: "integer", Description: "Count of successful branches"},
		{Key: "failed", Type: "integer", Description: "Count of failed branches"},
	},
}
```

Update `step.foreach` schema to include new config fields:

```go
{Key: "concurrency", Type: "integer", Required: false, Default: "0", Description: "Worker pool size. 0 = sequential. Time: O(⌈n/c⌉ × per_item). Space: O(c × context_size)."},
{Key: "error_strategy", Type: "string", Required: false, Default: "fail_fast", Options: []string{"fail_fast", "collect_errors"}, Description: "Error handling for concurrent mode. fail_fast: cancel on first error. collect_errors: continue, mark failed items."},
```

Add `error_count` to foreach outputs:

```go
{Key: "error_count", Type: "integer", Description: "Count of failed items (only present with error_strategy: collect_errors)"},
```

### Step 3: Update schema inference

In `schema/step_inference.go`, add a case for `step.parallel` in `InferStepOutputs`:

```go
	case "step.parallel":
		outputs := []InferredOutput{
			{Key: "results", Type: "map", Description: "Map of branch_name → branch output"},
			{Key: "errors", Type: "map", Description: "Map of branch_name → error string"},
			{Key: "completed", Type: "integer", Description: "Count of successful branches"},
			{Key: "failed", Type: "integer", Description: "Count of failed branches"},
		}
		// If steps are provided in config, list branch names
		if stepsRaw, ok := stepConfig["steps"].([]any); ok {
			for _, raw := range stepsRaw {
				if stepCfg, ok := raw.(map[string]any); ok {
					if name, ok := stepCfg["name"].(string); ok {
						outputs = append(outputs, InferredOutput{
							Key:         "results." + name,
							Type:        "(dynamic)",
							Description: "Output from branch " + name,
						})
					}
				}
			}
		}
		return outputs
```

### Step 4: Run all schema tests

Run: `cd /Users/jon/workspace/workflow && go test ./schema/... ./plugins/... -v`
Expected: All PASS.

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add plugins/pipelinesteps/plugin.go schema/
git commit -m "feat: register step.parallel in plugin, add schemas and inference"
```

---

## Task 5: Update Documentation + MCP Schema + LSP Registry

**Files:**
- Modify: `DOCUMENTATION.md`
- Modify: `lsp/registry.go` (add new template functions to LSP completions)
- Modify: `mcp/tools.go` (add step.parallel to MCP schema if needed)

### Step 1: Update DOCUMENTATION.md

**Add to pipeline step types table** (after the existing entries around line 158):

```markdown
| `step.parallel` | Executes named sub-steps concurrently and collects results. O(max(branch)) time |
```

**Update step.foreach entry** to mention concurrency:

```markdown
| `step.foreach` | Iterates over a slice and runs sub-steps per element. Optional `concurrency: N` for parallel processing |
```

**Add to template functions section** (after the existing Type/Utility functions section, around line 222):

```markdown
#### Collection Functions

| Function | Signature | Complexity | Description |
|----------|-----------|-----------|-------------|
| `sum` | `sum SLICE [KEY]` | O(n) | Sum numeric values. Optional KEY for maps |
| `pluck` | `pluck SLICE KEY` | O(n) | Extract one field from each map |
| `flatten` | `flatten SLICE` | O(n×m) | Flatten one level of nested slices |
| `unique` | `unique SLICE [KEY]` | O(n) | Deduplicate, preserving insertion order |
| `groupBy` | `groupBy SLICE KEY` | O(n) | Group maps by key → `map[string][]any` |
| `sortBy` | `sortBy SLICE KEY` | O(n log n) | Stable sort ascending by key |
| `first` | `first SLICE` | O(1) | First element, nil if empty |
| `last` | `last SLICE` | O(1) | Last element, nil if empty |
| `min` | `min SLICE [KEY]` | O(n) | Minimum numeric value |
| `max` | `max SLICE [KEY]` | O(n) | Maximum numeric value |
```

### Step 2: Update LSP template function registry

In `lsp/registry.go`, add the 10 new template functions to the list that powers completions (the `templateFunctions()` function or equivalent). Each entry needs name and description:

```go
{Name: "sum", Detail: "sum SLICE [KEY]", Documentation: "Sum numeric values in a slice. O(n)"},
{Name: "pluck", Detail: "pluck SLICE KEY", Documentation: "Extract one field from each map in a slice. O(n)"},
{Name: "flatten", Detail: "flatten SLICE", Documentation: "Flatten one level of nested slices. O(n×m)"},
{Name: "unique", Detail: "unique SLICE [KEY]", Documentation: "Deduplicate a slice, preserving insertion order. O(n)"},
{Name: "groupBy", Detail: "groupBy SLICE KEY", Documentation: "Group maps by key value. O(n)"},
{Name: "sortBy", Detail: "sortBy SLICE KEY", Documentation: "Stable sort ascending by key. O(n log n)"},
{Name: "first", Detail: "first SLICE", Documentation: "First element of a slice. O(1)"},
{Name: "last", Detail: "last SLICE", Documentation: "Last element of a slice. O(1)"},
{Name: "min", Detail: "min SLICE [KEY]", Documentation: "Minimum numeric value in a slice. O(n)"},
{Name: "max", Detail: "max SLICE [KEY]", Documentation: "Maximum numeric value in a slice. O(n)"},
```

### Step 3: Run full test suite

Run: `cd /Users/jon/workspace/workflow && go test -race ./...`
Expected: All PASS.

### Step 4: Commit

```bash
cd /Users/jon/workspace/workflow
git add DOCUMENTATION.md lsp/registry.go
git commit -m "docs: add step.parallel, concurrent foreach, and collection functions to docs and LSP"
```

---

## Task 6: Add Example Configs

**Files:**
- Create: `example/parallel-fan-out-config.yaml`

### Step 1: Create example config

Create `example/parallel-fan-out-config.yaml` demonstrating all three capabilities:

```yaml
app:
  name: parallel-demo
  version: "1.0"

modules:
  - name: server
    type: http.server
    config:
      address: ":8080"

  - name: router
    type: http.router
    config:
      module: server

workflows:
  - name: api
    type: pipeline
    trigger:
      type: http
    routes:
      # Fan-out: parallel API aggregation
      - path: /aggregate/{id}
        method: GET
        pipeline:
          steps:
            - type: step.request_parse
              name: parse
              config:
                path_params: [id]

            - type: step.parallel
              name: fetch-all
              config:
                error_strategy: collect_errors
                steps:
                  - name: profile
                    type: step.set
                    values:
                      name: "User {{ .path_params.id }}"
                      email: "user@example.com"
                  - name: orders
                    type: step.set
                    values:
                      items: "3"
                      total: "150.00"
                  - name: preferences
                    type: step.set
                    values:
                      theme: dark
                      language: en

            - type: step.json_response
              name: respond
              config:
                status_code: 200
                body: '{{ json .steps.fetch-all.results }}'

      # Fan-out: concurrent foreach
      - path: /batch
        method: POST
        pipeline:
          steps:
            - type: step.request_parse
              name: parse
              config:
                parse_body: true

            - type: step.foreach
              name: process-items
              config:
                collection: body.items
                item_var: item
                concurrency: 5
                error_strategy: collect_errors
                step:
                  name: process
                  type: step.set
                  values:
                    processed: "true"
                    id: "{{ .item.id }}"

            - type: step.set
              name: summary
              config:
                values:
                  total: '{{ .steps.process-items.count }}'
                  processed: '{{ json (pluck .steps.process-items.results "id") }}'

            - type: step.json_response
              name: respond
              config:
                status_code: 200

      # Map/reduce with template functions
      - path: /stats
        method: GET
        pipeline:
          steps:
            - type: step.set
              name: data
              config:
                values:
                  sales: |
                    {{ json (list
                      (dict "region" "east" "amount" 100)
                      (dict "region" "west" "amount" 200)
                      (dict "region" "east" "amount" 150)
                      (dict "region" "west" "amount" 50)
                    ) }}

            - type: step.set
              name: stats
              config:
                values:
                  total: '{{ sum .steps.data.sales "amount" }}'
                  max_sale: '{{ max .steps.data.sales "amount" }}'
                  min_sale: '{{ min .steps.data.sales "amount" }}'
                  regions: '{{ json (unique .steps.data.sales "region") }}'
                  by_region: '{{ json (groupBy .steps.data.sales "region") }}'

            - type: step.json_response
              name: respond
              config:
                status_code: 200
```

### Step 2: Commit

```bash
cd /Users/jon/workspace/workflow
git add example/parallel-fan-out-config.yaml
git commit -m "docs: add parallel fan-out example config"
```

---

## Verification

Run the full test suite with race detection:

```bash
cd /Users/jon/workspace/workflow && go test -race ./...
```

Run lint:

```bash
cd /Users/jon/workspace/workflow && go fmt ./... && golangci-lint run
```

Key verification points:
1. `step.parallel` — 3 branches complete in O(max(branch)) time, not sequential
2. `step.parallel` — fail_fast cancels in-flight branches
3. `step.parallel` — collect_errors returns partial results
4. `step.foreach` — `concurrency: 0` behaves identically to current behavior
5. `step.foreach` — `concurrency: N` preserves result order
6. `step.foreach` — concurrent fail_fast and collect_errors both work
7. Template functions — sum, pluck, flatten, unique, groupBy, sortBy, first, last, min, max
8. All existing tests still pass (no regressions)

## Parallelism

- Tasks 1, 2, 3: **Can run in parallel** (independent: parallel step, foreach enhancement, template functions)
- Task 4: After 1 + 2 (registers step.parallel, updates foreach schema)
- Task 5: After 3 + 4 (documents everything)
- Task 6: After 5 (example config references all features)
