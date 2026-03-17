package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// BranchStep evaluates a field and executes only the matched branch's sub-steps
// inline, then routes to merge_step. Unlike step.conditional (which jumps to a
// single named step and falls through the remaining pipeline steps), step.branch
// runs an isolated sub-pipeline for the matched branch and jumps to merge_step
// when done, skipping all other branches.
type BranchStep struct {
	name         string
	field        string
	branches     map[string][]PipelineStep
	defaultSteps []PipelineStep
	mergeStep    string
	tmpl         *TemplateEngine
}

// NewBranchStepFactory returns a StepFactory that creates BranchStep instances.
// registryFn is called at step-creation time (lazy pattern, same as step.foreach
// and step.parallel) so the full step registry is available even if step.branch
// is registered before all step types.
func NewBranchStepFactory(registryFn func() *StepRegistry) StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		field, _ := config["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("branch step %q: 'field' is required", name)
		}

		branchesRaw, _ := config["branches"].(map[string]any)
		if len(branchesRaw) == 0 {
			return nil, fmt.Errorf("branch step %q: 'branches' map is required and must not be empty", name)
		}

		branches := make(map[string][]PipelineStep, len(branchesRaw))
		for key, val := range branchesRaw {
			steps, err := buildBranchSteps(name, key, val, registryFn, app)
			if err != nil {
				return nil, err
			}
			branches[key] = steps
		}

		var defaultSteps []PipelineStep
		if defaultRaw, ok := config["default"]; ok {
			steps, err := buildBranchSteps(name, "default", defaultRaw, registryFn, app)
			if err != nil {
				return nil, err
			}
			defaultSteps = steps
		}

		mergeStep, _ := config["merge_step"].(string)

		return &BranchStep{
			name:         name,
			field:        field,
			branches:     branches,
			defaultSteps: defaultSteps,
			mergeStep:    mergeStep,
			tmpl:         NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *BranchStep) Name() string { return s.name }

// Execute resolves the field value, selects the matching branch, runs its
// sub-steps sequentially on the shared PipelineContext, and returns
// NextStep=merge_step so the parent executor jumps to the merge point.
func (s *BranchStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	tmplExpr := buildFieldTemplate(s.field)
	resolved, err := s.tmpl.Resolve(tmplExpr, pc)
	if err != nil {
		return nil, fmt.Errorf("branch step %q: failed to resolve field %q: %w", s.name, s.field, err)
	}

	subSteps, ok := s.branches[resolved]
	if !ok {
		if len(s.defaultSteps) == 0 {
			return nil, fmt.Errorf("branch step %q: value %q not found in branches and no default configured", s.name, resolved)
		}
		subSteps = s.defaultSteps
	}

	for _, step := range subSteps {
		result, err := step.Execute(ctx, pc)
		if err != nil {
			return nil, fmt.Errorf("branch step %q: sub-step %q failed: %w", s.name, step.Name(), err)
		}
		if result != nil && result.Output != nil {
			pc.MergeStepOutput(step.Name(), result.Output)
		} else {
			pc.MergeStepOutput(step.Name(), map[string]any{})
		}
		if result != nil && result.Stop {
			return &StepResult{
				Output: map[string]any{
					"matched_value": resolved,
					"branch":        resolved,
					"stopped":       true,
				},
				Stop: true,
			}, nil
		}
	}

	return &StepResult{
		Output: map[string]any{
			"matched_value": resolved,
			"branch":        resolved,
		},
		NextStep: s.mergeStep,
	}, nil
}

// buildBranchSteps parses a branch value (must be []any of step config maps)
// and builds the sub-steps using the registry.
func buildBranchSteps(parentName, branchKey string, val any, registryFn func() *StepRegistry, app modular.Application) ([]PipelineStep, error) {
	raw, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("branch step %q: branch %q must be a list of step configs", parentName, branchKey)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("branch step %q: branch %q must not be empty", parentName, branchKey)
	}

	steps := make([]PipelineStep, 0, len(raw))
	for i, item := range raw {
		stepCfg, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("branch step %q: branch %q step[%d] must be a map", parentName, branchKey, i)
		}
		step, err := buildSubStep(parentName, fmt.Sprintf("%s[%d]", branchKey, i), stepCfg, registryFn, app)
		if err != nil {
			return nil, fmt.Errorf("branch step %q: branch %q: %w", parentName, branchKey, err)
		}
		steps = append(steps, step)
	}
	return steps, nil
}
