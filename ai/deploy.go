package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"

	"gopkg.in/yaml.v3"
)

// DeployService bridges AI code generation with the dynamic component system.
// It generates components in dynamic format and loads them into the Yaegi
// interpreter at runtime.
type DeployService struct {
	aiService *Service
	registry  *dynamic.ComponentRegistry
	pool      *dynamic.InterpreterPool
	loader    *dynamic.Loader
	validator *Validator
}

// NewDeployService creates a DeployService that connects the AI service
// to the dynamic component registry.
func NewDeployService(ai *Service, registry *dynamic.ComponentRegistry, pool *dynamic.InterpreterPool) *DeployService {
	return &DeployService{
		aiService: ai,
		registry:  registry,
		pool:      pool,
		loader:    dynamic.NewLoader(pool, registry),
		validator: NewValidator(DefaultValidationConfig(), pool),
	}
}

// GenerateAndDeploy takes a natural language intent, generates the workflow
// config and any required components, loads the components into the dynamic
// registry, and returns the config.
func (d *DeployService) GenerateAndDeploy(ctx context.Context, intent string) (*config.WorkflowConfig, error) {
	req := GenerateRequest{
		Intent: intent,
		Constraints: []string{
			"Generate custom components in dynamic format (package component with exported functions, stdlib only)",
		},
	}

	resp, err := d.aiService.GenerateWorkflow(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("workflow generation failed: %w", err)
	}

	// Deploy each generated component into the dynamic system
	for _, comp := range resp.Components {
		if err := d.DeployComponent(ctx, comp); err != nil {
			return nil, fmt.Errorf("failed to deploy component %q: %w", comp.Name, err)
		}
	}

	return resp.Workflow, nil
}

// DeployComponent takes a ComponentSpec, generates dynamic-format code if
// needed, validates it, and loads it into the dynamic component registry.
func (d *DeployService) DeployComponent(ctx context.Context, spec ComponentSpec) error {
	source := spec.GoCode

	// If no code is provided, generate it using the AI service
	if source == "" {
		generated, err := d.aiService.GenerateComponent(ctx, spec)
		if err != nil {
			return fmt.Errorf("code generation failed: %w", err)
		}
		source = generated
	}

	// Validate and adapt the source if needed
	source, err := ensureDynamicFormat(source, spec.Name)
	if err != nil {
		return fmt.Errorf("source format validation failed: %w", err)
	}

	// Validate and fix the source if needed
	if d.validator != nil {
		fixedSource, result, verr := d.validator.ValidateAndFix(ctx, d.aiService, spec, source)
		if verr != nil {
			return fmt.Errorf("validation failed: %w", verr)
		}
		if !result.Valid {
			return fmt.Errorf("source validation failed after retries: %v", result.Errors)
		}
		source = fixedSource
	}

	// Load into the dynamic system (validates imports, compiles, registers)
	_, err = d.loader.LoadFromString(spec.Name, source)
	if err != nil {
		return fmt.Errorf("dynamic load failed: %w", err)
	}

	return nil
}

// SaveConfig writes a WorkflowConfig to a YAML file.
func (d *DeployService) SaveConfig(cfg *config.WorkflowConfig, path string) error {
	if cfg == nil {
		return fmt.Errorf("config must not be nil")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

// ensureDynamicFormat checks that source is a valid dynamic component
// (package component with exported functions and stdlib-only imports).
// If the source uses a different package name, it returns an error.
func ensureDynamicFormat(source, name string) (string, error) {
	// Verify the source declares package component
	if !strings.Contains(source, "package component") {
		return "", fmt.Errorf("source must use 'package component', got different package declaration")
	}

	// Validate that only allowed imports are used
	if err := dynamic.ValidateSource(source); err != nil {
		return "", err
	}

	return source, nil
}
