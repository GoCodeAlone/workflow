// Package configprovider registers the config.provider module type and its
// ConfigTransformHook. The hook runs before module registration to parse the
// config.provider schema, load values from declared sources, validate required
// keys, and expand {{config "key"}} references throughout the rest of the
// configuration.
package configprovider

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the config.provider module factory and a ConfigTransformHook
// that resolves {{config "key"}} references at config load time.
type Plugin struct {
	plugin.BaseEnginePlugin
	mu           sync.Mutex
	credentialed bool
	constructed  bool
	providerName string
	registry     *module.ConfigRegistry
	grants       []interfaces.StepCredentialGrant
}

// New creates a new config provider plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "configprovider",
				PluginVersion:     "1.0.0",
				PluginDescription: "Application configuration registry with schema validation, defaults, and source layering",
			},
			Manifest: plugin.PluginManifest{
				Name:        "configprovider",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Application configuration registry with schema validation, defaults, and source layering",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"config.provider"},
			},
		},
	}
}

// ModuleFactories returns the config.provider module factory.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"config.provider": func(name string, cfg map[string]any) modular.Module {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.credentialed {
				if p.constructed || p.registry == nil || name != p.providerName {
					return module.NewConfigProviderModuleWithCredentials(name, nil, nil)
				}
				p.constructed = true
				return module.NewConfigProviderModuleWithCredentials(name, p.registry, p.grants)
			}
			return module.NewConfigProviderModule(name, cfg)
		},
	}
}

// ConfigTransformHooks returns a high-priority hook that processes config.provider
// modules before any other modules are registered. It:
//  1. Finds config.provider modules in the config
//  2. Parses their schema definitions
//  3. Loads values from declared sources (defaults, env)
//  4. Validates all required keys are present
//  5. Expands {{config "key"}} references in all other module, workflow, trigger, and pipeline configs
func (p *Plugin) ConfigTransformHooks() []plugin.ConfigTransformHook {
	return []plugin.ConfigTransformHook{
		{
			Name:     "config-provider-expansion",
			Priority: 1000, // Run before other transform hooks
			Hook:     p.configTransformHook,
		},
	}
}

// configTransformHook processes all config.provider modules in the configuration.
func (p *Plugin) configTransformHook(cfg *config.WorkflowConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.credentialed {
		return fmt.Errorf("credentialed configprovider plugin cannot be reused; create a separate instance per application")
	}
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	var grants []interfaces.StepCredentialGrant
	providerCount := 0
	for _, modCfg := range cfg.Modules {
		if modCfg.Type != "config.provider" {
			continue
		}
		providerCount++
		if raw, exists := modCfg.Config["step_credentials"]; exists {
			decoded, err := module.DecodeStepCredentialGrants(raw)
			if err != nil {
				return err
			}
			grants = append(grants, decoded...)
		}
	}
	var registry *module.ConfigRegistry
	if len(grants) > 0 {
		if providerCount != 1 {
			return fmt.Errorf("step credentials require exactly one config.provider module")
		}
		if err := validateOriginalCredentialSteps(cfg, grants); err != nil {
			return err
		}
		p.credentialed = true
		registry = module.NewConfigRegistry()
	} else {
		registry = module.GetConfigRegistry()
		registry.Reset()
	}

	found := false
	for _, modCfg := range cfg.Modules {
		if modCfg.Type != "config.provider" {
			continue
		}
		found = true

		if err := processConfigProvider(registry, modCfg.Config); err != nil {
			return fmt.Errorf("config.provider module %q: %w", modCfg.Name, err)
		}
		if p.credentialed {
			p.providerName = modCfg.Name
		}
	}

	if !found {
		return nil // No config.provider modules — nothing to do
	}

	registry.Freeze()
	if p.credentialed {
		p.registry = registry
		p.grants = append([]interfaces.StepCredentialGrant(nil), grants...)
		module.MirrorConfigRegistry(registry)
	}

	// Expand {{config "key"}} in all module configs (except config.provider itself)
	for i := range cfg.Modules {
		if cfg.Modules[i].Type == "config.provider" {
			continue
		}
		module.ExpandConfigRefsMap(registry, cfg.Modules[i].Config)
	}

	// Expand in workflow configs
	for key, wf := range cfg.Workflows {
		if m, ok := wf.(map[string]any); ok {
			module.ExpandConfigRefsMap(registry, m)
			cfg.Workflows[key] = m
		}
	}

	// Expand in trigger configs
	for key, tr := range cfg.Triggers {
		if m, ok := tr.(map[string]any); ok {
			module.ExpandConfigRefsMap(registry, m)
			cfg.Triggers[key] = m
		}
	}

	// Expand in pipeline configs
	for key, pl := range cfg.Pipelines {
		if m, ok := pl.(map[string]any); ok {
			module.ExpandConfigRefsMap(registry, m)
			cfg.Pipelines[key] = m
		}
	}

	// Expand in platform configs
	module.ExpandConfigRefsMap(registry, cfg.Platform)

	return nil
}

func processConfigProvider(registry *module.ConfigRegistry, cfg map[string]any) error {
	return module.LoadConfigProvider(registry, cfg)
}

// validateOriginalCredentialSteps checks literals before any template expansion.
func validateOriginalCredentialSteps(cfg *config.WorkflowConfig, grants []interfaces.StepCredentialGrant) error {
	// Parse declared pipeline topology, not arbitrary nested maps or schema hints.
	var steps []config.PipelineStepConfig
	for _, raw := range cfg.Pipelines {
		data, err := json.Marshal(raw)
		if err != nil {
			return fmt.Errorf("invalid credentialed pipeline")
		}
		var pl config.PipelineConfig
		if err := json.Unmarshal(data, &pl); err != nil {
			return fmt.Errorf("invalid credentialed pipeline")
		}
		steps = append(steps, pl.Steps...)
		steps = append(steps, pl.Compensation...)
	}
	for _, g := range grants {
		matches := 0
		for _, step := range steps {
			if step.Type != g.StepType || step.Name != g.StepName {
				continue
			}
			matches++
			literal, ok := step.Config[g.Field].(string)
			if !ok || literal != g.Ref {
				return fmt.Errorf("original step credential literal does not match grant")
			}
		}
		if matches != 1 {
			return fmt.Errorf("step credential target is missing or ambiguous")
		}
	}
	return nil
}
