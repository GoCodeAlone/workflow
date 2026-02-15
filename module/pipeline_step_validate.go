package module

import (
	"context"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ValidateStep validates data in the pipeline context against a schema or
// a list of required fields.
type ValidateStep struct {
	name           string
	strategy       string
	requiredFields []string
	schema         map[string]any
	source         string // optional dotted path to validate (e.g. "steps.parse-request.body")
}

// NewValidateStepFactory returns a StepFactory that creates ValidateStep instances.
func NewValidateStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		strategy, _ := config["strategy"].(string)
		if strategy == "" {
			strategy = "required_fields"
		}

		source, _ := config["source"].(string)

		step := &ValidateStep{
			name:     name,
			strategy: strategy,
			source:   source,
		}

		switch strategy {
		case "json_schema":
			schema, ok := config["schema"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("validate step %q: json_schema strategy requires a 'schema' map", name)
			}
			step.schema = schema
		case "required_fields":
			rawFields, _ := config["required_fields"].([]any)
			if len(rawFields) == 0 {
				return nil, fmt.Errorf("validate step %q: required_fields strategy requires a non-empty 'required_fields' list", name)
			}
			fields := make([]string, 0, len(rawFields))
			for _, f := range rawFields {
				s, ok := f.(string)
				if !ok {
					return nil, fmt.Errorf("validate step %q: required_fields entries must be strings", name)
				}
				fields = append(fields, s)
			}
			step.requiredFields = fields
		default:
			return nil, fmt.Errorf("validate step %q: unknown strategy %q (expected json_schema or required_fields)", name, strategy)
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *ValidateStep) Name() string { return s.name }

// Execute validates pc.Current according to the configured strategy.
func (s *ValidateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	switch s.strategy {
	case "required_fields":
		return s.executeRequiredFields(pc)
	case "json_schema":
		return s.executeJSONSchema(pc)
	default:
		return nil, fmt.Errorf("validate step %q: unknown strategy %q", s.name, s.strategy)
	}
}

// resolveSource returns the data map to validate. If source is empty, returns
// pc.Current. Otherwise, resolves a dotted path like "steps.parse-request.body"
// into the appropriate nested map from the pipeline context.
func (s *ValidateStep) resolveSource(pc *PipelineContext) map[string]any {
	if s.source == "" {
		return pc.Current
	}

	// Use the template engine's data structure for resolution
	data := map[string]any{
		"steps": func() map[string]any {
			result := make(map[string]any)
			for k, v := range pc.StepOutputs {
				result[k] = v
			}
			return result
		}(),
	}

	// Walk the dotted path
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

// executeRequiredFields checks that every listed field exists in the source data.
func (s *ValidateStep) executeRequiredFields(pc *PipelineContext) (*StepResult, error) {
	data := s.resolveSource(pc)
	if data == nil {
		return nil, fmt.Errorf("validate step %q: source %q resolved to nil", s.name, s.source)
	}
	var missing []string
	for _, field := range s.requiredFields {
		if _, exists := data[field]; !exists {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("validate step %q: missing required fields: %s", s.name, strings.Join(missing, ", "))
	}
	return &StepResult{Output: map[string]any{}}, nil
}

// executeJSONSchema performs a basic type/required/properties check against the source data.
func (s *ValidateStep) executeJSONSchema(pc *PipelineContext) (*StepResult, error) {
	data := s.resolveSource(pc)
	if data == nil {
		return nil, fmt.Errorf("validate step %q: source %q resolved to nil", s.name, s.source)
	}
	// Check required fields from the schema
	if requiredRaw, ok := s.schema["required"]; ok {
		requiredList, ok := requiredRaw.([]any)
		if !ok {
			return nil, fmt.Errorf("validate step %q: schema 'required' must be an array", s.name)
		}
		var missing []string
		for _, r := range requiredList {
			field, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := data[field]; !exists {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("validate step %q: missing required fields: %s", s.name, strings.Join(missing, ", "))
		}
	}

	// Check property types if a properties section is provided
	if propsRaw, ok := s.schema["properties"]; ok {
		props, ok := propsRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("validate step %q: schema 'properties' must be a map", s.name)
		}
		for field, specRaw := range props {
			val, exists := data[field]
			if !exists {
				// Not required, skip
				continue
			}
			spec, ok := specRaw.(map[string]any)
			if !ok {
				continue
			}
			expectedType, _ := spec["type"].(string)
			if expectedType == "" {
				continue
			}
			if err := checkJSONType(field, val, expectedType); err != nil {
				return nil, fmt.Errorf("validate step %q: %w", s.name, err)
			}
		}
	}

	return &StepResult{Output: map[string]any{}}, nil
}

// checkJSONType validates that val conforms to the given JSON Schema type name.
func checkJSONType(field string, val any, expected string) error {
	switch expected {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", field, val)
		}
	case "number":
		switch val.(type) {
		case float64, float32, int, int64, int32:
			// ok
		default:
			return fmt.Errorf("field %q: expected number, got %T", field, val)
		}
	case "integer":
		switch val.(type) {
		case int, int64, int32, float64:
			// float64 is accepted because JSON unmarshalling yields float64
		default:
			return fmt.Errorf("field %q: expected integer, got %T", field, val)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected boolean, got %T", field, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected object, got %T", field, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("field %q: expected array, got %T", field, val)
		}
	}
	return nil
}
