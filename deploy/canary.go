package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// CanaryConfig holds the tunable parameters for canary deployments.
type CanaryConfig struct {
	InitialPercent float64       `json:"initial_percent"` // default 10
	Increment      float64       `json:"increment"`       // default 20
	Interval       time.Duration `json:"interval"`        // default 30s
	ErrorThreshold float64       `json:"error_threshold"` // default 5 (percent)
}

// DefaultCanaryConfig returns the default canary configuration.
func DefaultCanaryConfig() CanaryConfig {
	return CanaryConfig{
		InitialPercent: 10,
		Increment:      20,
		Interval:       30 * time.Second,
		ErrorThreshold: 5,
	}
}

// TrafficSplit tracks how traffic is distributed between versions.
type TrafficSplit struct {
	CanaryPercent float64 `json:"canary_percent"`
	StablePercent float64 `json:"stable_percent"`
	CanaryVersion int     `json:"canary_version"`
	StableVersion int     `json:"stable_version"`
}

// CanaryStrategy implements gradual traffic-shifting deployments.
type CanaryStrategy struct {
	mu     sync.RWMutex
	logger *slog.Logger
	splits map[string]*TrafficSplit // workflowID -> split

	// checkHealth is a pluggable health check function. Returns error rate (0-100).
	// If nil, defaults to always healthy (0% error rate).
	checkHealth func(ctx context.Context, workflowID string, version int) (float64, error)
}

// NewCanaryStrategy creates a new CanaryStrategy.
func NewCanaryStrategy(logger *slog.Logger) *CanaryStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &CanaryStrategy{
		logger: logger,
		splits: make(map[string]*TrafficSplit),
	}
}

// SetHealthCheck sets a custom health check function for canary evaluation.
func (s *CanaryStrategy) SetHealthCheck(fn func(ctx context.Context, workflowID string, version int) (float64, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkHealth = fn
}

// Name returns the strategy identifier.
func (s *CanaryStrategy) Name() string { return "canary" }

// Validate checks the canary-specific configuration.
func (s *CanaryStrategy) Validate(config map[string]any) error {
	if config == nil {
		return nil
	}

	if v, ok := config["initial_percent"]; ok {
		pct, err := toFloat64(v)
		if err != nil || pct <= 0 || pct > 100 {
			return fmt.Errorf("initial_percent must be between 0 and 100 (exclusive of 0)")
		}
	}

	if v, ok := config["increment"]; ok {
		inc, err := toFloat64(v)
		if err != nil || inc <= 0 || inc > 100 {
			return fmt.Errorf("increment must be between 0 and 100 (exclusive of 0)")
		}
	}

	if v, ok := config["error_threshold"]; ok {
		et, err := toFloat64(v)
		if err != nil || et < 0 || et > 100 {
			return fmt.Errorf("error_threshold must be between 0 and 100")
		}
	}

	if v, ok := config["interval"]; ok {
		switch t := v.(type) {
		case string:
			if _, err := time.ParseDuration(t); err != nil {
				return fmt.Errorf("invalid interval: %w", err)
			}
		case float64:
			if t <= 0 {
				return fmt.Errorf("interval must be positive")
			}
		case int:
			if t <= 0 {
				return fmt.Errorf("interval must be positive")
			}
		default:
			return fmt.Errorf("interval must be a duration string or number of seconds")
		}
	}

	return nil
}

// Execute performs a canary deployment with gradual traffic shifting.
func (s *CanaryStrategy) Execute(ctx context.Context, plan *DeploymentPlan) (*DeploymentResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("deployment plan is nil")
	}

	startedAt := time.Now()
	cfg := parseCanaryConfig(plan.Config)
	wfID := plan.WorkflowID

	// Initialize traffic split.
	split := &TrafficSplit{
		CanaryPercent: cfg.InitialPercent,
		StablePercent: 100 - cfg.InitialPercent,
		CanaryVersion: plan.ToVersion,
		StableVersion: plan.FromVersion,
	}

	s.mu.Lock()
	s.splits[wfID] = split
	s.mu.Unlock()

	s.logger.Info("canary deploy starting",
		"workflow", wfID,
		"initial_percent", cfg.InitialPercent,
		"from_version", plan.FromVersion,
		"to_version", plan.ToVersion,
	)

	// Gradually increase canary traffic.
	for {
		s.mu.RLock()
		currentPct := split.CanaryPercent
		s.mu.RUnlock()

		if currentPct >= 100 {
			break
		}

		if err := ctx.Err(); err != nil {
			return s.buildResult("failed", startedAt,
				fmt.Sprintf("cancelled at %.0f%% canary: %v", currentPct, err),
				false,
			), err
		}

		// Check health of canary.
		errorRate, err := s.getErrorRate(ctx, wfID, plan.ToVersion)
		if err != nil {
			s.mu.RLock()
			pct := split.CanaryPercent
			s.mu.RUnlock()
			return s.buildResult("failed", startedAt,
				fmt.Sprintf("health check failed at %.0f%% canary: %v", pct, err),
				false,
			), err
		}

		if errorRate > cfg.ErrorThreshold {
			s.mu.Lock()
			pct := split.CanaryPercent
			// Canary is unhealthy, roll back.
			s.logger.Warn("canary error threshold exceeded, rolling back",
				"workflow", wfID,
				"error_rate", errorRate,
				"threshold", cfg.ErrorThreshold,
				"canary_percent", pct,
			)
			split.CanaryPercent = 0
			split.StablePercent = 100
			s.mu.Unlock()

			return s.buildResult("rolled_back", startedAt,
				fmt.Sprintf("canary rolled back at %.0f%% due to error rate %.1f%% (threshold %.1f%%)",
					pct, errorRate, cfg.ErrorThreshold),
				true,
			), nil
		}

		// Increase canary traffic.
		s.mu.Lock()
		s.logger.Info("canary health check passed",
			"workflow", wfID,
			"canary_percent", split.CanaryPercent,
			"error_rate", errorRate,
		)
		split.CanaryPercent += cfg.Increment
		if split.CanaryPercent > 100 {
			split.CanaryPercent = 100
		}
		split.StablePercent = 100 - split.CanaryPercent
		newPct := split.CanaryPercent
		s.mu.Unlock()

		if newPct >= 100 {
			break
		}

		// Wait for the configured interval before next step.
		select {
		case <-ctx.Done():
			s.mu.RLock()
			pct := split.CanaryPercent
			s.mu.RUnlock()
			return s.buildResult("failed", startedAt,
				fmt.Sprintf("cancelled during interval wait at %.0f%% canary", pct),
				false,
			), ctx.Err()
		case <-time.After(cfg.Interval):
		}
	}

	// Full rollout complete.
	s.mu.Lock()
	split.CanaryPercent = 100
	split.StablePercent = 0
	split.StableVersion = plan.ToVersion
	s.mu.Unlock()

	s.logger.Info("canary deploy complete",
		"workflow", wfID,
		"version", plan.ToVersion,
	)

	return s.buildResult("success", startedAt,
		fmt.Sprintf("canary deploy complete: v%d is now at 100%%", plan.ToVersion),
		false,
	), nil
}

