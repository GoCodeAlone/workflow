package module

import (
	"context"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
)

// WorkflowCallMode determines how a workflow_call step waits for results.
type WorkflowCallMode string

const (
	// WorkflowCallModeSync executes the target pipeline synchronously and maps outputs back.
	WorkflowCallModeSync WorkflowCallMode = "sync"
	// WorkflowCallModeAsync fires the target pipeline and returns immediately without waiting.
	WorkflowCallModeAsync WorkflowCallMode = "async"
)

// PipelineLookupFn is a function that resolves a named pipeline by name.
// The engine provides this when building a WorkflowCallStep so the step can
// locate sibling pipelines at execution time without taking a direct dependency
// on the engine.
type PipelineLookupFn func(name string) (*Pipeline, bool)

// WorkflowCallStep invokes another pipeline registered in the same engine.
// It supports synchronous and asynchronous execution modes with input/output
// template mapping identical to the sub_workflow step pattern.
type WorkflowCallStep struct {
	name          string
	workflow      string           // target pipeline name
	mode          WorkflowCallMode // "sync" (default) or "async"
	inputMapping  map[string]string
	outputMapping map[string]string
	timeout       time.Duration
	lookup        PipelineLookupFn
	tmpl          *TemplateEngine
}

// NewWorkflowCallStepFactory returns a StepFactory for step.workflow_call.
// The lookup function is captured by closure so the step can resolve target
// pipelines at execution time (supporting pipelines registered after factory creation).
func NewWorkflowCallStepFactory(lookup PipelineLookupFn) StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		workflowName, _ := cfg["workflow"].(string)
		if workflowName == "" {
			return nil, fmt.Errorf("workflow_call step %q: 'workflow' is required", name)
		}

		mode := WorkflowCallModeSync
		if m, ok := cfg["mode"].(string); ok && m == string(WorkflowCallModeAsync) {
			mode = WorkflowCallModeAsync
		}

		step := &WorkflowCallStep{
			name:     name,
			workflow: workflowName,
			mode:     mode,
			timeout:  30 * time.Second,
			lookup:   lookup,
			tmpl:     NewTemplateEngine(),
		}

		if im, ok := cfg["input"].(map[string]any); ok {
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
func (s *WorkflowCallStep) Name() string { return s.name }

// Execute runs the target workflow. In sync mode it waits for the result and
// maps outputs back into the parent context. In async mode it dispatches the
// child pipeline in a goroutine and returns immediately.
func (s *WorkflowCallStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the target pipeline
	if s.lookup == nil {
		return nil, fmt.Errorf("workflow_call step %q: no pipeline lookup function configured", s.name)
	}
	target, ok := s.lookup(s.workflow)
	if !ok {
		return nil, fmt.Errorf("workflow_call step %q: pipeline %q not found — ensure it is defined in the application config", s.name, s.workflow)
	}

	// Build trigger data from input mapping or fall back to passing all current data
	triggerData := make(map[string]any)
	if s.inputMapping != nil {
		for childKey, tmplExpr := range s.inputMapping {
			resolved, resolveErr := s.tmpl.Resolve(tmplExpr, pc)
			if resolveErr != nil {
				return nil, fmt.Errorf("workflow_call step %q: failed to resolve input %q: %w", s.name, childKey, resolveErr)
			}
			triggerData[childKey] = resolved
		}
	} else {
		for k, v := range pc.Current {
			triggerData[k] = v
		}
	}

	if s.mode == WorkflowCallModeAsync {
		// Fire-and-forget: run in background goroutine with its own timeout
		go func() {
			asyncCtx, cancel := context.WithTimeout(context.Background(), s.timeout)
			defer cancel()
			_, _ = target.Execute(asyncCtx, triggerData) //nolint:errcheck
		}()
		return &StepResult{Output: map[string]any{"workflow": s.workflow, "mode": "async", "dispatched": true}}, nil
	}

	// Sync mode: apply timeout and wait for result
	syncCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	childCtx, err := target.Execute(syncCtx, triggerData)
	if err != nil {
		return nil, fmt.Errorf("workflow_call step %q: workflow %q failed: %w", s.name, s.workflow, err)
	}

	// Map outputs back to parent context
	output := make(map[string]any)
	if s.outputMapping != nil {
		for parentKey, childPath := range s.outputMapping {
			output[parentKey] = resolveOutputPath(childCtx, childPath)
		}
	} else {
		// No explicit mapping — return all child outputs under "result"
		output["result"] = childCtx.Current
	}

	return &StepResult{Output: output}, nil
}

// Ensure interface satisfaction at compile time.
var _ PipelineStep = (*WorkflowCallStep)(nil)
