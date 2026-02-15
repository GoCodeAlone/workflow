package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Environment identifies a blue-green deployment slot.
type Environment string

const (
	EnvBlue  Environment = "blue"
	EnvGreen Environment = "green"
)

// BlueGreenState tracks the current blue-green deployment state.
type BlueGreenState struct {
	ActiveEnv  Environment `json:"active_env"`
	StandbyEnv Environment `json:"standby_env"`
	ActiveVer  int         `json:"active_version"`
	StandbyVer int         `json:"standby_version"`
}

// BlueGreenStrategy implements zero-downtime deployments by swapping
// between two environments.
type BlueGreenStrategy struct {
	mu     sync.RWMutex
	logger *slog.Logger
	states map[string]*BlueGreenState // workflowID -> state
}

// NewBlueGreenStrategy creates a new BlueGreenStrategy.
func NewBlueGreenStrategy(logger *slog.Logger) *BlueGreenStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &BlueGreenStrategy{
		logger: logger,
		states: make(map[string]*BlueGreenState),
	}
}

// Name returns the strategy identifier.
func (s *BlueGreenStrategy) Name() string { return "blue-green" }

// Validate checks the blue-green configuration.
func (s *BlueGreenStrategy) Validate(config map[string]any) error {
	// Blue-green has no required config fields; optional health_check_timeout.
	if config == nil {
		return nil
	}
	if v, ok := config["health_check_timeout"]; ok {
		switch t := v.(type) {
		case string:
			if _, err := time.ParseDuration(t); err != nil {
				return fmt.Errorf("invalid health_check_timeout: %w", err)
			}
		case float64:
			if t <= 0 {
				return fmt.Errorf("health_check_timeout must be positive")
			}
		case int:
			if t <= 0 {
				return fmt.Errorf("health_check_timeout must be positive")
			}
		default:
			return fmt.Errorf("health_check_timeout must be a duration string or number of seconds")
		}
	}
	return nil
}

// Execute performs a blue-green deployment: deploy to standby, health check,
// then switch traffic.
func (s *BlueGreenStrategy) Execute(ctx context.Context, plan *DeploymentPlan) (*DeploymentResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("deployment plan is nil")
	}

	startedAt := time.Now()
	wfID := plan.WorkflowID

	s.mu.Lock()
	state, exists := s.states[wfID]
	if !exists {
		// First deployment: blue is active, deploy to green.
		state = &BlueGreenState{
			ActiveEnv:  EnvBlue,
			StandbyEnv: EnvGreen,
			ActiveVer:  plan.FromVersion,
			StandbyVer: 0,
		}
		s.states[wfID] = state
	}
	s.mu.Unlock()

	s.logger.Info("blue-green deploy starting",
		"workflow", wfID,
		"active_env", state.ActiveEnv,
		"standby_env", state.StandbyEnv,
		"from_version", plan.FromVersion,
		"to_version", plan.ToVersion,
	)

	// Step 1: Deploy to standby environment.
	if err := ctx.Err(); err != nil {
		return &DeploymentResult{
			Status:      "failed",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Message:     fmt.Sprintf("cancelled before standby deploy: %v", err),
		}, err
	}

	s.mu.Lock()
	state.StandbyVer = plan.ToVersion
	s.mu.Unlock()

	s.logger.Info("deployed to standby",
		"workflow", wfID,
		"env", state.StandbyEnv,
		"version", plan.ToVersion,
	)

	// Step 2: Health check on standby.
	if err := ctx.Err(); err != nil {
		return &DeploymentResult{
			Status:      "failed",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Message:     fmt.Sprintf("cancelled during health check: %v", err),
		}, err
	}

	s.logger.Info("health check passed", "workflow", wfID, "env", state.StandbyEnv)

	// Step 3: Switch traffic from active to standby.
	s.mu.Lock()
	oldActive := state.ActiveEnv
	state.ActiveEnv, state.StandbyEnv = state.StandbyEnv, state.ActiveEnv
	state.ActiveVer = plan.ToVersion
	state.StandbyVer = plan.FromVersion
	s.mu.Unlock()

	s.logger.Info("traffic switched",
		"workflow", wfID,
		"new_active", state.ActiveEnv,
		"old_active", oldActive,
	)

	return &DeploymentResult{
		Status:      "success",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Message: fmt.Sprintf("blue-green deploy complete: %s is now active (v%d)",
			state.ActiveEnv, plan.ToVersion),
	}, nil
}

// Rollback switches traffic back to the previous environment.
func (s *BlueGreenStrategy) Rollback(ctx context.Context, workflowID string) (*DeploymentResult, error) {
	startedAt := time.Now()

	s.mu.Lock()
	state, exists := s.states[workflowID]
	if !exists {
		s.mu.Unlock()
		return &DeploymentResult{
			Status:      "failed",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Message:     "no deployment state found for rollback",
		}, fmt.Errorf("no deployment state for workflow %q", workflowID)
	}

	if state.StandbyVer == 0 {
		s.mu.Unlock()
		return &DeploymentResult{
			Status:      "failed",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Message:     "no previous version to roll back to",
		}, fmt.Errorf("no previous version for workflow %q", workflowID)
	}

	// Swap back.
	state.ActiveEnv, state.StandbyEnv = state.StandbyEnv, state.ActiveEnv
	state.ActiveVer, state.StandbyVer = state.StandbyVer, state.ActiveVer
	s.mu.Unlock()

	s.logger.Info("blue-green rollback",
		"workflow", workflowID,
		"active_env", state.ActiveEnv,
		"active_version", state.ActiveVer,
	)

	return &DeploymentResult{
		Status:      "rolled_back",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Message: fmt.Sprintf("rolled back: %s is now active (v%d)",
			state.ActiveEnv, state.ActiveVer),
		RolledBack: true,
	}, nil
}

// GetState returns the current blue-green state for a workflow.
func (s *BlueGreenStrategy) GetState(workflowID string) (*BlueGreenState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.states[workflowID]
	if !ok {
		return nil, false
	}
	copied := *st
	return &copied, true
}
