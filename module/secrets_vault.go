package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretsVaultModule provides a HashiCorp Vault secret provider as a modular service.
// It supports two modes:
//   - "remote" (default): connects to an external Vault server
//   - "dev": manages a local Vault dev server subprocess
type SecretsVaultModule struct {
	name      string
	mode      string // "remote" or "dev"
	address   string
	token     string
	mountPath string
	namespace string
	provider  *secrets.VaultProvider
	devProv   *secrets.DevVaultProvider
	logger    modular.Logger
}

// NewSecretsVaultModule creates a new Vault secrets module.
func NewSecretsVaultModule(name string) *SecretsVaultModule {
	return &SecretsVaultModule{
		name:      name,
		mode:      "remote",
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

// SetMode sets the provider mode: "remote" or "dev".
func (m *SecretsVaultModule) SetMode(mode string) { m.mode = mode }

// Start initializes the Vault provider based on the configured mode.
func (m *SecretsVaultModule) Start(_ context.Context) error {
	switch m.mode {
	case "dev":
		return m.startDev()
	default:
		return m.startRemote()
	}
}

func (m *SecretsVaultModule) startRemote() error {
	cfg := secrets.VaultConfig{
		Address:   m.address,
		Token:     m.token,
		MountPath: m.mountPath,
		Namespace: m.namespace,
	}

	p, err := secrets.NewVaultProvider(cfg)
	if err != nil {
		return fmt.Errorf("secrets.vault: %w", err)
	}
	m.provider = p
	m.logger.Info("Vault secrets provider started (remote)", "address", m.address, "mount", m.mountPath)
	return nil
}

func (m *SecretsVaultModule) startDev() error {
	token := m.token
	if token == "" {
		token = "dev-root-token" //nolint:gosec // intentional default for dev mode only
	}

	devCfg := secrets.DevVaultConfig{
		RootToken: token,
		MountPath: m.mountPath,
	}
	if m.address != "" {
		devCfg.ListenAddr = m.address
	}

	dp, err := secrets.NewDevVaultProvider(devCfg)
	if err != nil {
		return fmt.Errorf("secrets.vault: dev mode: %w", err)
	}
	m.devProv = dp
	m.provider = dp.VaultProvider
	m.logger.Info("Vault secrets provider started (dev mode)", "address", dp.Addr(), "mount", m.mountPath)
	return nil
}

// Stop cleans up the Vault provider. For dev mode, this stops the subprocess.
func (m *SecretsVaultModule) Stop(_ context.Context) error {
	if m.devProv != nil {
		if err := m.devProv.Close(); err != nil {
			m.logger.Warn("Failed to stop vault dev server", "error", err)
		}
	}
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
