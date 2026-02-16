package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/GoCodeAlone/workflow/observability/tracing"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ExecutionTracker wraps pipeline execution with V1Store recording.
// It also implements EventRecorder so the pipeline can push step-level
// events that are persisted to execution_steps and execution_logs.
type ExecutionTracker struct {
	Store      *V1Store
	WorkflowID string

	// EventStoreRecorder is an optional EventRecorder (typically the
	// EventRecorderAdapter wrapping the SQLite event store) that should
	// receive copies of all events. When CQRS handler pipelines don't
	// have their own EventRecorder, this ensures events still flow to
	// the event store for the store browser and timeline features.
	EventStoreRecorder EventRecorder

	// Tracer is an optional OTEL WorkflowTracer. When set, the tracker
	// creates spans for each execution and step alongside DB writes.
	Tracer *tracing.WorkflowTracer

	// mu protects stepIDs and seqCounter during concurrent event recording.
	mu         sync.Mutex
	stepIDs    map[string]string     // step name -> step record ID
	stepSpans  map[string]trace.Span // step name -> OTEL span
	seqCounter int                   // auto-incrementing sequence number
	execID     string                // current execution ID
	execSpan   trace.Span            // OTEL span for current execution

	// chained is an optional upstream EventRecorder to forward events to.
	chained EventRecorder
}

// RecordEvent implements EventRecorder. It is called by the Pipeline for each
// execution event (step.started, step.completed, step.failed, etc.).
// Events are recorded best-effort — errors are silently ignored.
func (t *ExecutionTracker) RecordEvent(ctx context.Context, executionID string, eventType string, data map[string]any) error {
	// Forward to chained recorder first (if any)
	if t.chained != nil {
		_ = t.chained.RecordEvent(ctx, executionID, eventType, data)
	}

	if t.Store == nil || t.WorkflowID == "" {
		return nil
	}

	now := time.Now()

	switch eventType {
	case "step.started":
		t.handleStepStarted(ctx, executionID, data, now)
	case "step.completed":
		t.handleStepCompleted(data, now)
	case "step.failed":
		t.handleStepFailed(data, now)
	}

	// Write to execution_logs for ALL event types.
	t.writeLog(executionID, eventType, data, now)

	return nil
}

func (t *ExecutionTracker) handleStepStarted(ctx context.Context, executionID string, data map[string]any, now time.Time) {
	stepName, _ := data["step_name"].(string)
	if stepName == "" {
		return
	}

	stepID := uuid.New().String()
	stepType, _ := data["step_type"].(string)
	if stepType == "" {
		stepType = stepName
	}

	t.mu.Lock()
	if t.stepIDs == nil {
		t.stepIDs = make(map[string]string)
	}
	if t.stepSpans == nil {
		t.stepSpans = make(map[string]trace.Span)
	}
	t.seqCounter++
	seq := t.seqCounter
	t.stepIDs[stepName] = stepID

	// Start OTEL step span if tracer is configured
	if t.Tracer != nil {
		_, stepSpan := t.Tracer.StartStep(ctx, stepName, stepType)
		stepSpan.SetAttributes(
			attribute.String("execution.id", executionID),
			attribute.String("step.id", stepID),
			attribute.Int("step.sequence", seq),
		)
		t.stepSpans[stepName] = stepSpan
	}
	t.mu.Unlock()

	_ = t.Store.InsertExecutionStep(stepID, executionID, stepName, stepType, "running", seq, now)
}

func (t *ExecutionTracker) handleStepCompleted(data map[string]any, now time.Time) {
	stepName, _ := data["step_name"].(string)
	if stepName == "" {
		return
	}

	t.mu.Lock()
	stepID := t.stepIDs[stepName]
	stepSpan := t.stepSpans[stepName]
	delete(t.stepSpans, stepName)
	t.mu.Unlock()

	if stepID == "" {
		return
	}

	elapsed := parseDuration(data)
	_ = t.Store.CompleteExecutionStep(stepID, "completed", now, elapsed, "")

	// End OTEL span
	if stepSpan != nil {
		if t.Tracer != nil {
			t.Tracer.SetSuccess(stepSpan)
		}
		stepSpan.End()
	}
}

func (t *ExecutionTracker) handleStepFailed(data map[string]any, now time.Time) {
	stepName, _ := data["step_name"].(string)
	if stepName == "" {
		return
	}

	t.mu.Lock()
	stepID := t.stepIDs[stepName]
	stepSpan := t.stepSpans[stepName]
	delete(t.stepSpans, stepName)
	t.mu.Unlock()

	if stepID == "" {
		return
	}

	errMsg, _ := data["error"].(string)
	elapsed := parseDuration(data)
	_ = t.Store.CompleteExecutionStep(stepID, "failed", now, elapsed, errMsg)

	// End OTEL span with error
	if stepSpan != nil {
		if t.Tracer != nil && errMsg != "" {
			t.Tracer.RecordError(stepSpan, fmt.Errorf("%s", errMsg))
		}
		stepSpan.End()
	}
}

