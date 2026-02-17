package capability

import (
	"sort"
	"sync"

	"github.com/GoCodeAlone/workflow/config"
)

var (
	mappingMu sync.RWMutex

	// moduleTypeToCapabilities maps module type strings to their capability categories.
	// Populated by plugins during registration via RegisterModuleTypeMapping.
	moduleTypeToCapabilities = make(map[string][]string)

	// triggerTypeToCapabilities maps trigger types to capabilities.
	triggerTypeToCapabilities = make(map[string][]string)

	// workflowTypeToCapabilities maps workflow types to capabilities.
	workflowTypeToCapabilities = make(map[string][]string)
)

// RegisterModuleTypeMapping records that a module type requires certain capabilities.
func RegisterModuleTypeMapping(moduleType string, capabilities ...string) {
	mappingMu.Lock()
	defer mappingMu.Unlock()
	moduleTypeToCapabilities[moduleType] = append(moduleTypeToCapabilities[moduleType], capabilities...)
}

// RegisterTriggerTypeMapping records trigger type to capability mapping.
func RegisterTriggerTypeMapping(triggerType string, capabilities ...string) {
	mappingMu.Lock()
	defer mappingMu.Unlock()
	triggerTypeToCapabilities[triggerType] = append(triggerTypeToCapabilities[triggerType], capabilities...)
}

// RegisterWorkflowTypeMapping records workflow type to capability mapping.
func RegisterWorkflowTypeMapping(workflowType string, capabilities ...string) {
	mappingMu.Lock()
	defer mappingMu.Unlock()
	workflowTypeToCapabilities[workflowType] = append(workflowTypeToCapabilities[workflowType], capabilities...)
}

// ResetMappings clears all registered type-to-capability mappings. Intended for testing.
func ResetMappings() {
	mappingMu.Lock()
	defer mappingMu.Unlock()
	moduleTypeToCapabilities = make(map[string][]string)
	triggerTypeToCapabilities = make(map[string][]string)
	workflowTypeToCapabilities = make(map[string][]string)
}

// DetectRequired scans a WorkflowConfig and returns the set of capabilities needed.
// It inspects module types, trigger types, and workflow types and returns a
// deduplicated, sorted list of required capabilities.
func DetectRequired(cfg *config.WorkflowConfig) []string {
	seen := make(map[string]bool)

	mappingMu.RLock()
	defer mappingMu.RUnlock()

	// Scan modules for module types.
	for _, mod := range cfg.Modules {
		if caps, ok := moduleTypeToCapabilities[mod.Type]; ok {
			for _, c := range caps {
				seen[c] = true
			}
		}
	}

	// Scan triggers keys for trigger types.
	for triggerType := range cfg.Triggers {
		if caps, ok := triggerTypeToCapabilities[triggerType]; ok {
			for _, c := range caps {
				seen[c] = true
			}
		}
	}

	// Scan workflows keys for workflow types.
	for workflowType := range cfg.Workflows {
		if caps, ok := workflowTypeToCapabilities[workflowType]; ok {
			for _, c := range caps {
				seen[c] = true
			}
		}
	}

	result := make([]string, 0, len(seen))
	for c := range seen {
		result = append(result, c)
	}
	sort.Strings(result)
	return result
}
