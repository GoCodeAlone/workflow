package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

// SubWorkflowStep invokes a registered plugin workflow as a sub-workflow.
type SubWorkflowStep struct {
	name          string
	workflow      string // qualified name: "plugin:workflow"
	inputMapping  map[string]string
	outputMapping map[string]string
	timeout       time.Duration
	registry      *plugin.PluginWorkflowRegistry
	stepBuilder   SubWorkflowStepBuilder
	tmpl          *TemplateEngine
}

// SubWorkflowStepBuilder builds pipeline steps from a workflow config's pipeline
// definitions. This is injected by the engine so the sub_workflow step can
// construct child pipelines without a circular dependency on engine.
type SubWorkflowStepBuilder func(pipelineName string, cfg *config.WorkflowConfig, app modular.Application) (*Pipeline, error)

// NewSubWorkflowStepFactory returns a StepFactory that creates SubWorkflowStep
// instances. The registry and stepBuilder are captured by closure so that
// the factory has access to them at step creation time.
func NewSubWorkflowStepFactory(registry *plugin.PluginWorkflowRegistry, stepBuilder SubWorkflowStepBuilder) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		workflowName, _ := cfg["workflow"].(string)
		if workflowName == "" {
			return nil, fmt.Errorf("sub_workflow step %q: 'workflow' is required", name)
		}

		step := &SubWorkflowStep{
			name:        name,
			workflow:    workflowName,
			timeout:     30 * time.Second,
			registry:    registry,
			stepBuilder: stepBuilder,
			tmpl:        NewTemplateEngine(),
		}

		if im, ok := cfg["input_mapping"].(map[string]any); ok {
			step.inputMapping = make(map[string]string, len(im))
			for k, v := range im {
				if s, ok := v.(string); ok {
					step.inputMapping[k] = s
				}
			}
		}

		if om, ok := cfg["output_mapping"].(map[string]any); ok {
			step.outputMapping = make(map[string]string, len(om))
			for k, v := range om {
				if s, ok := v.(string); ok {
					step.outputMapping[k] = s
				}
			}
		}

		if timeout, ok := cfg["timeout"].(string); ok && timeout != "" {
			if d, err := time.ParseDuration(timeout); err == nil {
				step.timeout = d
			}
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *SubWorkflowStep) Name() string { return s.name }

// Execute runs the sub-workflow: looks up the embedded workflow, builds
// a child pipeline, maps inputs, executes, and maps outputs back.
func (s *SubWorkflowStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Look up the workflow in the registry
	ewf, ok := s.registry.Get(s.workflow)
	if !ok {
		return nil, fmt.Errorf("sub_workflow step %q: workflow %q not found in registry", s.name, s.workflow)
	}

	// Resolve workflow config — prefer parsed Config, fall back to YAML string
	wfCfg := ewf.Config
	if wfCfg == nil && ewf.ConfigYAML != "" {
		parsed, err := config.LoadFromString(ewf.ConfigYAML)
		if err != nil {
			return nil, fmt.Errorf("sub_workflow step %q: failed to parse workflow %q config YAML: %w", s.name, s.workflow, err)
		}
		wfCfg = parsed
	}
	if wfCfg == nil {
		return nil, fmt.Errorf("sub_workflow step %q: workflow %q has no config", s.name, s.workflow)
	}

	// Build the child pipeline from the workflow config.
	// Use the first pipeline defined, or the one matching the workflow name.
	childPipeline, err := s.stepBuilder(ewf.Name, wfCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("sub_workflow step %q: failed to build child pipeline for %q: %w", s.name, s.workflow, err)
	}

	// Map inputs from parent context to child trigger data
	triggerData := make(map[string]any)
	if s.inputMapping != nil {
		for childKey, tmplExpr := range s.inputMapping {
			resolved, resolveErr := s.tmpl.Resolve(tmplExpr, pc)
			if resolveErr != nil {
				return nil, fmt.Errorf("sub_workflow step %q: failed to resolve input %q: %w", s.name, childKey, resolveErr)
			}
			triggerData[childKey] = resolved
		}
	} else {
		// No explicit mapping — pass parent's current data
		for k, v := range pc.Current {
			triggerData[k] = v
		}
	}

	// Execute the child pipeline
	childCtx, err := childPipeline.Execute(ctx, triggerData)
	if err != nil {
		return nil, fmt.Errorf("sub_workflow step %q: child workflow %q failed: %w", s.name, s.workflow, err)
	}

	// Map outputs back to parent context
	output := make(map[string]any)
	if s.outputMapping != nil {
		for parentKey, childPath := range s.outputMapping {
			output[parentKey] = resolveOutputPath(childCtx, childPath)
		}
	} else {
		// No explicit mapping — return all child outputs under a "result" key
		output["result"] = childCtx.Current
	}

	return &StepResult{Output: output}, nil
}

// resolveOutputPath extracts a value from the child pipeline context using
// a dot-separated path. Supports "result.field" (from Current) and
// "steps.stepName.field" (from StepOutputs).
func resolveOutputPath(childCtx *PipelineContext, path string) any {
	// Try direct key in Current first
	if v, ok := childCtx.Current[path]; ok {
		return v
	}

	// Walk the dot-separated path through Current
	return walkPath(childCtx.Current, path)
}

// walkPath traverses a nested map using a dot-separated path.
func walkPath(data map[string]any, path string) any {
	parts := splitDotPath(path)
	var current any = data

	for _, part := range parts {
		switch m := current.(type) {
		case map[string]any:
			current = m[part]
		default:
			return nil
		}
	}

	return current
}

// splitDotPath splits a path by dots, e.g. "result.id" -> ["result", "id"].
func splitDotPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		parts = append(parts, path[start:])
	}
	return parts
}

// Ensure interface satisfaction
var _ PipelineStep = (*SubWorkflowStep)(nil)
