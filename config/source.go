package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigSource provides configuration from an arbitrary backend.
// Implementations must be safe for concurrent use.
type ConfigSource interface {
	// Load retrieves the current configuration.
	Load(ctx context.Context) (*WorkflowConfig, error)

	// Hash returns a content-addressable hash of the current config.
	// Used for change detection without full deserialization.
	Hash(ctx context.Context) (string, error)

	// Name returns a human-readable identifier for this source.
	Name() string
}

// ConfigChangeEvent is emitted when a ConfigSource detects a change.
type ConfigChangeEvent struct {
	Source  string
	OldHash string
	NewHash string
	Config  *WorkflowConfig
	Time    time.Time
}

// HashConfig returns the SHA256 hex digest of the YAML-serialized config.
func HashConfig(cfg *WorkflowConfig) (string, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
