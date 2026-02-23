package module

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/CrisisTextLine/modular"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidatePathParamStep validates that path parameters extracted by a
// request_parse step are present and optionally conform to a format (e.g. UUID).
type ValidatePathParamStep struct {
	name   string
	params []string
	format string // "uuid" or "" (non-empty only)
	source string // dotted path to the params map, e.g. "steps.parse-request.path_params"
}

// NewValidatePathParamStepFactory returns a StepFactory that creates ValidatePathParamStep instances.
func NewValidatePathParamStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		rawParams, _ := config["params"].([]any)
		if len(rawParams) == 0 {
			return nil, fmt.Errorf("validate_path_param step %q: 'params' list is required", name)
		}
		params := make([]string, 0, len(rawParams))
		for _, p := range rawParams {
			s, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("validate_path_param step %q: params entries must be strings", name)
			}
			params = append(params, s)
		}

		format, _ := config["format"].(string)
		source, _ := config["source"].(string)

		return &ValidatePathParamStep{
			name:   name,
			params: params,
			format: format,
			source: source,
		}, nil
	}
}

// Name returns the step name.
func (s *ValidatePathParamStep) Name() string { return s.name }

// Execute validates that each configured path parameter is present and
// optionally matches the required format.
func (s *ValidatePathParamStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	data := s.resolveSource(pc)
	if data == nil {
		return nil, fmt.Errorf("validate_path_param step %q: source %q resolved to nil", s.name, s.source)
	}

	var errors []string
	for _, param := range s.params {
		val, exists := data[param]
		if !exists {
			errors = append(errors, fmt.Sprintf("missing path parameter %q", param))
			continue
		}
		str, ok := val.(string)
		if !ok || str == "" {
			errors = append(errors, fmt.Sprintf("path parameter %q must be a non-empty string", param))
			continue
		}
		if s.format == "uuid" && !uuidPattern.MatchString(str) {
			errors = append(errors, fmt.Sprintf("path parameter %q must be a valid UUID, got %q", param, str))
		}
	}

	if len(errors) > 0 {
		return nil, fmt.Errorf("validate_path_param step %q: %s", s.name, strings.Join(errors, "; "))
	}

	return &StepResult{Output: map[string]any{"valid": true}}, nil
}

// resolveSource walks a dotted path into the pipeline context to find the
// parameter map to validate. Falls back to pc.Current when source is empty.
func (s *ValidatePathParamStep) resolveSource(pc *PipelineContext) map[string]any {
	if s.source == "" {
		return pc.Current
	}

	data := map[string]any{
		"steps": func() map[string]any {
			result := make(map[string]any)
			for k, v := range pc.StepOutputs {
				result[k] = v
			}
			return result
		}(),
	}

	parts := strings.Split(s.source, ".")
	current := data
	for _, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil
		}
		nested, ok := val.(map[string]any)
		if !ok {
			return nil
		}
		current = nested
	}
	return current
}
