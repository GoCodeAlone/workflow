package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

// FileSource loads config from a YAML file on disk.
type FileSource struct {
	path string
}

// NewFileSource creates a FileSource that reads from the given path.
func NewFileSource(path string) *FileSource {
	return &FileSource{path: path}
}

// Load reads the config file and returns a parsed WorkflowConfig.
// Supports both ApplicationConfig (multi-workflow) and WorkflowConfig formats.
func (s *FileSource) Load(_ context.Context) (*WorkflowConfig, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("file source: read %s: %w", s.path, err)
	}
	if IsApplicationConfig(data) {
		appCfg, err := LoadApplicationConfig(s.path)
		if err != nil {
			return nil, fmt.Errorf("file source: load application config: %w", err)
		}
		return MergeApplicationConfig(appCfg)
	}
	return LoadFromFile(s.path)
}

// Hash returns the SHA256 hex digest of the raw file bytes.
func (s *FileSource) Hash(_ context.Context) (string, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", fmt.Errorf("file source: read %s: %w", s.path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Name returns a human-readable identifier for this source.
func (s *FileSource) Name() string {
	return "file:" + s.path
}

// Path returns the filesystem path this source reads from.
func (s *FileSource) Path() string { return s.path }
