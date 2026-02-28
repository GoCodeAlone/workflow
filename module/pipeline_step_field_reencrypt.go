package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// FieldReencryptStep re-encrypts pipeline context data with the latest key version.
type FieldReencryptStep struct {
	name     string
	module   string // field-protection module name to look up
	tenantID string // template expression for tenant ID
	tmpl     *TemplateEngine
	app      modular.Application
}

// NewFieldReencryptStepFactory returns a StepFactory for step.field_reencrypt.
func NewFieldReencryptStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		moduleName, _ := config["module"].(string)
		if moduleName == "" {
			return nil, fmt.Errorf("field_reencrypt step %q: 'module' is required", name)
		}
		tenantID, _ := config["tenant_id"].(string)

		return &FieldReencryptStep{
			name:     name,
			module:   moduleName,
			tenantID: tenantID,
			tmpl:     NewTemplateEngine(),
			app:      app,
		}, nil
	}
}

// Name returns the step name.
func (s *FieldReencryptStep) Name() string { return s.name }

// Execute re-encrypts data by decrypting with the old key and encrypting with the current key.
func (s *FieldReencryptStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Look up the ProtectedFieldManager from the service registry.
	var manager *ProtectedFieldManager
	if err := s.app.GetService(s.module, &manager); err != nil {
		return nil, fmt.Errorf("field_reencrypt step %q: module %q not found: %w", s.name, s.module, err)
	}

	// Resolve tenant ID from template expression.
	tenantID := s.tenantID
	if tenantID != "" {
		resolved, err := s.tmpl.Resolve(tenantID, pc)
		if err == nil && resolved != "" {
			tenantID = resolved
		}
	}

	// Decrypt with old key version, then re-encrypt with current key.
	data := pc.Current
	if data == nil {
		return &StepResult{Output: map[string]any{"reencrypted": false, "reason": "no data"}}, nil
	}

	if err := manager.DecryptMap(ctx, tenantID, data); err != nil {
		return nil, fmt.Errorf("field_reencrypt step %q: decrypt: %w", s.name, err)
	}

	if err := manager.EncryptMap(ctx, tenantID, data); err != nil {
		return nil, fmt.Errorf("field_reencrypt step %q: encrypt: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"reencrypted": true}}, nil
}
