// Package scanner provides a built-in engine plugin that registers
// the security.scanner module type, implementing SecurityScannerProvider.
// It supports mock mode for testing. CLI mode (shelling out to semgrep,
// trivy, grype) is not yet implemented.
package scanner

import (
	"log/slog"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin registers the security.scanner module type.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new scanner plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "scanner",
				PluginVersion:     "1.0.0",
				PluginDescription: "Security scanner provider: SAST, container, and dependency scanning with mock mode for testing",
			},
			Manifest: plugin.PluginManifest{
				Name:        "scanner",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Security scanner provider with pluggable backends",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"security.scanner"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "security-scanner", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the plugin's capability contracts.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "security-scanner",
			Description: "Security scanning: SAST, container image, and dependency vulnerability scanning",
		},
	}
}

// ModuleFactories returns the security.scanner module factory.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"security.scanner": func(name string, cfg map[string]any) modular.Module {
			mod, err := NewScannerModule(name, cfg)
			if err != nil {
				slog.Error("security.scanner: failed to create module", "name", name, "error", err)
				return nil
			}
			return mod
		},
	}
}

// ModuleSchemas returns schemas for the security.scanner module.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		scannerModuleSchema(),
	}
}

func scannerModuleSchema() *schema.ModuleSchema {
	return &schema.ModuleSchema{
		Type:        "security.scanner",
		Description: "Security scanner provider supporting mock, semgrep, trivy, and grype backends",
		ConfigFields: []schema.ConfigFieldDef{
			{Key: "mode", Type: schema.FieldTypeSelect, Description: "Scanner mode: 'mock' for testing or 'cli' for real tools", DefaultValue: "mock", Options: []string{"mock", "cli"}},
			{Key: "semgrepBinary", Type: schema.FieldTypeString, Description: "Path to semgrep binary", DefaultValue: "semgrep"},
			{Key: "trivyBinary", Type: schema.FieldTypeString, Description: "Path to trivy binary", DefaultValue: "trivy"},
			{Key: "grypeBinary", Type: schema.FieldTypeString, Description: "Path to grype binary", DefaultValue: "grype"},
			{Key: "mockFindings", Type: schema.FieldTypeJSON, Description: "Mock findings to return (keyed by scan type: sast, container, deps)"},
		},
	}
}
