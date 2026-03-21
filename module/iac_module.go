package module

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/GoCodeAlone/modular"
)

// newAzureSharedKeyCredential is a thin wrapper so iac_module.go doesn't import azblob directly
// in multiple places and can be easily replaced with other credential types.
func newAzureSharedKeyCredential(name, key string) (*azblob.SharedKeyCredential, error) {
	return azblob.NewSharedKeyCredential(name, key)
}

// IaCModule registers an IaCStateStore in the service registry.
// Supported backends: "memory" (default), "filesystem", and "spaces"
// (DigitalOcean Spaces / S3-compatible).
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
	case "azure_blob":
		container, _ := m.config["container"].(string)
		prefix, _ := m.config["prefix"].(string)
		accountURL, _ := m.config["account_url"].(string)
		accountName, _ := m.config["account_name"].(string)
		accountKey, _ := m.config["account_key"].(string)
		if container == "" {
			return fmt.Errorf("iac.state %q: azure_blob backend requires 'container' config", m.name)
		}
		if accountURL == "" || accountName == "" || accountKey == "" {
			return fmt.Errorf("iac.state %q: azure_blob backend requires 'account_url', 'account_name', and 'account_key' config", m.name)
		}
		cred, err := newAzureSharedKeyCredential(accountName, accountKey)
		if err != nil {
			return fmt.Errorf("iac.state %q: azure_blob backend: credential: %w", m.name, err)
		}
		store, err := NewAzureBlobIaCStateStore(accountURL, container, prefix, *cred)
		if err != nil {
			return fmt.Errorf("iac.state %q: azure_blob backend: %w", m.name, err)
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
		return fmt.Errorf("iac.state %q: unsupported backend %q (use 'memory', 'filesystem', 'spaces', 'gcs', 'azure_blob', or 'postgres')", m.name, m.backend)
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
