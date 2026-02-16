package module

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// ExecutionTracker wraps pipeline execution with V1Store recording.
type ExecutionTracker struct {
	Store      *V1Store
	WorkflowID string
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

	// Best-effort: don't fail the request if tracking fails
	_ = t.Store.InsertExecution(execID, t.WorkflowID, triggerType, "running", startedAt)

	// Set execution ID on pipeline for event correlation
	pipeline.ExecutionID = execID

	pc, err := pipeline.Execute(ctx, triggerData)

	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	status := "completed"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
	}

	_ = t.Store.CompleteExecution(execID, status, completedAt, durationMs, errMsg)

	return pc, err
}
