// Package admin provides the built-in workflow admin UI configuration.
// When enabled via the --admin flag, the admin modules and routes are merged
// into the primary workflow engine, adding an authenticated management
// interface on a separate port (:8081 by default).
//
// The admin UI dogfoods the workflow engine: it uses the engine's own
// modules (http.server, http.router, auth.jwt, http.handler,
// static.fileserver) configured via an embedded YAML config.
package admin

import (
	_ "embed"
	"fmt"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

//go:embed config.yaml
var configData []byte

// LoadConfigRaw returns the raw embedded admin config YAML bytes.
func LoadConfigRaw() ([]byte, error) {
	if len(configData) == 0 {
		return nil, fmt.Errorf("embedded admin config is empty")
	}
	return configData, nil
}

// LoadConfig parses the embedded admin config and returns it.
func LoadConfig() (*config.WorkflowConfig, error) {
	var cfg config.WorkflowConfig
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse embedded admin config: %w", err)
	}
	return &cfg, nil
}

// MergeInto merges admin modules and workflows into the primary config.
// Admin modules are appended to the primary module list. Admin workflows
// use a separate key ("http-admin") so they don't conflict with the
// primary HTTP workflow — each gets its own router and server.
func MergeInto(primary *config.WorkflowConfig, admin *config.WorkflowConfig) {
	// Append admin modules
	primary.Modules = append(primary.Modules, admin.Modules...)

	// Merge workflows (admin uses distinct keys like "http-admin")
	if primary.Workflows == nil {
		primary.Workflows = make(map[string]any)
	}
	for wfType, adminWF := range admin.Workflows {
		if _, exists := primary.Workflows[wfType]; !exists {
			primary.Workflows[wfType] = adminWF
		}
	}

	// Merge triggers (admin triggers take precedence for new types)
	if len(admin.Triggers) > 0 {
		if primary.Triggers == nil {
			primary.Triggers = make(map[string]any)
		}
		for trigType, trigCfg := range admin.Triggers {
			if _, exists := primary.Triggers[trigType]; !exists {
				primary.Triggers[trigType] = trigCfg
			}
		}
	}
}
