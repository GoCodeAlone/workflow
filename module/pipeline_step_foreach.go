package module

import (
	"context"
	"fmt"
	"maps"

	"github.com/CrisisTextLine/modular"
)

// ForEachStep iterates over a collection and executes sub-steps for each item.
type ForEachStep struct {
	name       string
	collection string
	itemKey    string
	indexKey   string
	subSteps   []PipelineStep
	tmpl       *TemplateEngine
}

// NewForEachStepFactory returns a StepFactory that creates ForEachStep instances.
// registryFn is called at step-creation time to obtain the step registry used to
// build sub-steps. Passing a function (rather than the registry directly) allows
// the factory to be registered before the registry is fully populated, enabling
// sub-steps to themselves be any registered step type.
func NewForEachStepFactory(registryFn func() *StepRegistry, app modular.Application) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		registry := registryFn()

		collection, _ := config["collection"].(string)
		if collection == "" {
			return nil, fmt.Errorf("foreach step %q: 'collection' is required", name)
		}

		itemKey, _ := config["item_key"].(string)
		if itemKey == "" {
			itemKey = "item"
		}

		indexKey, _ := config["index_key"].(string)
		if indexKey == "" {
			indexKey = "index"
		}

		// Build sub-steps from inline config
		stepsRaw, _ := config["steps"].([]any)
		subSteps := make([]PipelineStep, 0, len(stepsRaw))
		for i, raw := range stepsRaw {
			stepCfg, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("foreach step %q: steps[%d] must be a map", name, i)
			}

			stepType, _ := stepCfg["type"].(string)
			if stepType == "" {
				return nil, fmt.Errorf("foreach step %q: steps[%d] missing 'type'", name, i)
			}

			stepName, _ := stepCfg["name"].(string)
			if stepName == "" {
				stepName = fmt.Sprintf("%s-sub-%d", name, i)
			}

			// Build the step config without meta fields
			subCfg := make(map[string]any)
			for k, v := range stepCfg {
				if k != "type" && k != "name" {
					subCfg[k] = v
				}
			}

			step, err := registry.Create(stepType, stepName, subCfg, app)
			if err != nil {
				return nil, fmt.Errorf("foreach step %q: failed to build sub-step %d (%s): %w", name, i, stepType, err)
			}
			subSteps = append(subSteps, step)
		}

		return &ForEachStep{
			name:       name,
			collection: collection,
			itemKey:    itemKey,
			indexKey:   indexKey,
			subSteps:   subSteps,
			tmpl:       NewTemplateEngine(),
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

	collected := make([]any, 0, len(items))

	for i, item := range items {
		// Create a child context with item and index injected
		childPC := s.buildChildContext(pc, item, i)

		// Execute each sub-step sequentially for this item
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

	// Build current: start with parent's current, inject item and index
	childCurrent := make(map[string]any)
	maps.Copy(childCurrent, parent.Current)
	childCurrent[s.itemKey] = item
	childCurrent[s.indexKey] = index

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
