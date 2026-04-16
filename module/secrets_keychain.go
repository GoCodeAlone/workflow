package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretsKeychainModule provides an OS-keychain-backed secret provider as a modular service.
// It uses the macOS Keychain, Linux Secret Service, or Windows Credential Manager
// via github.com/zalando/go-keyring.
type SecretsKeychainModule struct {
	name     string
	service  string
	provider *secrets.KeychainProvider
	logger   modular.Logger
}

// NewSecretsKeychainModule creates a new OS keychain secrets module.
func NewSecretsKeychainModule(name string) *SecretsKeychainModule {
	return &SecretsKeychainModule{
		name:   name,
		logger: &noopLogger{},
	}
}

func (m *SecretsKeychainModule) Name() string { return m.name }

func (m *SecretsKeychainModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *SecretsKeychainModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "OS Keychain Secrets Provider",
			Instance:    m,
		},
	}
}

func (m *SecretsKeychainModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetService sets the keychain service namespace.
func (m *SecretsKeychainModule) SetService(service string) { m.service = service }

// Start initializes the keychain provider.
func (m *SecretsKeychainModule) Start(_ context.Context) error {
	if m.service == "" {
		return fmt.Errorf("secrets.keychain: 'service' is required")
	}
	m.provider = secrets.NewKeychainProvider(m.service)
	m.logger.Info("Keychain secrets provider started", "service", m.service)
	return nil
}

// Stop is a no-op.
func (m *SecretsKeychainModule) Stop(_ context.Context) error {
	m.logger.Info("Keychain secrets provider stopped")
	return nil
}

// Provider returns the underlying secrets.Provider.
func (m *SecretsKeychainModule) Provider() secrets.Provider {
	return m.provider
}

// Get retrieves a secret from the OS keychain.
func (m *SecretsKeychainModule) Get(ctx context.Context, key string) (string, error) {
	if m.provider == nil {
		return "", fmt.Errorf("secrets.keychain: provider not initialized")
	}
	return m.provider.Get(ctx, key)
}
