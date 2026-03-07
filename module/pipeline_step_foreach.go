package module

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// ForEachStep iterates over a collection and executes sub-steps for each item.
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

// NewForEachStepFactory returns a StepFactory that creates ForEachStep instances.
// registryFn is called at step-creation time to obtain the step registry used to
// build sub-steps. Passing a function (rather than the registry directly) allows
// the factory to be registered before the registry is fully populated, enabling
// sub-steps to themselves be any registered step type.
func NewForEachStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		collection, _ := config["collection"].(string)
		if collection == "" {
			return nil, fmt.Errorf("foreach step %q: 'collection' is required", name)
		}

		// item_var is the canonical name; item_key is kept for backward compatibility.
		itemKey, _ := config["item_var"].(string)
		if itemKey == "" {
			itemKey, _ = config["item_key"].(string)
		}
		if itemKey == "" {
			itemKey = "item"
		}

		indexKey, _ := config["index_key"].(string)
		if indexKey == "" {
			indexKey = "index"
		}

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
		} else if errorStrategy != "fail_fast" && errorStrategy != "collect_errors" {
			return nil, fmt.Errorf("foreach step %q: invalid error_strategy %q (must be fail_fast or collect_errors)", name, errorStrategy)
		}

		// Detect presence of each key before type-asserting so we can give clear errors.
		_, hasSingleStep := config["step"]
		_, hasStepsList := config["steps"]

		if hasSingleStep && hasStepsList {
			return nil, fmt.Errorf("foreach step %q: 'step' and 'steps' are mutually exclusive", name)
		}

		// Build sub-steps: support a single "step" key or a "steps" list.
		var subSteps []PipelineStep

		switch {
		case hasSingleStep:
			singleRaw, ok := config["step"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("foreach step %q: 'step' must be a map", name)
			}
			step, err := buildSubStep(name, "step", singleRaw, registryFn, app)
			if err != nil {
				return nil, fmt.Errorf("foreach step %q: %w", name, err)
			}
			subSteps = []PipelineStep{step}

		case hasStepsList:
			stepsRaw, ok := config["steps"].([]any)
			if !ok {
				return nil, fmt.Errorf("foreach step %q: 'steps' must be a list", name)
			}
			subSteps = make([]PipelineStep, 0, len(stepsRaw))
			for i, raw := range stepsRaw {
				stepCfg, ok := raw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("foreach step %q: steps[%d] must be a map", name, i)
				}
				step, err := buildSubStep(name, fmt.Sprintf("sub-%d", i), stepCfg, registryFn, app)
				if err != nil {
					return nil, fmt.Errorf("foreach step %q: %w", name, err)
				}
				subSteps = append(subSteps, step)
			}

		default:
			subSteps = []PipelineStep{}
		}

		return &ForEachStep{
			name:          name,
			collection:    collection,
			itemKey:       itemKey,
			indexKey:      indexKey,
			subSteps:      subSteps,
			tmpl:          NewTemplateEngine(),
			concurrency:   concurrency,
			errorStrategy: errorStrategy,
		}, nil
	}
}

// Name returns the step name.
func (s *ForEachStep) Name() string { return s.name }

// Execute iterates over the collection and runs sub-steps for each item.
func (s *ForEachStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the collection from the pipeline context
	items, err := s.resolveCollection(pc)
	if err != nil {
		return nil, fmt.Errorf("foreach step %q: %w", s.name, err)
	}

	// Handle empty collections gracefully
	if len(items) == 0 {
		return &StepResult{
			Output: map[string]any{
				"results": []any{},
				"count":   0,
			},
		}, nil
	}

	if s.concurrency > 0 {
		return s.executeConcurrent(ctx, pc, items)
	}
	return s.executeSequential(ctx, pc, items)
}

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

