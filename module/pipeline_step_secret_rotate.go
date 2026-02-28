package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/secrets"
)

// SecretRotateStep rotates a secret in a RotationProvider and returns the new value.
type SecretRotateStep struct {
	name         string
	provider     string // service name of the secrets RotationProvider module
	key          string // the secret key to rotate
	notifyModule string // optional module name to notify of rotation
	app          modular.Application
}

// NewSecretRotateStepFactory returns a StepFactory for step.secret_rotate.
func NewSecretRotateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		provider, _ := config["provider"].(string)
		if provider == "" {
			return nil, fmt.Errorf("secret_rotate step %q: 'provider' is required", name)
		}

		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("secret_rotate step %q: 'key' is required", name)
		}

		notifyModule, _ := config["notify_module"].(string)

		return &SecretRotateStep{
			name:         name,
			provider:     provider,
			key:          key,
			notifyModule: notifyModule,
			app:          app,
		}, nil
	}
}

// Name returns the step name.
func (s *SecretRotateStep) Name() string { return s.name }

// Execute rotates the secret by calling RotationProvider.Rotate and returns output
// indicating the rotation was successful.
func (s *SecretRotateStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("secret_rotate step %q: no application context", s.name)
	}

	rp, err := s.resolveProvider()
	if err != nil {
		return nil, err
	}

	if _, err := rp.Rotate(ctx, s.key); err != nil {
		return nil, fmt.Errorf("secret_rotate step %q: rotate failed: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"rotated":  true,
		"key":      s.key,
		"provider": s.provider,
	}}, nil
}

// resolveProvider looks up the secrets provider from the service registry and
// asserts it implements secrets.RotationProvider.
func (s *SecretRotateStep) resolveProvider() (secrets.RotationProvider, error) {
	svc, ok := s.app.SvcRegistry()[s.provider]
	if !ok {
		return nil, fmt.Errorf("secret_rotate step %q: provider service %q not found", s.name, s.provider)
	}
	rp, ok := svc.(secrets.RotationProvider)
	if !ok {
		return nil, fmt.Errorf("secret_rotate step %q: service %q does not implement secrets.RotationProvider", s.name, s.provider)
	}
	return rp, nil
}
