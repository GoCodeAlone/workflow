package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ExecutionTracker wraps execution lifecycle to record executions, steps, and logs.
type ExecutionTracker struct {
	executions store.ExecutionStore
	logs       store.LogStore
}

// NewExecutionTracker creates a new ExecutionTracker.
func NewExecutionTracker(executions store.ExecutionStore, logs store.LogStore) *ExecutionTracker {
	return &ExecutionTracker{
		executions: executions,
		logs:       logs,
	}
}

// StartExecution begins tracking a new workflow execution.
func (t *ExecutionTracker) StartExecution(ctx context.Context, workflowID uuid.UUID, triggerType string, data json.RawMessage) (uuid.UUID, error) {
	now := time.Now()
	exec := &store.WorkflowExecution{
		ID:          uuid.New(),
		WorkflowID:  workflowID,
		TriggerType: triggerType,
		TriggerData: data,
		Status:      store.ExecutionStatusRunning,
		StartedAt:   now,
	}
	if err := t.executions.CreateExecution(ctx, exec); err != nil {
		return uuid.Nil, fmt.Errorf("start execution: %w", err)
	}
	return exec.ID, nil
}

// RecordStep records a step within an execution.
func (t *ExecutionTracker) RecordStep(ctx context.Context, executionID uuid.UUID, step *store.ExecutionStep) error {
	step.ExecutionID = executionID
	if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	return t.executions.CreateStep(ctx, step)
}

// CompleteExecution marks an execution as successfully completed.
func (t *ExecutionTracker) CompleteExecution(ctx context.Context, executionID uuid.UUID, output json.RawMessage) error {
	exec, err := t.executions.GetExecution(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get execution: %w", err)
	}

	now := time.Now()
	durationMs := now.Sub(exec.StartedAt).Milliseconds()
	exec.Status = store.ExecutionStatusCompleted
	exec.OutputData = output
	exec.CompletedAt = &now
	exec.DurationMs = &durationMs

	return t.executions.UpdateExecution(ctx, exec)
}

// FailExecution marks an execution as failed.
func (t *ExecutionTracker) FailExecution(ctx context.Context, executionID uuid.UUID, execErr error) error {
	exec, err := t.executions.GetExecution(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get execution: %w", err)
	}

	now := time.Now()
	durationMs := now.Sub(exec.StartedAt).Milliseconds()
	exec.Status = store.ExecutionStatusFailed
	exec.ErrorMessage = execErr.Error()
	exec.CompletedAt = &now
	exec.DurationMs = &durationMs

	return t.executions.UpdateExecution(ctx, exec)
}

// CancelExecution marks an execution as cancelled.
func (t *ExecutionTracker) CancelExecution(ctx context.Context, executionID uuid.UUID) error {
	exec, err := t.executions.GetExecution(ctx, executionID)
	if err != nil {
		return fmt.Errorf("get execution: %w", err)
	}

	now := time.Now()
	durationMs := now.Sub(exec.StartedAt).Milliseconds()
	exec.Status = store.ExecutionStatusCancelled
	exec.CompletedAt = &now
	exec.DurationMs = &durationMs

	return t.executions.UpdateExecution(ctx, exec)
}

// LogWriter returns an io.Writer that appends logs to the store at the given level.
func (t *ExecutionTracker) LogWriter(workflowID uuid.UUID, executionID uuid.UUID, level store.LogLevel) io.Writer {
	return &logWriter{
		logs:        t.logs,
		workflowID:  workflowID,
		executionID: executionID,
		level:       level,
	}
}

// logWriter implements io.Writer by appending execution logs.
type logWriter struct {
	logs        store.LogStore
	workflowID  uuid.UUID
	executionID uuid.UUID
	level       store.LogLevel
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	execID := w.executionID
	entry := &store.ExecutionLog{
		WorkflowID:  w.workflowID,
		ExecutionID: &execID,
		Level:       w.level,
		Message:     string(p),
	}
	if err := w.logs.Append(context.Background(), entry); err != nil {
		return 0, err
	}
	return len(p), nil
}
