// Package ai provides a plugin that registers AI pipeline step types
// (ai_complete, ai_classify, ai_extract), the dynamic.component module type,
// and the sub_workflow step.
package ai

import (
	"github.com/CrisisTextLine/modular"
	aiPkg "github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/module"
	pluginPkg "github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers AI step factories, dynamic.component module factory,
// and the sub_workflow step factory.
type Plugin struct {
	pluginPkg.BaseEnginePlugin

	aiRegistry       *aiPkg.AIModelRegistry
	dynamicRegistry  *dynamic.ComponentRegistry
	dynamicLoader    *dynamic.Loader
	workflowRegistry *pluginPkg.PluginWorkflowRegistry
}

// New creates a new AI plugin. Pass nil for any optional registries;
// the plugin will create defaults where needed.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: pluginPkg.BaseEnginePlugin{
			BaseNativePlugin: pluginPkg.BaseNativePlugin{
				PluginName:        "ai",
				PluginVersion:     "1.0.0",
				PluginDescription: "AI pipeline steps (complete, classify, extract), dynamic components, and sub-workflow orchestration",
			},
			Manifest: pluginPkg.PluginManifest{
				Name:        "ai",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "AI pipeline steps (complete, classify, extract), dynamic components, and sub-workflow orchestration",
				Tier:        pluginPkg.TierCore,
				ModuleTypes: []string{"dynamic.component"},
				StepTypes:   []string{"step.ai_complete", "step.ai_classify", "step.ai_extract", "step.sub_workflow"},
				Capabilities: []pluginPkg.CapabilityDecl{
					{Name: "ai-completion", Role: "provider", Priority: 50},
					{Name: "ai-classification", Role: "provider", Priority: 50},
					{Name: "ai-extraction", Role: "provider", Priority: 50},
				},
			},
		},
		aiRegistry:       aiPkg.NewAIModelRegistry(),
		workflowRegistry: pluginPkg.NewPluginWorkflowRegistry(),
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{Name: "ai-completion", Description: "AI text completion capabilities"},
		{Name: "ai-classification", Description: "AI text classification capabilities"},
		{Name: "ai-extraction", Description: "AI data extraction capabilities"},
	}
}

// SetAIRegistry sets a custom AI model registry (for sharing with other services).
func (p *Plugin) SetAIRegistry(reg *aiPkg.AIModelRegistry) {
	p.aiRegistry = reg
}

// SetDynamicRegistry sets the dynamic component registry for dynamic.component modules.
func (p *Plugin) SetDynamicRegistry(reg *dynamic.ComponentRegistry) {
	p.dynamicRegistry = reg
}

// SetDynamicLoader sets the dynamic loader for loading components from source files.
func (p *Plugin) SetDynamicLoader(loader *dynamic.Loader) {
	p.dynamicLoader = loader
}

// SetWorkflowRegistry sets the plugin workflow registry for sub_workflow steps.
func (p *Plugin) SetWorkflowRegistry(reg *pluginPkg.PluginWorkflowRegistry) {
	p.workflowRegistry = reg
}

// ModuleFactories returns module factories for the dynamic.component type.
func (p *Plugin) ModuleFactories() map[string]pluginPkg.ModuleFactory {
	return map[string]pluginPkg.ModuleFactory{
		"dynamic.component": func(name string, cfg map[string]any) modular.Module {
			if p.dynamicRegistry == nil {
				return nil
			}
			componentID := name
			if id, ok := cfg["componentId"].(string); ok && id != "" {
				componentID = id
			}
			// Load from source if loader is available
			if p.dynamicLoader != nil {
				if sourcePath, ok := cfg["source"].(string); ok && sourcePath != "" {
					sourcePath = config.ResolvePathInConfig(cfg, sourcePath)
					_, _ = p.dynamicLoader.LoadFromFile(componentID, sourcePath)
				}
			}
			comp, ok := p.dynamicRegistry.Get(componentID)
			if !ok {
				return nil
			}
			adapter := dynamic.NewModuleAdapter(comp)
			providesList := []string{name}
			if provides, ok := cfg["provides"].([]any); ok {
				for _, pv := range provides {
					if s, ok := pv.(string); ok {
						providesList = append(providesList, s)
					}
				}
			}
			adapter.SetProvides(providesList)
			if requires, ok := cfg["requires"].([]any); ok {
				svcs := make([]string, 0, len(requires))
				for _, r := range requires {
					if s, ok := r.(string); ok {
						svcs = append(svcs, s)
					}
				}
				adapter.SetRequires(svcs)
			}
			return adapter
		},
	}
}

// StepFactories returns step factories for AI steps and sub_workflow.
func (p *Plugin) StepFactories() map[string]pluginPkg.StepFactory {
	return map[string]pluginPkg.StepFactory{
		"step.ai_complete": wrapStepFactory(module.NewAICompleteStepFactory(p.aiRegistry)),
		"step.ai_classify": wrapStepFactory(module.NewAIClassifyStepFactory(p.aiRegistry)),
		"step.ai_extract":  wrapStepFactory(module.NewAIExtractStepFactory(p.aiRegistry)),
		"step.sub_workflow": wrapStepFactory(module.NewSubWorkflowStepFactory(
			p.workflowRegistry,
			func(pipelineName string, _ *config.WorkflowConfig, _ modular.Application) (*module.Pipeline, error) {
				return &module.Pipeline{Name: pipelineName}, nil
			},
		)),
	}
}

func wrapStepFactory(f module.StepFactory) pluginPkg.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}