outer:
	for i, item := range items {
		// Stop launching new goroutines if the context is already cancelled
		// (e.g., from a previous fail_fast error or external cancellation).
		if branchCtx.Err() != nil {
			break outer
		}

		i, item := i, item

		// Acquire a semaphore slot, but respect context cancellation so we don't
		// block indefinitely when fail_fast has already cancelled the context.
		select {
		case sem <- struct{}{}: // acquired slot; fall through to launch goroutine
		case <-branchCtx.Done():
			break outer
		}

		wg.Add(1)
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
		switch {
		case errs[i] != nil:
			errorCount++
			collected = append(collected, map[string]any{
				"_error": errs[i].Error(),
				"_index": i,
			})
		case results[i] != nil:
			collected = append(collected, results[i])
		default:
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

// resolveCollection resolves the collection field to a []any.
func (s *ForEachStep) resolveCollection(pc *PipelineContext) ([]any, error) {
	// Look up the field path directly in Current (handles simple keys)
	if val, ok := pc.Current[s.collection]; ok {
		return foreachToSlice(val)
	}

	// Try trigger data
	if val, ok := pc.TriggerData[s.collection]; ok {
		return foreachToSlice(val)
	}

	// Try dot-separated path through the full template data
	// (e.g., "steps.fetch.rows" or nested keys)
	data := make(map[string]any)
	maps.Copy(data, pc.Current)
	data["steps"] = pc.StepOutputs
	data["trigger"] = pc.TriggerData

	if val, found := foreachWalkPath(data, s.collection); found {
		return foreachToSlice(val)
	}

	return nil, fmt.Errorf("collection %q not found in context", s.collection)
}

// buildChildContext creates a child PipelineContext with item and index injected.
func (s *ForEachStep) buildChildContext(parent *PipelineContext, item any, index int) *PipelineContext {
	// Copy trigger data
	childTrigger := make(map[string]any)
	maps.Copy(childTrigger, parent.TriggerData)

	// Copy metadata
	childMeta := make(map[string]any)
	maps.Copy(childMeta, parent.Metadata)

	// Build current: start with parent's current, inject item and index.
	childCurrent := make(map[string]any)
	maps.Copy(childCurrent, parent.Current)
	childCurrent[s.itemKey] = item
	childCurrent[s.indexKey] = index

	// Inject a "foreach" map so templates can use {{.foreach.index}}.
	// Only set it when it won't conflict with the user-chosen item/index keys
	// or an existing "foreach" key in the parent context.
	if s.itemKey != "foreach" && s.indexKey != "foreach" {
		if _, exists := childCurrent["foreach"]; !exists {
			childCurrent["foreach"] = map[string]any{
				"index":   index,
				s.itemKey: item,
			}
		}
	}

	// Copy step outputs
	childOutputs := make(map[string]map[string]any)
	for k, v := range parent.StepOutputs {
		out := make(map[string]any)
		maps.Copy(out, v)
		childOutputs[k] = out
	}

	return &PipelineContext{
		TriggerData: childTrigger,
		StepOutputs: childOutputs,
		Current:     childCurrent,
		Metadata:    childMeta,
	}
}

// foreachWalkPath traverses a dot-separated path through nested maps.
// Returns the found value and true if found, or nil and false if not.
// It handles both map[string]any and map[string]map[string]any (step outputs).
func foreachWalkPath(data map[string]any, path string) (any, bool) {
	// Try the full path as a key first
	if val, ok := data[path]; ok {
		return val, true
	}

	// Walk dot-separated segments
	current := any(data)
	segments := foreachSplitPath(path)
	for _, seg := range segments {
		switch m := current.(type) {
		case map[string]any:
			val, ok := m[seg]
			if !ok {
				return nil, false
			}
			current = val
		case map[string]map[string]any:
			// Step outputs are stored as map[string]map[string]any
			val, ok := m[seg]
			if !ok {
				return nil, false
			}
			current = val
		default:
			return nil, false
		}
	}
	return current, true
}

// foreachSplitPath splits a dot-separated path into segments.
func foreachSplitPath(path string) []string {
	var segs []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			segs = append(segs, path[start:i])
			start = i + 1
		}
	}
	segs = append(segs, path[start:])
	return segs
}

// foreachToSlice converts a value to []any if possible.
func foreachToSlice(val any) ([]any, error) {
	switch v := val.(type) {
	case []any:
		return v, nil
	case []map[string]any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result, nil
	case []string:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result, nil
	case []int:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result, nil
	case nil:
		return []any{}, nil
	default:
		return nil, fmt.Errorf("expected a slice, got %T", val)
	}
}
