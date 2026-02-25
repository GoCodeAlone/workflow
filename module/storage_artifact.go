package module

import (
	"context"
	"io"
	"time"
)

// ArtifactStore is the interface for a named artifact storage backend.
// Unlike the execution-scoped artifact.Store, keys here are global (or
// caller-scoped) and support arbitrary metadata.
type ArtifactStore interface {
	// Upload stores content under key. metadata is optional.
	Upload(ctx context.Context, key string, reader io.Reader, metadata map[string]string) error

	// Download retrieves the content and metadata stored under key.
	// The caller must close the returned ReadCloser.
	Download(ctx context.Context, key string) (io.ReadCloser, map[string]string, error)

	// List returns all artifacts whose key starts with prefix.
	// Pass an empty prefix to list all artifacts.
	List(ctx context.Context, prefix string) ([]ArtifactInfo, error)

	// Delete removes the artifact stored under key.
	Delete(ctx context.Context, key string) error

	// Exists reports whether an artifact with the given key exists.
	Exists(ctx context.Context, key string) (bool, error)
}

// ArtifactInfo describes a stored artifact.
type ArtifactInfo struct {
	Key      string
	Size     int64
	Modified time.Time
	Metadata map[string]string
}
