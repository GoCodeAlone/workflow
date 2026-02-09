package ai

import (
	"context"

	"github.com/GoCodeAlone/workflow/config"
)

// WorkflowGenerator defines the interface for AI-powered workflow generation.
// Implementations include direct LLM clients and Copilot SDK wrappers.
type WorkflowGenerator interface {
	// GenerateWorkflow creates a complete workflow config from a natural language request.
	GenerateWorkflow(ctx context.Context, req GenerateRequest) (*GenerateResponse, error)

	// GenerateComponent generates Go source code for a component specification.
	GenerateComponent(ctx context.Context, spec ComponentSpec) (string, error)

	// SuggestWorkflow returns workflow suggestions for a given use case description.
	SuggestWorkflow(ctx context.Context, useCase string) ([]WorkflowSuggestion, error)

	// IdentifyMissingComponents analyzes a workflow config and returns specs for
	// components that need to be created.
	IdentifyMissingComponents(ctx context.Context, cfg *config.WorkflowConfig) ([]ComponentSpec, error)
}
