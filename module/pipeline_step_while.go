package module

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// WhileStep executes sub-steps repeatedly while a condition template evaluates
// as truthy, with a hard iteration cap to prevent runaway loops.
//
// Useful for paginated API calls where a "next page" token controls iteration:
//
//	type: step.while
//	name: paginate-pages
//	config:
//	  condition: "{{.steps.fetch_page.has_next_page}}"
//	  max_iterations: 100
//	  iteration_var: "iter"
//	  accumulate:
//	    key: "all_items"
//	    from: "{{.steps.fetch_page.items}}"
//	  steps:
//	    - type: step.http_call
//	      name: fetch_page
//	      config: { url: "...", method: GET }
type WhileStep struct {
	name          string
	condition     string
	maxIterations int
	iterationVar  string
	accumKey      string // empty = no accumulate
	accumFrom     string
	subSteps      []PipelineStep
	tmpl          *TemplateEngine
}

// NewWhileStepFactory returns a StepFactory that creates WhileStep instances.
// registryFn is called at step-creation time to obtain the step registry used to
// build sub-steps. Passing a function (rather than the registry directly) allows
// the factory to be registered before the registry is fully populated.
func NewWhileStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		// --- condition (required) ---
		condition, _ := config["condition"].(string)
		if condition == "" {
			return nil, fmt.Errorf("while step %q: 'condition' is required", name)
		}

		// --- max_iterations (default 1000) ---
		maxIterations := 1000
		if v, ok := config["max_iterations"]; ok {
			switch val := v.(type) {
			case int:
				maxIterations = val
			case float64:
				maxIterations = int(val)
			}
			if maxIterations <= 0 {
				return nil, fmt.Errorf("while step %q: 'max_iterations' must be > 0, got %d", name, maxIterations)
			}
		}

		// --- iteration_var (optional) ---
		iterationVar, _ := config["iteration_var"].(string)

		// --- accumulate (optional) ---
		var accumKey, accumFrom string
		if accRaw, ok := config["accumulate"]; ok {
			accMap, ok := accRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("while step %q: 'accumulate' must be a map", name)
			}
			accumKey, _ = accMap["key"].(string)
			accumFrom, _ = accMap["from"].(string)
			if accumKey == "" {
				return nil, fmt.Errorf("while step %q: 'accumulate.key' is required when accumulate is set", name)
			}
			if accumFrom == "" {
				return nil, fmt.Errorf("while step %q: 'accumulate.from' is required when accumulate is set", name)
			}
		}

		// --- step / steps (mutually exclusive) ---
		_, hasSingleStep := config["step"]
		_, hasStepsList := config["steps"]

		if hasSingleStep && hasStepsList {
			return nil, fmt.Errorf("while step %q: 'step' and 'steps' are mutually exclusive", name)
		}

		var subSteps []PipelineStep

		switch {
		case hasSingleStep:
			singleRaw, ok := config["step"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("while step %q: 'step' must be a map", name)
			}
			s, err := buildSubStep(name, "step", singleRaw, registryFn, app)
			if err != nil {
				return nil, fmt.Errorf("while step %q: %w", name, err)
			}
			subSteps = []PipelineStep{s}

		case hasStepsList:
			stepsRaw, ok := config["steps"].([]any)
			if !ok {
				return nil, fmt.Errorf("while step %q: 'steps' must be a list", name)
			}
			subSteps = make([]PipelineStep, 0, len(stepsRaw))
			for i, raw := range stepsRaw {
				stepCfg, ok := raw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("while step %q: steps[%d] must be a map", name, i)
				}
				s, err := buildSubStep(name, fmt.Sprintf("sub-%d", i), stepCfg, registryFn, app)
				if err != nil {
					return nil, fmt.Errorf("while step %q: %w", name, err)
				}
				subSteps = append(subSteps, s)
			}

		default:
			subSteps = []PipelineStep{}
		}

		return &WhileStep{
			name:          name,
			condition:     condition,
			maxIterations: maxIterations,
			iterationVar:  iterationVar,
			accumKey:      accumKey,
			accumFrom:     accumFrom,
			subSteps:      subSteps,
			tmpl:          NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *WhileStep) Name() string { return s.name }

