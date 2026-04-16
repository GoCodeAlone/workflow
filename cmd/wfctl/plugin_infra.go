package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/GoCodeAlone/workflow/config"
)

// LoadPluginManifests loads all plugin.json files from the given plugins directory.
// Each sub-directory is expected to contain a plugin.json. Returns a map keyed
// by plugin name. Missing or malformed manifests are silently skipped.
func LoadPluginManifests(pluginsDir string) (map[string]*config.PluginManifestFile, error) {
	manifests := make(map[string]*config.PluginManifestFile)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return manifests, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginsDir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // plugin dir without manifest — skip
		}

		var manifest config.PluginManifestFile
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue // malformed manifest — skip
		}

		key := manifest.Name
		if key == "" {
			key = entry.Name()
		}
		manifests[key] = &manifest
	}

	return manifests, nil
}

// DetectPluginInfraNeeds inspects which modules in cfg come from plugins and
// returns the union of their InfraRequirement declarations. Duplicate
// requirements (same type+name from multiple instances of the same module type)
// are collapsed to one entry.
func DetectPluginInfraNeeds(cfg *config.WorkflowConfig, manifests map[string]*config.PluginManifestFile) []config.InfraRequirement {
	if cfg == nil || len(manifests) == 0 {
		return nil
	}

	seen := make(map[string]bool) // deduplicate by "type:name"
	var needs []config.InfraRequirement

	addRequirements := func(reqs []config.InfraRequirement) {
		for i := range reqs {
			req := reqs[i]
			key := req.Type + ":" + req.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			needs = append(needs, req)
		}
	}

	// Check top-level modules
	for _, mod := range cfg.Modules {
		for _, manifest := range manifests {
			if spec, ok := manifest.ModuleInfraRequirements[mod.Type]; ok {
				addRequirements(spec.Requires)
			}
		}
	}

	// Check services[*].modules for multi-service configs
	for _, svc := range cfg.Services {
		if svc == nil {
			continue
		}
		for _, mod := range svc.Modules {
			for _, manifest := range manifests {
				if spec, ok := manifest.ModuleInfraRequirements[mod.Type]; ok {
					addRequirements(spec.Requires)
				}
			}
		}
	}

	return needs
}
