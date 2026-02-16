package module

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/artifact"
)

// ArtifactPushStep reads a file from sourcePath and stores it in the artifact store.
type ArtifactPushStep struct {
	name       string
	sourcePath string
	key        string
	dest       string
}

// NewArtifactPushStepFactory returns a StepFactory that creates ArtifactPushStep instances.
func NewArtifactPushStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		sourcePath, _ := config["source_path"].(string)
		if sourcePath == "" {
			return nil, fmt.Errorf("artifact_push step %q: 'source_path' is required", name)
		}

		key, _ := config["key"].(string)
		if key == "" {
			return nil, fmt.Errorf("artifact_push step %q: 'key' is required", name)
		}

		dest, _ := config["dest"].(string)
		if dest == "" {
			dest = "artifact_store"
		}

		return &ArtifactPushStep{
			name:       name,
			sourcePath: sourcePath,
			key:        key,
			dest:       dest,
		}, nil
	}
}

// Name returns the step name.
func (s *ArtifactPushStep) Name() string { return s.name }

// Execute reads the source file and stores it as an artifact.
func (s *ArtifactPushStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	var store artifact.Store
	if storeVal, ok := pc.Metadata["artifact_store"]; ok {
		store, _ = storeVal.(artifact.Store)
	}
	if store == nil {
		return nil, fmt.Errorf("artifact_push step %q: artifact store not found in pipeline metadata", s.name)
	}

	executionID, _ := pc.Metadata["execution_id"].(string)
	if executionID == "" {
		return nil, fmt.Errorf("artifact_push step %q: execution_id not found in pipeline metadata", s.name)
	}

	f, err := os.Open(s.sourcePath)
	if err != nil {
		return nil, fmt.Errorf("artifact_push step %q: failed to open source %q: %w", s.name, s.sourcePath, err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("artifact_push step %q: failed to stat source %q: %w", s.name, s.sourcePath, err)
	}

	// Compute checksum while reading
	hasher := sha256.New()
	tee := io.TeeReader(f, hasher)

	if err := store.Put(ctx, executionID, s.key, tee); err != nil {
		return nil, fmt.Errorf("artifact_push step %q: failed to store artifact: %w", s.name, err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	return &StepResult{
		Output: map[string]any{
			"key":      s.key,
			"size":     stat.Size(),
			"checksum": checksum,
			"dest":     s.dest,
		},
	}, nil
}
