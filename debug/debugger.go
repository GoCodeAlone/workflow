// Package debug provides interactive workflow debugging with breakpoint
// support, step-through execution, and state inspection.
package debug

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BreakpointType identifies what kind of breakpoint is set.
type BreakpointType string

const (
	BreakOnModule   BreakpointType = "module"
	BreakOnWorkflow BreakpointType = "workflow"
	BreakOnTrigger  BreakpointType = "trigger"
)

// Breakpoint represents a point where execution should pause.
type Breakpoint struct {
	ID        string         `json:"id"`
	Type      BreakpointType `json:"type"`
	Target    string         `json:"target"`    // module name, workflow type, or trigger name
	Condition string         `json:"condition"` // optional condition expression
	Enabled   bool           `json:"enabled"`
	HitCount  int            `json:"hit_count"`
}

// ExecutionState captures the current state of a paused execution.
type ExecutionState struct {
	Status       string         `json:"status"` // "running", "paused", "stopped", "idle"
	CurrentStep  string         `json:"current_step"`
	WorkflowType string         `json:"workflow_type"`
	Action       string         `json:"action"`
	Data         map[string]any `json:"data,omitempty"`
	PausedAt     *time.Time     `json:"paused_at,omitempty"`
	BreakpointID string         `json:"breakpoint_id,omitempty"`
	StepHistory  []StepRecord   `json:"step_history"`
}

// StepRecord records a single step that was executed.
type StepRecord struct {
	Step      string         `json:"step"`
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	Duration  time.Duration  `json:"duration"`
	Data      map[string]any `json:"data,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// WorkflowExecutor is the interface the debugger uses to trigger workflow execution.
// It matches the TriggerWorkflow signature on StdEngine.
type WorkflowExecutor interface {
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
}

// Debugger wraps workflow execution with breakpoint and step-through support.
type Debugger struct {
	mu          sync.Mutex
	breakpoints map[string]*Breakpoint
	state       ExecutionState
	nextBPID    int

	// pause/resume signaling
	pauseCh  chan struct{} // closed when debugger should pause
	resumeCh chan struct{} // closed when debugger should resume

}

// New creates a new Debugger.
func New() *Debugger {
	d := &Debugger{
		breakpoints: make(map[string]*Breakpoint),
		state: ExecutionState{
			Status:      "idle",
			StepHistory: make([]StepRecord, 0),
		},
		pauseCh:  make(chan struct{}),
		resumeCh: make(chan struct{}),
	}
	return d
}

// AddBreakpoint registers a new breakpoint and returns its ID.
func (d *Debugger) AddBreakpoint(bpType BreakpointType, target string, condition string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.nextBPID++
	id := fmt.Sprintf("bp-%d", d.nextBPID)
	d.breakpoints[id] = &Breakpoint{
		ID:        id,
		Type:      bpType,
		Target:    target,
		Condition: condition,
		Enabled:   true,
	}
	return id
}

// RemoveBreakpoint removes a breakpoint by ID.
func (d *Debugger) RemoveBreakpoint(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, ok := d.breakpoints[id]; !ok {
		return fmt.Errorf("breakpoint %q not found", id)
	}
	delete(d.breakpoints, id)
	return nil
}

// ListBreakpoints returns all breakpoints.
func (d *Debugger) ListBreakpoints() []*Breakpoint {
	d.mu.Lock()
	defer d.mu.Unlock()

	bps := make([]*Breakpoint, 0, len(d.breakpoints))
	for _, bp := range d.breakpoints {
		bps = append(bps, bp)
	}
	return bps
}

// State returns the current execution state.
func (d *Debugger) State() ExecutionState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

// Step advances one step when paused. Returns an error if not paused.
func (d *Debugger) Step() error {
	d.mu.Lock()
	if d.state.Status != "paused" {
		d.mu.Unlock()
		return fmt.Errorf("debugger is not paused (status: %s)", d.state.Status)
	}
	d.mu.Unlock()

	// Signal resume, but re-arm the pause for the next step
	select {
	case d.resumeCh <- struct{}{}:
	default:
	}
	return nil
}

// Continue resumes execution until the next breakpoint or completion.
func (d *Debugger) Continue() error {
	d.mu.Lock()
	if d.state.Status != "paused" {
		d.mu.Unlock()
		return fmt.Errorf("debugger is not paused (status: %s)", d.state.Status)
	}
	d.state.Status = "running"
	d.mu.Unlock()

	select {
	case d.resumeCh <- struct{}{}:
	default:
	}
	return nil
}

// CheckBreakpoint checks whether a step should trigger a pause based on
// active breakpoints. It is safe for concurrent use and is intended to be
// called from hooks in the workflow execution path.
func (d *Debugger) CheckBreakpoint(step string, stepType BreakpointType) (shouldPause bool, bpID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, bp := range d.breakpoints {
		if !bp.Enabled {
			continue
		}
		if bp.Type == stepType && bp.Target == step {
			bp.HitCount++
			return true, bp.ID
		}
	}
	return false, ""
}

// Pause puts the debugger into the paused state and blocks until a Step or
// Continue call is made. ctx can be used to abort the wait.
func (d *Debugger) Pause(ctx context.Context, step string, bpID string, data map[string]any) error {
	now := time.Now()
	d.mu.Lock()
	d.state.Status = "paused"
	d.state.CurrentStep = step
	d.state.PausedAt = &now
	d.state.BreakpointID = bpID
	d.state.Data = data
	d.mu.Unlock()

	// Wait for resume signal or context cancellation
	select {
	case <-ctx.Done():
		d.mu.Lock()
		d.state.Status = "stopped"
		d.mu.Unlock()
		return ctx.Err()
	case <-d.resumeCh:
		return nil
	}
}

// RecordStep adds a step to the execution history.
func (d *Debugger) RecordStep(step, stepType string, duration time.Duration, data map[string]any, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	record := StepRecord{
		Step:      step,
		Type:      stepType,
		Timestamp: time.Now(),
		Duration:  duration,
		Data:      data,
	}
	if err != nil {
		record.Error = err.Error()
	}
	d.state.StepHistory = append(d.state.StepHistory, record)
}

// SetRunning updates the state to indicate active execution.
func (d *Debugger) SetRunning(workflowType, action string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Status = "running"
	d.state.WorkflowType = workflowType
	d.state.Action = action
	d.state.StepHistory = make([]StepRecord, 0)
	d.state.PausedAt = nil
	d.state.BreakpointID = ""
}

// SetIdle resets the debugger to idle state.
func (d *Debugger) SetIdle() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.state.Status = "idle"
	d.state.CurrentStep = ""
	d.state.PausedAt = nil
	d.state.BreakpointID = ""
}

// Reset clears all state and breakpoints.
func (d *Debugger) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.breakpoints = make(map[string]*Breakpoint)
	d.state = ExecutionState{
		Status:      "idle",
		StepHistory: make([]StepRecord, 0),
	}
	d.nextBPID = 0
	// Drain and recreate channels
	d.resumeCh = make(chan struct{})
}
