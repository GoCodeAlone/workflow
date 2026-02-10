package ai

import "github.com/GoCodeAlone/workflow/config"

// WorkflowSuggestion represents a suggested workflow with confidence score.
type WorkflowSuggestion struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Config      *config.WorkflowConfig `json:"config"`
	Confidence  float64                `json:"confidence"`
}

// ComponentSpec describes a workflow component that may need to be created.
type ComponentSpec struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Interface   string `json:"interface"` // e.g., "modular.Module", "MessageHandler"
	GoCode      string `json:"goCode"`    // Generated Go source
}

// GenerateRequest contains the input for workflow generation.
type GenerateRequest struct {
	Intent      string            `json:"intent"`      // Natural language description
	Context     map[string]string `json:"context"`     // Additional context
	Constraints []string          `json:"constraints"` // Requirements/constraints
}

// GenerateResponse contains the output of workflow generation.
type GenerateResponse struct {
	Workflow    *config.WorkflowConfig `json:"workflow"`
	Components  []ComponentSpec        `json:"components"`
	Explanation string                 `json:"explanation"`
}

// Provider identifies an AI backend.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderCopilot   Provider = "copilot"
	ProviderAuto      Provider = "auto"
)
