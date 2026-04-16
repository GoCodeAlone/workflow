package module

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretSetProvider is the minimal interface required by SecretSetStep.
// Any module used by step.secret_set must expose a Set method matching this
// signature — either directly on the registered service, or on the underlying
// secrets.Provider accessible via a Provider() accessor. Built-in secrets
// modules (secrets.aws, secrets.vault, secrets.keychain) satisfy this via
// their Provider() method since the module wrappers don't expose Set directly.
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
//
// Empty resolved values are permitted (useful for clearing a secret).
// On partial failure (e.g., the 3rd of 5 keys fails), earlier writes are
// already committed — secrets backends have no transaction primitive.
// The returned error identifies which key failed.
func (s *SecretSetStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("secret_set step %q: no application context", s.name)
	}

	provider, err := s.resolveProvider()
	if err != nil {
		return nil, err
	}

	// Sort keys for deterministic write order. This ensures partial failures
	// (where provider.Set fails mid-way) are reproducible rather than
	// dependent on Go's random map iteration order.
	sortedKeys := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Strings(sortedKeys)

	setKeys := make([]string, 0, len(s.secrets))

	for _, secretKey := range sortedKeys {
		valueTemplate := s.secrets[secretKey]
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
// registry using the configured module name. It first checks if the service
// directly implements SecretSetProvider; if not, it checks for a Provider()
// accessor (used by SecretsAWSModule, SecretsVaultModule, SecretsKeychainModule)
// and asserts the underlying provider implements Set.
func (s *SecretSetStep) resolveProvider() (SecretSetProvider, error) {
	svc, ok := s.app.SvcRegistry()[s.moduleName]
	if !ok {
		return nil, fmt.Errorf("secret_set step %q: secrets module %q not found in service registry", s.name, s.moduleName)
	}

	// Direct: service itself implements Set.
	if provider, ok := svc.(SecretSetProvider); ok {
		return provider, nil
	}

	// Indirect: service exposes a Provider() accessor (e.g. SecretsAWSModule,
	// SecretsVaultModule, SecretsKeychainModule) whose underlying
	// secrets.Provider implements Set.
	type providerAccessor interface {
		Provider() secrets.Provider
	}
	if accessor, ok := svc.(providerAccessor); ok {
		underlying := accessor.Provider()
		if underlying != nil {
			if provider, ok := underlying.(SecretSetProvider); ok {
				return provider, nil
			}
		}
	}

	return nil, fmt.Errorf("secret_set step %q: service %q does not implement SecretSetProvider (Set method) directly or via Provider() accessor", s.name, s.moduleName)
}
