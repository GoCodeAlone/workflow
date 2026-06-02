// Package specgen serialises []interfaces.ResourceSpec to YAML in the
// resource-spec schema shape consumed by iac/specparse.ParseResourceSpecs.
//
// SpecToYAML is the inverse of specparse.ParseResourceSpecs: it emits the
// same field names ("name", "type", "config", "size", "depends_on", "hints")
// so that a re-parse round-trips without loss. Secret:// references in Config
// values are emitted verbatim — no expansion is performed.
package specgen

import (
	"github.com/GoCodeAlone/workflow/interfaces"
	"gopkg.in/yaml.v3"
)

// SpecToYAML marshals specs to YAML in the resource-spec schema.
// Each spec becomes a mapping with fields name, type, size (omitted when
// empty), config (omitted when nil), depends_on (omitted when empty), and
// hints (omitted when nil, with empty subfields omitted). Secret:// refs
// survive verbatim.
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
		if len(s.DependsOn) > 0 {
			m["depends_on"] = s.DependsOn
		}
		if s.Hints != nil {
			hints := map[string]any{}
			if s.Hints.CPU != "" {
				hints["cpu"] = s.Hints.CPU
			}
			if s.Hints.Memory != "" {
				hints["memory"] = s.Hints.Memory
			}
			if s.Hints.Storage != "" {
				hints["storage"] = s.Hints.Storage
			}
			m["hints"] = hints
		}
		items = append(items, m)
	}
	return yaml.Marshal(items)
}
