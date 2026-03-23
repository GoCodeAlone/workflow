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
// It calls TriggerWorkflow directly (bypassing cron timing) and returns the
// pipeline result. params is merged into the trigger data; pass nil for none.
func (h *Harness) FireSchedule(pipelineName string, params map[string]any) *Result {
	h.t.Helper()
	h.ensureStarted()

	holder := &module.PipelineResultHolder{}
	ctx := context.WithValue(h.t.Context(), module.PipelineResultContextKey, holder)

	if params == nil {
		params = map[string]any{}
	}

	start := time.Now()
	if err := h.engine.TriggerWorkflow(ctx, "pipeline:"+pipelineName, "execute", params); err != nil {
		return &Result{Error: err, Duration: time.Since(start)}
	}

	return &Result{
		Output:   holder.Get(),
		Duration: time.Since(start),
	}
}
