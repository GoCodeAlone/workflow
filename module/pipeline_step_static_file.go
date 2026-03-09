package module

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

// StaticFileStep serves a pre-loaded file from disk as an HTTP response.
// The file is read at init time (factory creation) for performance.
type StaticFileStep struct {
	name         string
	content      []byte
	contentType  string
	cacheControl string
}

// NewStaticFileStepFactory returns a StepFactory that creates StaticFileStep instances.
// The file is read from disk when the factory is invoked (at config load time).
func NewStaticFileStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		filePath, _ := cfg["file"].(string)
		if filePath == "" {
			return nil, fmt.Errorf("static_file step %q: 'file' is required", name)
		}

		contentType, _ := cfg["content_type"].(string)
		if contentType == "" {
			return nil, fmt.Errorf("static_file step %q: 'content_type' is required", name)
		}

		// Resolve file path relative to the config file directory.
		resolved := config.ResolvePathInConfig(cfg, filePath)

		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil, fmt.Errorf("static_file step %q: failed to read file %q: %w", name, resolved, err)
		}

		cacheControl, _ := cfg["cache_control"].(string)

		return &StaticFileStep{
			name:         name,
			content:      content,
			contentType:  contentType,
			cacheControl: cacheControl,
		}, nil
	}
}

func (s *StaticFileStep) Name() string { return s.name }

func (s *StaticFileStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter)
	if !ok {
		// No HTTP response writer — return content as output without writing HTTP.
		output := map[string]any{
			"content_type": s.contentType,
			"body":         string(s.content),
		}
		if s.cacheControl != "" {
			output["cache_control"] = s.cacheControl
		}
		return &StepResult{Output: output, Stop: true}, nil
	}

	w.Header().Set("Content-Type", s.contentType)
	if s.cacheControl != "" {
		w.Header().Set("Cache-Control", s.cacheControl)
	}

	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(s.content); err != nil {
		return nil, fmt.Errorf("static_file step %q: failed to write response: %w", s.name, err)
	}

	pc.Metadata["_response_handled"] = true

	return &StepResult{
		Output: map[string]any{
			"content_type": s.contentType,
		},
		Stop: true,
	}, nil
}
