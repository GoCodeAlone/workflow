package module

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ErrorStrategy defines how a pipeline handles step errors.
type ErrorStrategy string

const (
	ErrorStrategyStop       ErrorStrategy = "stop"
	ErrorStrategySkip       ErrorStrategy = "skip"
	ErrorStrategyCompensate ErrorStrategy = "compensate"
)

// EventRecorder is an optional interface for recording execution events.
// When set on Pipeline, execution events are appended for observability.
// The store.EventStore can satisfy this via an adapter at the wiring layer.
// This is a type alias for interfaces.EventRecorder so callers using
// module.EventRecorder or interfaces.EventRecorder interchangeably are unaffected.
type EventRecorder = interfaces.EventRecorder

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

	// EventRecorder is an optional recorder for execution events.
	// When nil (the default), no events are recorded. Events are best-effort:
	// recording failures are logged but never fail the pipeline.
	EventRecorder EventRecorder

	// ExecutionID identifies this pipeline execution for event correlation.
	// Set by the caller when event recording is desired.
	ExecutionID string

	// seqNum tracks the auto-incrementing sequence number for events within
	// this execution. It is reset at the start of each Execute call.
	seqNum int64
}

// recordEvent is a nil-safe helper that records an event via EventRecorder.
// If EventRecorder is nil, this is a no-op. Errors are logged but never
// returned — event recording is best-effort and must not fail the pipeline.
func (p *Pipeline) recordEvent(ctx context.Context, eventType string, data map[string]any) {
	if p.EventRecorder == nil {
		return
	}

	p.seqNum++

	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if err := p.EventRecorder.RecordEvent(ctx, p.ExecutionID, eventType, data); err != nil {
		logger.Warn("Failed to record execution event",
			"event_type", eventType,
			"execution_id", p.ExecutionID,
			"error", err,
		)
	}
}

// Execute runs the pipeline from trigger data.
func (p *Pipeline) Execute(ctx context.Context, triggerData map[string]any) (*PipelineContext, error) {
	// Reset sequence counter for this execution.
	p.seqNum = 0

	// Apply pipeline-level timeout
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}

	pipelineStart := time.Now()

	md := map[string]any{
		"pipeline":   p.Name,
		"started_at": pipelineStart.UTC().Format(time.RFC3339),
	}
	// Merge pre-seeded metadata (e.g., HTTP context for delegate steps)
	for k, v := range p.Metadata {
		md[k] = v
	}
	// If an HTTP response writer was threaded through the Go context (e.g. by
	// the HTTP trigger), inject it into the pipeline metadata so that steps
	// like step.json_response can write directly to the HTTP response.
	if rw := ctx.Value(HTTPResponseWriterContextKey); rw != nil {
		md["_http_response_writer"] = rw
	}
	pc := NewPipelineContext(triggerData, md)

	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("Pipeline started", "pipeline", p.Name, "steps", len(p.Steps))

	// Record execution.started
	p.recordEvent(ctx, "execution.started", map[string]any{
		"pipeline":   p.Name,
		"step_count": len(p.Steps),
	})

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
			p.recordEvent(ctx, "execution.failed", map[string]any{
				"error": fmt.Sprintf("pipeline %q cancelled: %v", p.Name, ctx.Err()),
			})
			return pc, fmt.Errorf("pipeline %q cancelled: %w", p.Name, ctx.Err())
		default:
		}

		startTime := time.Now()
		logger.Info("Step started", "pipeline", p.Name, "step", step.Name(), "index", i)

		// Record step.started
		p.recordEvent(ctx, "step.started", map[string]any{
			"step_name": step.Name(),
			"index":     i,
		})

		result, err := step.Execute(ctx, pc)
		elapsed := time.Since(startTime)

		if err != nil {
			logger.Error("Step failed", "pipeline", p.Name, "step", step.Name(), "error", err, "elapsed", elapsed)

			// Record step.failed
			p.recordEvent(ctx, "step.failed", map[string]any{
				"step_name": step.Name(),
				"error":     err.Error(),
				"elapsed":   elapsed.String(),
			})

			switch p.OnError {
			case ErrorStrategySkip:
				logger.Warn("Skipping failed step", "step", step.Name())

				// Record step.skipped
				p.recordEvent(ctx, "step.skipped", map[string]any{
					"step_name": step.Name(),
					"reason":    err.Error(),
				})

				pc.MergeStepOutput(step.Name(), map[string]any{"_error": err.Error(), "_skipped": true})
				i++
				continue
			case ErrorStrategyCompensate:
				// Record execution.failed before compensation
				p.recordEvent(ctx, "execution.failed", map[string]any{
					"error":    fmt.Sprintf("step %q failed: %v", step.Name(), err),
					"elapsed":  time.Since(pipelineStart).String(),
					"strategy": "compensate",
				})

				compErr := p.runCompensation(ctx, pc, logger)
				if compErr != nil {
					return pc, fmt.Errorf("step %q failed: %w (compensation also failed: %v)", step.Name(), err, compErr)
				}
				return pc, fmt.Errorf("step %q failed: %w (compensation executed)", step.Name(), err)
			default: // stop
				// Record execution.failed
				p.recordEvent(ctx, "execution.failed", map[string]any{
					"error":   fmt.Sprintf("step %q failed: %v", step.Name(), err),
					"elapsed": time.Since(pipelineStart).String(),
				})

				return pc, fmt.Errorf("step %q failed: %w", step.Name(), err)
			}
		}

		logger.Info("Step completed", "pipeline", p.Name, "step", step.Name(), "elapsed", elapsed)

		// Record step.completed
		p.recordEvent(ctx, "step.completed", map[string]any{
			"step_name": step.Name(),
			"elapsed":   elapsed.String(),
		})

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
				p.recordEvent(ctx, "execution.failed", map[string]any{
					"error": fmt.Sprintf("step %q routed to unknown step %q", step.Name(), result.NextStep),
				})
				return pc, fmt.Errorf("step %q routed to unknown step %q", step.Name(), result.NextStep)
			}
			i = nextIdx
			continue
		}

		i++
	}

	totalElapsed := time.Since(pipelineStart)

	pc.Metadata["completed_at"] = time.Now().UTC().Format(time.RFC3339)
	logger.Info("Pipeline completed", "pipeline", p.Name)

	// Record execution.completed
	p.recordEvent(ctx, "execution.completed", map[string]any{
		"pipeline": p.Name,
		"elapsed":  totalElapsed.String(),
	})

	return pc, nil
}

