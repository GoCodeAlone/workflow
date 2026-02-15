package module

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// ErrorStrategy defines how a pipeline handles step errors.
type ErrorStrategy string

const (
	ErrorStrategyStop       ErrorStrategy = "stop"
	ErrorStrategySkip       ErrorStrategy = "skip"
	ErrorStrategyCompensate ErrorStrategy = "compensate"
)

// Pipeline is an ordered sequence of steps with error handling.
type Pipeline struct {
	Name         string
	Steps        []PipelineStep
	OnError      ErrorStrategy
	Timeout      time.Duration
	Compensation []PipelineStep
	Logger       *slog.Logger
	// Metadata is pre-seeded metadata merged into the PipelineContext.
	// Used to pass HTTP context (request/response) for delegate steps.
	Metadata map[string]any
	// RoutePattern is the original route path pattern (e.g., "/api/v1/admin/companies/{id}")
	// used by step.request_parse for path parameter extraction.
	RoutePattern string
}

// Execute runs the pipeline from trigger data.
func (p *Pipeline) Execute(ctx context.Context, triggerData map[string]any) (*PipelineContext, error) {
	// Apply pipeline-level timeout
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	md := map[string]any{
		"pipeline":   p.Name,
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}
	// Merge pre-seeded metadata (e.g., HTTP context for delegate steps)
	for k, v := range p.Metadata {
		md[k] = v
	}
	pc := NewPipelineContext(triggerData, md)

	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("Pipeline started", "pipeline", p.Name, "steps", len(p.Steps))

	// Build step index for conditional routing
	stepIndex := make(map[string]int, len(p.Steps))
	for i, s := range p.Steps {
		stepIndex[s.Name()] = i
	}

	// Execute steps
	i := 0
	for i < len(p.Steps) {
		step := p.Steps[i]

		// Check context cancellation
		select {
		case <-ctx.Done():
			return pc, fmt.Errorf("pipeline %q cancelled: %w", p.Name, ctx.Err())
		default:
		}

		startTime := time.Now()
		logger.Info("Step started", "pipeline", p.Name, "step", step.Name(), "index", i)

		result, err := step.Execute(ctx, pc)
		elapsed := time.Since(startTime)

		if err != nil {
			logger.Error("Step failed", "pipeline", p.Name, "step", step.Name(), "error", err, "elapsed", elapsed)

			switch p.OnError {
			case ErrorStrategySkip:
				logger.Warn("Skipping failed step", "step", step.Name())
				pc.MergeStepOutput(step.Name(), map[string]any{"_error": err.Error(), "_skipped": true})
				i++
				continue
			case ErrorStrategyCompensate:
				compErr := p.runCompensation(ctx, pc, logger)
				if compErr != nil {
					return pc, fmt.Errorf("step %q failed: %w (compensation also failed: %v)", step.Name(), err, compErr)
				}
				return pc, fmt.Errorf("step %q failed: %w (compensation executed)", step.Name(), err)
			default: // stop
				return pc, fmt.Errorf("step %q failed: %w", step.Name(), err)
			}
		}

		logger.Info("Step completed", "pipeline", p.Name, "step", step.Name(), "elapsed", elapsed)

		// Merge output into context
		if result != nil && result.Output != nil {
			pc.MergeStepOutput(step.Name(), result.Output)
		} else {
			pc.MergeStepOutput(step.Name(), map[string]any{})
		}

		// Handle stop signal
		if result != nil && result.Stop {
			logger.Info("Pipeline stopped by step", "pipeline", p.Name, "step", step.Name())
			break
		}

		// Handle conditional routing
		if result != nil && result.NextStep != "" {
			nextIdx, ok := stepIndex[result.NextStep]
			if !ok {
				return pc, fmt.Errorf("step %q routed to unknown step %q", step.Name(), result.NextStep)
			}
			i = nextIdx
			continue
		}

		i++
	}

	pc.Metadata["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	logger.Info("Pipeline completed", "pipeline", p.Name)
	return pc, nil
}

// runCompensation executes compensation steps in reverse order.
func (p *Pipeline) runCompensation(ctx context.Context, pc *PipelineContext, logger *slog.Logger) error {
	if len(p.Compensation) == 0 {
		return nil
	}

	logger.Info("Running compensation", "pipeline", p.Name, "steps", len(p.Compensation))

	var firstErr error
	for i := len(p.Compensation) - 1; i >= 0; i-- {
		step := p.Compensation[i]
		_, err := step.Execute(ctx, pc)
		if err != nil {
			logger.Error("Compensation step failed", "step", step.Name(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
