package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// ValidateRequestBodyStep parses the JSON request body from the HTTP
// request and validates that all required fields are present.
type ValidateRequestBodyStep struct {
	name           string
	requiredFields []string
}

// NewValidateRequestBodyStepFactory returns a StepFactory that creates
// ValidateRequestBodyStep instances.
func NewValidateRequestBodyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		var required []string
		if raw, ok := config["required_fields"].([]any); ok {
			for _, f := range raw {
				s, ok := f.(string)
				if !ok {
					return nil, fmt.Errorf("validate_request_body step %q: required_fields entries must be strings", name)
				}
				required = append(required, s)
			}
		}

		return &ValidateRequestBodyStep{
			name:           name,
			requiredFields: required,
		}, nil
	}
}

// Name returns the step name.
func (s *ValidateRequestBodyStep) Name() string { return s.name }

// Execute parses the JSON body from the HTTP request and validates
// required fields are present. The parsed body is returned as output
// so downstream steps can reference it.
func (s *ValidateRequestBodyStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Try trigger data first (command handler may have pre-parsed)
	var body map[string]any
	if b, ok := pc.TriggerData["body"].(map[string]any); ok {
		body = b
	} else if b, ok := pc.Current["body"].(map[string]any); ok {
		body = b
	} else {
		req, _ := pc.Metadata["_http_request"].(*http.Request)
		if req != nil && req.Body != nil {
			bodyBytes, err := io.ReadAll(req.Body)
			if err != nil {
				return nil, fmt.Errorf("validate_request_body step %q: failed to read body: %w", s.name, err)
			}
			if len(bodyBytes) > 0 {
				if err := json.Unmarshal(bodyBytes, &body); err != nil {
					return nil, fmt.Errorf("validate_request_body step %q: invalid JSON body: %w", s.name, err)
				}
			}
		}
	}

	if body == nil && len(s.requiredFields) > 0 {
		return nil, fmt.Errorf("validate_request_body step %q: request body is required", s.name)
	}

	var missing []string
	for _, field := range s.requiredFields {
		if _, exists := body[field]; !exists {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("validate_request_body step %q: missing required fields: %s", s.name, strings.Join(missing, ", "))
	}

	return &StepResult{
		Output: map[string]any{
			"body": body,
		},
	}, nil
}
