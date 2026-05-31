// Package stubprovider provides a build-tagged loadable EnginePlugin that
// registers an in-process "stub" iac.provider module. This plugin is
// intended for scenario testing and integration tests only — it must NOT
// be linked into production server binaries.
//
// Loading is controlled by the "scenario_stub" build tag (see
// plugins/all/extras_stub.go). Without the tag, plugins/all.DefaultPlugins()
// does not include this plugin and cmd/server cannot load it.
//
// The registered module type is "iac.provider". When a config entry declares:
//
//	type: iac.provider
//	config:
//	  provider: stub
//
// the module's Init validates the provider field and logs a clear warning
// that no real cloud operations will occur. The module registers a
// stubprovider.Provider as a service under its own name so infra.admin can
// resolve it via app.GetService(<providerModuleName>, &iacProvider).
package stubprovider

import (
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/plugin"
)

// Plugin is the engine plugin that registers the stub iac.provider factory.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// Compile-time assertion that Plugin implements plugin.EnginePlugin.
var _ plugin.EnginePlugin = (*Plugin)(nil)

// New creates a new stub provider plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "stubprovider",
				PluginVersion:     "0.1.0",
				PluginDescription: "In-process stub iac.provider for scenario testing — no real cloud operations",
			},
			Manifest: plugin.PluginManifest{
				Name:        "stubprovider",
				Version:     "0.1.0",
				Author:      "GoCodeAlone",
				Description: "In-process stub iac.provider for scenario testing — no real cloud operations",
				ModuleTypes: []string{"iac.provider"},
			},
		},
	}
}

// ModuleFactories returns a factory for "iac.provider" that produces a
// stubModule whose ProvidesServices registers a stubprovider.Provider.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"iac.provider": func(name string, cfg map[string]any) modular.Module {
			return &stubModule{name: name, cfg: cfg}
		},
	}
}

// stubModule is the in-process iac.provider module instantiated by the factory.
type stubModule struct {
	name     string
	cfg      map[string]any
	provider *stubprovider.Provider
}

// Name returns the module instance name.
func (m *stubModule) Name() string { return m.name }

// Init validates config and prepares the stub provider.
// Returns an error when config.provider != "stub" to prevent
// accidentally loading this module as a real cloud provider.
func (m *stubModule) Init(app modular.Application) error {
	pt, _ := m.cfg["provider"].(string)
	if pt != "stub" {
		return fmt.Errorf("iac/stubprovider: module %q: provider must be 'stub', got %q — this plugin cannot proxy real cloud providers", m.name, pt)
	}
	m.provider = stubprovider.New()
	app.Logger().Warn("infra.admin stub provider: NO real cloud operations — demo/test only", "module", m.name)
	return nil
}

// ProvidesServices registers the stub provider under the module name so
// infra.admin can resolve it via app.GetService(m.name, &iacProvider).
func (m *stubModule) ProvidesServices() []modular.ServiceProvider {
	if m.provider == nil {
		// Not yet initialised; provider is set in Init.
		// Return a pre-allocated instance so the DI graph can wire
		// services before Init is called.
		m.provider = stubprovider.New()
	}
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "stub iac.provider — in-process, no real cloud ops",
			Instance:    m.provider,
		},
	}
}

// RequiresServices returns nil — the stub provider has no service dependencies.
func (m *stubModule) RequiresServices() []modular.ServiceDependency { return nil }
