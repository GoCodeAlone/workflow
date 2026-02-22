// Package license provides an EnginePlugin that registers the license.validator
// module type, which validates licenses against a remote server and gates
// premium plugin access.
package license

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides the license.validator module type.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new license Plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "license",
				PluginVersion:     "1.0.0",
				PluginDescription: "License validation with remote server, local cache, and grace period",
			},
			Manifest: plugin.PluginManifest{
				Name:        "license",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "License validation with remote server, local cache, and grace period",
				ModuleTypes: []string{"license.validator"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "license-validation", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "license-validation",
			Description: "License key validation against a remote server with caching and grace period",
		},
	}
}

// ModuleFactories returns the factory for license.validator.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"license.validator": func(name string, cfg map[string]any) modular.Module {
			mod, err := module.NewLicenseModule(name, cfg)
			if err != nil {
				// Return nil — the engine will catch nil and report an error.
				return nil
			}
			return mod
		},
	}
}

// ModuleSchemas returns the UI schema definition for license.validator.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "license.validator",
			Label:       "License Validator",
			Category:    "infrastructure",
			Description: "Validates license keys against a remote server with local caching and offline grace period",
			Inputs:      []schema.ServiceIODef{},
			Outputs: []schema.ServiceIODef{
				{Name: "license-validator", Type: "LicenseValidator", Description: "License validation service for feature gating"},
			},
			ConfigFields: []schema.ConfigFieldDef{
				{
					Key:         "server_url",
					Label:       "License Server URL",
					Type:        schema.FieldTypeString,
					Description: "URL of the license validation server (leave empty for offline/starter mode)",
					Placeholder: "https://license.gocodalone.com/api/v1",
				},
				{
					Key:         "license_key",
					Label:       "License Key",
					Type:        schema.FieldTypeString,
					Description: "License key (supports $ENV_VAR expansion; also reads WORKFLOW_LICENSE_KEY env var)",
					Placeholder: "$WORKFLOW_LICENSE_KEY",
					Sensitive:   true,
				},
				{
					Key:          "cache_ttl",
					Label:        "Cache TTL",
					Type:         schema.FieldTypeDuration,
					DefaultValue: "1h",
					Description:  "How long to cache a valid license result before re-validating",
					Placeholder:  "1h",
				},
				{
					Key:          "grace_period",
					Label:        "Grace Period",
					Type:         schema.FieldTypeDuration,
					DefaultValue: "72h",
					Description:  "How long to allow operation when the license server is unreachable",
					Placeholder:  "72h",
				},
				{
					Key:          "refresh_interval",
					Label:        "Refresh Interval",
					Type:         schema.FieldTypeDuration,
					DefaultValue: "1h",
					Description:  "How often the background goroutine re-validates the license",
					Placeholder:  "1h",
				},
			},
			DefaultConfig: map[string]any{
				"cache_ttl":        "1h",
				"grace_period":     "72h",
				"refresh_interval": "1h",
			},
		},
	}
}
