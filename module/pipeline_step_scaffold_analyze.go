package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/scaffold"
)

// ScaffoldAnalyzeStep reads an OpenAPI spec from the HTTP request body,
// analyzes it, and returns the parsed resource/operation structure as JSON.
type ScaffoldAnalyzeStep struct {
	name  string
	title string
	theme string
}

// NewScaffoldAnalyzeStepFactory returns a StepFactory that creates ScaffoldAnalyzeStep instances.
func NewScaffoldAnalyzeStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		title, _ := config["title"].(string)
		theme, _ := config["theme"].(string)
		return &ScaffoldAnalyzeStep{
			name:  name,
			title: title,
			theme: theme,
		}, nil
	}
}

// Name returns the step name.
func (s *ScaffoldAnalyzeStep) Name() string { return s.name }

// Execute reads the OpenAPI spec from the request body, calls scaffold.AnalyzeOnly,
// and writes the result as a JSON response.
func (s *ScaffoldAnalyzeStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	req, _ := pc.Metadata["_http_request"].(*http.Request)
	w, hasWriter := pc.Metadata["_http_response_writer"].(http.ResponseWriter)

	// Read spec bytes from request body or current context.
	specBytes, err := s.readSpecBytes(req, pc)
	if err != nil {
		if hasWriter {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			pc.Metadata["_response_handled"] = true
			return &StepResult{Output: map[string]any{"error": err.Error()}, Stop: true}, nil
		}
		return nil, fmt.Errorf("step.ui_scaffold_analyze %q: %w", s.name, err)
	}

	// Analyze the spec.
	opts := scaffold.Options{
		Title: s.title,
		Theme: s.theme,
	}
	data, err := scaffold.AnalyzeOnly(specBytes, opts)
	if err != nil {
		if hasWriter {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			pc.Metadata["_response_handled"] = true
			return &StepResult{Output: map[string]any{"error": err.Error()}, Stop: true}, nil
		}
		return nil, fmt.Errorf("step.ui_scaffold_analyze %q: %w", s.name, err)
	}

	if hasWriter {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if encErr := json.NewEncoder(w).Encode(data); encErr != nil {
			return nil, fmt.Errorf("step.ui_scaffold_analyze %q: encoding response: %w", s.name, encErr)
		}
		pc.Metadata["_response_handled"] = true
		return &StepResult{Output: map[string]any{"status": 200}, Stop: true}, nil
	}

	// No HTTP writer — return the data as output for testing/non-HTTP pipelines.
	raw, _ := json.Marshal(data)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return &StepResult{Output: out}, nil
}

// readSpecBytes extracts the OpenAPI spec bytes from the request body or pipeline context.
func (s *ScaffoldAnalyzeStep) readSpecBytes(req *http.Request, pc *PipelineContext) ([]byte, error) {
	if req != nil && req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
		defer req.Body.Close()
		if len(body) > 0 {
			return body, nil
		}
	}
	// Fall back to body already parsed into context.
	if raw, ok := pc.Current["spec"].(string); ok {
		return []byte(raw), nil
	}
	if raw, ok := pc.TriggerData["body"].(string); ok {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("no OpenAPI spec provided in request body")
}
