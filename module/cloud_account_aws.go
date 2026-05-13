package module

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AWSConfigProvider extends CloudCredentialProvider with AWS SDK config loading.
// Platform modules that need to call AWS APIs type-assert their CloudCredentialProvider
// to this interface to obtain a properly configured aws.Config.
type AWSConfigProvider interface {
	CloudCredentialProvider
	// AWSConfig returns a configured aws.Config for the current credential set.
	AWSConfig(ctx context.Context) (aws.Config, error)
}

// AWSConfig builds an aws.Config from the cloud.account configuration.
// Supports credential types: static/access_key, role_arn, env, profile, default.
// This satisfies the AWSConfigProvider interface.
func (m *CloudAccount) AWSConfig(ctx context.Context) (aws.Config, error) {
	region := m.region

	credsMap, _ := m.config["credentials"].(map[string]any)
	credType := "default"
	if credsMap != nil {
		if t, ok := credsMap["type"].(string); ok && t != "" {
			credType = t
		}
	}

	switch credType {
	case "static", "access_key":
		accessKey, _ := credsMap["accessKey"].(string)
		secretKey, _ := credsMap["secretKey"].(string)
		sessionToken, _ := credsMap["sessionToken"].(string)
		return config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
			),
		)

	case "role_arn":
		roleARN, _ := credsMap["roleArn"].(string)
		if roleARN == "" {
			return aws.Config{}, fmt.Errorf("cloud.account %q: role_arn credential requires 'roleArn'", m.name)
		}
		baseCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return aws.Config{}, fmt.Errorf("cloud.account %q: loading base config for role_arn: %w", m.name, err)
		}
		stsClient := sts.NewFromConfig(baseCfg)
		provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN)
		return config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(aws.NewCredentialsCache(provider)),
		)

	case "env", "default":
		return config.LoadDefaultConfig(ctx, config.WithRegion(region))

	case "profile":
		profile := ""
		if credsMap != nil {
			profile, _ = credsMap["profile"].(string)
		}
		if profile == "" {
			profile = "default"
		}
		return config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithSharedConfigProfile(profile),
		)

	default:
		return aws.Config{}, fmt.Errorf("cloud.account %q: AWSConfig unsupported credential type %q", m.name, credType)
	}
}

// ValidateCredentials calls sts:GetCallerIdentity to verify the AWS credentials work.
func (m *CloudAccount) ValidateCredentials(ctx context.Context) error {
	cfg, err := m.AWSConfig(ctx)
	if err != nil {
		return fmt.Errorf("cloud.account %q: AWSConfig failed: %w", m.name, err)
	}
	stsClient := sts.NewFromConfig(cfg)
	_, err = stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("cloud.account %q: GetCallerIdentity failed: %w", m.name, err)
	}
	return nil
}