// runCompensation executes compensation steps in reverse order.
func (p *Pipeline) runCompensation(ctx context.Context, pc *PipelineContext, logger *slog.Logger) error {
	if len(p.Compensation) == 0 {
		return nil
	}

	logger.Info("Running compensation", "pipeline", p.Name, "steps", len(p.Compensation))

	// Record saga.compensating
	p.recordEvent(ctx, "saga.compensating", map[string]any{
		"pipeline":   p.Name,
		"step_count": len(p.Compensation),
	})

	var firstErr error
	for i := len(p.Compensation) - 1; i >= 0; i-- {
		step := p.Compensation[i]

		p.recordEvent(ctx, "step.started", map[string]any{
			"step_name": step.Name(),
			"step_type": "compensation",
		})

		_, err := step.Execute(ctx, pc)
		if err != nil {
			logger.Error("Compensation step failed", "step", step.Name(), "error", err)

			p.recordEvent(ctx, "step.failed", map[string]any{
				"step_name": step.Name(),
				"step_type": "compensation",
				"error":     err.Error(),
			})

			if firstErr == nil {
				firstErr = err
			}
		} else {
			p.recordEvent(ctx, "step.compensated", map[string]any{
				"step_name": step.Name(),
			})
		}
	}

	if firstErr == nil {
		p.recordEvent(ctx, "saga.compensated", map[string]any{
			"pipeline": p.Name,
		})
	}

	return firstErr
}

// SetLogger sets the logger for pipeline execution if one is not already set.
// This implements part of interfaces.PipelineRunner and allows the handler
// to inject a logger without directly accessing the Logger field.
func (p *Pipeline) SetLogger(logger *slog.Logger) {
	if p.Logger == nil {
		p.Logger = logger
	}
}

// SetEventRecorder sets the event recorder for pipeline execution if one is
// not already set. This implements part of interfaces.PipelineRunner.
func (p *Pipeline) SetEventRecorder(recorder interfaces.EventRecorder) {
	if p.EventRecorder == nil {
		p.EventRecorder = recorder
	}
}

// Run executes the pipeline and returns the merged result data map.
// It implements interfaces.PipelineRunner by wrapping Execute and
// returning PipelineContext.Current so callers need not import PipelineContext.
func (p *Pipeline) Run(ctx context.Context, data map[string]any) (map[string]any, error) {
	pc, err := p.Execute(ctx, data)
	if err != nil {
		return nil, err
	}
	return pc.Current, nil
}
