package module

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/artifact"
)

// ArtifactPullStep retrieves an artifact from a configured source
// (previous execution, URL, or S3) and writes it to a destination path.
type ArtifactPullStep struct {
	name        string
	source      string // "previous_execution", "url", "s3"
	executionID string
	key         string
	url         string
	dest        string
}

// NewArtifactPullStepFactory returns a StepFactory that creates ArtifactPullStep instances.
func NewArtifactPullStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		source, _ := config["source"].(string)
		if source == "" {
			return nil, fmt.Errorf("artifact_pull step %q: 'source' is required (previous_execution, url, or s3)", name)
		}

		switch source {
		case "previous_execution", "url", "s3":
			// valid
		default:
			return nil, fmt.Errorf("artifact_pull step %q: invalid source %q (expected previous_execution, url, or s3)", name, source)
		}

		dest, _ := config["dest"].(string)
		if dest == "" {
			return nil, fmt.Errorf("artifact_pull step %q: 'dest' is required", name)
		}

		executionID, _ := config["execution_id"].(string)
		key, _ := config["key"].(string)
		urlStr, _ := config["url"].(string)

		if source == "previous_execution" || source == "s3" {
			if key == "" {
				return nil, fmt.Errorf("artifact_pull step %q: 'key' is required for source %q", name, source)
			}
		}
		if source == "url" {
			if urlStr == "" {
				return nil, fmt.Errorf("artifact_pull step %q: 'url' is required for source \"url\"", name)
			}
		}

		return &ArtifactPullStep{
			name:        name,
			source:      source,
			executionID: executionID,
			key:         key,
			url:         urlStr,
			dest:        dest,
		}, nil
	}
}

// Name returns the step name.
func (s *ArtifactPullStep) Name() string { return s.name }

// Execute pulls the artifact from the configured source and writes it to dest.
func (s *ArtifactPullStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	var reader io.ReadCloser
	var size int64

	switch s.source {
	case "previous_execution", "s3":
		store, execID, err := s.resolveStore(pc)
		if err != nil {
			return nil, fmt.Errorf("artifact_pull step %q: %w", s.name, err)
		}
		reader, err = store.Get(ctx, execID, s.key)
		if err != nil {
			return nil, fmt.Errorf("artifact_pull step %q: failed to get artifact %q from execution %q: %w",
				s.name, s.key, execID, err)
		}

	case "url":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
		if err != nil {
			return nil, fmt.Errorf("artifact_pull step %q: invalid URL: %w", s.name, err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("artifact_pull step %q: HTTP request failed: %w", s.name, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("artifact_pull step %q: HTTP %d from %s", s.name, resp.StatusCode, s.url)
		}
		reader = resp.Body
		size = resp.ContentLength
	}
	defer reader.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(s.dest), 0o755); err != nil {
		return nil, fmt.Errorf("artifact_pull step %q: failed to create dest directory: %w", s.name, err)
	}

	f, err := os.Create(s.dest)
	if err != nil {
		return nil, fmt.Errorf("artifact_pull step %q: failed to create dest file: %w", s.name, err)
	}
	defer f.Close()

	written, err := io.Copy(f, reader)
	if err != nil {
		return nil, fmt.Errorf("artifact_pull step %q: failed to write artifact: %w", s.name, err)
	}

	if size <= 0 {
		size = written
	}

	return &StepResult{
		Output: map[string]any{
			"source":        s.source,
			"key":           s.key,
			"dest":          s.dest,
			"size":          size,
			"bytes_written": written,
		},
	}, nil
}

// resolveStore retrieves the artifact store and execution ID from the pipeline context.
func (s *ArtifactPullStep) resolveStore(pc *PipelineContext) (artifact.Store, string, error) {
	var store artifact.Store
	if storeVal, ok := pc.Metadata["artifact_store"]; ok {
		store, _ = storeVal.(artifact.Store)
	}
	if store == nil {
		return nil, "", fmt.Errorf("artifact store not found in pipeline metadata")
	}

	execID := s.executionID
	if execID == "" {
		execID, _ = pc.Metadata["execution_id"].(string)
	}
	if execID == "" {
		return nil, "", fmt.Errorf("execution_id not specified and not found in pipeline metadata")
	}

	return store, execID, nil
}
