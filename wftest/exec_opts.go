package wftest

import (
	"context"
	"fmt"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ExecOption configures a single pipeline execution (used by ExecutePipelineOpts).
type ExecOption func(*execConfig)

type execConfig struct {
	stopAfter string
}

// StopAfter halts pipeline execution after the named step completes.
// Steps after the named step are not executed.
func StopAfter(stepName string) ExecOption {
	return func(c *execConfig) { c.stopAfter = stepName }
}

// ExecutePipelineOpts runs a named pipeline with optional per-execution options.
// It supports StopAfter to halt execution at a named step.
func (h *Harness) ExecutePipelineOpts(name string, data map[string]any, opts ...ExecOption) *Result {
	h.t.Helper()

	cfg := &execConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	if cfg.stopAfter == "" {
		return h.ExecutePipeline(name, data)
	}

	return h.executePipelineWithStopAfter(name, data, cfg.stopAfter)
}

// executePipelineWithStopAfter injects a sentinel step after the named step
// that signals the pipeline to stop, then executes the pipeline.
func (h *Harness) executePipelineWithStopAfter(name string, data map[string]any, stopAfter string) *Result {
	h.t.Helper()

	pipeline, ok := h.engine.GetPipeline(name)
	if !ok {
		return &Result{Error: fmt.Errorf("pipeline %q not found", name)}
	}

	// Find the target step index.
	stopIdx := -1
	for i, step := range pipeline.Steps {
		if step.Name() == stopAfter {
			stopIdx = i
			break
		}
	}
	if stopIdx == -1 {
		return &Result{Error: fmt.Errorf("stop_after: step %q not found in pipeline %q", stopAfter, name)}
	}

	// Insert a sentinel step immediately after the target step.
	sentinel := &stopSentinel{}
	insertAt := stopIdx + 1
	pipeline.Steps = append(pipeline.Steps, nil)
	copy(pipeline.Steps[insertAt+1:], pipeline.Steps[insertAt:])
	pipeline.Steps[insertAt] = sentinel

	// Remove the sentinel after execution regardless of outcome.
	defer func() {
		pipeline.Steps = append(pipeline.Steps[:insertAt], pipeline.Steps[insertAt+1:]...)
	}()

	// Execute using the same logic as ExecutePipeline.
	ctx := h.t.Context()
	start := time.Now()
	pc, err := pipeline.Execute(ctx, data)
	if err != nil {
		return &Result{Error: err, Duration: time.Since(start)}
	}

	output := pc.Current
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		output = pipeOut
	}

	// Remove the sentinel's own entry from StepOutputs so callers only see
	// real steps in StepResults.
	if pc.StepOutputs != nil {
		delete(pc.StepOutputs, sentinel.Name())
	}

	return &Result{
		Output:      output,
		StepResults: pc.StepOutputs,
		Duration:    time.Since(start),
	}
}

// stopSentinel is a PipelineStep that signals the pipeline to stop immediately.
type stopSentinel struct{}

func (s *stopSentinel) Name() string { return "__wftest_stop_sentinel__" }

func (s *stopSentinel) Execute(_ context.Context, _ *interfaces.PipelineContext) (*interfaces.StepResult, error) {
	return &interfaces.StepResult{Stop: true}, nil
}
