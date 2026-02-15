package store

import (
	"context"
	"io"
	"time"
)

// FileInfo describes metadata about a file in a storage provider.
type FileInfo struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
	IsDir   bool      `json:"isDir"`
}

// StorageProvider defines the interface for file storage backends.
type StorageProvider interface {
	// List returns file entries under the given prefix.
	List(ctx context.Context, prefix string) ([]FileInfo, error)
	// Get retrieves a file by path.
	Get(ctx context.Context, path string) (io.ReadCloser, error)
	// Put writes a file at the given path.
	Put(ctx context.Context, path string, reader io.Reader) error
	// Delete removes a file at the given path.
	Delete(ctx context.Context, path string) error
	// Stat returns metadata for a file.
	Stat(ctx context.Context, path string) (FileInfo, error)
}
