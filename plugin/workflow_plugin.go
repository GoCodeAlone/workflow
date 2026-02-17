package plugin

import (
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

// WorkflowPlugin extends NativePlugin with the ability to contribute
// embedded workflows that can be invoked as sub-workflows from pipelines.
type WorkflowPlugin interface {
	NativePlugin
	EmbeddedWorkflows() []EmbeddedWorkflow
}

// EmbeddedWorkflow describes a workflow contributed by a plugin.
type EmbeddedWorkflow struct {
	Name         string                           `json:"name"`
	Description  string                           `json:"description"`
	Config       *config.WorkflowConfig           `json:"-"`
	ConfigYAML   string                           `json:"configYaml,omitempty"`
	InputSchema  map[string]schema.ConfigFieldDef `json:"inputSchema,omitempty"`
	OutputSchema map[string]schema.ConfigFieldDef `json:"outputSchema,omitempty"`
}
