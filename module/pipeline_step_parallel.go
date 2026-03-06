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
