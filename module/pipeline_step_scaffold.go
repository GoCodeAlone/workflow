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

// ScaffoldStep generates a Vite+React+TypeScript UI scaffold from an OpenAPI
// spec in the request body and returns it as a downloadable ZIP archive.
type ScaffoldStep struct {
	name     string
	title    string
	theme    string
	auth     bool
	filename string
}

// NewScaffoldStepFactory returns a StepFactory that creates ScaffoldStep instances.
func NewScaffoldStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		title, _ := config["title"].(string)
		theme, _ := config["theme"].(string)
		auth, _ := config["auth"].(bool)
		filename, _ := config["filename"].(string)
		if filename == "" {
			filename = "scaffold.zip"
		}
		return &ScaffoldStep{
			name:     name,
			title:    title,
			theme:    theme,
			auth:     auth,
			filename: filename,
		}, nil
	}
}

// Name returns the step name.
func (s *ScaffoldStep) Name() string { return s.name }

// Execute reads the OpenAPI spec from the request body, generates scaffold files,
// and writes them as a ZIP response.
func (s *ScaffoldStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	req, _ := pc.Metadata["_http_request"].(*http.Request)
	w, hasWriter := pc.Metadata["_http_response_writer"].(http.ResponseWriter)

	// Read spec bytes from request body or pipeline context.
	specBytes, err := s.readSpecBytes(req, pc)
	if err != nil {
		if hasWriter {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			pc.Metadata["_response_handled"] = true
			return &StepResult{Output: map[string]any{"error": err.Error()}, Stop: true}, nil
		}
		return nil, fmt.Errorf("step.ui_scaffold %q: %w", s.name, err)
	}

	// Analyze the spec.
	opts := scaffold.Options{
		Title: s.title,
		Theme: s.theme,
		Auth:  s.auth,
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
		return nil, fmt.Errorf("step.ui_scaffold %q: analyzing spec: %w", s.name, err)
	}

	// Generate ZIP.
	zipBytes, err := scaffold.GenerateToZip(*data)
	if err != nil {
		if hasWriter {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			pc.Metadata["_response_handled"] = true
			return &StepResult{Output: map[string]any{"error": err.Error()}, Stop: true}, nil
		}
		return nil, fmt.Errorf("step.ui_scaffold %q: generating zip: %w", s.name, err)
	}

	if hasWriter {
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", s.filename))
		w.WriteHeader(http.StatusOK)
		if _, writeErr := w.Write(zipBytes); writeErr != nil { //nolint:gosec // response is binary zip data with proper Content-Type header
			return nil, fmt.Errorf("step.ui_scaffold %q: writing response: %w", s.name, writeErr)
		}
		pc.Metadata["_response_handled"] = true
		return &StepResult{Output: map[string]any{"status": 200, "bytes": len(zipBytes)}, Stop: true}, nil
	}

	// No HTTP writer — return ZIP bytes in output for testing/non-HTTP pipelines.
	return &StepResult{
		Output: map[string]any{
			"zip_bytes": zipBytes,
			"bytes":     len(zipBytes),
		},
	}, nil
}

// readSpecBytes extracts the OpenAPI spec bytes from the request body or pipeline context.
func (s *ScaffoldStep) readSpecBytes(req *http.Request, pc *PipelineContext) ([]byte, error) {
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
