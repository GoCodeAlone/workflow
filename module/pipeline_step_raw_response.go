package module

import (
	"context"
	"fmt"
	"net/http"

	"github.com/CrisisTextLine/modular"
)

// RawResponseStep writes a non-JSON HTTP response (e.g. XML, HTML, plain text)
// with a custom status code, content type, and optional headers, then stops the pipeline.
type RawResponseStep struct {
	name        string
	status      int
	contentType string
	headers     map[string]string
	body        string
	bodyFrom    string
	tmpl        *TemplateEngine
}

// NewRawResponseStepFactory returns a StepFactory that creates RawResponseStep instances.
func NewRawResponseStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		contentType, _ := config["content_type"].(string)
		if contentType == "" {
			return nil, fmt.Errorf("raw_response step %q: 'content_type' is required", name)
		}

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

		body, _ := config["body"].(string)
		bodyFrom, _ := config["body_from"].(string)

		return &RawResponseStep{
			name:        name,
			status:      status,
			contentType: contentType,
			headers:     headers,
			body:        body,
			bodyFrom:    bodyFrom,
			tmpl:        NewTemplateEngine(),
		}, nil
	}
}

func (s *RawResponseStep) Name() string { return s.name }

func (s *RawResponseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the response body
	responseBody := s.resolveBody(pc)

	w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter)
	if !ok {
		// No response writer — return the body as output without writing HTTP
		output := map[string]any{
			"status":       s.status,
			"content_type": s.contentType,
		}
		if responseBody != "" {
			output["body"] = responseBody
		}
		return &StepResult{Output: output, Stop: true}, nil
	}

	// Set Content-Type header
	w.Header().Set("Content-Type", s.contentType)

	// Set additional headers
	for k, v := range s.headers {
		w.Header().Set(k, v)
	}

	// Write status code
	w.WriteHeader(s.status)

	// Write body
	if responseBody != "" {
		if _, err := w.Write([]byte(responseBody)); err != nil {
			return nil, fmt.Errorf("raw_response step %q: failed to write response: %w", s.name, err)
		}
	}

	// Mark response as handled
	pc.Metadata["_response_handled"] = true

	return &StepResult{
		Output: map[string]any{
			"status":       s.status,
			"content_type": s.contentType,
		},
		Stop: true,
	}, nil
}

// resolveBody determines the response body string from the step configuration.
func (s *RawResponseStep) resolveBody(pc *PipelineContext) string {
	if s.bodyFrom != "" {
		val := resolveBodyFrom(s.bodyFrom, pc)
		if str, ok := val.(string); ok {
			return str
		}
		if val != nil {
			return fmt.Sprintf("%v", val)
		}
		return ""
	}
	if s.body != "" {
		resolved, err := s.tmpl.Resolve(s.body, pc)
		if err != nil {
			return s.body // fallback to unresolved
		}
		return resolved
	}
	return ""
}
