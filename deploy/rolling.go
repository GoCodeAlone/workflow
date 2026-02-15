package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RollingConfig holds the tunable parameters for rolling deployments.
type RollingConfig struct {
	BatchSize int           `json:"batch_size"` // default 1
	Delay     time.Duration `json:"delay"`      // default 5s
}

// DefaultRollingConfig returns the default rolling configuration.
func DefaultRollingConfig() RollingConfig {
	return RollingConfig{
		BatchSize: 1,
		Delay:     5 * time.Second,
	}
}

// RollingStrategy implements simple rolling update deployments,
// updating instances one batch at a time.
type RollingStrategy struct {
	logger *slog.Logger
}

// NewRollingStrategy creates a new RollingStrategy.
func NewRollingStrategy(logger *slog.Logger) *RollingStrategy {
	if logger == nil {
		logger = slog.Default()
	}
	return &RollingStrategy{
		logger: logger,
	}
}

// Name returns the strategy identifier.
func (s *RollingStrategy) Name() string { return "rolling" }

// Validate checks the rolling-specific configuration.
func (s *RollingStrategy) Validate(config map[string]any) error {
	if config == nil {
		return nil
	}

	if v, ok := config["batch_size"]; ok {
		bs, err := toInt(v)
		if err != nil || bs < 1 {
			return fmt.Errorf("batch_size must be a positive integer")
		}
	}

	if v, ok := config["delay"]; ok {
		switch t := v.(type) {
		case string:
			if _, err := time.ParseDuration(t); err != nil {
				return fmt.Errorf("invalid delay: %w", err)
			}
		case float64:
			if t < 0 {
				return fmt.Errorf("delay must be non-negative")
			}
		case int:
			if t < 0 {
				return fmt.Errorf("delay must be non-negative")
			}
		default:
			return fmt.Errorf("delay must be a duration string or number of seconds")
		}
	}

	return nil
}

// Execute performs a rolling deployment, processing instances in batches.
func (s *RollingStrategy) Execute(ctx context.Context, plan *DeploymentPlan) (*DeploymentResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("deployment plan is nil")
	}

	startedAt := time.Now()
	cfg := parseRollingConfig(plan.Config)
	wfID := plan.WorkflowID

	// Determine number of instances to update (default to batch_size * 3 if not specified).
	totalInstances := cfg.BatchSize * 3
	if v, ok := plan.Config["instances"]; ok {
		if n, err := toInt(v); err == nil && n > 0 {
			totalInstances = n
		}
	}

	s.logger.Info("rolling deploy starting",
		"workflow", wfID,
		"batch_size", cfg.BatchSize,
		"delay", cfg.Delay,
		"total_instances", totalInstances,
		"from_version", plan.FromVersion,
		"to_version", plan.ToVersion,
	)

	updated := 0
	batch := 0
	for updated < totalInstances {
		batch++

		if err := ctx.Err(); err != nil {
			return &DeploymentResult{
				Status:      "failed",
				StartedAt:   startedAt,
				CompletedAt: time.Now(),
				Message:     fmt.Sprintf("cancelled during batch %d: %v", batch, err),
			}, err
		}

		// Calculate batch size for this iteration.
		batchCount := cfg.BatchSize
		if updated+batchCount > totalInstances {
			batchCount = totalInstances - updated
		}

		s.logger.Info("rolling batch update",
			"workflow", wfID,
			"batch", batch,
			"count", batchCount,
			"updated_so_far", updated,
		)

		updated += batchCount

		// Delay between batches (skip after last batch).
		if updated < totalInstances && cfg.Delay > 0 {
			select {
			case <-ctx.Done():
				return &DeploymentResult{
					Status:      "failed",
					StartedAt:   startedAt,
					CompletedAt: time.Now(),
					Message:     fmt.Sprintf("cancelled during delay after batch %d: %v", batch, ctx.Err()),
				}, ctx.Err()
			case <-time.After(cfg.Delay):
			}
		}
	}

	s.logger.Info("rolling deploy complete",
		"workflow", wfID,
		"total_updated", updated,
		"version", plan.ToVersion,
	)

	return &DeploymentResult{
		Status:      "success",
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		Message:     fmt.Sprintf("rolling deploy complete: %d instances updated to v%d", updated, plan.ToVersion),
	}, nil
}

// parseRollingConfig extracts RollingConfig from a raw map, using defaults for missing fields.
func parseRollingConfig(raw map[string]any) RollingConfig {
	cfg := DefaultRollingConfig()
	if raw == nil {
		return cfg
	}

	if v, ok := raw["batch_size"]; ok {
		if n, err := toInt(v); err == nil && n > 0 {
			cfg.BatchSize = n
		}
	}
	if v, ok := raw["delay"]; ok {
		switch t := v.(type) {
		case string:
			if d, err := time.ParseDuration(t); err == nil {
				cfg.Delay = d
			}
		case float64:
			cfg.Delay = time.Duration(t) * time.Second
		case int:
			cfg.Delay = time.Duration(t) * time.Second
		}
	}

	return cfg
}

// toInt converts various numeric types to int.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", v)
	}
}
