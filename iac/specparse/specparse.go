// Package specparse converts an already-decoded []any of spec maps (as
// produced by YAML/JSON config loaders) into []interfaces.ResourceSpec.
//
// This is the in-memory parser — it does NOT read files or expand secret://
// references. Secret refs pass through verbatim so that downstream JIT
// substitution (iac/jitsubst) can expand them at apply time.
package specparse

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ParseResourceSpecs converts a raw config value ([]any of map[string]any)
// into []interfaces.ResourceSpec. A nil raw value is allowed and returns a
// nil slice. Secret:// refs in config values are preserved verbatim.
func ParseResourceSpecs(raw any) ([]interfaces.ResourceSpec, error) {
	if raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("specs must be a list, got %T", raw)
	}
	specs := make([]interfaces.ResourceSpec, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("specs[%d] must be a map, got %T", i, item)
		}
		spec := interfaces.ResourceSpec{}
		if n, ok := m["name"].(string); ok {
			spec.Name = n
		}
		if t, ok := m["type"].(string); ok {
			spec.Type = t
		}
		if c, ok := m["config"].(map[string]any); ok {
			spec.Config = c
		}
		if sz, ok := m["size"].(string); ok {
			spec.Size = interfaces.Size(sz)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}
