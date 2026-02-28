// Package license provides an EnginePlugin that registers the license.validator
// module type, which validates licenses against a remote server and gates
// premium plugin access.
package license

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/licensing"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

//go:embed keys/license.pub
var embeddedPublicKey []byte

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
				WiringHooks: []string{"license-validator-wiring"},
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

// WiringHooks returns a hook that wires an Ed25519 OfflineValidator (and optional
// CompositeValidator) to the PluginLoader when WORKFLOW_LICENSE_TOKEN is set.
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "license-validator-wiring",
			Priority: 20,
			Hook:     licenseValidatorWiringHook,
		},
	}
}

// engineWithLoader is a local interface to retrieve a PluginLoader from the
// registered "workflowEngine" service without importing the engine package.
type engineWithLoader interface {
	PluginLoader() *plugin.PluginLoader
}

// licenseValidatorAdapter implements plugin.LicenseValidator by delegating to a
// licensing.Validator. When an OfflineValidator is available, it is used for
// authoritative plugin validation. Otherwise the HTTP validator's LicenseInfo is
// checked for tier and feature membership.
type licenseValidatorAdapter struct {
	validator licensing.Validator
	offline   *licensing.OfflineValidator // may be nil
}

func (a *licenseValidatorAdapter) ValidatePlugin(pluginName string) error {
	if a.offline != nil {
		return a.offline.ValidatePlugin(pluginName)
	}
	info := a.validator.GetLicenseInfo()
	if info == nil {
		return fmt.Errorf("no license loaded")
	}
	if info.Tier != "professional" && info.Tier != "enterprise" {
		return fmt.Errorf("license tier %q does not permit premium plugins", info.Tier)
	}
	for _, f := range info.Features {
		if f == pluginName {
			return nil
		}
	}
	return fmt.Errorf("plugin %q is not licensed", pluginName)
}

// licenseValidatorWiringHook reads WORKFLOW_LICENSE_TOKEN, creates an
// OfflineValidator (and optionally a CompositeValidator), and registers it on
// the PluginLoader if the engine is available in the service registry.
func licenseValidatorWiringHook(app modular.Application, _ *config.WorkflowConfig) error {
	tokenStr := os.Getenv("WORKFLOW_LICENSE_TOKEN")

	// Scan the service registry for the engine and any registered HTTP validator.
	var loader *plugin.PluginLoader
	var httpValidator *licensing.HTTPValidator
	for _, svc := range app.SvcRegistry() {
		if e, ok := svc.(engineWithLoader); ok && loader == nil {
			loader = e.PluginLoader()
		}
		if hv, ok := svc.(*licensing.HTTPValidator); ok && httpValidator == nil {
			httpValidator = hv
		}
	}

	if tokenStr == "" {
		// No offline token configured — wire HTTP validator if available.
		if loader != nil && httpValidator != nil {
			loader.SetLicenseValidator(&licenseValidatorAdapter{validator: httpValidator})
		}
		return nil
	}

	offline, err := licensing.NewOfflineValidator(embeddedPublicKey, tokenStr)
	if err != nil {
		return fmt.Errorf("license-validator-wiring: create offline validator: %w", err)
	}

	var lv plugin.LicenseValidator
	if httpValidator != nil {
		lv = licensing.NewCompositeValidator(offline, httpValidator)
	} else {
		lv = offline
	}

	if loader != nil {
		loader.SetLicenseValidator(lv)
	}
	return nil
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
