package module

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/modular"
)

// JSONResponseStep writes an HTTP JSON response with a custom status code and stops the pipeline.
type JSONResponseStep struct {
	name       string
	status     int
	statusFrom string
	headers    map[string]string
	body       map[string]any
	bodyRaw    any // for non-map bodies (arrays, literals)
	bodyFrom   string
	tmpl       *TemplateEngine
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

		var body map[string]any
		var bodyRaw any
		if b, ok := config["body"].(map[string]any); ok {
			body = b
		} else if config["body"] != nil {
			// Support non-map bodies like arrays or literals
			bodyRaw = config["body"]
		}
		bodyFrom, _ := config["body_from"].(string)
		statusFrom, _ := config["status_from"].(string)

		return &JSONResponseStep{
			name:       name,
			status:     status,
			statusFrom: statusFrom,
			headers:    headers,
			body:       body,
			bodyRaw:    bodyRaw,
			bodyFrom:   bodyFrom,
			tmpl:       NewTemplateEngine(),
		}, nil
	}
}

func (s *JSONResponseStep) Name() string { return s.name }

// resolveStatus returns the effective HTTP status code for the response.
// If status_from is set, it resolves the value from the pipeline context and
// converts it to an integer. The resolved value must be a whole number within
// the valid HTTP status code range (100–599); otherwise it falls back to the
// static status (or 200 by default).
func (s *JSONResponseStep) resolveStatus(pc *PipelineContext) int {
	if s.statusFrom != "" {
		if val := resolveBodyFrom(s.statusFrom, pc); val != nil {
			var code int
			valid := false
			switch v := val.(type) {
			case int:
				code = v
				valid = true
			case float64:
				// Only accept whole numbers — reject 404.9, etc.
				if v == float64(int(v)) {
					code = int(v)
					valid = true
				}
			case int64:
				code = int(v)
				valid = true
			}
			if valid && code >= 100 && code <= 599 {
				return code
			}
		}
	}
	return s.status
}

func (s *JSONResponseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	status := s.resolveStatus(pc)

	w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter)
	if !ok {
		// No response writer — return the body as output without writing HTTP
		responseBody := s.resolveResponseBody(pc)
		output := map[string]any{
			"status": status,
		}
		if responseBody != nil {
			output["body"] = responseBody
		}
		return &StepResult{Output: output, Stop: true}, nil
	}

	// Determine response body
	responseBody := s.resolveResponseBody(pc)

	// Detect Go raw map/slice strings in response values — these indicate
	// a template resolved to a Go fmt.Sprint representation instead of JSON.
	// This is always a bug (e.g. {{ .steps.X.row }} instead of body_from).
	warnRawGoValues(s.name, responseBody)

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	for k, v := range s.headers {
		w.Header().Set(k, v)
	}

	// Write status code
	w.WriteHeader(status)

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
			"status": status,
		},
		Stop: true,
	}, nil
}

// resolveResponseBody determines the response body from the step configuration.
func (s *JSONResponseStep) resolveResponseBody(pc *PipelineContext) any {
	if s.bodyFrom != "" {
		return resolveBodyFrom(s.bodyFrom, pc)
	}
	if s.body != nil {
		result := make(map[string]any, len(s.body))
		for k, v := range s.body {
			resolved, err := s.resolveBodyValue(v, pc)
			if err != nil {
				return s.body // fallback to unresolved
			}
			result[k] = resolved
		}
		return result
	}
	if s.bodyRaw != nil {
		// Resolve template expressions in raw string bodies.
		if str, ok := s.bodyRaw.(string); ok {
			resolved, err := s.tmpl.Resolve(str, pc)
			if err != nil {
				return s.bodyRaw
			}
			return resolved
		}
		return s.bodyRaw
	}
	return nil
}

// resolveBodyValue resolves a single body value, supporting:
//   - `_from` references that inject raw step output values
//   - nested maps and slices
//   - template strings resolved via the TemplateEngine.
//
// `_from` is treated as a special directive only when it is the sole key in a map,
// e.g. `{"_from": "steps.fetch.rows"}`. This keeps the semantics simple and avoids
// ambiguity: the entire value is replaced with the referenced data.
//
// As a consequence, `_from` cannot be combined with other fields or template
// expressions in the same map node. Configuration authors can still mix raw
// injections and templated fields by using `_from` on a sibling field in the
// parent object instead.
func (s *JSONResponseStep) resolveBodyValue(v any, pc *PipelineContext) (any, error) {
	switch val := v.(type) {
	case map[string]any:
		// Check for _from reference, used only when it is the single key:
		// {"_from": "steps.fetch.rows"}. Combining `_from` with other keys in
		// the same map is intentionally not supported.
		if from, ok := val["_from"].(string); ok && len(val) == 1 {
			return resolveBodyFrom(from, pc), nil
		}
		// Recurse into nested map
		result := make(map[string]any, len(val))
		for k, item := range val {
			resolved, err := s.resolveBodyValue(item, pc)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", k, err)
			}
			result[k] = resolved
		}
		return result, nil
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			resolved, err := s.resolveBodyValue(item, pc)
			if err != nil {
				return nil, err
			}
			result[i] = resolved
		}
		return result, nil
	case string:
		return s.tmpl.Resolve(val, pc)
	default:
		return v, nil
	}
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

// warnRawGoValues recursively inspects a response body for string values that
// look like Go's default fmt.Sprint representation of maps or slices
// (e.g. "map[key:value ...]" or "[map[...] map[...]]"). These indicate a
// template expression resolved to a raw Go value instead of proper JSON.
// This is always a configuration error — the pipeline should use body_from
// or index into specific fields instead.
func warnRawGoValues(stepName string, body any) {
	switch v := body.(type) {
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok && isGoRawValue(s) {
				slog.Warn("json_response: field contains Go raw map/slice string instead of JSON — use body_from or index into specific fields",
					"step", stepName, "field", k, "value_prefix", truncateForLog(s, 80))
			}
			warnRawGoValues(stepName, val)
		}
	case []any:
		for _, item := range v {
			warnRawGoValues(stepName, item)
		}
	case string:
		if isGoRawValue(v) {
			slog.Warn("json_response: response body contains Go raw map/slice string instead of JSON — use body_from or index into specific fields",
				"step", stepName, "value_prefix", truncateForLog(v, 80))
		}
	}
}

// isGoRawValue detects strings that look like Go's fmt.Sprint output for maps
// or slices: "map[...]" or "[map[...]]".
func isGoRawValue(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "map[") || strings.HasPrefix(s, "[map[")
}

// truncate returns the first n characters of a string.
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
