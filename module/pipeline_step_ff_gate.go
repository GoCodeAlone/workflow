package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/featureflag"
)

// FFGateStep combines feature flag evaluation with conditional routing.
// Based on the flag result, it routes to either the on_enabled or on_disabled step.
type FFGateStep struct {
	name       string
	flag       string
	onEnabled  string // next step name when flag is enabled/true
	onDisabled string // next step name when flag is disabled/false
	userFrom   string // template expression for user key
	groupFrom  string // template expression for group
	service    *featureflag.Service
	tmpl       *TemplateEngine
}

// NewFFGateStepFactory returns a StepFactory that creates FFGateStep instances.
func NewFFGateStepFactory(service *featureflag.Service) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		flag, _ := config["flag"].(string)
		if flag == "" {
			return nil, fmt.Errorf("ff_gate step %q: 'flag' is required", name)
		}

		onEnabled, _ := config["on_enabled"].(string)
		if onEnabled == "" {
			return nil, fmt.Errorf("ff_gate step %q: 'on_enabled' is required", name)
		}

		onDisabled, _ := config["on_disabled"].(string)
		if onDisabled == "" {
			return nil, fmt.Errorf("ff_gate step %q: 'on_disabled' is required", name)
		}

		userFrom, _ := config["user_from"].(string)
		groupFrom, _ := config["group_from"].(string)

		return &FFGateStep{
			name:       name,
			flag:       flag,
			onEnabled:  onEnabled,
			onDisabled: onDisabled,
			userFrom:   userFrom,
			groupFrom:  groupFrom,
			service:    service,
			tmpl:       NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *FFGateStep) Name() string { return s.name }

// Execute evaluates the flag and routes to the appropriate next step.
func (s *FFGateStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	evalCtx := featureflag.EvaluationContext{
		Attributes: make(map[string]string),
	}

	// Resolve user key from template expression
	if s.userFrom != "" {
		resolved, err := s.tmpl.Resolve(s.userFrom, pc)
		if err != nil {
			return nil, fmt.Errorf("ff_gate step %q: failed to resolve user_from %q: %w", s.name, s.userFrom, err)
		}
		evalCtx.UserKey = resolved
	}

	// Resolve group from template expression
	if s.groupFrom != "" {
		resolved, err := s.tmpl.Resolve(s.groupFrom, pc)
		if err != nil {
			return nil, fmt.Errorf("ff_gate step %q: failed to resolve group_from %q: %w", s.name, s.groupFrom, err)
		}
		evalCtx.Attributes["groups"] = resolved
	}

	flagVal, err := s.service.Evaluate(ctx, s.flag, evalCtx)
	if err != nil {
		return nil, fmt.Errorf("ff_gate step %q: failed to evaluate flag %q: %w", s.name, s.flag, err)
	}

	// Determine enabled status
	enabled := false
	switch v := flagVal.Value.(type) {
	case bool:
		enabled = v
	case string:
		enabled = v != "" && v != "false" && v != "0"
	default:
		enabled = flagVal.Value != nil
	}

	nextStep := s.onDisabled
	if enabled {
		nextStep = s.onEnabled
	}

	return &StepResult{
		Output: map[string]any{
			"flag":      s.flag,
			"enabled":   enabled,
			"next_step": nextStep,
		},
		NextStep: nextStep,
	}, nil
}
