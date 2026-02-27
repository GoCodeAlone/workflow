package interfaces

import "context"

// Reconfigurable is optionally implemented by modules that support
// runtime reconfiguration without requiring a full engine restart.
// When a config change affects only modules implementing this interface,
// the engine can perform a surgical update instead of a full stop/rebuild/start.
type Reconfigurable interface {
	// Reconfigure applies new configuration to a running module.
	// The module should:
	//   1. Validate the new config
	//   2. Gracefully drain in-flight work
	//   3. Apply the new configuration
	//   4. Resume accepting new work
	// Returns an error if the new config is invalid or cannot be applied.
	Reconfigure(ctx context.Context, newConfig map[string]any) error
}
