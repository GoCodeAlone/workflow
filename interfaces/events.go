package interfaces

import (
	"context"
	"time"
)

// EventEmitter publishes workflow and step lifecycle events.
// *module.WorkflowEventEmitter satisfies this interface.
// All methods must be safe to call when no event bus is configured (no-ops).
type EventEmitter interface {
	EmitWorkflowStarted(ctx context.Context, workflowType, action string, data map[string]any)
	EmitWorkflowCompleted(ctx context.Context, workflowType, action string, duration time.Duration, results map[string]any)
	EmitWorkflowFailed(ctx context.Context, workflowType, action string, duration time.Duration, err error)
}

// MetricsRecorder records workflow execution metrics.
// *module.MetricsCollector satisfies this interface.
// All methods must be safe to call when no metrics backend is configured (no-ops).
type MetricsRecorder interface {
	RecordWorkflowExecution(workflowType, action, status string)
	RecordWorkflowDuration(workflowType, action string, duration time.Duration)
}
