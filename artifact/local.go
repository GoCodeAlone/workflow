package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// LocalStore implements Store using the local filesystem.
// Artifacts are stored under {baseDir}/artifacts/{executionID}/{key}.
// Metadata (size, checksum, timestamp) is tracked in memory.
type LocalStore struct {
	baseDir  string
	mu       sync.RWMutex
	metadata map[string]map[string]Artifact // executionID -> key -> Artifact
}

// NewLocalStore creates a new LocalStore rooted at baseDir.
func NewLocalStore(baseDir string) *LocalStore {
	return &LocalStore{
		baseDir:  baseDir,
		metadata: make(map[string]map[string]Artifact),
	}
}

// artifactPath returns the filesystem path for a given artifact.
func (s *LocalStore) artifactPath(executionID, key string) string {
	return filepath.Join(s.baseDir, "artifacts", executionID, key)
}

// Put stores an artifact on the local filesystem, computing SHA256 as it writes.
func (s *LocalStore) Put(_ context.Context, executionID, key string, reader io.Reader) error {
	path := s.artifactPath(executionID, key)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create artifact file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	size, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("failed to write artifact: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.metadata[executionID] == nil {
		s.metadata[executionID] = make(map[string]Artifact)
	}
	s.metadata[executionID][key] = Artifact{
		Key:       key,
		Size:      size,
		CreatedAt: time.Now(),
		Checksum:  checksum,
	}

	return nil
}

// Get retrieves an artifact from the local filesystem.
func (s *LocalStore) Get(_ context.Context, executionID, key string) (io.ReadCloser, error) {
	s.mu.RLock()
	execs, ok := s.metadata[executionID]
	if !ok {
		s.mu.RUnlock()
		return nil, fmt.Errorf("no artifacts for execution %q", executionID)
	}
	if _, ok := execs[key]; !ok {
		s.mu.RUnlock()
		return nil, fmt.Errorf("artifact %q not found for execution %q", key, executionID)
	}
	s.mu.RUnlock()

	path := s.artifactPath(executionID, key)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open artifact: %w", err)
	}

	return f, nil
}

// List returns all artifacts for a given execution ID, sorted by key.
func (s *LocalStore) List(_ context.Context, executionID string) ([]Artifact, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	execs, ok := s.metadata[executionID]
	if !ok {
		return nil, nil
	}

	artifacts := make([]Artifact, 0, len(execs))
	for _, a := range execs {
		artifacts = append(artifacts, a)
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Key < artifacts[j].Key
	})

	return artifacts, nil
}

// Delete removes an artifact from the local filesystem and metadata.
func (s *LocalStore) Delete(_ context.Context, executionID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	execs, ok := s.metadata[executionID]
	if !ok {
		return fmt.Errorf("no artifacts for execution %q", executionID)
	}
	if _, ok := execs[key]; !ok {
		return fmt.Errorf("artifact %q not found for execution %q", key, executionID)
	}

	path := s.artifactPath(executionID, key)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete artifact file: %w", err)
	}

	delete(execs, key)
	if len(execs) == 0 {
		delete(s.metadata, executionID)
	}

	return nil
}
