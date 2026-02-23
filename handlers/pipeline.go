package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// PipelineWorkflowHandler manages and executes pipeline-based workflows.
type PipelineWorkflowHandler struct {
	pipelines     map[string]interfaces.PipelineRunner
	stepRegistry  interfaces.StepRegistryProvider
	logger        *slog.Logger
	eventRecorder interfaces.EventRecorder
}

// NewPipelineWorkflowHandler creates a new PipelineWorkflowHandler.
func NewPipelineWorkflowHandler() *PipelineWorkflowHandler {
	return &PipelineWorkflowHandler{
		pipelines: make(map[string]interfaces.PipelineRunner),
	}
}

// SetStepRegistry sets the step registry used to create pipeline steps.
func (h *PipelineWorkflowHandler) SetStepRegistry(registry interfaces.StepRegistryProvider) {
	h.stepRegistry = registry
}

// SetLogger sets the logger for pipeline execution.
func (h *PipelineWorkflowHandler) SetLogger(logger *slog.Logger) {
	h.logger = logger
}

// SetEventRecorder sets the event recorder for pipeline execution events.
// When set, each pipeline execution will record events to this recorder.
func (h *PipelineWorkflowHandler) SetEventRecorder(recorder interfaces.EventRecorder) {
	h.eventRecorder = recorder
}

// AddPipeline registers a named pipeline with the handler.
func (h *PipelineWorkflowHandler) AddPipeline(name string, p interfaces.PipelineRunner) {
	h.pipelines[name] = p
}

// CanHandle returns true if a pipeline with the given name exists.
// It matches both "pipeline:<name>" prefixed keys and exact pipeline names.
func (h *PipelineWorkflowHandler) CanHandle(workflowType string) bool {
	// Check for "pipeline:" prefix
	if strings.HasPrefix(workflowType, "pipeline:") {
		name := strings.TrimPrefix(workflowType, "pipeline:")
		_, ok := h.pipelines[name]
		return ok
	}

	// Check for exact match
	_, ok := h.pipelines[workflowType]
	return ok
}

// ConfigureWorkflow receives pipeline configuration and builds Pipeline objects.
func (h *PipelineWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	cfgMap, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid pipeline workflow configuration format")
	}

	_ = app // app is available if needed for step creation via the registry
	_ = cfgMap

	// Pipeline configuration is handled via AddPipeline from the engine's
	// configurePipelines method, which has access to the full config parsing.
	return nil
}

// ExecuteWorkflow runs the named pipeline and returns the pipeline context's Current data.
func (h *PipelineWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, _ string, data map[string]any) (map[string]any, error) {
	// Resolve pipeline name
	name := workflowType
	name = strings.TrimPrefix(name, "pipeline:")

	pipeline, ok := h.pipelines[name]
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", name)
	}

	// Inject logger and event recorder via interface methods.
	if h.logger != nil {
		pipeline.SetLogger(h.logger)
	}
	if h.eventRecorder != nil {
		pipeline.SetEventRecorder(h.eventRecorder)
	}

	result, err := pipeline.Run(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("pipeline %q execution failed: %w", name, err)
	}

	return result, nil
}
