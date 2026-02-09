package dynamic

import (
	"context"

	"github.com/CrisisTextLine/modular"
)

// ModuleAdapter wraps a DynamicComponent as a modular.Module so it can
// participate in the modular dependency system.
type ModuleAdapter struct {
	component  *DynamicComponent
	moduleName string
	provides   []string
	requires   []string
}

// NewModuleAdapter creates a new ModuleAdapter wrapping the given component.
func NewModuleAdapter(component *DynamicComponent) *ModuleAdapter {
	return &ModuleAdapter{
		component:  component,
		moduleName: component.Name(),
	}
}

// Name returns the component name.
func (a *ModuleAdapter) Name() string {
	return a.moduleName
}

// Init initializes the adapter by collecting required services and passing
// them to the underlying component, then registering provided services.
func (a *ModuleAdapter) Init(app modular.Application) error {
	services := make(map[string]interface{})
	for _, svcName := range a.requires {
		var svc interface{}
		if err := app.GetService(svcName, &svc); err == nil {
			services[svcName] = svc
		}
	}

	if err := a.component.Init(services); err != nil {
		return err
	}

	for _, svcName := range a.provides {
		if err := app.RegisterService(svcName, a.component); err != nil {
			return err
		}
	}

	return nil
}

// SetProvides sets the list of service names this adapter provides.
func (a *ModuleAdapter) SetProvides(services []string) {
	a.provides = services
}

// SetRequires sets the list of service names this adapter requires.
func (a *ModuleAdapter) SetRequires(services []string) {
	a.requires = services
}

// Execute delegates execution to the underlying component.
func (a *ModuleAdapter) Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return a.component.Execute(ctx, params)
}

// ProvidesServices returns the services provided by this adapter.
func (a *ModuleAdapter) ProvidesServices() []modular.ServiceProvider {
	providers := make([]modular.ServiceProvider, 0, len(a.provides))
	for _, svcName := range a.provides {
		providers = append(providers, modular.ServiceProvider{
			Name:        svcName,
			Description: "Dynamic component service: " + svcName,
			Instance:    a.component,
		})
	}
	return providers
}

// RequiresServices returns the services required by this adapter.
func (a *ModuleAdapter) RequiresServices() []modular.ServiceDependency {
	deps := make([]modular.ServiceDependency, 0, len(a.requires))
	for _, svcName := range a.requires {
		deps = append(deps, modular.ServiceDependency{
			Name:     svcName,
			Required: false,
		})
	}
	return deps
}
