// Package scheduler provides a plugin that registers the scheduler workflow
// handler and the schedule trigger factory.
package scheduler

import (
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the scheduler workflow handler and schedule trigger.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new scheduler plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "scheduler-plugin",
				PluginVersion:     "1.0.0",
				PluginDescription: "Scheduler workflow handler and schedule trigger for cron-based job execution",
			},
			Manifest: plugin.PluginManifest{
				Name:          "scheduler-plugin",
				Version:       "1.0.0",
				Author:        "GoCodeAlone",
				Description:   "Scheduler workflow handler and schedule trigger for cron-based job execution",
				WorkflowTypes: []string{"scheduler"},
				TriggerTypes:  []string{"schedule"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "job-scheduling", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "job-scheduling",
			Description: "Cron-based job scheduling and schedule trigger support",
		},
	}
}

// WorkflowHandlers returns the scheduler workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"scheduler": func() any {
			return handlers.NewSchedulerWorkflowHandler()
		},
	}
}

// TriggerFactories returns the schedule trigger factory.
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"schedule": func() any {
			return module.NewScheduleTrigger()
		},
	}
}
