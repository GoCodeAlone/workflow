package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ModuleConfig represents a single module configuration
type ModuleConfig struct {
	Name      string                 `json:"name" yaml:"name"`
	Type      string                 `json:"type" yaml:"type"`
	Config    map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn []string               `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Branches  map[string]string      `json:"branches,omitempty" yaml:"branches,omitempty"`
}

// WorkflowConfig represents the overall configuration for the workflow engine
type WorkflowConfig struct {
	Modules   []ModuleConfig         `json:"modules" yaml:"modules"`
	Workflows map[string]interface{} `json:"workflows" yaml:"workflows"`
	Triggers  map[string]interface{} `json:"triggers" yaml:"triggers"`
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
		Workflows: make(map[string]interface{}),
		Triggers:  make(map[string]interface{}),
	}
}
