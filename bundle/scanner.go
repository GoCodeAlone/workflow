package bundle

import "github.com/GoCodeAlone/workflow/config"

// knownPathFields maps module types to the config fields that contain file paths.
var knownPathFields = map[string][]string{
	"dynamic.component": {"source"},
	"static.fileserver": {"root"},
	"storage.sqlite":    {"dbPath"},
	"auth.jwt":          {"seedFile"},
	"api.handler":       {"seedFile"},
}

// ScanReferencedPaths examines a workflow config and returns relative paths
// that are explicitly referenced in module configurations.
func ScanReferencedPaths(cfg *config.WorkflowConfig) []string {
	var paths []string
	seen := make(map[string]bool)

	for _, mod := range cfg.Modules {
		fields, ok := knownPathFields[mod.Type]
		if !ok {
			continue
		}
		for _, field := range fields {
			if val, ok := mod.Config[field].(string); ok && val != "" {
				if !seen[val] {
					seen[val] = true
					paths = append(paths, val)
				}
			}
		}
	}
	return paths
}
