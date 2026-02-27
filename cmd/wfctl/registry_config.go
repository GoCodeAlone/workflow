package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RegistryConfig defines wfctl plugin registry configuration.
type RegistryConfig struct {
	Registries []RegistrySourceConfig `yaml:"registries" json:"registries"`
}

// RegistrySourceConfig defines a single registry source.
type RegistrySourceConfig struct {
	Name     string `yaml:"name" json:"name"`         // e.g. "default", "my-org"
	Type     string `yaml:"type" json:"type"`         // "github" (only type for now)
	Owner    string `yaml:"owner" json:"owner"`       // GitHub owner/org
	Repo     string `yaml:"repo" json:"repo"`         // GitHub repo name
	Branch   string `yaml:"branch" json:"branch"`     // Git branch, default "main"
	Priority int    `yaml:"priority" json:"priority"` // Lower = higher priority
}

// DefaultRegistryConfig returns the built-in config with GoCodeAlone/workflow-registry.
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		Registries: []RegistrySourceConfig{
			{
				Name:     "default",
				Type:     "github",
				Owner:    registryOwner,
				Repo:     registryRepo,
				Branch:   registryBranch,
				Priority: 0,
			},
		},
	}
}

// LoadRegistryConfig loads configuration from the first found config file, or returns the default.
// Search order: --registry-config flag path, .wfctl.yaml in CWD, ~/.config/wfctl/config.yaml
func LoadRegistryConfig(explicitPath string) (*RegistryConfig, error) {
	paths := []string{}
	if explicitPath != "" {
		paths = append(paths, explicitPath)
	}
	paths = append(paths, ".wfctl.yaml")
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "wfctl", "config.yaml"))
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg RegistryConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse registry config %s: %w", p, err)
		}
		// Ensure defaults
		for i := range cfg.Registries {
			if cfg.Registries[i].Branch == "" {
				cfg.Registries[i].Branch = "main"
			}
			if cfg.Registries[i].Type == "" {
				cfg.Registries[i].Type = "github"
			}
		}
		return &cfg, nil
	}
	return DefaultRegistryConfig(), nil
}

// SaveRegistryConfig writes a registry config to a YAML file.
func SaveRegistryConfig(path string, cfg *RegistryConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal registry config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// MarshalJSON implements json.Marshaler for RegistryConfig (used in `registry list --json`).
func (c *RegistryConfig) MarshalJSON() ([]byte, error) {
	type Alias RegistryConfig
	return json.Marshal((*Alias)(c))
}
