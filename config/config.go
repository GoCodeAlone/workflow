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
}

// WorkflowConfig represents the overall configuration for the workflow engine
type WorkflowConfig struct {
	Modules   []ModuleConfig         `json:"modules" yaml:"modules"`
	Workflows map[string]interface{} `json:"workflows" yaml:"workflows"`
	Triggers  map[string]interface{} `json:"triggers" yaml:"triggers"`
	
	// Configuration sections for modular modules
	Auth       map[string]interface{} `json:"auth,omitempty" yaml:"auth,omitempty"`
	Cache      map[string]interface{} `json:"cache,omitempty" yaml:"cache,omitempty"`
	Database   map[string]interface{} `json:"database,omitempty" yaml:"database,omitempty"`
	Scheduler  map[string]interface{} `json:"scheduler,omitempty" yaml:"scheduler,omitempty"`
	HTTPServer map[string]interface{} `json:"httpserver,omitempty" yaml:"httpserver,omitempty"`
	Chimux     map[string]interface{} `json:"chimux,omitempty" yaml:"chimux,omitempty"`
	EventBus   map[string]interface{} `json:"eventbus,omitempty" yaml:"eventbus,omitempty"`
	HTTPClient map[string]interface{} `json:"httpclient,omitempty" yaml:"httpclient,omitempty"`
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

// NewEmptyWorkflowConfig creates a new empty workflow configuration
func NewEmptyWorkflowConfig() *WorkflowConfig {
	return &WorkflowConfig{
		Modules:   make([]ModuleConfig, 0),
		Workflows: make(map[string]interface{}),
		Triggers:  make(map[string]interface{}),
	}
}
