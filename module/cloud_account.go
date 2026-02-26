package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
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
	creds := &CloudCredentials{
		Provider: m.provider,
		Region:   m.region,
	}

	// Read top-level provider-specific config fields.
	if pid, ok := m.config["project_id"].(string); ok {
		creds.ProjectID = pid
	}
	if sid, ok := m.config["subscription_id"].(string); ok {
		creds.SubscriptionID = sid
	}

	if m.provider == "mock" {
		return m.resolveMockCredentials(creds)
	}

	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		// No credentials configured — return empty (valid for some providers)
		return creds, nil
	}

	m.credType, _ = credsMap["type"].(string)
	if m.credType == "" {
		m.credType = "static"
	}

	// Store creds on m so resolvers can write into it directly.
	m.creds = creds

	providerResolvers, ok := credentialResolvers[m.provider]
	if !ok {
		return nil, fmt.Errorf("unknown cloud provider: %s", m.provider)
	}
	resolver, ok := providerResolvers[m.credType]
	if !ok {
		return nil, fmt.Errorf("unsupported credential type %q for provider %q", m.credType, m.provider)
	}
	if err := resolver.Resolve(m); err != nil {
		return nil, err
	}
	return m.creds, nil
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