// Execute runs sub-steps in a loop while the condition template evaluates truthy.
func (s *WhileStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	var accumulator []any
	if s.accumKey != "" {
		accumulator = make([]any, 0)
	}

	i := 0
	for {
		// Respect context cancellation before each iteration.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Evaluate the condition against the current pipeline context.
		condStr, err := s.tmpl.Resolve(s.condition, pc)
		if err != nil {
			return nil, fmt.Errorf("while step %q: condition resolve error: %w", s.name, err)
		}

		if !whileIsTruthy(condStr) {
			break
		}

		// Check hard cap.
		if i >= s.maxIterations {
			return nil, fmt.Errorf("while step %q: exceeded max_iterations (%d)", s.name, s.maxIterations)
		}

		// Build child context for this iteration.
		childPC := s.buildChildContext(pc, i)

		// Run sub-steps sequentially.
		ranThisIter := make([]string, 0, len(s.subSteps))
		for _, subStep := range s.subSteps {
			result, execErr := subStep.Execute(ctx, childPC)
			if execErr != nil {
				return nil, fmt.Errorf("while step %q: iteration %d, sub-step %q failed: %w",
					s.name, i, subStep.Name(), execErr)
			}
			if result != nil && result.Output != nil {
				childPC.MergeStepOutput(subStep.Name(), result.Output)
				ranThisIter = append(ranThisIter, subStep.Name())
			}
			if result != nil && result.Stop {
				break
			}
		}

		// Propagate only the sub-steps that ran this iteration back to the parent
		// so the condition can see their outputs. Avoids re-merging the full set of
		// deep-copied parent outputs on every iteration.
		for _, stepName := range ranThisIter {
			pc.MergeStepOutput(stepName, childPC.StepOutputs[stepName])
		}

		// Accumulate if configured.
		if s.accumKey != "" {
			fromStr, resolveErr := s.tmpl.Resolve(s.accumFrom, pc)
			if resolveErr != nil {
				return nil, fmt.Errorf("while step %q: accumulate.from resolve error: %w", s.name, resolveErr)
			}

			// Retrieve the raw value from step outputs for array detection.
			// resolveStr is a string representation; we need the actual value.
			val := s.resolveAccumValue(fromStr, childPC, pc)
			if val != nil {
				if slice, err := foreachToSlice(val); err == nil {
					accumulator = append(accumulator, slice...)
				} else {
					accumulator = append(accumulator, val)
				}
			}
		}

		i++
	}

	output := map[string]any{
		"iterations": i,
	}
	if s.accumKey != "" {
		output[s.accumKey] = accumulator
	}
	return &StepResult{Output: output}, nil
}

// buildChildContext creates a child PipelineContext for one iteration, optionally
// injecting iteration_var with {index, first}.
func (s *WhileStep) buildChildContext(parent *PipelineContext, index int) *PipelineContext {
	childTrigger := make(map[string]any)
	maps.Copy(childTrigger, parent.TriggerData)

	childMeta := make(map[string]any)
	maps.Copy(childMeta, parent.Metadata)

	childCurrent := make(map[string]any)
	maps.Copy(childCurrent, parent.Current)

	if s.iterationVar != "" {
		childCurrent[s.iterationVar] = map[string]any{
			"index": index,
			"first": index == 0,
		}
	}

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

// resolveAccumValue extracts the actual value for accumulation.
// accumFrom resolves to a string via the template engine; we want the native value
// (e.g. []any) so we look it up in the step outputs directly.
// This mirrors the approach used by foreach's resolveCollection.
func (s *WhileStep) resolveAccumValue(resolvedStr string, childPC, parentPC *PipelineContext) any {
	// If the resolved string contains "<no value>", skip.
	if strings.Contains(resolvedStr, "<no value>") {
		return nil
	}

	// Try to find the raw value from step outputs by walking the template expression.
	// accumFrom is a template like "{{.steps.fetch.items}}" — extract the path and look it up.
	path := extractTemplatePath(s.accumFrom)
	if path != "" {
		data := make(map[string]any)
		maps.Copy(data, parentPC.Current)
		data["steps"] = parentPC.StepOutputs
		data["trigger"] = parentPC.TriggerData
		data["meta"] = parentPC.Metadata
		if val, found := foreachWalkPath(data, path); found {
			return val
		}
	}

	// Fall back to the string value if we couldn't find a native value.
	if resolvedStr == "" {
		return nil
	}
	return resolvedStr
}

// extractTemplatePath extracts the dot-path from a simple template expression.
// E.g. "{{.steps.fetch.items}}" → "steps.fetch.items".
// Returns empty string for complex expressions that can't be parsed as a path.
func extractTemplatePath(tmplStr string) string {
	s := strings.TrimSpace(tmplStr)
	if !strings.HasPrefix(s, "{{") || !strings.HasSuffix(s, "}}") {
		return ""
	}
	inner := strings.TrimSpace(s[2 : len(s)-2])
	if !strings.HasPrefix(inner, ".") {
		return ""
	}
	// Reject complex expressions (spaces, functions, etc.)
	inner = inner[1:] // strip leading dot
	if strings.ContainsAny(inner, " \t()\"'|") {
		return ""
	}
	return inner
}

// whileIsTruthy evaluates a value for truthiness according to while-step semantics:
//   - bool → as-is
//   - string → non-empty AND not "false" AND not "0" AND not "<no value>"
//   - int/int64/float64 → non-zero
//   - nil → false
//   - anything else → true
func whileIsTruthy(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		if val == "" || val == "false" || val == "0" {
			return false
		}
		if strings.Contains(val, "<no value>") {
			return false
		}
		return true
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case nil:
		return false
	default:
		return true
	}
}
