package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// JSONResponseStep writes an HTTP JSON response with a custom status code and stops the pipeline.
type JSONResponseStep struct {
	name     string
	status   int
	headers  map[string]string
	body     map[string]any
	bodyFrom string
	tmpl     *TemplateEngine
}

// NewJSONResponseStepFactory returns a StepFactory that creates JSONResponseStep instances.
func NewJSONResponseStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		status := 200
		if s, ok := config["status"]; ok {
			switch v := s.(type) {
			case int:
				status = v
			case float64:
				status = int(v)
			}
		}

		var headers map[string]string
		if h, ok := config["headers"].(map[string]any); ok {
			headers = make(map[string]string, len(h))
			for k, v := range h {
				if s, ok := v.(string); ok {
					headers[k] = s
				}
			}
		}

		body, _ := config["body"].(map[string]any)
		bodyFrom, _ := config["body_from"].(string)

		return &JSONResponseStep{
			name:     name,
			status:   status,
			headers:  headers,
			body:     body,
			bodyFrom: bodyFrom,
			tmpl:     NewTemplateEngine(),
		}, nil
	}
}

func (s *JSONResponseStep) Name() string { return s.name }

func (s *JSONResponseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter)
	if !ok {
		// No response writer — return the body as output without writing HTTP
		var responseBody any
		if s.bodyFrom != "" {
			responseBody = resolveBodyFrom(s.bodyFrom, pc)
		} else if s.body != nil {
			resolved, err := s.tmpl.ResolveMap(s.body, pc)
			if err != nil {
				return nil, fmt.Errorf("json_response step %q: failed to resolve body: %w", s.name, err)
			}
			responseBody = resolved
		}
		output := map[string]any{
			"status": s.status,
		}
		if responseBody != nil {
			output["body"] = responseBody
		}
		return &StepResult{Output: output, Stop: true}, nil
	}

	// Determine response body
	var responseBody any
	if s.bodyFrom != "" {
		responseBody = resolveBodyFrom(s.bodyFrom, pc)
	} else if s.body != nil {
		resolved, err := s.tmpl.ResolveMap(s.body, pc)
		if err != nil {
			return nil, fmt.Errorf("json_response step %q: failed to resolve body: %w", s.name, err)
		}
		responseBody = resolved
	}

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	for k, v := range s.headers {
		w.Header().Set(k, v)
	}

	// Write status code
	w.WriteHeader(s.status)

	// Write body
	if responseBody != nil {
		if err := json.NewEncoder(w).Encode(responseBody); err != nil {
			return nil, fmt.Errorf("json_response step %q: failed to encode response: %w", s.name, err)
		}
	}

	// Mark response as handled
	pc.Metadata["_response_handled"] = true

	return &StepResult{
		Output: map[string]any{
			"status": s.status,
		},
		Stop: true,
	}, nil
}

// resolveBodyFrom resolves a dotted path like "steps.get-company.row" from the
// pipeline context. It looks in StepOutputs first (for "steps.X.Y" paths),
// then in Current.
func resolveBodyFrom(path string, pc *PipelineContext) any {
	parts := strings.SplitN(path, ".", 2)
	if len(parts) < 2 {
		// Single key — look in Current
		if val, ok := pc.Current[path]; ok {
			return val
		}
		return nil
	}

	prefix := parts[0]
	rest := parts[1]

	if prefix == "steps" {
		// "steps.stepName.field..." — look in StepOutputs
		stepParts := strings.SplitN(rest, ".", 2)
		stepName := stepParts[0]
		stepOutput, ok := pc.StepOutputs[stepName]
		if !ok {
			return nil
		}
		if len(stepParts) == 1 {
			return stepOutput
		}
		return resolveNestedPath(stepOutput, stepParts[1])
	}

	// Generic dotted path in Current
	return resolveNestedPath(pc.Current, path)
}

// resolveNestedPath walks a map[string]any using a dotted path.
func resolveNestedPath(data map[string]any, path string) any {
	parts := strings.Split(path, ".")
	var current any = data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}
