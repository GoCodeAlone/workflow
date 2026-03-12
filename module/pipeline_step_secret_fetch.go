package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// SecretFetchProvider is the minimal interface required by SecretFetchStep.
// Both SecretsAWSModule and SecretsVaultModule satisfy this interface.
type SecretFetchProvider interface {
	Get(ctx context.Context, key string) (string, error)
}

// SecretFetchStep fetches one or more secrets from a named secrets module
// (e.g. secrets.aws, secrets.vault) and exposes the resolved values as step
// outputs. Secret IDs / ARNs are Go template expressions evaluated against
// the live PipelineContext, enabling per-tenant dynamic resolution:
//
//	config:
//	  module: aws-secrets
//	  secrets:
//	    token_url: "{{.steps.lookup.row.token_url_arn}}"
//	    client_id: "{{.steps.lookup.row.client_id_arn}}"
type SecretFetchStep struct {
	name       string
	moduleName string            // service name registered by the secrets module
	secrets    map[string]string // output key → secret ID/ARN (may contain templates)
	app        modular.Application
	tmpl       *TemplateEngine
}

// NewSecretFetchStepFactory returns a StepFactory that creates SecretFetchStep instances.
func NewSecretFetchStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		moduleName, _ := config["module"].(string)
		if moduleName == "" {
			return nil, fmt.Errorf("secret_fetch step %q: 'module' is required", name)
		}

		raw, _ := config["secrets"].(map[string]any)
		if len(raw) == 0 {
			return nil, fmt.Errorf("secret_fetch step %q: 'secrets' map is required and must not be empty", name)
		}

		secretMap := make(map[string]string, len(raw))
		for k, v := range raw {
			idStr, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("secret_fetch step %q: secrets[%q] must be a string (secret ID or ARN)", name, k)
			}
			secretMap[k] = idStr
		}

		return &SecretFetchStep{
			name:       name,
			moduleName: moduleName,
			secrets:    secretMap,
			app:        app,
			tmpl:       NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *SecretFetchStep) Name() string { return s.name }

// Execute resolves the secret IDs/ARNs using the pipeline context (enabling
// per-tenant dynamic resolution), fetches each secret from the named secrets
// module, and returns the resolved values as step output.
func (s *SecretFetchStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("secret_fetch step %q: no application context", s.name)
	}

	provider, err := s.resolveProvider()
	if err != nil {
		return nil, err
	}

	output := make(map[string]any, len(s.secrets)+1)

	for outputKey, idTemplate := range s.secrets {
		// Resolve the secret ID/ARN template against the current pipeline context.
		// This enables tenant-aware ARNs such as:
		//   "arn:aws:secretsmanager:us-east-1:123:secret:{{.tenant_id}}-creds"
		resolvedID, resolveErr := s.tmpl.Resolve(idTemplate, pc)
		if resolveErr != nil {
			return nil, fmt.Errorf("secret_fetch step %q: failed to resolve secret ID for %q: %w", s.name, outputKey, resolveErr)
		}

		value, fetchErr := provider.Get(ctx, resolvedID)
		if fetchErr != nil {
			return nil, fmt.Errorf("secret_fetch step %q: failed to fetch secret %q (id=%q): %w", s.name, outputKey, resolvedID, fetchErr)
		}

		output[outputKey] = value
	}

	output["fetched"] = true
	return &StepResult{Output: output}, nil
}

// resolveProvider looks up the SecretFetchProvider from the application service
// registry using the configured module name.
func (s *SecretFetchStep) resolveProvider() (SecretFetchProvider, error) {
	svc, ok := s.app.SvcRegistry()[s.moduleName]
	if !ok {
		return nil, fmt.Errorf("secret_fetch step %q: secrets module %q not found in service registry", s.name, s.moduleName)
	}
	provider, ok := svc.(SecretFetchProvider)
	if !ok {
		return nil, fmt.Errorf("secret_fetch step %q: service %q does not implement SecretFetchProvider (Get method)", s.name, s.moduleName)
	}
	return provider, nil
}
