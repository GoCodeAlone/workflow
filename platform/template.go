package platform

import "context"

// TemplateRegistry manages versioned platform workflow templates.
// Templates are parameterized, reusable definitions of capability
// declarations that can be instantiated with specific values.
type TemplateRegistry interface {
	// Register adds or updates a template in the registry.
	Register(ctx context.Context, template *WorkflowTemplate) error

	// Get retrieves a specific template by name and version.
	Get(ctx context.Context, name, version string) (*WorkflowTemplate, error)

	// GetLatest retrieves the latest version of a template by name.
	GetLatest(ctx context.Context, name string) (*WorkflowTemplate, error)

	// List returns summaries of all available templates.
	List(ctx context.Context) ([]*WorkflowTemplateSummary, error)

	// Resolve instantiates a template with the given parameters, producing
	// a set of concrete capability declarations.
	Resolve(ctx context.Context, name, version string, params map[string]any) ([]CapabilityDeclaration, error)
}

// WorkflowTemplate is a parameterized, reusable platform workflow definition.
// Users define templates with parameters that are substituted at resolution time
// to produce concrete capability declarations.
type WorkflowTemplate struct {
	// Name is the template identifier.
	Name string `yaml:"name" json:"name"`

	// Version is the template version following semantic versioning.
	Version string `yaml:"version" json:"version"`

	// Description is a human-readable description of what the template provides.
	Description string `yaml:"description" json:"description"`

	// Parameters are the configurable inputs for this template.
	Parameters []TemplateParameter `yaml:"parameters" json:"parameters"`

	// Capabilities are the capability declarations with parameter placeholders.
	Capabilities []CapabilityDeclaration `yaml:"capabilities" json:"capabilities"`

	// Outputs are the declared outputs of this template, referencing resource outputs.
	Outputs []TemplateOutput `yaml:"outputs" json:"outputs"`

	// Tier is the infrastructure tier this template targets.
	Tier Tier `yaml:"tier" json:"tier"`
}

// TemplateParameter describes a configurable input for a workflow template.
type TemplateParameter struct {
	// Name is the parameter identifier used in placeholder expressions.
	Name string `yaml:"name" json:"name"`

	// Type is the parameter data type: "string", "int", "bool", "list", "map".
	Type string `yaml:"type" json:"type"`

	// Required indicates whether the parameter must be provided during resolution.
	Required bool `yaml:"required" json:"required"`

	// Default is the value used when the parameter is not provided.
	Default any `yaml:"default,omitempty" json:"default,omitempty"`

	// Description is a human-readable description of the parameter.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Validation is a regex pattern or constraint expression for validating the value.
	Validation string `yaml:"validation,omitempty" json:"validation,omitempty"`
}

// TemplateOutput declares a named output of a template that references
// a resource output from the resolved capabilities.
type TemplateOutput struct {
	// Name is the output identifier.
	Name string `yaml:"name" json:"name"`

	// Value is an expression referencing a resource output (e.g., "service-name.endpoint").
	Value string `yaml:"value" json:"value"`
}

// WorkflowTemplateSummary is a condensed view of a template for listing purposes.
type WorkflowTemplateSummary struct {
	// Name is the template identifier.
	Name string `json:"name"`

	// Version is the template version.
	Version string `json:"version"`

	// Description is a human-readable description.
	Description string `json:"description"`

	// Parameters lists the parameter names accepted by this template.
	Parameters []string `json:"parameters"`
}
