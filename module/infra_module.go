package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// InfraModule bridges a YAML `infra.*` module declaration to an [interfaces.IaCProvider]'s
// [interfaces.ResourceDriver] for a single resource type.
//
// One factory function covers all 13 infra.* types — the type string is baked
// in at registration time via [NewInfraModuleFactory].
//
// Config keys:
//   - provider  — name of the module that registered an IaCProvider service (required)
//   - size      — xs/s/m/l/xl sizing tier (optional, defaults to "s")
//   - resources — map with cpu/memory/storage override hints (optional)
//   - Any additional keys are forwarded verbatim to the driver as resource config.
type InfraModule struct {
	name      string
	infraType string // e.g. "infra.database"
	config    map[string]any
	size      interfaces.Size
	hints     *interfaces.ResourceHints
	provider  interfaces.IaCProvider
	driver    interfaces.ResourceDriver
}

// NewInfraModule creates an InfraModule for the given infraType (e.g. "infra.database").
func NewInfraModule(name, infraType string, cfg map[string]any) *InfraModule {
	return &InfraModule{
		name:      name,
		infraType: infraType,
		config:    cfg,
	}
}

// NewInfraModuleFactory returns a ModuleFactory that always creates InfraModules
// of the given infraType. Register one per infra.* type in the plugin.
func NewInfraModuleFactory(infraType string) func(name string, cfg map[string]any) modular.Module {
	return func(name string, cfg map[string]any) modular.Module {
		return NewInfraModule(name, infraType, cfg)
	}
}

// Name returns the module name.
func (m *InfraModule) Name() string { return m.name }

// Init resolves the IaCProvider, obtains the ResourceDriver for this module's
// resource type, and registers the module as "<name>.driver" in the SvcRegistry.
func (m *InfraModule) Init(app modular.Application) error {
	// Parse size tier
	if s, ok := m.config["size"].(string); ok && s != "" {
		m.size = interfaces.Size(s)
	}
	if m.size == "" {
		m.size = interfaces.SizeS
	}

	// Parse optional resource hints
	if r, ok := m.config["resources"].(map[string]any); ok {
		m.hints = &interfaces.ResourceHints{
			CPU:     infraStrVal(r, "cpu"),
			Memory:  infraStrVal(r, "memory"),
			Storage: infraStrVal(r, "storage"),
		}
	}

	// Resolve IaCProvider from the service registry by the configured name
	providerName, _ := m.config["provider"].(string)
	if providerName == "" {
		return fmt.Errorf("infra module %q (%s): 'provider' config is required", m.name, m.infraType)
	}

	var providerSvc any
	if err := app.GetService(providerName, &providerSvc); err != nil {
		return fmt.Errorf("infra module %q (%s): provider %q not found: %w", m.name, m.infraType, providerName, err)
	}

	provider, ok := providerSvc.(interfaces.IaCProvider)
	if !ok {
		return fmt.Errorf("infra module %q (%s): service %q does not implement IaCProvider", m.name, m.infraType, providerName)
	}
	m.provider = provider

	// Obtain the driver for this resource type from the provider
	driver, err := m.provider.ResourceDriver(m.infraType)
	if err != nil {
		return fmt.Errorf("infra module %q: provider %q has no driver for %q: %w", m.name, providerName, m.infraType, err)
	}
	m.driver = driver

	// Register as "<name>.driver" so IaC pipeline steps can look it up.
	if err := app.RegisterService(m.name+".driver", m); err != nil {
		return fmt.Errorf("infra module %q: register driver service: %w", m.name, err)
	}

	// Bridge to deployment steps: register a DeployDriver at "<name>" so that
	// step.deploy_rolling (and friends) can resolve this module by its plain name.
	//
	// Priority (highest first):
	//   1. Provider implements BlueGreenDriverProvider → also exposes BlueGreenDriver.
	//   2. Provider implements CanaryDriverProvider    → also exposes CanaryDriver.
	//   3. Provider implements DeployDriverProvider    → uses a provider-supplied driver.
	//   4. Fallback: wrap the ResourceDriver in an infraDeployAdapter.
	//
	// Registrations are best-effort; if a service already exists at "<name>" we
	// skip silently rather than fail the whole module.
	m.registerDeployDrivers(app)
	return nil
}

