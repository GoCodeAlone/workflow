// Package interfaces defines shared interface types used across the workflow
// engine, handlers, and module packages. Placing these interfaces here breaks
// the direct handler→module concrete-type dependency and enables each package
// to be tested in isolation with mocks.
package interfaces

import (
	"context"
	"log/slog"
)

// EventRecorder records pipeline execution events for observability.
// *store.EventRecorderAdapter and any compatible recorder satisfy this interface.
type EventRecorder interface {
	RecordEvent(ctx context.Context, executionID string, eventType string, data map[string]any) error
}

// PipelineRunner is the interface satisfied by *module.Pipeline.
// It allows workflow handlers to execute pipelines without importing
// the concrete module types, enabling handler unit tests with mocks.
type PipelineRunner interface {
	// Run executes the pipeline with the given trigger data and returns the
	// merged result map (equivalent to PipelineContext.Current).
	Run(ctx context.Context, data map[string]any) (map[string]any, error)

	// SetLogger sets the logger used for pipeline execution.
	// Implementations should be idempotent: if a logger is already set,
	// a subsequent call should be a no-op.
	SetLogger(logger *slog.Logger)

	// SetEventRecorder sets the recorder used for pipeline execution events.
	// Implementations should be idempotent: if a recorder is already set,
	// a subsequent call should be a no-op.
	SetEventRecorder(recorder EventRecorder)
}

// StepRegistryProvider exposes the step types registered in a step registry.
// *module.StepRegistry satisfies this interface.
type StepRegistryProvider interface {
	// Types returns all registered step type names.
	Types() []string
}
