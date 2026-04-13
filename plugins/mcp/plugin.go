// Package mcp provides the MCP (Model Context Protocol) engine plugin.
// It registers the mcp_tool trigger type, the mcp workflow handler type,
// and the mcp.registry module type.
package mcp

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin provides MCP-related module types, trigger types, and workflow handlers.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new MCPPlugin instance.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "workflow-plugin-mcp",
				PluginVersion:     "1.0.0",
				PluginDescription: "MCP tool triggers, workflow handlers, and server registry",
			},
			Manifest: plugin.PluginManifest{
				Name:          "workflow-plugin-mcp",
				Version:       "1.0.0",
				Author:        "GoCodeAlone",
				Description:   "MCP tool triggers, workflow handlers, and server registry",
				Tier:          plugin.TierCore,
				ModuleTypes:   []string{"mcp.registry"},
				TriggerTypes:  []string{"mcp_tool"},
				WorkflowTypes: []string{"mcp"},
			},
		},
	}
}

// ModuleFactories returns the factory for the mcp.registry module type.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"mcp.registry": func(name string, cfg map[string]any) modular.Module {
			return newRegistryModule(name, parseRegistryConfig(cfg))
		},
	}
}

// parseRegistryConfig converts a raw config map to MCPRegistryConfig.
func parseRegistryConfig(cfg map[string]any) module.MCPRegistryConfig {
	if cfg == nil {
		return module.MCPRegistryConfig{}
	}
	var out module.MCPRegistryConfig
	if v, ok := cfg["log_on_init"].(bool); ok {
		out.LogOnInit = v
	}
	if v, ok := cfg["expose_admin_api"].(bool); ok {
		out.ExposeAdminAPI = v
	}
	if v, ok := cfg["audit_tool_calls"].(bool); ok {
		out.AuditToolCalls = v
	}
	return out
}

// TriggerFactories returns trigger constructors for the mcp_tool trigger type.
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"mcp_tool": func() any {
			return module.NewMCPToolTrigger()
		},
	}
}

// WorkflowHandlers returns workflow handler factories for the mcp workflow type.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"mcp": func() any {
			return handlers.NewMCPWorkflowHandler()
		},
	}
}

// registryModule wraps MCPRegistry as a modular.Module.
type registryModule struct {
	name     string
	cfg      module.MCPRegistryConfig
	registry *module.MCPRegistry
}

func newRegistryModule(name string, cfg module.MCPRegistryConfig) *registryModule {
	return &registryModule{
		name:     name,
		cfg:      cfg,
		registry: module.NewMCPRegistry(),
	}
}

func (m *registryModule) Name() string           { return m.name }
func (m *registryModule) Dependencies() []string { return nil }
func (m *registryModule) Init(_ modular.Application) error {
	if m.cfg.LogOnInit {
		m.registry.Logger().Info("mcp.registry module initialized", "name", m.name)
	}
	return nil
}

// Registry returns the underlying MCPRegistry for wiring hooks.
func (m *registryModule) Registry() *module.MCPRegistry {
	return m.registry
}
