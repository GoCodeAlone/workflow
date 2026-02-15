package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SagaConfig defines saga behavior for a pipeline.
type SagaConfig struct {
	Enabled           bool          `yaml:"enabled" json:"enabled"`
	Timeout           time.Duration `yaml:"timeout" json:"timeout"`
	CompensationOrder string        `yaml:"compensation_order" json:"compensation_order"` // "reverse" or "forward"
	TrackCompensation bool          `yaml:"track_compensation" json:"track_compensation"`
}

// CompensationStep defines how to undo a step's effects.
type CompensationStep struct {
	StepName string         `json:"step_name"`
	Type     string         `json:"type"` // step type to run for compensation
	Config   map[string]any `json:"config"`
}

// SagaState tracks the state of a saga execution.
type SagaState struct {
	ID               string            `json:"id"`
	PipelineName     string            `json:"pipeline_name"`
	Status           SagaStatus        `json:"status"`
	Config           SagaConfig        `json:"config"`
	CompletedSteps   []CompletedStep   `json:"completed_steps"`
	CompensatedSteps []CompensatedStep `json:"compensated_steps,omitempty"`
	FailedStep       string            `json:"failed_step,omitempty"`
	FailureError     string            `json:"failure_error,omitempty"`
	StartedAt        time.Time         `json:"started_at"`
	CompletedAt      *time.Time        `json:"completed_at,omitempty"`

	// compensations stores the compensation config registered for each step,
	// keyed by step name. Not exported; used internally by the Coordinator.
	compensations map[string]*CompensationStep
}

// SagaStatus represents the current status of a saga.
type SagaStatus string

const (
	SagaRunning      SagaStatus = "running"
	SagaCompensating SagaStatus = "compensating"
	SagaCompensated  SagaStatus = "compensated"
	SagaCompleted    SagaStatus = "completed"
	SagaFailed       SagaStatus = "failed"
)

// CompletedStep records a successfully executed step.
type CompletedStep struct {
	Name        string         `json:"name"`
	Output      map[string]any `json:"output"`
	CompletedAt time.Time      `json:"completed_at"`
}

// CompensatedStep records a compensation that was executed.
type CompensatedStep struct {
	Name          string    `json:"name"`
	CompensatedAt time.Time `json:"compensated_at"`
	Error         string    `json:"error,omitempty"`
}

// CompensationPlan contains the ordered list of compensations to execute.
type CompensationPlan struct {
	SagaID string
	Steps  []CompensationAction
}

// CompensationAction pairs a compensation step definition with the original
// step's output so the compensation logic can reference what was produced.
type CompensationAction struct {
	StepName     string
	Compensation CompensationStep
	StepOutput   map[string]any // original step's output, available to compensation
}

// Coordinator manages saga execution and compensation.
type Coordinator struct {
	logger *slog.Logger
	sagas  map[string]*SagaState
	mu     sync.RWMutex
}

// NewCoordinator creates a new Coordinator with the given logger.
func NewCoordinator(logger *slog.Logger) *Coordinator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{
		logger: logger,
		sagas:  make(map[string]*SagaState),
	}
}

// StartSaga begins tracking a new saga execution.
func (c *Coordinator) StartSaga(id, pipelineName string, config SagaConfig) *SagaState {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := &SagaState{
		ID:             id,
		PipelineName:   pipelineName,
		Status:         SagaRunning,
		Config:         config,
		CompletedSteps: make([]CompletedStep, 0),
		StartedAt:      time.Now(),
		compensations:  make(map[string]*CompensationStep),
	}

	c.sagas[id] = state
	c.logger.Info("Saga started", "saga_id", id, "pipeline", pipelineName)
	return state
}

// RecordStepCompleted records a step completion with its output and optional
// compensation config.
func (c *Coordinator) RecordStepCompleted(sagaID string, step CompletedStep, compensation *CompensationStep) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return fmt.Errorf("saga %q not found", sagaID)
	}

	if state.Status != SagaRunning {
		return fmt.Errorf("saga %q is not running (status: %s)", sagaID, state.Status)
	}

	state.CompletedSteps = append(state.CompletedSteps, step)

	if compensation != nil {
		state.compensations[step.Name] = compensation
	}

	c.logger.Info("Saga step completed",
		"saga_id", sagaID,
		"step", step.Name,
		"has_compensation", compensation != nil,
	)
	return nil
}

// CompleteSaga marks a saga as successfully completed.
func (c *Coordinator) CompleteSaga(sagaID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return fmt.Errorf("saga %q not found", sagaID)
	}

	if state.Status != SagaRunning {
		return fmt.Errorf("saga %q is not running (status: %s)", sagaID, state.Status)
	}

	now := time.Now()
	state.Status = SagaCompleted
	state.CompletedAt = &now

	c.logger.Info("Saga completed", "saga_id", sagaID, "pipeline", state.PipelineName)
	return nil
}