// Rollback immediately shifts all traffic back to the stable version.
func (s *CanaryStrategy) Rollback(ctx context.Context, workflowID string) (*DeploymentResult, error) {
	startedAt := time.Now()

	s.mu.Lock()
	split, exists := s.splits[workflowID]
	if !exists {
		s.mu.Unlock()
		return &DeploymentResult{
			Status:      "failed",
			StartedAt:   startedAt,
			CompletedAt: time.Now(),
			Message:     "no canary state found for rollback",
		}, fmt.Errorf("no canary state for workflow %q", workflowID)
	}

	oldCanaryPct := split.CanaryPercent
	split.CanaryPercent = 0
	split.StablePercent = 100
	s.mu.Unlock()

	s.logger.Info("canary rollback",
		"workflow", workflowID,
		"was_canary_percent", oldCanaryPct,
		"stable_version", split.StableVersion,
	)

	return &DeploymentResult{
		Status:      "rolled_back",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Message: fmt.Sprintf("canary rolled back from %.0f%%: v%d is stable",
			oldCanaryPct, split.StableVersion),
		RolledBack: true,
	}, nil
}

// GetSplit returns the current traffic split for a workflow.
func (s *CanaryStrategy) GetSplit(workflowID string) (*TrafficSplit, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	split, ok := s.splits[workflowID]
	if !ok {
		return nil, false
	}
	copied := *split
	return &copied, true
}

// getErrorRate calls the health check function or returns 0 if none is set.
func (s *CanaryStrategy) getErrorRate(ctx context.Context, workflowID string, version int) (float64, error) {
	s.mu.RLock()
	fn := s.checkHealth
	s.mu.RUnlock()

	if fn == nil {
		return 0, nil // No health check: assume healthy.
	}
	return fn(ctx, workflowID, version)
}

func (s *CanaryStrategy) buildResult(status string, startedAt time.Time, message string, rolledBack bool) *DeploymentResult {
	return &DeploymentResult{
		Status:      status,
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Message:     message,
		RolledBack:  rolledBack,
	}
}

// parseCanaryConfig extracts CanaryConfig from a raw map, using defaults for missing fields.
func parseCanaryConfig(raw map[string]any) CanaryConfig {
	cfg := DefaultCanaryConfig()
	if raw == nil {
		return cfg
	}

	if v, ok := raw["initial_percent"]; ok {
		if f, err := toFloat64(v); err == nil {
			cfg.InitialPercent = f
		}
	}
	if v, ok := raw["increment"]; ok {
		if f, err := toFloat64(v); err == nil {
			cfg.Increment = f
		}
	}
	if v, ok := raw["error_threshold"]; ok {
		if f, err := toFloat64(v); err == nil {
			cfg.ErrorThreshold = f
		}
	}
	if v, ok := raw["interval"]; ok {
		switch t := v.(type) {
		case string:
			if d, err := time.ParseDuration(t); err == nil {
				cfg.Interval = d
			}
		case float64:
			cfg.Interval = time.Duration(t) * time.Second
		case int:
			cfg.Interval = time.Duration(t) * time.Second
		}
	}

	return cfg
}

// toFloat64 converts various numeric types to float64.
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
