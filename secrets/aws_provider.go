package secrets

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// AWSConfig holds configuration for AWS Secrets Manager.
type AWSConfig struct {
	Region          string `json:"region" yaml:"region"`
	AccessKeyID     string `json:"accessKeyId,omitempty" yaml:"accessKeyId,omitempty"`
	SecretAccessKey string `json:"secretAccessKey,omitempty" yaml:"secretAccessKey,omitempty"`
}

// AWSSecretsManagerProvider reads secrets from AWS Secrets Manager.
type AWSSecretsManagerProvider struct {
	config AWSConfig
	client *secretsmanager.Client
}

// NewAWSSecretsManagerProvider creates a new AWS Secrets Manager provider.
// If AccessKeyID/SecretAccessKey are empty, it falls back to the default AWS credential chain.
func NewAWSSecretsManagerProvider(cfg AWSConfig) (*AWSSecretsManagerProvider, error) {
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderInit, err)
	}

	client := secretsmanager.NewFromConfig(awsCfg)
	return &AWSSecretsManagerProvider{config: cfg, client: client}, nil
}

func (p *AWSSecretsManagerProvider) Name() string { return "aws-sm" }

func (p *AWSSecretsManagerProvider) Get(ctx context.Context, key string) (string, error) {
	if key == "" {
		return "", ErrInvalidKey
	}

	out, err := p.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(key),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotFound, err)
	}

	if out.SecretString != nil {
		return *out.SecretString, nil
	}

	return "", fmt.Errorf("%w: secret %q has no string value", ErrNotFound, key)
}

func (p *AWSSecretsManagerProvider) Set(_ context.Context, _ string, _ string) error {
	return fmt.Errorf("%w: aws secrets manager provider is read-only", ErrUnsupported)
}

func (p *AWSSecretsManagerProvider) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("%w: aws secrets manager provider is read-only", ErrUnsupported)
}

func (p *AWSSecretsManagerProvider) List(_ context.Context) ([]string, error) {
	return nil, fmt.Errorf("%w: aws secrets manager list not implemented", ErrUnsupported)
}

// Config returns the provider's AWS configuration.
func (p *AWSSecretsManagerProvider) Config() AWSConfig { return p.config }

// SetClient allows overriding the Secrets Manager client (for testing).
func (p *AWSSecretsManagerProvider) SetClient(client *secretsmanager.Client) {
	p.client = client
}
