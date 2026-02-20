// Package featureflags provides a plugin that registers the feature flag
// service module and associated pipeline steps (feature_flag, ff_gate).
package featureflags

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the featureflag.service module type and step.feature_flag / step.ff_gate step factories.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new feature-flags plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "feature-flags",
				PluginVersion:     "1.0.0",
				PluginDescription: "Feature flag service module and pipeline steps (feature_flag, ff_gate)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "feature-flags",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Feature flag service module and pipeline steps (feature_flag, ff_gate)",
				ModuleTypes: []string{"featureflag.service"},
				StepTypes:   []string{"step.feature_flag", "step.ff_gate"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "feature-flags", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// UIPages returns the UI page definitions for this plugin.
// The feature-flags nav item only appears when this plugin is enabled.
func (p *Plugin) UIPages() []plugin.UIPageDef {
	return []plugin.UIPageDef{
		{ID: "feature-flags", Label: "Feature Flags", Icon: "flag", Category: "global", Order: 5},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "feature-flags",
			Description: "Feature flag evaluation and gating for pipeline steps",
		},
	}
}

// ModuleFactories returns the module factories for the feature flag service.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"featureflag.service": func(name string, config map[string]any) modular.Module {
			ffCfg := module.FeatureFlagModuleConfig{}
			if v, ok := config["provider"].(string); ok {
				ffCfg.Provider = v
			}
			if v, ok := config["cache_ttl"].(string); ok {
				ffCfg.CacheTTL = v
			}
			if v, ok := config["sse_enabled"].(bool); ok {
				ffCfg.SSEEnabled = v
			}
			if v, ok := config["db_path"].(string); ok {
				ffCfg.DBPath = v
			}
			ffMod, err := module.NewFeatureFlagModule(name, ffCfg)
			if err != nil {
				// Return nil; the engine will catch missing module
				return nil
			}
			return ffMod
		},
	}
}

// StepFactories returns step factories for feature flag steps.
// Note: The step.feature_flag and step.ff_gate steps require the FF service,
// which is only available after the featureflag.service module is initialized.
// These placeholder factories return an error directing users to configure the
// featureflag.service module first. The engine's BuildFromConfig wires the real
// factories when the featureflag.service module is loaded.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.feature_flag": func(name string, config map[string]any, _ modular.Application) (any, error) {
			// The real factory is wired by engine.go when featureflag.service is loaded.
			// This placeholder ensures the step type is registered in the manifest.
			return nil, errServiceRequired("feature_flag", name)
		},
		"step.ff_gate": func(name string, config map[string]any, _ modular.Application) (any, error) {
			return nil, errServiceRequired("ff_gate", name)
		},
	}
}

func errServiceRequired(stepType, name string) error {
	return &featureFlagServiceError{stepType: stepType, stepName: name}
}

type featureFlagServiceError struct {
	stepType string
	stepName string
}

func (e *featureFlagServiceError) Error() string {
	return e.stepType + " step " + e.stepName + ": featureflag.service module must be loaded first"
}
