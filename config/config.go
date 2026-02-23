package config

import (
	"fmt"
	"os"
	pathpkg "path/filepath"

	"gopkg.in/yaml.v3"
)

// WorkflowRef is a reference to a workflow config file within an application config.
type WorkflowRef struct {
	// File is the path to the workflow YAML config file (relative to the application config).
	File string `json:"file" yaml:"file"`
	// Name is an optional override for the workflow's name within the application namespace.
	// If empty, the filename stem (without extension) is used.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// ApplicationInfo holds top-level metadata about a multi-workflow application.
type ApplicationInfo struct {
	// Name is the application name.
	Name string `json:"name" yaml:"name"`
	// Workflows lists the workflow config files that make up this application.
	Workflows []WorkflowRef `json:"workflows" yaml:"workflows"`
}

// ApplicationConfig is the top-level config for a multi-workflow application.
// It references multiple workflow config files that share a module registry.
type ApplicationConfig struct {
	// Application holds the application-level metadata and workflow references.
	Application ApplicationInfo `json:"application" yaml:"application"`
	// ConfigDir is the directory of the application config file, used for resolving relative paths.
	ConfigDir string `json:"-" yaml:"-"`
}

// LoadApplicationConfig loads an application config from a YAML file.
func LoadApplicationConfig(filepath string) (*ApplicationConfig, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read application config file: %w", err)
	}

	var cfg ApplicationConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse application config file: %w", err)
	}

	// Store the config file's directory for relative path resolution
	absPath, err := pathpkg.Abs(filepath)
	if err == nil {
		cfg.ConfigDir = pathpkg.Dir(absPath)
	}

	return &cfg, nil
}

// IsApplicationConfig returns true if the YAML data contains an application-level config
// (i.e., has an "application" key with a "workflows" section).
func IsApplicationConfig(data []byte) bool {
	var probe struct {
		Application *struct {
			Workflows []any `yaml:"workflows"`
		} `yaml:"application"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Application != nil && len(probe.Application.Workflows) > 0
}

// ModuleConfig represents a single module configuration
type ModuleConfig struct {
	Name      string            `json:"name" yaml:"name"`
	Type      string            `json:"type" yaml:"type"`
	Config    map[string]any    `json:"config,omitempty" yaml:"config,omitempty"`
	DependsOn []string          `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Branches  map[string]string `json:"branches,omitempty" yaml:"branches,omitempty"`
}

// RequiresConfig declares what capabilities and plugins a workflow needs.
type RequiresConfig struct {
	Capabilities []string            `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Plugins      []PluginRequirement `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

// PluginRequirement specifies a required plugin with optional version constraint.
type PluginRequirement struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// WorkflowConfig represents the overall configuration for the workflow engine
type WorkflowConfig struct {
	Modules   []ModuleConfig  `json:"modules" yaml:"modules"`
	Workflows map[string]any  `json:"workflows" yaml:"workflows"`
	Triggers  map[string]any  `json:"triggers" yaml:"triggers"`
	Pipelines map[string]any  `json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	Platform  map[string]any  `json:"platform,omitempty" yaml:"platform,omitempty"`
	Requires  *RequiresConfig `json:"requires,omitempty" yaml:"requires,omitempty"`
	ConfigDir string          `json:"-" yaml:"-"` // directory containing the config file, used for relative path resolution
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

// ResolvePathInConfig resolves a path relative to the _config_dir stored in
// a module's config map. If the path is already absolute or no _config_dir
// is present, the original path is returned.
func ResolvePathInConfig(cfg map[string]any, path string) string {
	if path == "" {
		return path
	}
	if pathpkg.IsAbs(path) {
		return path
	}
	if dir, ok := cfg["_config_dir"].(string); ok && dir != "" {
		return pathpkg.Join(dir, path)
	}
	return path
}

// MergeApplicationConfig loads all workflow config files referenced by an
// ApplicationConfig and merges them into a single WorkflowConfig. This is
// useful for callers that need a single combined config (e.g., the server's
// admin merge step) before passing it to the engine.
//
// Module name conflicts across files are reported as errors.
func MergeApplicationConfig(appCfg *ApplicationConfig) (*WorkflowConfig, error) {
	if appCfg == nil {
		return nil, fmt.Errorf("application config is nil")
	}

	combined := NewEmptyWorkflowConfig()
	seenModules := make(map[string]string)

	for _, ref := range appCfg.Application.Workflows {
		if ref.File == "" {
			return nil, fmt.Errorf("application %q: workflow reference has no 'file' field", appCfg.Application.Name)
		}

		filePath := ref.File
		if !pathpkg.IsAbs(filePath) && appCfg.ConfigDir != "" {
			filePath = pathpkg.Join(appCfg.ConfigDir, filePath)
		}

		wfCfg, err := LoadFromFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("application %q: failed to load workflow file %q: %w", appCfg.Application.Name, ref.File, err)
		}

		// Derive a name for error messages
		wfName := ref.Name
		if wfName == "" {
			base := pathpkg.Base(filePath)
			wfName = base[:len(base)-len(pathpkg.Ext(base))]
		}

		for _, modCfg := range wfCfg.Modules {
			if existing, conflict := seenModules[modCfg.Name]; conflict {
				return nil, fmt.Errorf("application %q: module name conflict: module %q is defined in both %q and %q",
					appCfg.Application.Name, modCfg.Name, existing, wfName)
			}
			seenModules[modCfg.Name] = wfName
		}

		combined.Modules = append(combined.Modules, wfCfg.Modules...)
		for k, v := range wfCfg.Workflows {
			combined.Workflows[k] = v
		}
		for k, v := range wfCfg.Triggers {
			combined.Triggers[k] = v
		}
		for k, v := range wfCfg.Pipelines {
			combined.Pipelines[k] = v
		}
		if combined.ConfigDir == "" {
			combined.ConfigDir = wfCfg.ConfigDir
		}
	}

	return combined, nil
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
