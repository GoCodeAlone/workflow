package module

import "github.com/CrisisTextLine/modular"

// ServiceModule wraps any Go object as a modular.Module, registering it
// in the service registry under the given name. This allows delegate-based
// dispatch: a QueryHandler or CommandHandler can name a delegate service,
// and that service (if it implements http.Handler) handles the actual HTTP
// dispatch.
type ServiceModule struct {
	name    string
	service any
}

// NewServiceModule creates a ServiceModule that registers svc under name.
func NewServiceModule(name string, svc any) *ServiceModule {
	return &ServiceModule{name: name, service: svc}
}

func (m *ServiceModule) Name() string { return m.name }

func (m *ServiceModule) Init(_ modular.Application) error { return nil }

func (m *ServiceModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Service delegate: " + m.name,
			Instance:    m.service,
		},
	}
}

func (m *ServiceModule) RequiresServices() []modular.ServiceDependency {
	return nil
}
