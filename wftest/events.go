package wftest

import (
	"context"
	"time"

	"github.com/GoCodeAlone/workflow/module"
)

// FireEvent simulates an eventbus trigger firing for the given topic. It finds
// all EventBusTrigger instances registered with the engine and invokes their
// subscriptions synchronously using the provided context, so the pipeline runs
// inline before FireEvent returns.
//
// Unlike going through the real EventBus pub/sub channel, this bypasses the
// async dispatch goroutine, making it safe and deterministic for tests.
//
// The engine is started lazily on the first call so that trigger subscriptions
// are configured. The YAML config must include an eventbus trigger for the topic.
func (h *Harness) FireEvent(topic string, data map[string]any) *Result {
	h.t.Helper()
	h.ensureStarted()

	if data == nil {
		data = map[string]any{}
	}

	holder := &module.PipelineResultHolder{}
	ctx := context.WithValue(h.t.Context(), module.PipelineResultContextKey, holder)

	start := time.Now()
	for _, trigger := range h.engine.Triggers() {
		eb, ok := trigger.(*module.EventBusTrigger)
		if !ok {
			continue
		}
		if err := eb.InvokeForTopic(ctx, topic, data); err != nil {
			return &Result{Error: err, Duration: time.Since(start)}
		}
	}

	return &Result{
		Output:   holder.Get(),
		Duration: time.Since(start),
	}
}

// FireSchedule simulates a schedule trigger firing for the named pipeline.
// It calls ExecutePipelineContext directly (bypassing cron timing) so that
// step execution results are captured in the returned Result, exactly as they
// are for ExecutePipeline. params is merged into the trigger data; pass nil
// for none.
func (h *Harness) FireSchedule(pipelineName string, params map[string]any) *Result {
	h.t.Helper()
	h.ensureStarted()

	if params == nil {
		params = map[string]any{}
	}

	start := time.Now()
	pc, err := h.engine.ExecutePipelineContext(h.t.Context(), pipelineName, params)
	if err != nil {
		return &Result{Error: err, Duration: time.Since(start)}
	}

	// Prefer explicit pipeline output if step.pipeline_output was used.
	output := pc.Current
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		output = pipeOut
	}

	return &Result{
		Output:      output,
		StepResults: pc.StepOutputs,
		Duration:    time.Since(start),
	}
}
