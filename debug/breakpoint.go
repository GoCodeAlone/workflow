package debug

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// BreakpointManager manages pipeline execution breakpoints.
// It tracks breakpoints keyed by "pipeline:step" and maintains
// a registry of paused executions that can be resumed via the API.
type BreakpointManager struct {
	mu          sync.RWMutex
	breakpoints map[string]*PipelineBreakpoint // key: "pipeline:step"
	paused      map[string]*PausedExecution
	nextID      int64
	logger      *slog.Logger
}

// PipelineBreakpoint represents a breakpoint set on a specific pipeline step.
type PipelineBreakpoint struct {
	ID           string `json:"id"`
	PipelineName string `json:"pipeline_name"`
	StepName     string `json:"step_name"`
	Condition    string `json:"condition,omitempty"` // optional: only break if condition is true
	Enabled      bool   `json:"enabled"`
	HitCount     int64  `json:"hit_count"`
}

// PausedExecution captures the state of a pipeline execution that has been
// paused at a breakpoint. The resume channel is used to unblock the
// execution goroutine once a resume action is sent.
type PausedExecution struct {
	ID           string         `json:"id"`
	PipelineName string         `json:"pipeline_name"`
	StepName     string         `json:"step_name"`
	StepIndex    int            `json:"step_index"`
	Context      map[string]any `json:"context"`
	PausedAt     time.Time      `json:"paused_at"`
	resume       chan ResumeAction
}

// ResumeAction describes how a paused execution should continue.
type ResumeAction struct {
	Action string         `json:"action"` // "continue", "skip", "abort", "step_over"
	Data   map[string]any `json:"data"`   // optional: modified context data to inject
}

// breakpointKey returns the map key for a pipeline/step combination.
func breakpointKey(pipeline, step string) string {
	return pipeline + ":" + step
}

// NewBreakpointManager creates a new BreakpointManager.
func NewBreakpointManager(logger *slog.Logger) *BreakpointManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &BreakpointManager{
		breakpoints: make(map[string]*PipelineBreakpoint),
		paused:      make(map[string]*PausedExecution),
		logger:      logger,
	}
}

// SetBreakpoint adds or updates a breakpoint on the given pipeline step.
// If a breakpoint already exists for the pipeline/step pair, it is replaced.
// Returns the created breakpoint.
func (m *BreakpointManager) SetBreakpoint(pipeline, step string, condition string) *PipelineBreakpoint {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := breakpointKey(pipeline, step)
	id := fmt.Sprintf("pbp-%d", atomic.AddInt64(&m.nextID, 1))

	bp := &PipelineBreakpoint{
		ID:           id,
		PipelineName: pipeline,
		StepName:     step,
		Condition:    condition,
		Enabled:      true,
	}
	m.breakpoints[key] = bp

	m.logger.Info("Breakpoint set",
		"id", id,
		"pipeline", pipeline,
		"step", step,
		"condition", condition,
	)
	return bp
}

// RemoveBreakpoint removes the breakpoint for the given pipeline/step.
// Returns true if a breakpoint was removed, false if none existed.
func (m *BreakpointManager) RemoveBreakpoint(pipeline, step string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := breakpointKey(pipeline, step)
	if _, ok := m.breakpoints[key]; !ok {
		return false
	}
	delete(m.breakpoints, key)

	m.logger.Info("Breakpoint removed", "pipeline", pipeline, "step", step)
	return true
}

// EnableBreakpoint enables the breakpoint for the given pipeline/step.
// Returns true if the breakpoint was found and enabled.
func (m *BreakpointManager) EnableBreakpoint(pipeline, step string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := breakpointKey(pipeline, step)
	bp, ok := m.breakpoints[key]
	if !ok {
		return false
	}
	bp.Enabled = true
	m.logger.Info("Breakpoint enabled", "pipeline", pipeline, "step", step)
	return true
}

// DisableBreakpoint disables the breakpoint for the given pipeline/step
// without removing it. Returns true if the breakpoint was found.
func (m *BreakpointManager) DisableBreakpoint(pipeline, step string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := breakpointKey(pipeline, step)
	bp, ok := m.breakpoints[key]
	if !ok {
		return false
	}
	bp.Enabled = false
	m.logger.Info("Breakpoint disabled", "pipeline", pipeline, "step", step)
	return true
}

