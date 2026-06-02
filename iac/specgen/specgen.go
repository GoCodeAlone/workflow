// Package specgen serialises []interfaces.ResourceSpec to YAML in the
// resource-spec schema shape consumed by iac/specparse.ParseResourceSpecs.
//
// SpecToYAML is the inverse of specparse.ParseResourceSpecs: it emits the
// same field names ("name", "type", "config", "size") so that a re-parse
// round-trips without loss. Secret:// references in Config values are emitted
// verbatim — no expansion is performed.
package specgen

import (
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// SpecToYAML marshals specs to YAML in the resource-spec schema.
// Each spec becomes a mapping with fields name, type, size (omitted when
// empty), and config (omitted when nil). Secret:// refs survive verbatim.
func SpecToYAML(specs []interfaces.ResourceSpec) ([]byte, error) {
	items := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		m := map[string]any{
			"name": s.Name,
			"type": s.Type,
		}
		if s.Size != "" {
			m["size"] = string(s.Size)
		}
		if s.Config != nil {
			m["config"] = s.Config
		}
		items = append(items, m)
	}
	return yaml.Marshal(items)
}
