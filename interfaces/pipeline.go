// Package interfaces defines shared interface types used across the workflow
// engine, handlers, and module packages. Placing these interfaces here breaks
// the direct handler→module concrete-type dependency and enables each package
// to be tested in isolation with mocks.
package interfaces

import (
	"context"
	"log/slog"
	"maps"
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

// PipelineContext carries data through a pipeline execution.
type PipelineContext struct {
	// TriggerData is the original data from the trigger (immutable after creation).
	TriggerData map[string]any

	// StepOutputs maps step-name -> output from each completed step.
	StepOutputs map[string]map[string]any

	// Current is the merged state: trigger data + all step outputs.
	// Steps read from Current and their output is merged back into it.
	Current map[string]any

	// Metadata holds execution metadata (pipeline name, trace ID, etc.)
	Metadata map[string]any
}

// NewPipelineContext creates a PipelineContext initialized with trigger data.
func NewPipelineContext(triggerData map[string]any, metadata map[string]any) *PipelineContext {
	current := make(map[string]any)
	if triggerData != nil {
		maps.Copy(current, triggerData)
	}

	td := make(map[string]any)
	if triggerData != nil {
		maps.Copy(td, triggerData)
	}

	md := make(map[string]any)
	if metadata != nil {
		maps.Copy(md, metadata)
	}

	return &PipelineContext{
		TriggerData: td,
		StepOutputs: make(map[string]map[string]any),
		Current:     current,
		Metadata:    md,
	}
}

// MergeStepOutput records a step's output and merges it into Current.
func (pc *PipelineContext) MergeStepOutput(stepName string, output map[string]any) {
	if output == nil {
		return
	}

	stepOut := make(map[string]any)
	maps.Copy(stepOut, output)
	pc.StepOutputs[stepName] = stepOut

	maps.Copy(pc.Current, output)
}

// StepResult is the output of a single pipeline step execution.
type StepResult struct {
	// Output is the data produced by this step.
	Output map[string]any

	// NextStep overrides the default next step (for conditional routing).
	// Empty string means continue to the next step in sequence.
	NextStep string

	// Stop indicates the pipeline should stop after this step (success).
	Stop bool
}

// PipelineStep is a single composable unit of work in a pipeline.
type PipelineStep interface {
	// Name returns the step's unique name within the pipeline.
	Name() string

	// Execute runs the step with the pipeline context.
	Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error)
}

// StepRegistrar manages step type registration and creation.
// It embeds StepRegistryProvider for type enumeration and adds
// a Create method for instantiating steps. Register is intentionally
// omitted from this interface because factory signatures use
// modular.Application (a concrete type) which cannot be expressed
// here without creating an import cycle.
type StepRegistrar interface {
	StepRegistryProvider
	// Create instantiates a PipelineStep of the given type.
	// app must be a modular.Application; it is typed as any to avoid
	// coupling this interfaces package to the modular dependency.
	Create(stepType, name string, config map[string]any, app any) (PipelineStep, error)
}
