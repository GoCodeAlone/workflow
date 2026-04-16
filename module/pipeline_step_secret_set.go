package module

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// SecretSetProvider is the minimal interface required by SecretSetStep.
// Both SecretsAWSModule and SecretsVaultModule satisfy this interface via
// the secrets.Provider Set method.
type SecretSetProvider interface {
	Set(ctx context.Context, key, value string) error
}

// SecretSetStep writes one or more secrets to a named secrets module
// (e.g. secrets.aws, secrets.vault). Secret values are Go template expressions
// evaluated against the live PipelineContext, enabling dynamic values from
// prior step outputs or trigger data:
//
//	config:
//	  module: zoom-secrets
//	  secrets:
//	    client_id: "{{.steps.form.client_id}}"
//	    client_secret: "{{.steps.form.client_secret}}"
type SecretSetStep struct {
	name       string
	moduleName string            // service name registered by the secrets module
	secrets    map[string]string // secret key → value template (may contain Go templates)
	app        modular.Application
	tmpl       *TemplateEngine
}

// NewSecretSetStepFactory returns a StepFactory that creates SecretSetStep instances.
func NewSecretSetStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		moduleName, _ := config["module"].(string)
		if moduleName == "" {
			return nil, fmt.Errorf("secret_set step %q: 'module' is required", name)
		}

		raw, _ := config["secrets"].(map[string]any)
		if len(raw) == 0 {
			return nil, fmt.Errorf("secret_set step %q: 'secrets' map is required and must not be empty", name)
		}

		secretMap := make(map[string]string, len(raw))
		for k, v := range raw {
			if strings.TrimSpace(k) == "" {
				return nil, fmt.Errorf("secret_set step %q: secrets key must not be empty", name)
			}
			valStr, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("secret_set step %q: secrets[%q] must be a string (value or template)", name, k)
			}
			secretMap[k] = valStr
		}

		return &SecretSetStep{
			name:       name,
			moduleName: moduleName,
			secrets:    secretMap,
			app:        app,
			tmpl:       NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *SecretSetStep) Name() string { return s.name }

// Execute resolves the value templates using the pipeline context, writes each
// secret to the named secrets module via provider.Set, and returns the list of
// written keys as step output for observability.
func (s *SecretSetStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("secret_set step %q: no application context", s.name)
	}

	provider, err := s.resolveProvider()
	if err != nil {
		return nil, err
	}

	setKeys := make([]string, 0, len(s.secrets))

	for secretKey, valueTemplate := range s.secrets {
		// Resolve the value template against the current pipeline context.
		// This enables dynamic values such as form fields from prior steps:
		//   "{{.steps.form.client_id}}"
		resolvedValue, resolveErr := s.tmpl.Resolve(valueTemplate, pc)
		if resolveErr != nil {
			return nil, fmt.Errorf("secret_set step %q: failed to resolve value for %q: %w", s.name, secretKey, resolveErr)
		}

		if setErr := provider.Set(ctx, secretKey, resolvedValue); setErr != nil {
			return nil, fmt.Errorf("secret_set step %q: failed to set secret %q: %w", s.name, secretKey, setErr)
		}

		setKeys = append(setKeys, secretKey)
	}

	// Sort for deterministic output ordering.
	sort.Strings(setKeys)

	return &StepResult{Output: map[string]any{
		"set_keys": setKeys,
	}}, nil
}

// resolveProvider looks up the SecretSetProvider from the application service
// registry using the configured module name.
func (s *SecretSetStep) resolveProvider() (SecretSetProvider, error) {
	svc, ok := s.app.SvcRegistry()[s.moduleName]
	if !ok {
		return nil, fmt.Errorf("secret_set step %q: secrets module %q not found in service registry", s.name, s.moduleName)
	}
	provider, ok := svc.(SecretSetProvider)
	if !ok {
		return nil, fmt.Errorf("secret_set step %q: service %q does not implement SecretSetProvider (Set method)", s.name, s.moduleName)
	}
	return provider, nil
}
