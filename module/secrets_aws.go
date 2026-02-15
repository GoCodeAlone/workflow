package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretsAWSModule provides an AWS Secrets Manager secret provider as a modular service.
type SecretsAWSModule struct {
	name            string
	region          string
	accessKeyID     string
	secretAccessKey string
	provider        *secrets.AWSSecretsManagerProvider
	logger          modular.Logger
}

// NewSecretsAWSModule creates a new AWS Secrets Manager module.
func NewSecretsAWSModule(name string) *SecretsAWSModule {
	return &SecretsAWSModule{
		name:   name,
		region: "us-east-1",
		logger: &noopLogger{},
	}
}

func (m *SecretsAWSModule) Name() string { return m.name }

func (m *SecretsAWSModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *SecretsAWSModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "AWS Secrets Manager Provider",
			Instance:    m,
		},
	}
}

func (m *SecretsAWSModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetRegion sets the AWS region.
func (m *SecretsAWSModule) SetRegion(region string) { m.region = region }

// SetAccessKeyID sets the AWS access key ID.
func (m *SecretsAWSModule) SetAccessKeyID(id string) { m.accessKeyID = id }

// SetSecretAccessKey sets the AWS secret access key.
func (m *SecretsAWSModule) SetSecretAccessKey(key string) { m.secretAccessKey = key }

// Start initializes the AWS Secrets Manager provider.
func (m *SecretsAWSModule) Start(_ context.Context) error {
	cfg := secrets.AWSConfig{
		Region:          m.region,
		AccessKeyID:     m.accessKeyID,
		SecretAccessKey: m.secretAccessKey,
	}

	p, err := secrets.NewAWSSecretsManagerProvider(cfg)
	if err != nil {
		return fmt.Errorf("secrets.aws: %w", err)
	}
	m.provider = p
	m.logger.Info("AWS Secrets Manager provider started", "region", m.region)
	return nil
}

// Stop is a no-op.
func (m *SecretsAWSModule) Stop(_ context.Context) error {
	m.logger.Info("AWS Secrets Manager provider stopped")
	return nil
}

// Provider returns the underlying secrets.Provider.
func (m *SecretsAWSModule) Provider() secrets.Provider {
	return m.provider
}

// Get retrieves a secret from AWS Secrets Manager.
func (m *SecretsAWSModule) Get(ctx context.Context, key string) (string, error) {
	if m.provider == nil {
		return "", fmt.Errorf("secrets.aws: provider not initialized")
	}
	return m.provider.Get(ctx, key)
}
