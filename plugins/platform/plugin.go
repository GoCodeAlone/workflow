// Package platform provides an EnginePlugin that registers all platform-related
// module types, the platform workflow handler, the reconciliation trigger,
// and the platform template step.
package platform

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin is the platform EnginePlugin.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new platform plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "platform",
				PluginVersion:     "1.0.0",
				PluginDescription: "Platform infrastructure modules, workflow handler, reconciliation trigger, and template step",
			},
			Manifest: plugin.PluginManifest{
				Name:          "platform",
				Version:       "1.0.0",
				Author:        "GoCodeAlone",
				Description:   "Platform infrastructure modules, workflow handler, reconciliation trigger, and template step",
				Tier:          plugin.TierCore,
				ModuleTypes:   []string{"platform.provider", "platform.resource", "platform.context"},
				StepTypes:     []string{"step.platform_template"},
				TriggerTypes:  []string{"reconciliation"},
				WorkflowTypes: []string{"platform"},
			},
		},
	}
}

// ModuleFactories returns factory functions for platform module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"platform.provider": func(name string, cfg map[string]any) modular.Module {
			providerName := ""
			if pn, ok := cfg["name"].(string); ok {
				providerName = pn
			}
			svcName := "platform.provider"
			if providerName != "" {
				svcName = "platform.provider." + providerName
			}
			return module.NewServiceModule(name, map[string]any{
				"provider_name": providerName,
				"service_name":  svcName,
				"config":        cfg,
			})
		},
		"platform.resource": func(name string, cfg map[string]any) modular.Module {
			return module.NewServiceModule(name, cfg)
		},
		"platform.context": func(name string, cfg map[string]any) modular.Module {
			return module.NewServiceModule(name, cfg)
		},
	}
}

// StepFactories returns the platform template step factory.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.platform_template": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewPlatformTemplateStepFactory()(name, cfg, app)
		},
	}
}

// TriggerFactories returns the reconciliation trigger factory.
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"reconciliation": func() any {
			return module.NewReconciliationTrigger()
		},
	}
}

// WorkflowHandlers returns the platform workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"platform": func() any {
			return handlers.NewPlatformWorkflowHandler()
		},
	}
}

// ModuleSchemas returns UI schema definitions for platform module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "platform.provider",
			Label:       "Platform Provider",
			Category:    "infrastructure",
			Description: "Cloud infrastructure provider (e.g., Terraform, Pulumi)",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "name", Label: "Provider Name", Type: schema.FieldTypeString, Required: true, Description: "Name of the platform provider"},
			},
		},
		{
			Type:        "platform.resource",
			Label:       "Platform Resource",
			Category:    "infrastructure",
			Description: "Infrastructure resource managed by a platform provider",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "type", Label: "Resource Type", Type: schema.FieldTypeString, Required: true, Description: "Type of infrastructure resource"},
			},
		},
		{
			Type:        "platform.context",
			Label:       "Platform Context",
			Category:    "infrastructure",
			Description: "Execution context for platform operations",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "path", Label: "Context Path", Type: schema.FieldTypeString, Required: true, Description: "Path identifying this context"},
			},
		},
	}
}
