package config

import (
	"sort"
	"strings"
)

// DesiredEnvironmentNames returns the environment names declared as desired
// state in workflow config. Runtime placeholders such as ${WORKFLOW_ENV} are
// intentionally skipped because they are resolved outside static config.
func DesiredEnvironmentNames(cfg *WorkflowConfig) []string {
	if cfg == nil {
		return nil
	}
	names := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || IsRuntimeEnvironmentPlaceholder(name) {
			return
		}
		names[name] = true
	}

	for name := range cfg.Environments {
		add(name)
	}
	if cfg.CI != nil && cfg.CI.Deploy != nil {
		for name := range cfg.CI.Deploy.Environments {
			add(name)
		}
	}
	if cfg.Platform != nil {
		if name, ok := cfg.Platform["environment"].(string); ok {
			add(name)
		}
	}
	for _, store := range cfg.SecretStores {
		if store == nil || store.Config == nil {
			continue
		}
		if name, ok := store.Config["environment"].(string); ok {
			add(name)
		}
	}

	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// IsRuntimeEnvironmentPlaceholder reports whether name is a runtime environment
// variable reference rather than a static provider environment name.
func IsRuntimeEnvironmentPlaceholder(name string) bool {
	return strings.Contains(name, "${") || strings.HasPrefix(name, "$")
}
