package module

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
)

// HTTPProxyStep forwards the original HTTP request to a dynamically resolved
// backend URL and writes the backend response directly to the client.
// It is designed for API gateway / reverse-proxy use cases where the backend
// URL is determined by earlier pipeline steps (e.g. a database lookup).
type HTTPProxyStep struct {
	name           string
	backendURLKey  string
	resourceKey    string
	forwardHeaders []string
	timeout        time.Duration
	httpClient     *http.Client
}

// NewHTTPProxyStepFactory returns a StepFactory that creates HTTPProxyStep instances.
func NewHTTPProxyStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		step := &HTTPProxyStep{
			name:          name,
			backendURLKey: "backend_url",
			resourceKey:   "path_params.resource",
			timeout:       30 * time.Second,
			httpClient:    http.DefaultClient,
		}

		if key, ok := config["backend_url_key"].(string); ok && key != "" {
			step.backendURLKey = key
		}

		if key, ok := config["resource_key"].(string); ok && key != "" {
			step.resourceKey = key
		}

		if headers, ok := config["forward_headers"]; ok {
			switch v := headers.(type) {
			case []string:
				step.forwardHeaders = v
			case []any:
				for _, h := range v {
					if s, ok := h.(string); ok {
						step.forwardHeaders = append(step.forwardHeaders, s)
					}
				}
			}
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
func (s *HTTPProxyStep) Name() string { return s.name }

// Execute forwards the original HTTP request to the resolved backend URL and
// writes the backend response directly to the response writer.
func (s *HTTPProxyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve backend URL from pipeline context
	backendURL := s.resolveStringValue(s.backendURLKey, pc)
	if backendURL == "" {
		return nil, fmt.Errorf("http_proxy step %q: backend URL not found at key %q in pipeline context", s.name, s.backendURLKey)
	}

	// Resolve optional resource path suffix
	resource := s.resolveStringValue(s.resourceKey, pc)

	// Build the target URL
	targetURL := strings.TrimRight(backendURL, "/")
	if resource != "" {
		targetURL += "/" + strings.TrimLeft(resource, "/")
	}

	// Get the original HTTP request from metadata
	origReq, _ := pc.Metadata["_http_request"].(*http.Request)

	// Append original query string
	if origReq != nil && origReq.URL.RawQuery != "" {
		targetURL += "?" + origReq.URL.RawQuery
	}

	// Determine the HTTP method
	method := http.MethodGet
	if origReq != nil {
		method = origReq.Method
	}

	// Build the request body
	var bodyReader io.Reader
	if origReq != nil && origReq.Body != nil {
		bodyReader = origReq.Body
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, method, targetURL, bodyReader) //nolint:gosec // G107: URL is dynamically resolved from pipeline context
	if err != nil {
		return nil, fmt.Errorf("http_proxy step %q: failed to create proxy request: %w", s.name, err)
	}

	// Forward Content-Length from the original request
	if origReq != nil && origReq.ContentLength > 0 {
		proxyReq.ContentLength = origReq.ContentLength
	}

	// Forward configured headers from the original request
	if origReq != nil && len(s.forwardHeaders) > 0 {
		for _, h := range s.forwardHeaders {
			if vals := origReq.Header.Values(h); len(vals) > 0 {
				for _, v := range vals {
					proxyReq.Header.Add(h, v)
				}
			}
		}
	}

	// Execute the proxy request
	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("http_proxy step %q: proxy request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	// Read the backend response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_proxy step %q: failed to read proxy response: %w", s.name, err)
	}

	// Try to write directly to the response writer if available
	w, hasWriter := pc.Metadata["_http_response_writer"].(http.ResponseWriter)
	if hasWriter {
		// Copy backend response headers
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}

		// Write status code
		w.WriteHeader(resp.StatusCode)

		// Write body
		if len(respBody) > 0 {
			if _, writeErr := w.Write(respBody); writeErr != nil {
				return nil, fmt.Errorf("http_proxy step %q: failed to write response: %w", s.name, writeErr)
			}
		}

		// Mark response as handled
		pc.Metadata["_response_handled"] = true

		return &StepResult{
			Output: map[string]any{
				"status_code": resp.StatusCode,
				"proxied_to":  targetURL,
			},
			Stop: true,
		}, nil
	}

	// No response writer available — return the proxied response as output
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

	return &StepResult{
		Output: map[string]any{
			"status_code": resp.StatusCode,
			"headers":     respHeaders,
			"body":        string(respBody),
			"proxied_to":  targetURL,
		},
		Stop: true,
	}, nil
}

// resolveStringValue resolves a dot-path key from the pipeline context.
// It supports nested paths like "path_params.resource" by traversing
// pc.Current as a nested map.
func (s *HTTPProxyStep) resolveStringValue(key string, pc *PipelineContext) string {
	parts := strings.Split(key, ".")
	var current any = pc.Current

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = m[part]
		if !ok {
			return ""
		}
	}

	if str, ok := current.(string); ok {
		return str
	}
	return ""
}
