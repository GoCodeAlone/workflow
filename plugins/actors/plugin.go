// Package actors provides actor model support for the workflow engine via goakt v4.
// It enables stateful long-lived entities, distributed execution, structured fault
// recovery, and message-driven workflows alongside existing pipeline-based workflows.
package actors

import (
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides actor model support for the workflow engine.
type Plugin struct {
	plugin.BaseEnginePlugin
	stepRegistry         interfaces.StepRegistryProvider
	concreteStepRegistry *module.StepRegistry
	logger               *slog.Logger
}

// New creates a new actors plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "actors",
				PluginVersion:     "1.0.0",
				PluginDescription: "Actor model support with goakt v4 — stateful entities, distributed execution, and fault-tolerant message-driven workflows",
			},
			Manifest: plugin.PluginManifest{
				Name:        "actors",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Actor model support with goakt v4",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"actor.system",
					"actor.pool",
				},
				StepTypes: []string{
					"step.actor_send",
					"step.actor_ask",
				},
				WorkflowTypes: []string{"actors"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "actor-system", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// SetStepRegistry is called by the engine to inject the step registry.
func (p *Plugin) SetStepRegistry(registry interfaces.StepRegistryProvider) {
	p.stepRegistry = registry
	if concrete, ok := registry.(*module.StepRegistry); ok {
		p.concreteStepRegistry = concrete
	}
}

// SetLogger is called by the engine to inject the logger.
func (p *Plugin) SetLogger(logger *slog.Logger) {
	p.logger = logger
}

// Capabilities returns the plugin's capability contracts.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "actor-system",
			Description: "Actor model runtime: stateful actors, distributed execution, fault-tolerant message-driven workflows",
		},
	}
}

// ModuleFactories returns actor module factories.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"actor.system": func(name string, cfg map[string]any) modular.Module {
			mod, err := NewActorSystemModule(name, cfg)
			if err != nil {
				if p.logger != nil {
					p.logger.Error("failed to create actor.system module", "name", name, "error", err)
				}
				return nil
			}
			if p.logger != nil {
				mod.logger = p.logger
			}
			return mod
		},
		"actor.pool": func(name string, cfg map[string]any) modular.Module {
			mod, err := NewActorPoolModule(name, cfg)
			if err != nil {
				if p.logger != nil {
					p.logger.Error("failed to create actor.pool module", "name", name, "error", err)
				}
				return nil
			}
			if p.logger != nil {
				mod.logger = p.logger
			}
			return mod
		},
	}
}

// StepFactories returns actor step factories.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{}
}

// WorkflowHandlers returns the actor workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{}
}

// ModuleSchemas returns schemas for actor modules.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{}
}

// StepSchemas returns schemas for actor steps.
func (p *Plugin) StepSchemas() []*schema.StepSchema {
	return []*schema.StepSchema{}
}
