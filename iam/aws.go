package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/store"
)

// AWSConfig holds configuration for the AWS IAM provider.
type AWSConfig struct {
	AccountID string `json:"account_id"`
	Region    string `json:"region"`
}

// AWSIAMProvider validates AWS IAM ARNs and maps them to roles.
// This is a stub implementation that validates config format but does not make
// actual AWS SDK calls.
type AWSIAMProvider struct{}

func (p *AWSIAMProvider) Type() store.IAMProviderType {
	return store.IAMProviderAWS
}

func (p *AWSIAMProvider) ValidateConfig(config json.RawMessage) error {
	var c AWSConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid aws config: %w", err)
	}
	if c.AccountID == "" {
		return fmt.Errorf("account_id is required")
	}
	return nil
}

func (p *AWSIAMProvider) ResolveIdentities(_ context.Context, config json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error) {
	arn, ok := credentials["arn"]
	if !ok || arn == "" {
		return nil, fmt.Errorf("arn credential required")
	}

	// Validate ARN format: arn:aws:iam::ACCOUNT:role/ROLENAME
	if !strings.HasPrefix(arn, "arn:aws:") {
		return nil, fmt.Errorf("invalid AWS ARN format")
	}

	return []ExternalIdentity{
		{
			Provider:   string(store.IAMProviderAWS),
			Identifier: arn,
			Attributes: map[string]string{"arn": arn},
		},
	}, nil
}

func (p *AWSIAMProvider) TestConnection(_ context.Context, config json.RawMessage) error {
	return p.ValidateConfig(config)
}