// registerDeployDrivers registers deploy-capable drivers at "<name>" (and
// "<name>.bluegreen" / "<name>.canary" if available).
func (m *InfraModule) registerDeployDrivers(app modular.Application) {
	// BlueGreenDriverProvider → register at "<name>" as BlueGreenDriver.
	if bgp, ok := m.provider.(BlueGreenDriverProvider); ok {
		if bgd := bgp.ProvideBlueGreenDriver(m.name); bgd != nil {
			_ = app.RegisterService(m.name, bgd)
			return
		}
	}

	// CanaryDriverProvider → register at "<name>" as CanaryDriver.
	if cp, ok := m.provider.(CanaryDriverProvider); ok {
		if cd := cp.ProvideCanaryDriver(m.name); cd != nil {
			_ = app.RegisterService(m.name, cd)
			return
		}
	}

	// DeployDriverProvider → register a provider-supplied DeployDriver.
	if dp, ok := m.provider.(DeployDriverProvider); ok {
		if dd := dp.ProvideDeployDriver(m.name); dd != nil {
			_ = app.RegisterService(m.name, dd)
			return
		}
	}

	// Fallback: wrap the ResourceDriver in a generic adapter.
	_ = app.RegisterService(m.name, &infraDeployAdapter{im: m})
}

// ProvidesServices declares the driver service registrations.
func (m *InfraModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name + ".driver",
			Description: "InfraModule driver: " + m.infraType + " (" + m.name + ")",
			Instance:    m,
		},
		{
			Name:        m.name,
			Description: "InfraModule deploy driver: " + m.infraType + " (" + m.name + ")",
			Instance:    m,
		},
	}
}

// RequiresServices declares a dependency on the configured IaCProvider service.
func (m *InfraModule) RequiresServices() []modular.ServiceDependency {
	providerName, _ := m.config["provider"].(string)
	if providerName == "" {
		return nil
	}
	return []modular.ServiceDependency{
		{Name: providerName, Required: true},
	}
}

// Driver returns the underlying ResourceDriver (nil before Init).
func (m *InfraModule) Driver() interfaces.ResourceDriver { return m.driver }

// Provider returns the underlying IaCProvider (nil before Init).
func (m *InfraModule) Provider() interfaces.IaCProvider { return m.provider }

// InfraType returns the abstract resource type string (e.g. "infra.database").
func (m *InfraModule) InfraType() string { return m.infraType }

// Size returns the resolved size tier.
func (m *InfraModule) Size() interfaces.Size { return m.size }

// Hints returns the resource hints, or nil if none were configured.
func (m *InfraModule) Hints() *interfaces.ResourceHints { return m.hints }

// ResourceConfig returns the module config with standard keys (provider, size,
// resources) stripped, leaving only the resource-specific fields.
func (m *InfraModule) ResourceConfig() map[string]any {
	out := make(map[string]any, len(m.config))
	for k, v := range m.config {
		switch k {
		case "provider", "size", "resources":
			// skip standard fields
		default:
			out[k] = v
		}
	}
	return out
}

// Create provisions a new resource by delegating to the underlying driver.
func (m *InfraModule) Create(ctx context.Context) (*interfaces.ResourceOutput, error) {
	return m.driver.Create(ctx, m.buildSpec())
}

// Read retrieves the current state of the resource from the provider.
func (m *InfraModule) Read(ctx context.Context) (*interfaces.ResourceOutput, error) {
	return m.driver.Read(ctx, interfaces.ResourceRef{Name: m.name, Type: m.infraType})
}

// Update reconciles the resource to match the current config.
func (m *InfraModule) Update(ctx context.Context) (*interfaces.ResourceOutput, error) {
	return m.driver.Update(ctx, interfaces.ResourceRef{Name: m.name, Type: m.infraType}, m.buildSpec())
}

// Delete removes the resource via the underlying driver.
func (m *InfraModule) Delete(ctx context.Context) error {
	return m.driver.Delete(ctx, interfaces.ResourceRef{Name: m.name, Type: m.infraType})
}

// buildSpec constructs a ResourceSpec from this module's current config.
func (m *InfraModule) buildSpec() interfaces.ResourceSpec {
	return interfaces.ResourceSpec{
		Name:   m.name,
		Type:   m.infraType,
		Config: m.ResourceConfig(),
		Size:   m.size,
		Hints:  m.hints,
	}
}

// infraStrVal extracts a string value from a map by key.
func infraStrVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
