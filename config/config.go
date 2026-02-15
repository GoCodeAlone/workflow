package config

import (
	"fmt"
	"os"
	pathpkg "path/filepath"

	"gopkg.in/yaml.v3"
)

// ModuleConfig represents a single module configuration
type ModuleConfig struct {
	Name      string            `json:"name" yaml:"name"`
	Type      string            `json:"type" yaml:"type"`
	Config    map[string]any    `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Branches  map[string]string `json:"branches,omitempty" yaml:"branches,omitempty"`
}

// WorkflowConfig represents the overall configuration for the workflow engine
type WorkflowConfig struct {
	Modules   []ModuleConfig `json:"modules" yaml:"modules"`
	Workflows map[string]any `json:"workflows" yaml:"workflows"`
	Triggers  map[string]any `json:"triggers" yaml:"triggers"`
	Pipelines map[string]any `json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	ConfigDir string         `json:"-" yaml:"-"` // directory containing the config file, used for relative path resolution
}

// ResolveRelativePath resolves a path relative to the config file's directory.
// If the path is absolute, it is returned as-is.
func (c *WorkflowConfig) ResolveRelativePath(path string) string {
	if path == "" || c.ConfigDir == "" {
		return path
	}
	if pathpkg.IsAbs(path) {
		return path
	}
	return pathpkg.Join(c.ConfigDir, path)
}

// LoadFromFile loads a workflow configuration from a YAML file
func LoadFromFile(filepath string) (*WorkflowConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Store the config file's directory for relative path resolution
	absPath, err := pathpkg.Abs(filepath)
	if err == nil {
		cfg.ConfigDir = pathpkg.Dir(absPath)
	}

	return &cfg, nil
}

// LoadFromString loads a workflow configuration from a YAML string.
func LoadFromString(yamlContent string) (*WorkflowConfig, error) {
	var cfg WorkflowConfig
	if err := yaml.Unmarshal([]byte(yamlContent), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config string: %w", err)
	}
	return &cfg, nil
}

// NewEmptyWorkflowConfig creates a new empty workflow configuration
func NewEmptyWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		Modules:   make([]ModuleConfig, 0),
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}
}
