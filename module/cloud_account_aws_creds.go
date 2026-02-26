package module

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func init() {
	RegisterCredentialResolver(&awsStaticResolver{})
	RegisterCredentialResolver(&awsEnvResolver{})
	RegisterCredentialResolver(&awsProfileResolver{})
	RegisterCredentialResolver(&awsRoleARNResolver{})
}

// awsStaticResolver resolves AWS credentials from static config fields.
type awsStaticResolver struct{}

func (r *awsStaticResolver) Provider() string      { return "aws" }
func (r *awsStaticResolver) CredentialType() string { return "static" }

func (r *awsStaticResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap != nil {
		m.creds.AccessKey, _ = credsMap["accessKey"].(string)
		m.creds.SecretKey, _ = credsMap["secretKey"].(string)
		m.creds.SessionToken, _ = credsMap["sessionToken"].(string)
		m.creds.RoleARN, _ = credsMap["roleArn"].(string)
	}
	return nil
}

// awsEnvResolver resolves AWS credentials from environment variables.
type awsEnvResolver struct{}

func (r *awsEnvResolver) Provider() string      { return "aws" }
func (r *awsEnvResolver) CredentialType() string { return "env" }

func (r *awsEnvResolver) Resolve(m *CloudAccount) error {
	m.creds.AccessKey = os.Getenv("AWS_ACCESS_KEY_ID")
	if m.creds.AccessKey == "" {
		m.creds.AccessKey = os.Getenv("AWS_ACCESS_KEY")
	}
	m.creds.SecretKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	if m.creds.SecretKey == "" {
		m.creds.SecretKey = os.Getenv("AWS_SECRET_KEY")
	}
	m.creds.SessionToken = os.Getenv("AWS_SESSION_TOKEN")
	m.creds.RoleARN = os.Getenv("AWS_ROLE_ARN")
	return nil
}

// awsProfileResolver resolves AWS credentials from a named shared-config profile
// using aws-sdk-go-v2/config.LoadDefaultConfig with WithSharedConfigProfile.
type awsProfileResolver struct{}

func (r *awsProfileResolver) Provider() string      { return "aws" }
func (r *awsProfileResolver) CredentialType() string { return "profile" }

func (r *awsProfileResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	profile := ""
	if credsMap != nil {
		profile, _ = credsMap["profile"].(string)
	}
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}

	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["profile"] = profile

	// Load credentials from the named profile using the AWS SDK.
	// A missing local profile file is normal in CI/prod — don't hard-fail.
	ctx := context.Background()
	cfg, loadErr := config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile))
	if loadErr != nil {
		return nil //nolint:nilerr // missing profile is normal in CI
	}
	creds, credErr := cfg.Credentials.Retrieve(ctx)
	if credErr != nil {
		return nil //nolint:nilerr // credential retrieval failure is non-fatal
	}
	m.creds.AccessKey = creds.AccessKeyID
	m.creds.SecretKey = creds.SecretAccessKey
	m.creds.SessionToken = creds.SessionToken
	return nil
}

// awsRoleARNResolver resolves AWS credentials via STS AssumeRole.
// It loads base credentials (from the environment or inline config), then calls
// sts:AssumeRole to obtain temporary credentials for the target role.
type awsRoleARNResolver struct{}

func (r *awsRoleARNResolver) Provider() string      { return "aws" }
func (r *awsRoleARNResolver) CredentialType() string { return "role_arn" }

func (r *awsRoleARNResolver) Resolve(m *CloudAccount) error {
	credsMap, _ := m.config["credentials"].(map[string]any)
	if credsMap == nil {
		return nil
	}

	roleARN, _ := credsMap["roleArn"].(string)
	externalID, _ := credsMap["externalId"].(string)

	// Always record the role ARN so AWSConfig() can use stscreds.AssumeRoleProvider.
	m.creds.RoleARN = roleARN
	if m.creds.Extra == nil {
		m.creds.Extra = map[string]string{}
	}
	m.creds.Extra["external_id"] = externalID

	if roleARN == "" {
		return fmt.Errorf("awsRoleARNResolver: roleArn is required")
	}

	sessionName, _ := credsMap["sessionName"].(string)
	if sessionName == "" {
		sessionName = "workflow-session"
	}

	// Build base credentials. Inline accessKey/secretKey take priority over the
	// default credential chain.
	ctx := context.Background()
	var baseCfgOpts []func(*config.LoadOptions) error
	if region := m.region; region != "" {
		baseCfgOpts = append(baseCfgOpts, config.WithRegion(region))
	}
	accessKey, _ := credsMap["accessKey"].(string)
	secretKey, _ := credsMap["secretKey"].(string)
	if accessKey != "" && secretKey != "" {
		sessionToken, _ := credsMap["sessionToken"].(string)
		baseCfgOpts = append(baseCfgOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken),
		))
	}

	baseCfg, loadErr := config.LoadDefaultConfig(ctx, baseCfgOpts...)
	if loadErr != nil {
		// AWSConfig() will retry via stscreds.AssumeRoleProvider at call time.
		return nil //nolint:nilerr // config load failure is non-fatal
	}

	stsClient := sts.NewFromConfig(baseCfg)
	input := &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleARN),
		RoleSessionName: aws.String(sessionName),
	}
	if externalID != "" {
		input.ExternalId = aws.String(externalID)
	}

	out, assumeErr := stsClient.AssumeRole(ctx, input)
	if assumeErr != nil {
		// AssumeRole may fail at config-load time without real credentials;
		// AWSConfig() handles deferred token refresh via stscreds.
		return nil //nolint:nilerr // AssumeRole failure handled by deferred refresh
	}

	if out.Credentials != nil {
		m.creds.AccessKey = aws.ToString(out.Credentials.AccessKeyId)
		m.creds.SecretKey = aws.ToString(out.Credentials.SecretAccessKey)
		m.creds.SessionToken = aws.ToString(out.Credentials.SessionToken)
	}
	return nil
}
