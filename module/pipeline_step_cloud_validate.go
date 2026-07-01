package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// CloudValidateStep validates that a cloud.account's credentials are resolvable
// and non-empty. It looks up a CloudCredentialProvider from the service registry
// by account name, calls GetCredentials, and returns a summary JSON result.
type CloudValidateStep struct {
	name        string
	account     string // service name of the CloudAccount module
	accountFrom string // pipeline context path resolving to a CloudAccount service name
	app         modular.Application
}

// NewCloudValidateStepFactory returns a StepFactory for step.cloud_validate.
func NewCloudValidateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		account, _ := config["account"].(string)
		accountFrom, _ := config["account_from"].(string)
		if account == "" && accountFrom == "" {
			return nil, fmt.Errorf("cloud_validate step %q: either 'account' or 'account_from' is required", name)
		}
		if account != "" && accountFrom != "" {
			return nil, fmt.Errorf("cloud_validate step %q: only one of 'account' or 'account_from' may be set", name)
		}
		return &CloudValidateStep{
			name:        name,
			account:     account,
			accountFrom: accountFrom,
			app:         app,
		}, nil
	}
}

// Name returns the step name.
func (s *CloudValidateStep) Name() string { return s.name }

// Execute validates the configured cloud account's credentials.
func (s *CloudValidateStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	account, err := s.resolveAccount(pc)
	if err != nil {
		return nil, fmt.Errorf("cloud_validate step %q: %w", s.name, err)
	}

	provider, err := s.resolveProvider(account)
	if err != nil {
		return nil, fmt.Errorf("cloud_validate step %q: %w", s.name, err)
	}

	creds, credErr := provider.GetCredentials(ctx)
	valid := credErr == nil
	var credErrMsg string
	if !valid {
		credErrMsg = credErr.Error()
	} else {
		valid = s.validateCreds(provider.Provider(), creds)
	}

	output := map[string]any{
		"account":  account,
		"provider": provider.Provider(),
		"region":   provider.Region(),
		"valid":    valid,
	}
	if credErrMsg != "" {
		output["error"] = credErrMsg
	}

	if creds != nil {
		switch provider.Provider() {
		case "gcp":
			if creds.ProjectID != "" {
				output["project_id"] = creds.ProjectID
			}
		case "azure":
			if creds.TenantID != "" {
				output["tenant_id"] = creds.TenantID
			}
			if creds.SubscriptionID != "" {
				output["subscription_id"] = creds.SubscriptionID
			}
		}
	}

	return &StepResult{Output: output}, nil
}

func (s *CloudValidateStep) resolveAccount(pc *PipelineContext) (string, error) {
	if s.account != "" {
		return s.account, nil
	}
	raw := resolveBodyFrom(s.accountFrom, pc)
	account, ok := raw.(string)
	if !ok || account == "" {
		return "", fmt.Errorf("account_from %q resolved to %T, want non-empty string", s.accountFrom, raw)
	}
	return account, nil
}

// resolveProvider looks up the CloudCredentialProvider from the service registry.
func (s *CloudValidateStep) resolveProvider(account string) (CloudCredentialProvider, error) {
	if s.app == nil {
		return nil, fmt.Errorf("no application context")
	}
	svc, ok := s.app.SvcRegistry()[account]
	if !ok {
		return nil, fmt.Errorf("account service %q not found in registry", account)
	}
	provider, ok := svc.(CloudCredentialProvider)
	if !ok {
		return nil, fmt.Errorf("service %q does not implement CloudCredentialProvider", account)
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
		// static/env: AccessKey + SecretKey populated directly.
		if creds.AccessKey != "" && creds.SecretKey != "" {
			return true
		}
		// profile/role_arn (post-Task 13 rewrite): resolver records a
		// credential_source marker; the aws plugin performs the SDK-bearing
		// resolution at the point of use.
		if src, ok := creds.Extra["credential_source"]; ok && src != "" {
			return true
		}
		// role_arn marker also records RoleARN on creds — accept it as a
		// signal that the resolver ran successfully.
		if creds.RoleARN != "" {
			return true
		}
		return false
	case "gcp":
		return creds.ProjectID != "" || len(creds.ServiceAccountJSON) > 0
	case "azure":
		// client_credentials: requires tenant+client+secret
		// managed_identity/cli: valid if SubscriptionID or credential_source is set
		if creds.TenantID != "" && creds.ClientID != "" {
			return true
		}
		if creds.SubscriptionID != "" {
			return true
		}
		if src, ok := creds.Extra["credential_source"]; ok && src != "" {
			return true
		}
		return false
	case "kubernetes":
		return len(creds.Kubeconfig) > 0
	case "mock":
		return creds.AccessKey != "" && creds.SecretKey != ""
	default:
		return creds.Token != ""
	}
}
