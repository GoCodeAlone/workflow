package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/CrisisTextLine/modular"
)

// DelegateStep forwards the HTTP request to a named service implementing
// http.Handler. This is a "passthrough" pipeline step: the delegate service
// handles the full HTTP response (status, headers, body). Because the
// delegate writes to the ResponseWriter directly, this step sets
// _response_handled in pipeline metadata and returns Stop: true.
type DelegateStep struct {
	name    string
	service string
	app     modular.Application
}

// NewDelegateStepFactory returns a StepFactory that creates DelegateStep instances.
func NewDelegateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		service, _ := config["service"].(string)
		if service == "" {
			return nil, fmt.Errorf("delegate step %q: 'service' is required", name)
		}

		return &DelegateStep{
			name:    name,
			service: service,
			app:     app,
		}, nil
	}
}

// Name returns the step name.
func (s *DelegateStep) Name() string { return s.name }

// Execute forwards the request to the delegate service.
// It reads _http_request and _http_response_writer from the pipeline context
// metadata. If these are present (live HTTP context), the delegate writes
// directly to the response writer. If not present (e.g., test context), it
// uses httptest.ResponseRecorder and returns the captured response as output.
func (s *DelegateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the service from the registry
	if s.app == nil {
		return nil, fmt.Errorf("delegate step %q: no application context", s.name)
	}

	svc, ok := s.app.SvcRegistry()[s.service]
	if !ok {
		return nil, fmt.Errorf("delegate step %q: service %q not found in registry", s.name, s.service)
	}

	handler, ok := svc.(http.Handler)
	if !ok {
		return nil, fmt.Errorf("delegate step %q: service %q does not implement http.Handler", s.name, s.service)
	}

	// Check for live HTTP context in metadata
	req, hasReq := pc.Metadata["_http_request"].(*http.Request)
	w, hasWriter := pc.Metadata["_http_response_writer"].(http.ResponseWriter)

	if hasReq && hasWriter {
		// Live HTTP context: delegate writes directly to the response writer
		handler.ServeHTTP(w, req)
		pc.Metadata["_response_handled"] = true
		return &StepResult{
			Output: map[string]any{
				"delegated_to": s.service,
			},
			Stop: true,
		}, nil
	}

	// No live HTTP context: use a recorder to capture the response.
	// Reconstruct a minimal request from trigger data.
	method, _ := pc.TriggerData["method"].(string)
	if method == "" {
		method = "GET"
	}
	path, _ := pc.TriggerData["path"].(string)
	if path == "" {
		path = "/"
	}

	var bodyReader io.Reader
	if body, ok := pc.TriggerData["body"]; ok {
		data, err := json.Marshal(body)
		if err == nil {
			bodyReader = bytes.NewReader(data)
		}
	}

	testReq, err := http.NewRequest(method, path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("delegate step %q: failed to create request: %w", s.name, err)
	}
	if bodyReader != nil {
		testReq.Header.Set("Content-Type", "application/json")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, testReq)

	result := recorder.Result()
	defer result.Body.Close()
	respBody, _ := io.ReadAll(result.Body)

	output := map[string]any{
		"delegated_to": s.service,
		"status_code":  result.StatusCode,
	}

	var jsonResp any
	if json.Unmarshal(respBody, &jsonResp) == nil {
		output["body"] = jsonResp
	} else {
		output["body"] = string(respBody)
	}

	return &StepResult{Output: output}, nil
}
