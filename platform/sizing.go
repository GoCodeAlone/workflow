package platform

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ResolveSizing returns concrete resource defaults for the given size tier,
// with any non-empty fields in hints overriding the tier defaults.
// Returns an error if size is not a recognised tier.
func ResolveSizing(resourceType string, size interfaces.Size, hints *interfaces.ResourceHints) (interfaces.SizingDefaults, error) {
	defaults, ok := interfaces.SizingMap[size]
	if !ok {
		return interfaces.SizingDefaults{}, fmt.Errorf("unknown size tier %q", size)
	}

	if hints == nil {
		return defaults, nil
	}

	if hints.CPU != "" {
		defaults.CPU = hints.CPU
	}
	if hints.Memory != "" {
		defaults.Memory = hints.Memory
	}
	if hints.Storage != "" {
		defaults.DBStorage = hints.Storage
	}

	return defaults, nil
}
