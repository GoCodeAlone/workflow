// Package modularcompat provides a plugin that registers CrisisTextLine/modular
// framework module adapters: scheduler.modular, cache.modular, database.modular.
package modularcompat

import (
	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/cache"
	database "github.com/CrisisTextLine/modular/modules/database/v2"
	"github.com/CrisisTextLine/modular/modules/scheduler"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers modular framework compatibility module factories.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new modular compatibility plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "modular-compat",
				PluginVersion:     "1.0.0",
				PluginDescription: "CrisisTextLine/modular framework compatibility modules (scheduler, cache, database)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "modular-compat",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "CrisisTextLine/modular framework compatibility modules (scheduler, cache, database)",
				ModuleTypes: []string{"scheduler.modular", "cache.modular", "database.modular"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "scheduler", Role: "provider", Priority: 30},
					{Name: "cache", Role: "provider", Priority: 30},
					{Name: "database", Role: "provider", Priority: 30},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "scheduler",
			Description: "Job scheduling via CrisisTextLine/modular scheduler module",
		},
		{
			Name:        "cache",
			Description: "Caching via CrisisTextLine/modular cache module",
		},
		{
			Name:        "database",
			Description: "Database access via CrisisTextLine/modular database module",
		},
	}
}

// ModuleFactories returns module factories that delegate to the modular framework modules.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"scheduler.modular": func(_ string, _ map[string]any) modular.Module {
			return scheduler.NewModule()
		},
		"cache.modular": func(_ string, _ map[string]any) modular.Module {
			return cache.NewModule()
		},
		"database.modular": func(_ string, _ map[string]any) modular.Module {
			return database.NewModule()
		},
	}
}
