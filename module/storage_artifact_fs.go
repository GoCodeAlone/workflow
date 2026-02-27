package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
)

// ArtifactFSConfig holds configuration for the filesystem artifact store module.
type ArtifactFSConfig struct {
	BasePath string
	MaxSize  int64 // 0 means unlimited
}

// ArtifactFSModule is a modular.Module that provides a filesystem-backed ArtifactStore.
// Module type: storage.artifact with backend: filesystem.
type ArtifactFSModule struct {
	name   string
	cfg    ArtifactFSConfig
	mu     sync.RWMutex
	logger modular.Logger
}

// NewArtifactFSModule creates a new filesystem artifact store module.
func NewArtifactFSModule(name string, cfg ArtifactFSConfig) *ArtifactFSModule {
	return &ArtifactFSModule{
		name:   name,
		cfg:    cfg,
		logger: &noopLogger{},
	}
}

func (m *ArtifactFSModule) Name() string { return m.name }

func (m *ArtifactFSModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

func (m *ArtifactFSModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Filesystem artifact store",
			Instance:    m,
		},
	}
}

func (m *ArtifactFSModule) RequiresServices() []modular.ServiceDependency {
	return nil
}

func (m *ArtifactFSModule) Start(_ context.Context) error {
	if err := os.MkdirAll(m.cfg.BasePath, 0o750); err != nil {
		return fmt.Errorf("artifact store %q: failed to create base path %q: %w", m.name, m.cfg.BasePath, err)
	}
	m.logger.Info("Artifact store started", "name", m.name, "path", m.cfg.BasePath)
	return nil
}

func (m *ArtifactFSModule) Stop(_ context.Context) error {
	m.logger.Info("Artifact store stopped", "name", m.name)
	return nil
}

// artifactPath returns the filesystem path for an artifact key.
func (m *ArtifactFSModule) artifactPath(key string) string {
	// Sanitize key: collapse leading slashes so we don't escape basePath.
	key = strings.TrimPrefix(key, "/")
	return filepath.Join(m.cfg.BasePath, filepath.FromSlash(key))
}

// metaPath returns the sidecar metadata path for an artifact.
func metaPath(artifactPath string) string {
	return artifactPath + ".meta"
}

// artifactMeta is the sidecar JSON structure.
type artifactMeta struct {
	Size     int64             `json:"size"`
	Modified time.Time         `json:"modified"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Upload stores content under key with optional metadata.
func (m *ArtifactFSModule) Upload(_ context.Context, key string, reader io.Reader, metadata map[string]string) error {
	path := m.artifactPath(key)

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // G703: key sanitized by artifactPath (TrimPrefix + filepath.Join)
		return fmt.Errorf("artifact store %q: Upload %q: failed to create directory: %w", m.name, key, err)
	}

	f, err := os.Create(path) //nolint:gosec // G703: key sanitized by artifactPath
	if err != nil {
		return fmt.Errorf("artifact store %q: Upload %q: failed to create file: %w", m.name, key, err)
	}

	size, copyErr := io.Copy(f, reader)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("artifact store %q: Upload %q: failed to write: %w", m.name, key, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("artifact store %q: Upload %q: failed to close: %w", m.name, key, closeErr)
	}

	if m.cfg.MaxSize > 0 && size > m.cfg.MaxSize {
		_ = os.Remove(path) //nolint:gosec // G703: key sanitized by artifactPath
		return fmt.Errorf("artifact store %q: Upload %q: size %d exceeds limit %d", m.name, key, size, m.cfg.MaxSize)
	}

	meta := artifactMeta{
		Size:     size,
		Modified: time.Now().UTC(),
		Metadata: metadata,
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("artifact store %q: Upload %q: failed to marshal metadata: %w", m.name, key, err)
	}
	if err := os.WriteFile(metaPath(path), metaData, 0o600); err != nil { //nolint:gosec // G306
		return fmt.Errorf("artifact store %q: Upload %q: failed to write metadata: %w", m.name, key, err)
	}

	return nil
}

// Download retrieves content and metadata for key.
func (m *ArtifactFSModule) Download(_ context.Context, key string) (io.ReadCloser, map[string]string, error) {
	path := m.artifactPath(key)

	m.mu.RLock()
	defer m.mu.RUnlock()

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("artifact store %q: Download %q: not found", m.name, key)
		}
		return nil, nil, fmt.Errorf("artifact store %q: Download %q: failed to open: %w", m.name, key, err)
	}

	var md map[string]string
	if raw, err := os.ReadFile(metaPath(path)); err == nil {
		var meta artifactMeta
		if json.Unmarshal(raw, &meta) == nil {
			md = meta.Metadata
		}
	}

	return f, md, nil
}

// List returns ArtifactInfo for all artifacts whose key starts with prefix.
func (m *ArtifactFSModule) List(_ context.Context, prefix string) ([]ArtifactInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []ArtifactInfo

	err := filepath.Walk(m.cfg.BasePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		// Skip sidecar metadata files.
		if strings.HasSuffix(path, ".meta") {
			return nil
		}

		// Derive key from path relative to basePath.
		rel, relErr := filepath.Rel(m.cfg.BasePath, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip entries with unparseable paths
		}
		key := filepath.ToSlash(rel)

		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}

		ai := ArtifactInfo{
			Key:      key,
			Size:     info.Size(),
			Modified: info.ModTime().UTC(),
		}

		// Enrich with sidecar metadata if available.
		if raw, err := os.ReadFile(metaPath(path)); err == nil {
			var meta artifactMeta
			if json.Unmarshal(raw, &meta) == nil {
				if !meta.Modified.IsZero() {
					ai.Modified = meta.Modified
				}
				ai.Metadata = meta.Metadata
			}
		}

		results = append(results, ai)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("artifact store %q: List: walk error: %w", m.name, err)
	}

	return results, nil
}

// Delete removes the artifact and its sidecar.
func (m *ArtifactFSModule) Delete(_ context.Context, key string) error {
	path := m.artifactPath(key)

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("artifact store %q: Delete %q: not found", m.name, key)
		}
		return fmt.Errorf("artifact store %q: Delete %q: %w", m.name, key, err)
	}
	// Best-effort sidecar removal.
	_ = os.Remove(metaPath(path))
	return nil
}

// Exists reports whether an artifact with the given key exists.
func (m *ArtifactFSModule) Exists(_ context.Context, key string) (bool, error) {
	path := m.artifactPath(key)
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("artifact store %q: Exists %q: %w", m.name, key, err)
}
