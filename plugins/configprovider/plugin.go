// Package configprovider registers the config.provider module type and its
// ConfigTransformHook. The hook runs before module registration to parse the
// config.provider schema, load values from declared sources, validate required
// keys, and expand {{config "key"}} references throughout the rest of the
// configuration.
package configprovider

import (
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the config.provider module factory and a ConfigTransformHook
// that resolves {{config "key"}} references at config load time.
type Plugin struct {
	plugin.BaseEnginePlugin
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
			Hook:     configTransformHook,
		},
	}
}

// configTransformHook processes all config.provider modules in the configuration.
func configTransformHook(cfg *config.WorkflowConfig) error {
	registry := module.GetConfigRegistry()
	registry.Reset()

	found := false
	for _, modCfg := range cfg.Modules {
		if modCfg.Type != "config.provider" {
			continue
		}
		found = true

		if err := processConfigProvider(registry, modCfg.Config); err != nil {
			return fmt.Errorf("config.provider module %q: %w", modCfg.Name, err)
		}
	}

	if !found {
		return nil // No config.provider modules — nothing to do
	}

	registry.Freeze()

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

// processConfigProvider parses a single config.provider module's config,
// loads sources, and validates required keys.
func processConfigProvider(registry *module.ConfigRegistry, cfg map[string]any) error {
	// Parse schema
	schemaRaw, ok := cfg["schema"].(map[string]any)
	if !ok {
		return fmt.Errorf("config.provider requires a 'schema' section")
	}
	schemaEntries, err := module.ParseSchema(schemaRaw)
	if err != nil {
		return fmt.Errorf("invalid schema: %w", err)
	}

	// Parse and load sources
	sourcesRaw, ok := cfg["sources"]
	if !ok {
		return fmt.Errorf("config.provider requires a 'sources' section")
	}
	sourcesSlice, ok := sourcesRaw.([]any)
	if !ok {
		return fmt.Errorf("'sources' must be a list")
	}
	sources := make([]map[string]any, 0, len(sourcesSlice))
	for _, s := range sourcesSlice {
		sm, ok := s.(map[string]any)
		if !ok {
			return fmt.Errorf("each source must be a map")
		}
		sources = append(sources, sm)
	}

	if err := module.LoadConfigSources(registry, sources, schemaEntries); err != nil {
		return fmt.Errorf("loading config sources: %w", err)
	}

	// Validate required keys
	if err := module.ValidateRequired(registry, schemaEntries); err != nil {
		return err
	}

	return nil
}