func (t *ExecutionTracker) writeLog(executionID, eventType string, data map[string]any, now time.Time) {
	level := "event"
	message := eventType
	moduleName := ""

	if stepName, ok := data["step_name"].(string); ok {
		moduleName = stepName
	}

	switch eventType {
	case "step.failed", "execution.failed":
		level = "error"
		if errMsg, ok := data["error"].(string); ok {
			message = fmt.Sprintf("%s: %s", eventType, errMsg)
		}
	case "step.started":
		level = "info"
		message = fmt.Sprintf("Step started: %s", moduleName)
	case "step.completed":
		level = "info"
		if elapsed, ok := data["elapsed"].(string); ok {
			message = fmt.Sprintf("Step completed: %s (%s)", moduleName, elapsed)
		} else {
			message = fmt.Sprintf("Step completed: %s", moduleName)
		}
	case "execution.started":
		level = "info"
		message = fmt.Sprintf("Execution started: pipeline=%v", data["pipeline"])
	case "execution.completed":
		level = "info"
		if elapsed, ok := data["elapsed"].(string); ok {
			message = fmt.Sprintf("Execution completed (%s)", elapsed)
		}
	}

	fieldsJSON := "{}"
	if len(data) > 0 {
		if b, err := json.Marshal(data); err == nil {
			fieldsJSON = string(b)
		}
	}

	// Write info/error-level log entry
	_ = t.Store.InsertLog(t.WorkflowID, executionID, level, message, moduleName, fieldsJSON, now)

	// Also write event-level entry for the events view (if not already event level)
	if level != "event" {
		_ = t.Store.InsertLog(t.WorkflowID, executionID, "event", eventType, moduleName, fieldsJSON, now)
	}
}

// extractTriggeredBy tries to extract the user identity from JWT claims
// on the request context. It checks for email, sub, and user_id fields.
func extractTriggeredBy(r *http.Request) string {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok || claims == nil {
		return ""
	}
	// Try common claim fields in priority order
	for _, key := range []string{"email", "sub", "user_id", "preferred_username", "name"} {
		if v, ok := claims[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// parseDuration extracts elapsed duration from event data as milliseconds.
func parseDuration(data map[string]any) int64 {
	if elapsed, ok := data["elapsed"].(string); ok {
		if d, err := time.ParseDuration(elapsed); err == nil {
			return d.Milliseconds()
		}
	}
	return 0
}

// TrackPipelineExecution wraps a pipeline execution call, recording the
// execution and its steps in the V1Store. It returns the PipelineContext
// and any error from the underlying pipeline execution.
func (t *ExecutionTracker) TrackPipelineExecution(
	ctx context.Context,
	pipeline *Pipeline,
	triggerData map[string]any,
	r *http.Request,
) (*PipelineContext, error) {
	if t == nil || t.Store == nil {
		return pipeline.Execute(ctx, triggerData)
	}

	execID := uuid.New().String()
	triggerType := "http"
	if r != nil {
		triggerType = fmt.Sprintf("%s %s", r.Method, r.URL.Path)
	}
	startedAt := time.Now()

	// Reset per-execution state
	t.mu.Lock()
	t.stepIDs = make(map[string]string)
	t.stepSpans = make(map[string]trace.Span)
	t.seqCounter = 0
	t.execID = execID
	t.execSpan = nil
	t.mu.Unlock()

	// Extract user info from JWT claims on the request context
	triggeredBy := ""
	if r != nil {
		triggeredBy = extractTriggeredBy(r)
	}

	// Start OTEL execution span if tracer is configured
	execCtx := ctx
	if t.Tracer != nil {
		var span trace.Span
		execCtx, span = t.Tracer.StartWorkflow(ctx, t.WorkflowID, triggerType)
		span.SetAttributes(
			attribute.String("execution.id", execID),
			attribute.String("workflow.id", t.WorkflowID),
		)
		if triggeredBy != "" {
			span.SetAttributes(attribute.String("triggered_by", triggeredBy))
		}
		t.mu.Lock()
		t.execSpan = span
		t.mu.Unlock()
	}

	// Best-effort: don't fail the request if tracking fails
	_ = t.Store.InsertExecution(execID, t.WorkflowID, triggerType, "running", triggeredBy, startedAt)

	// Set execution ID on pipeline for event correlation
	pipeline.ExecutionID = execID

	// Set ourselves as EventRecorder, chaining to any existing one.
	// If the pipeline already has an EventRecorder (e.g., from PipelineWorkflowHandler),
	// chain to that. Otherwise, chain to the EventStoreRecorder if available.
	// IMPORTANT: Never chain to ourselves — that causes infinite recursion.
	if pipeline.EventRecorder != nil && pipeline.EventRecorder != EventRecorder(t) {
		t.chained = pipeline.EventRecorder
	} else if t.EventStoreRecorder != nil && t.EventStoreRecorder != EventRecorder(t) {
		t.chained = t.EventStoreRecorder
	} else {
		t.chained = nil
	}
	pipeline.EventRecorder = t

	pc, pipeErr := pipeline.Execute(execCtx, triggerData)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	status := "completed"
	errMsg := ""
	if pipeErr != nil {
		status = "failed"
		errMsg = pipeErr.Error()
	}

	_ = t.Store.CompleteExecution(execID, status, completedAt, durationMs, errMsg)

	// End OTEL execution span
	t.mu.Lock()
	span := t.execSpan
	t.execSpan = nil
	t.mu.Unlock()
	if span != nil {
		span.SetAttributes(
			attribute.Int64("execution.duration_ms", durationMs),
			attribute.String("execution.status", status),
		)
		if pipeErr != nil {
			t.Tracer.RecordError(span, pipeErr)
		} else {
			t.Tracer.SetSuccess(span)
		}
		span.End()
	}

	return pc, pipeErr
}
