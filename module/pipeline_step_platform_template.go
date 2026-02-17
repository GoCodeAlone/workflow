package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// PlatformTemplateStep is a pipeline step that resolves a platform template
// with parameters and outputs the resulting CapabilityDeclarations.
type PlatformTemplateStep struct {
	name            string
	templateName    string
	templateVersion string
	parameters      map[string]any
	registry        platform.TemplateRegistry
}

// NewPlatformTemplateStepFactory returns a StepFactory that creates PlatformTemplateStep instances.
// The step looks up the TemplateRegistry from the modular Application's service registry.
func NewPlatformTemplateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		templateName, _ := config["template_name"].(string)
		if templateName == "" {
			return nil, fmt.Errorf("platform_template step %q: 'template_name' is required", name)
		}

		templateVersion, _ := config["template_version"].(string)

		var params map[string]any
		if rawParams, ok := config["parameters"].(map[string]any); ok {
			params = rawParams
		}

		// Look up TemplateRegistry from the service registry if available.
		var registry platform.TemplateRegistry
		if app != nil {
			var reg platform.TemplateRegistry
			if err := app.GetService("platform.TemplateRegistry", &reg); err == nil {
				registry = reg
			}
		}

		return &PlatformTemplateStep{
			name:            name,
			templateName:    templateName,
			templateVersion: templateVersion,
			parameters:      params,
			registry:        registry,
		}, nil
	}
}

// Name returns the step name.
func (s *PlatformTemplateStep) Name() string { return s.name }

// Execute resolves the configured template with parameters and outputs
// the resolved CapabilityDeclarations under the "resolved_resources" key.
func (s *PlatformTemplateStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("platform_template step %q: TemplateRegistry not available", s.name)
	}

	// Merge step parameters with any parameters from the pipeline context.
	params := make(map[string]any)
	if contextParams, ok := pc.Current["template_parameters"].(map[string]any); ok {
		for k, v := range contextParams {
			params[k] = v
		}
	}
	// Step-level parameters override context parameters.
	for k, v := range s.parameters {
		params[k] = v
	}

	caps, err := s.registry.Resolve(ctx, s.templateName, s.templateVersion, params)
	if err != nil {
		return nil, fmt.Errorf("platform_template step %q: resolve failed: %w", s.name, err)
	}

	// Convert CapabilityDeclarations to serializable maps.
	resources := make([]map[string]any, len(caps))
	for i, cap := range caps {
		resources[i] = map[string]any{
			"name":       cap.Name,
			"type":       cap.Type,
			"tier":       cap.Tier,
			"properties": cap.Properties,
			"dependsOn":  cap.DependsOn,
		}
	}

	return &StepResult{
		Output: map[string]any{
			"resolved_resources": resources,
			"template_name":      s.templateName,
			"template_version":   s.templateVersion,
		},
	}, nil
}
