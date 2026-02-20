// Package pipelinesteps provides a plugin that registers generic pipeline step
// types: validate, transform, conditional, set, log, delegate, jq, publish,
// http_call, request_parse, db_query, db_exec, json_response.
package pipelinesteps

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers generic pipeline step factories.
type Plugin struct {
	plugin.BaseEnginePlugin
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
				Description: "Generic pipeline step types (validate, transform, conditional, set, log, delegate, jq, etc.)",
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

// wrapStepFactory converts a module.StepFactory to a plugin.StepFactory,
// threading the modular.Application through so steps like db_exec and
// db_query can access the service registry.
func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, config map[string]any, app modular.Application) (any, error) {
		return f(name, config, app)
	}
}
