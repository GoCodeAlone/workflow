package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// buildGeneratorMetadata constructs a GeneratorMetadata snapshot describing
// the wfctl binary version and the versions of all IaC provider plugins
// installed in the plugin directory.  It is called just before a plan or
// apply result is persisted so that operators can later inspect what
// toolchain version produced the stored state.
//
// Plugin discovery is best-effort: unreadable or malformed plugin.json files
// are silently skipped rather than causing plan/apply to fail.  The wfctl
// version always appears even when no plugins are found.
func buildGeneratorMetadata() interfaces.GeneratorMetadata {
	return interfaces.GeneratorMetadata{
		WfctlVersion: version,
		Plugins:      collectIaCPluginVersions(),
	}
}

// collectIaCPluginVersions scans the plugin directory for subdirectories that
// contain a plugin.json declaring an iacProvider capability, and returns the
// name and version of each such plugin.
//
// The plugin directory is resolved using the same WFCTL_PLUGIN_DIR env var
// that discoverAndLoadIaCProvider uses, defaulting to ./data/plugins.
func collectIaCPluginVersions() []interfaces.PluginVersionInfo {
	pluginDir := os.Getenv("WFCTL_PLUGIN_DIR")
	if pluginDir == "" {
		pluginDir = "./data/plugins"
	}

	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		// Plugin dir absent or unreadable — return empty list without error.
		return nil
	}

	var infos []interfaces.PluginVersionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(pluginDir, entry.Name(), "plugin.json"))
		if err != nil {
			continue
		}
		var m iacPluginManifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		// Only include plugins that declare an IaC provider capability.
		if m.Capabilities.IaCProvider.Name == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = entry.Name()
		}
		infos = append(infos, interfaces.PluginVersionInfo{
			Name:    name,
			Version: m.Version,
		})
	}
	return infos
}