// ListBreakpoints returns all registered breakpoints.
func (m *BreakpointManager) ListBreakpoints() []*PipelineBreakpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bps := make([]*PipelineBreakpoint, 0, len(m.breakpoints))
	for _, bp := range m.breakpoints {
		bps = append(bps, bp)
	}
	return bps
}

// ClearAll removes all breakpoints and aborts all paused executions.
func (m *BreakpointManager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.breakpoints = make(map[string]*PipelineBreakpoint)

	// Abort all paused executions so they don't hang forever.
	for id, pe := range m.paused {
		select {
		case pe.resume <- ResumeAction{Action: "abort"}:
		default:
		}
		delete(m.paused, id)
	}

	m.logger.Info("All breakpoints and paused executions cleared")
}

// CheckBreakpoint checks whether execution should pause at the given
// pipeline/step. It evaluates the breakpoint's enabled state and optional
// condition. Returns true if the execution should pause.
//
// Condition evaluation: if a condition string is set, it is matched against
// a key in the context map. If the context value for that key is truthy
// (non-nil, non-false, non-zero, non-empty-string), the breakpoint fires.
// If no condition is set, the breakpoint always fires when enabled.
func (m *BreakpointManager) CheckBreakpoint(pipeline, step string, ctx map[string]any) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := breakpointKey(pipeline, step)
	bp, ok := m.breakpoints[key]
	if !ok {
		return false
	}
	if !bp.Enabled {
		return false
	}

	// Evaluate condition if present
	if bp.Condition != "" && ctx != nil {
		val, exists := ctx[bp.Condition]
		if !exists {
			return false
		}
		if !isTruthy(val) {
			return false
		}
	}

	bp.HitCount++
	return true
}

// isTruthy returns whether a value should be considered "true" for
// condition evaluation purposes.
func isTruthy(val any) bool {
	if val == nil {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v != ""
	default:
		return true
	}
}

// Pause registers a paused execution and returns a channel that will
// receive the ResumeAction when Resume is called. The calling goroutine
// should block on this channel.
func (m *BreakpointManager) Pause(executionID, pipeline, step string, stepIndex int, context map[string]any) <-chan ResumeAction {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan ResumeAction, 1)

	// Snapshot the context
	snapshot := make(map[string]any, len(context))
	for k, v := range context {
		snapshot[k] = v
	}

	pe := &PausedExecution{
		ID:           executionID,
		PipelineName: pipeline,
		StepName:     step,
		StepIndex:    stepIndex,
		Context:      snapshot,
		PausedAt:     time.Now(),
		resume:       ch,
	}
	m.paused[executionID] = pe

	m.logger.Info("Execution paused",
		"execution_id", executionID,
		"pipeline", pipeline,
		"step", step,
		"step_index", stepIndex,
	)
	return ch
}

// Resume sends a resume action to a paused execution, unblocking it.
// Returns an error if the execution ID is not found.
func (m *BreakpointManager) Resume(executionID string, action ResumeAction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pe, ok := m.paused[executionID]
	if !ok {
		return fmt.Errorf("paused execution %q not found", executionID)
	}

	pe.resume <- action
	delete(m.paused, executionID)

	m.logger.Info("Execution resumed",
		"execution_id", executionID,
		"action", action.Action,
	)
	return nil
}

// ListPaused returns all currently paused executions.
func (m *BreakpointManager) ListPaused() []*PausedExecution {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*PausedExecution, 0, len(m.paused))
	for _, pe := range m.paused {
		result = append(result, pe)
	}
	return result
}

// GetPaused returns a specific paused execution by ID.
func (m *BreakpointManager) GetPaused(executionID string) (*PausedExecution, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pe, ok := m.paused[executionID]
	return pe, ok
}

// ShouldPause implements BreakpointInterceptor.
func (m *BreakpointManager) ShouldPause(pipeline, step string, context map[string]any) bool {
	return m.CheckBreakpoint(pipeline, step, context)
}

// WaitForResume implements BreakpointInterceptor. It pauses the execution
// and blocks until a ResumeAction is received.
func (m *BreakpointManager) WaitForResume(executionID, pipeline, step string, stepIndex int, context map[string]any) (ResumeAction, error) {
	ch := m.Pause(executionID, pipeline, step, stepIndex, context)
	action, ok := <-ch
	if !ok {
		return ResumeAction{}, fmt.Errorf("resume channel closed for execution %q", executionID)
	}
	return action, nil
}
