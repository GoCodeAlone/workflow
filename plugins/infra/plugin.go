// Package infra provides an EnginePlugin that registers all 13 abstract infra.*
// module types. Each type delegates provisioning to an [interfaces.IaCProvider]
// (resolved at Init time from the service registry) and exposes the provider's
// [interfaces.ResourceDriver] under "<module-name>.driver" for use by pipeline steps.
package infra

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// infraTypes is the complete list of abstract infrastructure resource types.
var infraTypes = []string{
	"infra.container_service",
	"infra.k8s_cluster",
	"infra.database",
	"infra.cache",
	"infra.vpc",
	"infra.load_balancer",
	"infra.dns",
	"infra.registry",
	"infra.api_gateway",
	"infra.firewall",
	"infra.iam_role",
	"infra.storage",
	"infra.certificate",
}

// Plugin registers all infra.* abstract module types.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new infra plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "infra",
				PluginVersion:     "1.0.0",
				PluginDescription: "Abstract infra.* module types with IaCProvider delegation",
			},
			Manifest: plugin.PluginManifest{
				Name:        "infra",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Abstract infra.* module types with IaCProvider delegation",
				Tier:        plugin.TierCore,
				ModuleTypes: infraTypes,
			},
		},
	}
}

// ModuleFactories returns a factory for each infra.* type.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	factories := make(map[string]plugin.ModuleFactory, len(infraTypes))
	for _, t := range infraTypes {
		factories[t] = func(infraType string) plugin.ModuleFactory {
			return func(name string, cfg map[string]any) modular.Module {
				return module.NewInfraModule(name, infraType, cfg)
			}
		}(t)
	}
	return factories
}

// ModuleSchemas returns UI schema definitions for each infra.* type.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	commonFields := []schema.ConfigFieldDef{
		{Key: "provider", Label: "Provider Module", Type: schema.FieldTypeString, Required: true, Description: "Name of the iac.provider module to delegate to"},
		{Key: "size", Label: "Size Tier", Type: schema.FieldTypeString, Description: "Sizing tier: xs/s/m/l/xl (default: s)"},
		{Key: "resources", Label: "Resource Hints", Type: schema.FieldTypeJSON, Description: "Optional cpu/memory/storage overrides"},
	}

	schemas := make([]*schema.ModuleSchema, 0, len(infraTypes))
	for _, t := range infraTypes {
		schemas = append(schemas, &schema.ModuleSchema{
			Type:         t,
			Label:        t,
			Category:     "infrastructure",
			Description:  "Abstract infrastructure resource: " + t,
			ConfigFields: commonFields,
		})
	}
	return schemas
}
