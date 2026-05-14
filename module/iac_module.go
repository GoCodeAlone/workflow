package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// IaCModule registers an IaCStateStore in the service registry.
// Supported in-core backends: "memory" (default), "filesystem", "spaces"
// (DigitalOcean Spaces / S3-compatible), "gcs", and "postgres" — plus any
// backend provided by a loaded plugin (e.g. "azure_blob" via
// workflow-plugin-azure).
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
	case "spaces":
		region, _ := m.config["region"].(string)
		bucket, _ := m.config["bucket"].(string)
		prefix, _ := m.config["prefix"].(string)
		accessKey, _ := m.config["accessKey"].(string)
		secretKey, _ := m.config["secretKey"].(string)
		endpoint, _ := m.config["endpoint"].(string)
		if bucket == "" {
			return fmt.Errorf("iac.state %q: spaces backend requires 'bucket' config", m.name)
		}
		store, err := NewSpacesIaCStateStore(region, bucket, prefix, accessKey, secretKey, endpoint)
		if err != nil {
			return fmt.Errorf("iac.state %q: spaces backend: %w", m.name, err)
		}
		m.store = store
	case "gcs":
		bucket, _ := m.config["bucket"].(string)
		prefix, _ := m.config["prefix"].(string)
		if bucket == "" {
			return fmt.Errorf("iac.state %q: gcs backend requires 'bucket' config", m.name)
		}
		store, err := NewGCSIaCStateStore(context.Background(), bucket, prefix)
		if err != nil {
			return fmt.Errorf("iac.state %q: gcs backend: %w", m.name, err)
		}
		m.store = store
	case "postgres":
		dsn, _ := m.config["dsn"].(string)
		if dsn == "" {
			return fmt.Errorf("iac.state %q: postgres backend requires 'dsn' config", m.name)
		}
		store, err := NewPostgresIaCStateStore(context.Background(), dsn)
		if err != nil {
			return fmt.Errorf("iac.state %q: postgres backend: %w", m.name, err)
		}
		m.store = store
	default:
		// Not a core in-process backend — consult the plugin-backend registry.
		// The engine populates iacStateBackendRegistryInstance at plugin-load
		// time; a resolved backend is served over gRPC via grpcIaCStateStore.
		if client, ok := iacStateBackendRegistryInstance.resolve(m.backend); ok {
			m.store = newGRPCIaCStateStore(client)
			break
		}
		return fmt.Errorf("iac.state %q: unsupported backend %q (use 'memory', 'filesystem', 'spaces', 'gcs', 'azure_blob', or 'postgres', or load the plugin that provides it)", m.name, m.backend)
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
