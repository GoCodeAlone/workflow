// Package timeline provides a plugin that registers the timeline.service
// module type for config-driven timeline/replay handler initialization.
package timeline

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	evstore "github.com/GoCodeAlone/workflow/store"
)

// Plugin registers the timeline.service module type.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new timeline plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "timeline",
				PluginVersion:     "1.0.0",
				PluginDescription: "Timeline and replay service module for execution visualization",
			},
			Manifest: plugin.PluginManifest{
				Name:        "timeline",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Timeline and replay service module for execution visualization",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"timeline.service"},
			},
		},
	}
}

// ModuleFactories returns the module factories for the timeline service.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"timeline.service": func(name string, config map[string]any) modular.Module {
			// The timeline module needs an EventStore. It discovers the event
			// store from the config's "event_store" key, which should reference
			// a service name registered by an eventstore.service module.
			// At factory time we don't have the Application yet, so we use a
			// deferred-init approach: create a stub that resolves at Init().
			return &deferredTimelineModule{
				name:           name,
				eventStoreName: stringFromConfig(config, "event_store", "admin-event-store"),
			}
		},
	}
}

// deferredTimelineModule resolves the event store dependency at Init() time.
type deferredTimelineModule struct {
	name           string
	eventStoreName string
	inner          *module.TimelineServiceModule
}

func (m *deferredTimelineModule) Name() string { return m.name }

func (m *deferredTimelineModule) Init(app modular.Application) error {
	// Look up the event store from the service registry
	var store *evstore.SQLiteEventStore
	if err := app.GetService(m.eventStoreName, &store); err != nil || store == nil {
		// Fallback: try to find any EventStore in the registry
		for _, svc := range app.SvcRegistry() {
			if es, ok := svc.(*evstore.SQLiteEventStore); ok {
				store = es
				break
			}
		}
	}
	if store == nil {
		// No event store available — the module will provide stub handlers
		return nil
	}
	m.inner = module.NewTimelineServiceModule(m.name, store)
	return nil
}

func (m *deferredTimelineModule) ProvidesServices() []modular.ServiceProvider {
	if m.inner != nil {
		return m.inner.ProvidesServices()
	}
	return nil
}

func (m *deferredTimelineModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

func stringFromConfig(config map[string]any, key, defaultVal string) string {
	if v, ok := config[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}
