// Package pipelinesteps provides a plugin that registers generic pipeline step
// types: validate, transform, conditional, set, log, delegate, jq, publish,
// http_call, request_parse, db_query, db_exec, json_response.
// It also provides the PipelineWorkflowHandler for composable pipelines.
package pipelinesteps

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers generic pipeline step factories and the pipeline workflow handler.
type Plugin struct {
	plugin.BaseEnginePlugin
	// pipelineHandler is retained so the wiring hook can inject dependencies.
	pipelineHandler *handlers.PipelineWorkflowHandler
}

// New creates a new pipeline-steps plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "pipeline-steps",
				PluginVersion:     "1.0.0",
				PluginDescription: "Generic pipeline step types (validate, transform, conditional, set, log, delegate, jq, etc.)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "pipeline-steps",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Generic pipeline step types and pipeline workflow handler",
				Tier:        plugin.TierCore,
				StepTypes: []string{
					"step.validate",
					"step.transform",
					"step.conditional",
					"step.set",
					"step.log",
					"step.delegate",
					"step.jq",
					"step.publish",
					"step.http_call",
					"step.request_parse",
					"step.db_query",
					"step.db_exec",
					"step.json_response",
				},
				WorkflowTypes: []string{"pipeline"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "pipeline-steps", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "pipeline-steps",
			Description: "Generic pipeline step operations: validate, transform, conditional, set, log, delegate, jq, etc.",
		},
	}
}

// StepFactories returns the step factories provided by this plugin.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.validate":      wrapStepFactory(module.NewValidateStepFactory()),
		"step.transform":     wrapStepFactory(module.NewTransformStepFactory()),
		"step.conditional":   wrapStepFactory(module.NewConditionalStepFactory()),
		"step.set":           wrapStepFactory(module.NewSetStepFactory()),
		"step.log":           wrapStepFactory(module.NewLogStepFactory()),
		"step.delegate":      wrapStepFactory(module.NewDelegateStepFactory()),
		"step.jq":            wrapStepFactory(module.NewJQStepFactory()),
		"step.publish":       wrapStepFactory(module.NewPublishStepFactory()),
		"step.http_call":     wrapStepFactory(module.NewHTTPCallStepFactory()),
		"step.request_parse": wrapStepFactory(module.NewRequestParseStepFactory()),
		"step.db_query":      wrapStepFactory(module.NewDBQueryStepFactory()),
		"step.db_exec":       wrapStepFactory(module.NewDBExecStepFactory()),
		"step.json_response": wrapStepFactory(module.NewJSONResponseStepFactory()),
	}
}

// WorkflowHandlers returns the pipeline workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"pipeline": func() any {
			p.pipelineHandler = handlers.NewPipelineWorkflowHandler()
			return p.pipelineHandler
		},
	}
}

// PipelineHandler returns the plugin's pipeline handler instance, if created.
// This is used by the engine's wiring hook to inject StepRegistry and Logger.
func (p *Plugin) PipelineHandler() *handlers.PipelineWorkflowHandler {
	return p.pipelineHandler
}

// wrapStepFactory converts a module.StepFactory to a plugin.StepFactory,
// threading the modular.Application through so steps like db_exec and
// db_query can access the service registry.
func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, config map[string]any, app modular.Application) (any, error) {
		return f(name, config, app)
	}
}
