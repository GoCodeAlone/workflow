package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/CrisisTextLine/modular"
)

// HTTPCallStep makes an HTTP request as a pipeline step.
type HTTPCallStep struct {
	name    string
	url     string
	method  string
	headers map[string]string
	body    map[string]any
	timeout time.Duration
	tmpl    *TemplateEngine
}

// NewHTTPCallStepFactory returns a StepFactory that creates HTTPCallStep instances.
func NewHTTPCallStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		url, _ := config["url"].(string)
		if url == "" {
			return nil, fmt.Errorf("http_call step %q: 'url' is required", name)
		}

		method, _ := config["method"].(string)
		if method == "" {
			method = "GET"
		}

		step := &HTTPCallStep{
			name:    name,
			url:     url,
			method:  method,
			timeout: 30 * time.Second,
			tmpl:    NewTemplateEngine(),
		}

		if headers, ok := config["headers"].(map[string]any); ok {
			step.headers = make(map[string]string, len(headers))
			for k, v := range headers {
				if s, ok := v.(string); ok {
					step.headers[k] = s
				}
			}
		}

		if body, ok := config["body"].(map[string]any); ok {
			step.body = body
		}

		if timeout, ok := config["timeout"].(string); ok && timeout != "" {
			if d, err := time.ParseDuration(timeout); err == nil {
				step.timeout = d
			}
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *HTTPCallStep) Name() string { return s.name }

// Execute performs the HTTP request and returns the response.
func (s *HTTPCallStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Resolve URL template
	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to resolve url: %w", s.name, err)
	}

	var bodyReader io.Reader
	if s.body != nil {
		resolvedBody, resolveErr := s.tmpl.ResolveMap(s.body, pc)
		if resolveErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to resolve body: %w", s.name, resolveErr)
		}
		data, marshalErr := json.Marshal(resolvedBody)
		if marshalErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to marshal body: %w", s.name, marshalErr)
		}
		bodyReader = bytes.NewReader(data)
	} else if s.method != "GET" && s.method != "HEAD" {
		data, marshalErr := json.Marshal(pc.Current)
		if marshalErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to marshal current data: %w", s.name, marshalErr)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, s.method, resolvedURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to create request: %w", s.name, err)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range s.headers {
		resolved, resolveErr := s.tmpl.Resolve(v, pc)
		if resolveErr != nil {
			req.Header.Set(k, v)
		} else {
			req.Header.Set(k, resolved)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to read response: %w", s.name, err)
	}

	// Build response headers map
	respHeaders := make(map[string]any, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) == 1 {
			respHeaders[k] = v[0]
		} else {
			vals := make([]any, len(v))
			for i, hv := range v {
				vals[i] = hv
			}
			respHeaders[k] = vals
		}
	}

	output := map[string]any{
		"status_code": resp.StatusCode,
		"status":      resp.Status,
		"headers":     respHeaders,
	}

	// Try to parse response as JSON
	var jsonResp any
	if json.Unmarshal(respBody, &jsonResp) == nil {
		output["body"] = jsonResp
	} else {
		output["body"] = string(respBody)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http_call step %q: HTTP %d: %s", s.name, resp.StatusCode, string(respBody))
	}

	return &StepResult{Output: output}, nil
}
