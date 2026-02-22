// Package integration provides a plugin that registers the integration
// workflow handler for connector-based integration workflows.
package integration

import (
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the integration workflow handler.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new integration plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "integration-plugin",
				PluginVersion:     "1.0.0",
				PluginDescription: "Integration workflow handler for connector-based multi-system workflows",
			},
			Manifest: plugin.PluginManifest{
				Name:          "integration-plugin",
				Version:       "1.0.0",
				Author:        "GoCodeAlone",
				Description:   "Integration workflow handler for connector-based multi-system workflows",
				Tier:          plugin.TierCore,
				WorkflowTypes: []string{"integration"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "integration-connectors", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "integration-connectors",
			Description: "Multi-system integration via HTTP, webhook, and database connectors",
		},
	}
}

// WorkflowHandlers returns the integration workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"integration": func() any {
			return handlers.NewIntegrationWorkflowHandler()
		},
	}
}
