// Package dlq provides a plugin that registers the dlq.service module type
// for config-driven dead letter queue initialization.
package dlq

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin registers the dlq.service module type.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new DLQ plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "dlq",
				PluginVersion:     "1.0.0",
				PluginDescription: "Dead letter queue service module for failed message management",
			},
			Manifest: plugin.PluginManifest{
				Name:        "dlq",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Dead letter queue service module for failed message management",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"dlq.service"},
			},
		},
	}
}

// ModuleFactories returns the module factories for the DLQ service.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"dlq.service": func(name string, config map[string]any) modular.Module {
			cfg := module.DLQServiceConfig{
				MaxRetries:    3,
				RetentionDays: 30,
			}
			if v, ok := config["max_retries"].(int); ok {
				cfg.MaxRetries = v
			} else if v, ok := config["max_retries"].(float64); ok {
				cfg.MaxRetries = int(v)
			}
			if v, ok := config["retention_days"].(int); ok {
				cfg.RetentionDays = v
			} else if v, ok := config["retention_days"].(float64); ok {
				cfg.RetentionDays = int(v)
			}
			return module.NewDLQServiceModule(name, cfg)
		},
	}
}
