// Package eventstore provides a plugin that registers the eventstore.service
// module type for config-driven event store initialization.
package eventstore

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the eventstore.service module type.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new eventstore plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "eventstore",
				PluginVersion:     "1.0.0",
				PluginDescription: "Event store service module for execution event persistence",
			},
			Manifest: plugin.PluginManifest{
				Name:        "eventstore",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Event store service module for execution event persistence",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"eventstore.service"},
			},
		},
	}
}

// ModuleFactories returns the module factories for the event store service.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"eventstore.service": func(name string, config map[string]any) modular.Module {
			cfg := module.EventStoreServiceConfig{
				DBPath:        "data/events.db",
				RetentionDays: 90,
			}
			if v, ok := config["db_path"].(string); ok {
				cfg.DBPath = v
			}
			if v, ok := config["retention_days"].(int); ok {
				cfg.RetentionDays = v
			} else if v, ok := config["retention_days"].(float64); ok {
				cfg.RetentionDays = int(v)
			}
			mod, err := module.NewEventStoreServiceModule(name, cfg)
			if err != nil {
				return nil
			}
			return mod
		},
	}
}
