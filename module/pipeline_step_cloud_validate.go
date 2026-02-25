package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// CloudValidateStep validates that a cloud.account's credentials are resolvable
// and non-empty. It looks up a CloudCredentialProvider from the service registry
// by account name, calls GetCredentials, and returns a summary JSON result.
type CloudValidateStep struct {
	name    string
	account string // service name of the CloudAccount module
	app     modular.Application
}

// NewCloudValidateStepFactory returns a StepFactory for step.cloud_validate.
func NewCloudValidateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		account, _ := config["account"].(string)
		if account == "" {
			return nil, fmt.Errorf("cloud_validate step %q: 'account' is required", name)
		}
		return &CloudValidateStep{
			name:    name,
			account: account,
			app:     app,
		}, nil
	}
}

// Name returns the step name.
func (s *CloudValidateStep) Name() string { return s.name }

// Execute validates the configured cloud account's credentials.
func (s *CloudValidateStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := s.resolveProvider()
	if err != nil {
		return nil, fmt.Errorf("cloud_validate step %q: %w", s.name, err)
	}

	creds, err := provider.GetCredentials(ctx)
	if err != nil {
		return &StepResult{Output: map[string]any{
			"account":  s.account,
			"provider": provider.Provider(),
			"region":   provider.Region(),
			"valid":    false,
			"error":    err.Error(),
		}}, nil
	}

	valid := s.validateCreds(provider.Provider(), creds)

	output := map[string]any{
		"account":  s.account,
		"provider": provider.Provider(),
		"region":   provider.Region(),
		"valid":    valid,
	}

	// For AWS: note where STS GetCallerIdentity would be called
	// Production: use aws-sdk-go-v2/service/sts GetCallerIdentity to confirm
	// credentials are actually valid (not just non-empty).
	if provider.Provider() == "aws" {
		output["sts_check"] = "stub: call STS GetCallerIdentity for live validation"
	}

	return &StepResult{Output: output}, nil
}

// resolveProvider looks up the CloudCredentialProvider from the service registry.
func (s *CloudValidateStep) resolveProvider() (CloudCredentialProvider, error) {
	if s.app == nil {
		return nil, fmt.Errorf("no application context")
	}
	svc, ok := s.app.SvcRegistry()[s.account]
	if !ok {
		return nil, fmt.Errorf("account service %q not found in registry", s.account)
	}
	provider, ok := svc.(CloudCredentialProvider)
	if !ok {
		return nil, fmt.Errorf("service %q does not implement CloudCredentialProvider", s.account)
	}
	return provider, nil
}

// validateCreds checks that the essential credential fields are non-empty.
func (s *CloudValidateStep) validateCreds(provider string, creds *CloudCredentials) bool {
	if creds == nil {
		return false
	}
	switch provider {
	case "aws":
		return creds.AccessKey != "" && creds.SecretKey != ""
	case "gcp":
		return creds.ProjectID != "" || len(creds.ServiceAccountJSON) > 0
	case "kubernetes":
		return len(creds.Kubeconfig) > 0
	case "mock":
		return creds.AccessKey != "" && creds.SecretKey != ""
	default:
		return creds.Token != ""
	}
}
