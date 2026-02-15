package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretsVaultModule provides a HashiCorp Vault secret provider as a modular service.
type SecretsVaultModule struct {
	name      string
	address   string
	token     string
	mountPath string
	namespace string
	provider  *secrets.VaultProvider
	logger    modular.Logger
}

// NewSecretsVaultModule creates a new Vault secrets module.
func NewSecretsVaultModule(name string) *SecretsVaultModule {
	return &SecretsVaultModule{
		name:      name,
		mountPath: "secret",
		logger:    &noopLogger{},
	}
}

func (m *SecretsVaultModule) Name() string { return m.name }

func (m *SecretsVaultModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *SecretsVaultModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Vault Secret Provider",
			Instance:    m,
		},
	}
}

func (m *SecretsVaultModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetAddress sets the Vault server address.
func (m *SecretsVaultModule) SetAddress(addr string) { m.address = addr }

// SetToken sets the Vault authentication token.
func (m *SecretsVaultModule) SetToken(token string) { m.token = token }

// SetMountPath sets the KV v2 mount path.
func (m *SecretsVaultModule) SetMountPath(path string) { m.mountPath = path }

// SetNamespace sets the Vault namespace.
func (m *SecretsVaultModule) SetNamespace(ns string) { m.namespace = ns }

// Start initializes the Vault provider.
func (m *SecretsVaultModule) Start(_ context.Context) error {
	cfg := secrets.VaultConfig{
		Address:   m.address,
		Token:     m.token,
		MountPath: m.mountPath,
		Namespace: m.namespace,
	}

	p, err := secrets.NewVaultProviderHTTP(cfg)
	if err != nil {
		return fmt.Errorf("secrets.vault: %w", err)
	}
	m.provider = p
	m.logger.Info("Vault secrets provider started", "address", m.address, "mount", m.mountPath)
	return nil
}

// Stop is a no-op.
func (m *SecretsVaultModule) Stop(_ context.Context) error {
	m.logger.Info("Vault secrets provider stopped")
	return nil
}

// Provider returns the underlying secrets.Provider.
func (m *SecretsVaultModule) Provider() secrets.Provider {
	return m.provider
}

// Get retrieves a secret from Vault.
func (m *SecretsVaultModule) Get(ctx context.Context, key string) (string, error) {
	if m.provider == nil {
		return "", fmt.Errorf("secrets.vault: provider not initialized")
	}
	return m.provider.Get(ctx, key)
}
