package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// CloudCredentialProvider provides cloud credentials to other modules.
type CloudCredentialProvider interface {
	Provider() string
	Region() string
	GetCredentials(ctx context.Context) (*CloudCredentials, error)
}

// CloudCredentials holds resolved credentials for a cloud provider.
type CloudCredentials struct {
	Provider string
	Region   string
	// AWS
	AccessKey    string //nolint:gosec // G117: config struct field, not a hardcoded secret
	SecretKey    string
	SessionToken string //nolint:gosec // G117: config struct field, not a hardcoded secret
	RoleARN      string
	// GCP
	ProjectID          string
	ServiceAccountJSON []byte
	// Azure
	TenantID       string
	ClientID       string
	ClientSecret   string //nolint:gosec // G117: config struct field, not a hardcoded secret
	SubscriptionID string
	// Kubernetes
	Kubeconfig []byte
	Context    string
	// Generic / DigitalOcean
	Token string
	Extra map[string]string
}

// CloudAccount is a workflow module that stores cloud provider credentials
// and exposes them via the CloudCredentialProvider interface in the service registry.
type CloudAccount struct {
	name     string
	config   map[string]any
	provider string
	region   string
	credType string
	creds    *CloudCredentials
}

// NewCloudAccount creates a new CloudAccount module.
func NewCloudAccount(name string, cfg map[string]any) *CloudAccount {
	return &CloudAccount{name: name, config: cfg}
}

// Name returns the module name.
func (m *CloudAccount) Name() string { return m.name }

// Init resolves credentials and registers the module as a service.
func (m *CloudAccount) Init(app modular.Application) error {
	m.provider, _ = m.config["provider"].(string)
	if m.provider == "" {
		m.provider = "mock"
	}
	m.region, _ = m.config["region"].(string)

	var err error
	m.creds, err = m.resolveCredentials()
	if err != nil {
		return fmt.Errorf("cloud.account %q: failed to resolve credentials: %w", m.name, err)
	}

	return app.RegisterService(m.name, m)
}

// ProvidesServices declares the service this module provides.
func (m *CloudAccount) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Cloud account: " + m.name,
			Instance:    m,
		},
	}
}

// RequiresServices returns nil — cloud.account has no service dependencies.
func (m *CloudAccount) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Provider returns the cloud provider name (e.g. "aws", "gcp", "mock").
func (m *CloudAccount) Provider() string { return m.provider }

// Region returns the primary region.
func (m *CloudAccount) Region() string { return m.region }

// GetCredentials returns the resolved credentials.
func (m *CloudAccount) GetCredentials(_ context.Context) (*CloudCredentials, error) {
	if m.creds == nil {
		return nil, fmt.Errorf("cloud.account %q: credentials not initialized", m.name)
	}
	return m.creds, nil
}

// resolveCredentials resolves credentials based on provider and credential type config.
// It dispatches to registered CloudCredentialResolvers via the global registry.
func (m *CloudAccount) resolveCredentials() (*CloudCredentials, error) {
	return m.resolveCredentialsContext(context.Background(), false)
}

func (m *CloudAccount) resolveCredentialsContext(ctx context.Context, externalOnly bool) (*CloudCredentials, error) {
	creds := &CloudCredentials{
		Provider: m.provider,
		Region:   m.region,
	}

	if m.provider == "mock" {
		return m.resolveMockCredentials(creds)
	}

	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		// No credentials configured remains a valid built-in compatibility path.
		// Preserve the historical top-level provider fields without selecting or
		// invoking an external resolver.
		m.populateBuiltinProviderFields(creds)
		return creds, nil
	}

	m.credType, _ = credsMap["type"].(string)
	if m.credType == "" {
		m.credType = "static"
	}

	// Store creds on m so resolvers can write into it directly.
	m.creds = creds

	resolver, err := selectCredentialResolver(m.provider, m.credType, externalOnly)
	if err != nil {
		return nil, err
	}
	// Preserve the additive v0.86 built-in behavior while keeping the external
	// adapter provider-neutral. Provider-shaped top-level fields are never
	// interpreted on the external path; the plugin receives the opaque config.
	if _, external := resolver.(*externalCloudCredentialResolver); !external {
		m.populateBuiltinProviderFields(creds)
	}
	if contextual, ok := resolver.(contextCloudCredentialResolver); ok {
		err = contextual.ResolveContext(ctx, m)
	} else {
		err = resolver.Resolve(m)
	}
	if err != nil {
		return nil, err
	}
	return m.creds, nil
}

func (m *CloudAccount) populateBuiltinProviderFields(credentials *CloudCredentials) {
	if projectID, ok := m.config["project_id"].(string); ok {
		credentials.ProjectID = projectID
	}
	if subscriptionID, ok := m.config["subscription_id"].(string); ok {
		credentials.SubscriptionID = subscriptionID
	}
}

// ResolveExternalCloudCredentials resolves a provider/type exclusively through
// registered external CredentialResolver services. It provides a
// context-aware API for callers that must require plugin ownership instead of
// the additive v0.86 built-in fallback.
func ResolveExternalCloudCredentials(ctx context.Context, provider, credentialType string, cfg map[string]any) (*CloudCredentials, error) {
	account := &CloudAccount{
		config:   cfg,
		provider: provider,
		credType: credentialType,
		creds:    &CloudCredentials{Provider: provider},
	}
	if region, ok := cfg["region"].(string); ok {
		account.region = region
		account.creds.Region = region
	}
	resolver, err := selectCredentialResolver(provider, credentialType, true)
	if err != nil {
		return nil, err
	}
	if contextual, ok := resolver.(contextCloudCredentialResolver); ok {
		err = contextual.ResolveContext(ctx, account)
	} else {
		err = resolver.Resolve(account)
	}
	if err != nil {
		return nil, err
	}
	return account.creds, nil
}

func (m *CloudAccount) resolveMockCredentials(creds *CloudCredentials) (*CloudCredentials, error) {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		creds.AccessKey, _ = credsMap["accessKey"].(string)
		creds.SecretKey, _ = credsMap["secretKey"].(string)
		creds.Token, _ = credsMap["token"].(string)
	}
	if creds.AccessKey == "" {
		creds.AccessKey = "mock-access-key"
	}
	if creds.SecretKey == "" {
		creds.SecretKey = "mock-secret-key"
	}
	if creds.Region == "" {
		creds.Region = "us-mock-1"
	}
	return creds, nil
}
