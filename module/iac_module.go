package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// IaCModule registers an IaCStateStore in the service registry.
// Supported backends: "memory" (default) and "filesystem".
//
// Config example:
//
//	modules:
//	  - name: iac-state
//	    type: iac.state
//	    config:
//	      backend: filesystem
//	      directory: /var/lib/workflow/iac-state
type IaCModule struct {
	name    string
	backend string
	config  map[string]any
	store   IaCStateStore
}

// NewIaCModule creates a new IaC state module.
func NewIaCModule(name string, cfg map[string]any) *IaCModule {
	return &IaCModule{name: name, config: cfg}
}

// Name returns the module name.
func (m *IaCModule) Name() string { return m.name }

// Init constructs the state store backend and registers it as a service.
func (m *IaCModule) Init(app modular.Application) error {
	m.backend, _ = m.config["backend"].(string)
	if m.backend == "" {
		m.backend = "memory"
	}

	switch m.backend {
	case "memory":
		m.store = NewMemoryIaCStateStore()
	case "filesystem":
		dir, _ := m.config["directory"].(string)
		if dir == "" {
			dir = "/var/lib/workflow/iac-state"
		}
		m.store = NewFSIaCStateStore(dir)
	default:
		return fmt.Errorf("iac.state %q: unsupported backend %q (use 'memory' or 'filesystem')", m.name, m.backend)
	}

	return app.RegisterService(m.name, m.store)
}

// ProvidesServices declares the IaCStateStore service.
func (m *IaCModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "IaC state store (" + m.backend + "): " + m.name,
			Instance:    m.store,
		},
	}
}

// RequiresServices returns nil — iac.state has no service dependencies.
func (m *IaCModule) RequiresServices() []modular.ServiceDependency { return nil }

// Start is a no-op for the memory backend; the filesystem backend creates the directory.
func (m *IaCModule) Start(_ context.Context) error {
	if fs, ok := m.store.(*FSIaCStateStore); ok {
		if err := fs.ensureDir(); err != nil {
			return fmt.Errorf("iac.state %q: Start: %w", m.name, err)
		}
	}
	return nil
}

// Stop is a no-op.
func (m *IaCModule) Stop(_ context.Context) error { return nil }
