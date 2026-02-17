package plugin

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

// MissingCapabilitiesError is returned when a workflow config requires capabilities
// that no loaded plugin provides.
type MissingCapabilitiesError struct {
	Missing []string
}

func (e *MissingCapabilitiesError) Error() string {
	return fmt.Sprintf("missing required capabilities: %s", strings.Join(e.Missing, ", "))
}

// ResolveWorkflowDependencies checks that all capabilities required by a
// workflow config are satisfied by loaded plugins. If cfg.Requires is nil,
// returns nil (auto-detection will be added later). For each required
// capability, checks capabilityReg.HasProvider(). Returns
// MissingCapabilitiesError if any are missing.
func (m *EnginePluginManager) ResolveWorkflowDependencies(cfg *config.WorkflowConfig) error {
	if cfg.Requires == nil {
		return nil
	}

	var missing []string
	for _, cap := range cfg.Requires.Capabilities {
		if !m.capabilityReg.HasProvider(cap) {
			missing = append(missing, cap)
		}
	}

	if len(missing) > 0 {
		return &MissingCapabilitiesError{Missing: missing}
	}

	return nil
}