// TriggerCompensation initiates compensation for a failed saga.
// It returns a CompensationPlan containing the ordered list of compensation
// steps to execute. The order is determined by the saga's CompensationOrder
// config ("reverse" by default, or "forward").
func (c *Coordinator) TriggerCompensation(ctx context.Context, sagaID string, failedStep string, err error) (*CompensationPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = ctx // reserved for future use (e.g. deadline propagation)

	state, ok := c.sagas[sagaID]
	if !ok {
		return nil, fmt.Errorf("saga %q not found", sagaID)
	}

	if state.Status != SagaRunning {
		return nil, fmt.Errorf("saga %q is not running (status: %s)", sagaID, state.Status)
	}

	state.Status = SagaCompensating
	state.FailedStep = failedStep
	if err != nil {
		state.FailureError = err.Error()
	}

	c.logger.Info("Saga compensation triggered",
		"saga_id", sagaID,
		"failed_step", failedStep,
		"error", err,
	)

	// Build ordered list of compensation actions. Only include steps that
	// have a registered compensation.
	plan := &CompensationPlan{SagaID: sagaID}

	// Collect steps that have compensations in the order they completed.
	type stepEntry struct {
		name   string
		output map[string]any
		comp   CompensationStep
	}
	var entries []stepEntry

	for _, cs := range state.CompletedSteps {
		comp, hasComp := state.compensations[cs.Name]
		if hasComp {
			entries = append(entries, stepEntry{
				name:   cs.Name,
				output: cs.Output,
				comp:   *comp,
			})
		}
	}

	// Apply ordering.
	switch state.Config.CompensationOrder {
	case "forward":
		// Keep the order as-is (same as completion order).
		for _, e := range entries {
			plan.Steps = append(plan.Steps, CompensationAction{
				StepName:     e.name,
				Compensation: e.comp,
				StepOutput:   e.output,
			})
		}
	default: // "reverse" or unset
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			plan.Steps = append(plan.Steps, CompensationAction{
				StepName:     e.name,
				Compensation: e.comp,
				StepOutput:   e.output,
			})
		}
	}

	return plan, nil
}

// RecordCompensation records the result of executing a single compensation step.
func (c *Coordinator) RecordCompensation(sagaID string, stepName string, compErr error) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return fmt.Errorf("saga %q not found", sagaID)
	}

	cs := CompensatedStep{
		Name:          stepName,
		CompensatedAt: time.Now(),
	}
	if compErr != nil {
		cs.Error = compErr.Error()
	}

	state.CompensatedSteps = append(state.CompensatedSteps, cs)

	c.logger.Info("Saga compensation step recorded",
		"saga_id", sagaID,
		"step", stepName,
		"error", compErr,
	)
	return nil
}

// FinishCompensation marks the saga as fully compensated or failed depending
// on whether any compensation step errors occurred.
func (c *Coordinator) FinishCompensation(sagaID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return fmt.Errorf("saga %q not found", sagaID)
	}

	now := time.Now()
	state.CompletedAt = &now

	// Check if any compensation step had an error.
	hasError := false
	for _, cs := range state.CompensatedSteps {
		if cs.Error != "" {
			hasError = true
			break
		}
	}

	if hasError {
		state.Status = SagaFailed
		c.logger.Warn("Saga compensation finished with errors", "saga_id", sagaID)
	} else {
		state.Status = SagaCompensated
		c.logger.Info("Saga fully compensated", "saga_id", sagaID)
	}

	return nil
}

// GetState returns the current state of a saga.
func (c *Coordinator) GetState(sagaID string) (*SagaState, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return nil, fmt.Errorf("saga %q not found", sagaID)
	}

	return state, nil
}

// ListSagas returns all active/recent sagas.
func (c *Coordinator) ListSagas() []*SagaState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]*SagaState, 0, len(c.sagas))
	for _, s := range c.sagas {
		result = append(result, s)
	}
	return result
}

// IsTimedOut checks whether a saga has exceeded its configured timeout.
func (c *Coordinator) IsTimedOut(sagaID string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	state, ok := c.sagas[sagaID]
	if !ok {
		return false, fmt.Errorf("saga %q not found", sagaID)
	}

	if state.Config.Timeout <= 0 {
		return false, nil
	}

	return time.Since(state.StartedAt) > state.Config.Timeout, nil
}

// TimeoutSaga marks a running saga as failed due to timeout and returns
// a CompensationPlan so the caller can execute compensations.
func (c *Coordinator) TimeoutSaga(ctx context.Context, sagaID string) (*CompensationPlan, error) {
	c.mu.RLock()
	state, ok := c.sagas[sagaID]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("saga %q not found", sagaID)
	}

	if state.Status != SagaRunning {
		return nil, fmt.Errorf("saga %q is not running (status: %s)", sagaID, state.Status)
	}

	return c.TriggerCompensation(ctx, sagaID, "", fmt.Errorf("saga timed out after %s", state.Config.Timeout))
}
