package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	iamsdk "github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AWSConfig holds configuration for the AWS IAM provider.
type AWSConfig struct {
	AccountID       string `json:"account_id"`
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
}

// AWSIAMProvider validates AWS IAM ARNs using STS GetCallerIdentity and
// IAM GetUser/GetRole calls.
type AWSIAMProvider struct{}

func (p *AWSIAMProvider) Type() store.IAMProviderType {
	return store.IAMProviderAWS
}

func (p *AWSIAMProvider) ValidateConfig(cfgRaw json.RawMessage) error {
	var c AWSConfig
	if err := json.Unmarshal(cfgRaw, &c); err != nil {
		return fmt.Errorf("invalid aws config: %w", err)
	}
	if c.AccountID == "" {
		return fmt.Errorf("account_id is required")
	}
	return nil
}

// ResolveIdentities resolves an AWS ARN to an ExternalIdentity, using
// STS GetCallerIdentity and IAM GetUser/GetRole to enrich attributes.
// Falls back to ARN-only identity when credentials are unavailable.
func (p *AWSIAMProvider) ResolveIdentities(ctx context.Context, cfgRaw json.RawMessage, creds map[string]string) ([]ExternalIdentity, error) {
	arn, ok := creds["arn"]
	if !ok || arn == "" {
		return nil, fmt.Errorf("arn credential required")
	}

	if !strings.HasPrefix(arn, "arn:aws:") {
		return nil, fmt.Errorf("invalid AWS ARN format")
	}

	var awsCfg AWSConfig
	if err := json.Unmarshal(cfgRaw, &awsCfg); err != nil {
		return nil, fmt.Errorf("invalid aws config: %w", err)
	}

	attrs := map[string]string{"arn": arn}

	sdkCfg, err := buildAWSSDKConfig(ctx, awsCfg)
	if err != nil {
		return []ExternalIdentity{{
			Provider:   string(store.IAMProviderAWS),
			Identifier: arn,
			Attributes: attrs,
		}}, nil
	}

	// Verify caller identity via STS.
	stsClient := sts.NewFromConfig(sdkCfg)
	callerOut, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err == nil {
		if callerOut.Arn != nil {
			attrs["caller_arn"] = aws.ToString(callerOut.Arn)
		}
		if callerOut.UserId != nil {
			attrs["user_id"] = aws.ToString(callerOut.UserId)
		}
		if callerOut.Account != nil {
			attrs["account"] = aws.ToString(callerOut.Account)
		}
	}

	// Enrich with IAM user or role details when the ARN references one.
	iamClient := iamsdk.NewFromConfig(sdkCfg)
	arnParts := strings.Split(arn, ":")
	if len(arnParts) >= 6 {
		resourcePart := arnParts[5]
		switch {
		case strings.HasPrefix(resourcePart, "user/"):
			userName := strings.TrimPrefix(resourcePart, "user/")
			userOut, uErr := iamClient.GetUser(ctx, &iamsdk.GetUserInput{
				UserName: aws.String(userName),
			})
			if uErr == nil && userOut.User != nil {
				attrs["name"] = aws.ToString(userOut.User.UserName)
				attrs["type"] = "user"
				if userOut.User.Arn != nil {
					attrs["arn"] = aws.ToString(userOut.User.Arn)
				}
			}
		case strings.HasPrefix(resourcePart, "role/"):
			roleName := strings.TrimPrefix(resourcePart, "role/")
			roleOut, rErr := iamClient.GetRole(ctx, &iamsdk.GetRoleInput{
				RoleName: aws.String(roleName),
			})
			if rErr == nil && roleOut.Role != nil {
				attrs["name"] = aws.ToString(roleOut.Role.RoleName)
				attrs["type"] = "role"
				if roleOut.Role.Arn != nil {
					attrs["arn"] = aws.ToString(roleOut.Role.Arn)
				}
			}
		}
	}

	return []ExternalIdentity{{
		Provider:   string(store.IAMProviderAWS),
		Identifier: arn,
		Attributes: attrs,
	}}, nil
}

// TestConnection calls sts:GetCallerIdentity to verify connectivity and credentials.
func (p *AWSIAMProvider) TestConnection(ctx context.Context, cfgRaw json.RawMessage) error {
	if err := p.ValidateConfig(cfgRaw); err != nil {
		return err
	}

	var awsCfg AWSConfig
	if err := json.Unmarshal(cfgRaw, &awsCfg); err != nil {
		return fmt.Errorf("invalid aws config: %w", err)
	}

	sdkCfg, err := buildAWSSDKConfig(ctx, awsCfg)
	if err != nil {
		return fmt.Errorf("aws iam: building SDK config: %w", err)
	}

	stsClient := sts.NewFromConfig(sdkCfg)
	out, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("aws iam: GetCallerIdentity failed: %w", err)
	}

	if awsCfg.AccountID != "" && out.Account != nil && aws.ToString(out.Account) != awsCfg.AccountID {
		return fmt.Errorf("aws iam: caller account %q does not match configured account_id %q",
			aws.ToString(out.Account), awsCfg.AccountID)
	}

	return nil
}

// buildAWSSDKConfig builds an aws.Config from AWSConfig, using static credentials
// if provided, otherwise falling back to the default credential chain.
func buildAWSSDKConfig(ctx context.Context, c AWSConfig) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if c.Region != "" {
		opts = append(opts, awsconfig.WithRegion(c.Region))
	}
	if c.AccessKeyID != "" && c.SecretAccessKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken),
		))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}
