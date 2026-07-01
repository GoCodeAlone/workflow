package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// IaCProviderModule aliases an external plugin-served IaC provider into the
// application service graph under an app-local module name.
//
// Config keys:
//   - plugin: external plugin service name to use, for example workflow-plugin-aws
//   - service: fallback spelling for plugin
//   - provider: legacy/provider shorthand such as aws or digitalocean
//   - remaining keys are passed to provider.Initialize
type IaCProviderModule struct {
	name       string
	config     map[string]any
	pluginName string
	provider   interfaces.IaCProvider
}

// NewIaCProviderModule creates an iac.provider module.
func NewIaCProviderModule(name string, cfg map[string]any) *IaCProviderModule {
	return &IaCProviderModule{name: name, config: cfg}
}

func (m *IaCProviderModule) Name() string { return m.name }

// Init resolves the external provider service, initializes it with the
// provider-specific config, and registers the provider under this module name.
func (m *IaCProviderModule) Init(app modular.Application) error {
	m.pluginName = iacProviderString(m.config, "plugin")
	if m.pluginName == "" {
		m.pluginName = iacProviderString(m.config, "service")
	}
	if m.pluginName == "" {
		m.pluginName = pluginNameFromProvider(iacProviderString(m.config, "provider"))
	}
	if m.pluginName == "" {
		return fmt.Errorf("iac.provider %q: 'plugin' config is required (or provider shorthand such as aws)", m.name)
	}

	var svc any
	if err := app.GetService(m.pluginName, &svc); err != nil {
		return fmt.Errorf("iac.provider %q: plugin service %q not found: %w", m.name, m.pluginName, err)
	}

	provider, ok := svc.(interfaces.IaCProvider)
	if !ok {
		return fmt.Errorf("iac.provider %q: service %q does not implement IaCProvider", m.name, m.pluginName)
	}
	m.provider = provider

	if err := provider.Initialize(context.Background(), m.providerConfig()); err != nil {
		return fmt.Errorf("iac.provider %q: initialize plugin %q: %w", m.name, m.pluginName, err)
	}

	if m.name == m.pluginName {
		return nil
	}
	if err := app.RegisterService(m.name, provider); err != nil {
		return fmt.Errorf("iac.provider %q: register provider alias: %w", m.name, err)
	}
	return nil
}

func (m *IaCProviderModule) providerConfig() map[string]any {
	out := make(map[string]any, len(m.config))
	for k, v := range m.config {
		switch k {
		case "plugin", "service", "_config_dir":
			continue
		default:
			out[k] = v
		}
	}
	return out
}

func iacProviderString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, _ := cfg[key].(string)
	return v
}

func pluginNameFromProvider(provider string) string {
	switch provider {
	case "aws", "azure", "digitalocean", "gcp":
		return "workflow-plugin-" + provider
	default:
		return ""
	}
}

// ProvidesServices declares the app-local IaCProvider service alias.
func (m *IaCProviderModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{{
		Name:        m.name,
		Description: "IaC provider alias: " + m.name,
		Instance:    m.provider,
	}}
}

// RequiresServices declares the external plugin service dependency.
func (m *IaCProviderModule) RequiresServices() []modular.ServiceDependency {
	pluginName := iacProviderString(m.config, "plugin")
	if pluginName == "" {
		pluginName = iacProviderString(m.config, "service")
	}
	if pluginName == "" {
		pluginName = pluginNameFromProvider(iacProviderString(m.config, "provider"))
	}
	if pluginName == "" {
		return nil
	}
	return []modular.ServiceDependency{{Name: pluginName, Required: true}}
}

func (m *IaCProviderModule) Start(context.Context) error { return nil }

func (m *IaCProviderModule) Stop(context.Context) error { return nil }

// Provider returns the resolved provider after Init.
func (m *IaCProviderModule) Provider() interfaces.IaCProvider { return m.provider }
