package artifact

import (
	"context"
	"io"
	"time"
)

// Store defines the interface for artifact storage backends.
// Artifacts are scoped by execution ID and identified by key.
type Store interface {
	// Put stores an artifact for the given execution.
	// The reader's content is consumed and stored under the given key.
	// SHA256 checksum is computed during storage.
	Put(ctx context.Context, executionID, key string, reader io.Reader) error

	// Get retrieves an artifact by execution ID and key.
	// The caller is responsible for closing the returned ReadCloser.
	Get(ctx context.Context, executionID, key string) (io.ReadCloser, error)

	// List returns all artifacts for a given execution ID.
	List(ctx context.Context, executionID string) ([]Artifact, error)

	// Delete removes an artifact by execution ID and key.
	Delete(ctx context.Context, executionID, key string) error
}

// Artifact represents metadata about a stored artifact.
type Artifact struct {
	Key       string    `json:"key"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	Checksum  string    `json:"checksum"` // SHA256 hex digest
}
