package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/featureflag"
)

// FeatureFlagStep evaluates a feature flag within a pipeline and stores
// the result in the pipeline context under a configurable output key.
type FeatureFlagStep struct {
	name      string
	flag      string
	userFrom  string // template expression for user key
	groupFrom string // template expression for group
	outputKey string
	service   *featureflag.Service
	tmpl      *TemplateEngine
}

// NewFeatureFlagStepFactory returns a StepFactory that creates FeatureFlagStep instances.
// The factory captures the FF service via closure so steps can evaluate flags at runtime.
func NewFeatureFlagStepFactory(service *featureflag.Service) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		flag, _ := config["flag"].(string)
		if flag == "" {
			return nil, fmt.Errorf("feature_flag step %q: 'flag' is required", name)
		}

		outputKey, _ := config["output_key"].(string)
		if outputKey == "" {
			outputKey = flag // default output key is the flag name
		}

		userFrom, _ := config["user_from"].(string)
		groupFrom, _ := config["group_from"].(string)

		return &FeatureFlagStep{
			name:      name,
			flag:      flag,
			userFrom:  userFrom,
			groupFrom: groupFrom,
			outputKey: outputKey,
			service:   service,
			tmpl:      NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *FeatureFlagStep) Name() string { return s.name }

// Execute evaluates the configured feature flag and puts the result into the
// pipeline context. The output is a map keyed by output_key containing:
//
//	{enabled: bool, variant: string, value: any}
func (s *FeatureFlagStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	evalCtx := featureflag.EvaluationContext{
		Attributes: make(map[string]string),
	}

	// Resolve user key from template expression
	if s.userFrom != "" {
		resolved, err := s.tmpl.Resolve(s.userFrom, pc)
		if err != nil {
			return nil, fmt.Errorf("feature_flag step %q: failed to resolve user_from %q: %w", s.name, s.userFrom, err)
		}
		evalCtx.UserKey = resolved
	}

	// Resolve group from template expression
	if s.groupFrom != "" {
		resolved, err := s.tmpl.Resolve(s.groupFrom, pc)
		if err != nil {
			return nil, fmt.Errorf("feature_flag step %q: failed to resolve group_from %q: %w", s.name, s.groupFrom, err)
		}
		evalCtx.Attributes["groups"] = resolved
	}

	flagVal, err := s.service.Evaluate(ctx, s.flag, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("feature_flag step %q: failed to evaluate flag %q: %w", s.name, s.flag, err)
	}

	// Determine enabled status from the value
	enabled := false
	variant := ""
	switch v := flagVal.Value.(type) {
	case bool:
		enabled = v
		if v {
			variant = "true"
		} else {
			variant = "false"
		}
	case string:
		enabled = v != "" && v != "false" && v != "0"
		variant = v
	default:
		enabled = flagVal.Value != nil
		variant = fmt.Sprintf("%v", flagVal.Value)
	}

	output := map[string]any{
		s.outputKey: map[string]any{
			"enabled": enabled,
			"variant": variant,
			"value":   flagVal.Value,
		},
	}

	return &StepResult{
		Output: output,
	}, nil
}
