package modernize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ManifestRule is a JSON-serializable modernize rule that external plugins can
// declare in their plugin.json manifest under the "modernizeRules" key. It
// supports the most common migration patterns: renaming module types, renaming
// step types, and renaming config keys within a specific module or step type.
//
// Example plugin.json snippet:
//
//	{
//	  "modernizeRules": [
//	    {
//	      "id": "myplugin-rename-type",
//	      "description": "Rename old.module.type to new.module.type",
//	      "severity": "error",
//	      "oldModuleType": "old.module.type",
//	      "newModuleType": "new.module.type"
//	    },
//	    {
//	      "id": "myplugin-rename-key",
//	      "description": "Rename old_key to new_key in my.module config",
//	      "severity": "warning",
//	      "moduleType": "my.module",
//	      "oldKey": "old_key",
//	      "newKey": "new_key"
//	    }
//	  ]
//	}
type ManifestRule struct {
	// ID is a unique, kebab-case identifier for the rule (e.g., "myplugin-rename-type").
	ID string `json:"id"`
	// Description is a human-readable summary of what the rule detects/fixes.
	Description string `json:"description"`
	// Severity is "error" or "warning" (default: "warning").
	Severity string `json:"severity,omitempty"`
	// Message overrides the auto-generated finding message.
	Message string `json:"message,omitempty"`

	// OldModuleType and NewModuleType trigger a module type rename rule.
	// When set, the rule detects any module with type == OldModuleType
	// and, when fixed, renames it to NewModuleType.
	OldModuleType string `json:"oldModuleType,omitempty"`
	NewModuleType string `json:"newModuleType,omitempty"`

	// OldStepType and NewStepType trigger a step type rename rule.
	// When set, the rule detects any pipeline step with type == OldStepType
	// and, when fixed, renames it to NewStepType.
	OldStepType string `json:"oldStepType,omitempty"`
	NewStepType string `json:"newStepType,omitempty"`

	// ModuleType and OldKey/NewKey trigger a module config key rename rule.
	// Detects modules of the given type that have OldKey in their config
	// and, when fixed, renames the key to NewKey.
	ModuleType string `json:"moduleType,omitempty"`

	// StepType and OldKey/NewKey trigger a step config key rename rule.
	// Detects steps of the given type that have OldKey in their config
	// and, when fixed, renames the key to NewKey.
	StepType string `json:"stepType,omitempty"`

	// OldKey is the config key to detect (used with ModuleType or StepType).
	OldKey string `json:"oldKey,omitempty"`
	// NewKey is the replacement config key (used with ModuleType or StepType).
	NewKey string `json:"newKey,omitempty"`
}

// Validate returns an error if the ManifestRule is misconfigured.
func (mr ManifestRule) Validate() error {
	if mr.ID == "" {
		return fmt.Errorf("modernize rule: id is required")
	}
	if mr.Description == "" {
		return fmt.Errorf("modernize rule %q: description is required", mr.ID)
	}
	// Exactly one rule kind must be configured.
	kinds := 0
	if mr.OldModuleType != "" {
		kinds++
		if mr.NewModuleType == "" {
			return fmt.Errorf("modernize rule %q: newModuleType is required when oldModuleType is set", mr.ID)
		}
	}
	if mr.OldStepType != "" {
		kinds++
		if mr.NewStepType == "" {
			return fmt.Errorf("modernize rule %q: newStepType is required when oldStepType is set", mr.ID)
		}
	}
	if mr.ModuleType != "" && mr.OldKey != "" {
		kinds++
		if mr.NewKey == "" {
			return fmt.Errorf("modernize rule %q: newKey is required when moduleType + oldKey is set", mr.ID)
		}
	}
	if mr.StepType != "" && mr.OldKey != "" {
		kinds++
		if mr.NewKey == "" {
			return fmt.Errorf("modernize rule %q: newKey is required when stepType + oldKey is set", mr.ID)
		}
	}
	if kinds == 0 {
		return fmt.Errorf("modernize rule %q: must specify at least one of: oldModuleType, oldStepType, or moduleType/stepType + oldKey", mr.ID)
	}
	if kinds > 1 {
		return fmt.Errorf("modernize rule %q: exactly one rule kind must be configured; found %d kinds set", mr.ID, kinds)
	}
	if mr.Severity != "" && mr.Severity != "error" && mr.Severity != "warning" {
		return fmt.Errorf("modernize rule %q: severity must be \"error\" or \"warning\", got %q", mr.ID, mr.Severity)
	}
	return nil
}

// ToRule converts a ManifestRule into a Rule with appropriate Check and Fix
// functions. Returns an error if the rule is misconfigured (see Validate).
func (mr ManifestRule) ToRule() (Rule, error) {
	if err := mr.Validate(); err != nil {
		return Rule{}, err
	}

	sev := mr.Severity
	if sev == "" {
		sev = "warning"
	}

	switch {
	case mr.OldModuleType != "":
		return mr.toModuleTypeRenameRule(sev), nil
	case mr.OldStepType != "":
		return mr.toStepTypeRenameRule(sev), nil
	case mr.ModuleType != "" && mr.OldKey != "":
		return mr.toModuleConfigKeyRenameRule(sev), nil
	default: // StepType + OldKey
		return mr.toStepConfigKeyRenameRule(sev), nil
	}
}

// MustToRule is like ToRule but panics on misconfiguration. It is intended for
// use in plugin initialisation code where a malformed rule is a programming error.
func (mr ManifestRule) MustToRule() Rule {
	r, err := mr.ToRule()
	if err != nil {
		panic(fmt.Sprintf("modernize.ManifestRule.MustToRule: %v", err))
	}
	return r
}

// toModuleTypeRenameRule builds a Rule that detects and renames a module type.
func (mr ManifestRule) toModuleTypeRenameRule(sev string) Rule {
	oldType := mr.OldModuleType
	newType := mr.NewModuleType
	id := mr.ID
	msg := mr.Message
	if msg == "" {
		msg = fmt.Sprintf("Module type %q is deprecated; use %q instead", oldType, newType)
	}
	return Rule{
		ID:          id,
		Description: mr.Description,
		Severity:    sev,
		Check: func(root *yaml.Node, _ []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode != nil && typeNode.Kind == yaml.ScalarNode && typeNode.Value == oldType {
					findings = append(findings, Finding{
						RuleID:  id,
						Line:    typeNode.Line,
						Message: msg,
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode != nil && typeNode.Kind == yaml.ScalarNode && typeNode.Value == oldType {
					changes = append(changes, Change{
						RuleID:      id,
						Line:        typeNode.Line,
						Description: fmt.Sprintf("Renamed module type %q -> %q", oldType, newType),
					})
					typeNode.Value = newType
				}
			})
			return changes
		},
	}
}

// toStepTypeRenameRule builds a Rule that detects and renames a step type.
func (mr ManifestRule) toStepTypeRenameRule(sev string) Rule {
	oldType := mr.OldStepType
	newType := mr.NewStepType
	id := mr.ID
	msg := mr.Message
	if msg == "" {
		msg = fmt.Sprintf("Step type %q is deprecated; use %q instead", oldType, newType)
	}
	return Rule{
		ID:          id,
		Description: mr.Description,
		Severity:    sev,
		Check: func(root *yaml.Node, _ []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, oldType, func(step *yaml.Node) {
				typeNode := findMapValue(step, "type")
				if typeNode != nil {
					findings = append(findings, Finding{
						RuleID:  id,
						Line:    typeNode.Line,
						Message: msg,
						Fixable: true,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachStepOfType(root, oldType, func(step *yaml.Node) {
				typeNode := findMapValue(step, "type")
				if typeNode != nil && typeNode.Kind == yaml.ScalarNode {
					changes = append(changes, Change{
						RuleID:      id,
						Line:        typeNode.Line,
						Description: fmt.Sprintf("Renamed step type %q -> %q", oldType, newType),
					})
					typeNode.Value = newType
				}
			})
			return changes
		},
	}
}

// toModuleConfigKeyRenameRule builds a Rule that renames a config key in
// modules of a specific type.
func (mr ManifestRule) toModuleConfigKeyRenameRule(sev string) Rule {
	moduleType := mr.ModuleType
	oldKey := mr.OldKey
	newKey := mr.NewKey
	id := mr.ID
	msg := mr.Message
	if msg == "" {
		msg = fmt.Sprintf("Config key %q is deprecated in %q modules; use %q instead", oldKey, moduleType, newKey)
	}
	msgCollision := fmt.Sprintf("Config key %q is deprecated in %q modules (cannot auto-rename: %q already exists)", oldKey, moduleType, newKey)
	return Rule{
		ID:          id,
		Description: mr.Description,
		Severity:    sev,
		Check: func(root *yaml.Node, _ []byte) []Finding {
			var findings []Finding
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode == nil || typeNode.Value != moduleType {
					return
				}
				cfg := findMapValue(mod, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				keyNode := findMapKeyNode(cfg, oldKey)
				if keyNode != nil {
					// If newKey already exists this is still a finding but not auto-fixable.
					fixable := findMapKeyNode(cfg, newKey) == nil
					findingMsg := msg
					if !fixable {
						findingMsg = msgCollision
					}
					findings = append(findings, Finding{
						RuleID:  id,
						Line:    keyNode.Line,
						Message: findingMsg,
						Fixable: fixable,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachModule(root, func(mod *yaml.Node) {
				typeNode := findMapValue(mod, "type")
				if typeNode == nil || typeNode.Value != moduleType {
					return
				}
				cfg := findMapValue(mod, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				keyNode := findMapKeyNode(cfg, oldKey)
				if keyNode == nil {
					return
				}
				// Skip rename if newKey already exists to avoid duplicate keys.
				if findMapKeyNode(cfg, newKey) != nil {
					return
				}
				changes = append(changes, Change{
					RuleID:      id,
					Line:        keyNode.Line,
					Description: fmt.Sprintf("Renamed config key %q -> %q in %q module", oldKey, newKey, moduleType),
				})
				keyNode.Value = newKey
			})
			return changes
		},
	}
}

// toStepConfigKeyRenameRule builds a Rule that renames a config key in
// steps of a specific type.
func (mr ManifestRule) toStepConfigKeyRenameRule(sev string) Rule {
	stepType := mr.StepType
	oldKey := mr.OldKey
	newKey := mr.NewKey
	id := mr.ID
	msg := mr.Message
	if msg == "" {
		msg = fmt.Sprintf("Config key %q is deprecated in %q steps; use %q instead", oldKey, stepType, newKey)
	}
	msgCollision := fmt.Sprintf("Config key %q is deprecated in %q steps (cannot auto-rename: %q already exists)", oldKey, stepType, newKey)
	return Rule{
		ID:          id,
		Description: mr.Description,
		Severity:    sev,
		Check: func(root *yaml.Node, _ []byte) []Finding {
			var findings []Finding
			forEachStepOfType(root, stepType, func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				keyNode := findMapKeyNode(cfg, oldKey)
				if keyNode != nil {
					// If newKey already exists this is still a finding but not auto-fixable.
					fixable := findMapKeyNode(cfg, newKey) == nil
					findingMsg := msg
					if !fixable {
						findingMsg = msgCollision
					}
					findings = append(findings, Finding{
						RuleID:  id,
						Line:    keyNode.Line,
						Message: findingMsg,
						Fixable: fixable,
					})
				}
			})
			return findings
		},
		Fix: func(root *yaml.Node) []Change {
			var changes []Change
			forEachStepOfType(root, stepType, func(step *yaml.Node) {
				cfg := findMapValue(step, "config")
				if cfg == nil || cfg.Kind != yaml.MappingNode {
					return
				}
				keyNode := findMapKeyNode(cfg, oldKey)
				if keyNode == nil {
					return
				}
				// Skip rename if newKey already exists to avoid duplicate keys.
				if findMapKeyNode(cfg, newKey) != nil {
					return
				}
				changes = append(changes, Change{
					RuleID:      id,
					Line:        keyNode.Line,
					Description: fmt.Sprintf("Renamed config key %q -> %q in %q step", oldKey, newKey, stepType),
				})
				keyNode.Value = newKey
			})
			return changes
		},
	}
}

// findMapKeyNode returns the key node (not value node) for a given key in a
// mapping node. Used when the key itself needs to be renamed.
func findMapKeyNode(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i] // return the key node, not the value node
		}
	}
	return nil
}

// pluginManifestModernize is the minimal subset of plugin.json used to load
// modernize rules. It avoids importing the plugin package to prevent cycles.
type pluginManifestModernize struct {
	ModernizeRules []ManifestRule `json:"modernizeRules"`
}

// LoadRulesFromDir scans pluginDir for subdirectories containing a plugin.json
// manifest with a "modernizeRules" field, converts each manifest rule to a Rule,
// and returns the combined slice. Subdirectories with missing or malformed
// manifests are silently skipped. Returns an error only if pluginDir cannot be
// read at all.
func LoadRulesFromDir(pluginDir string) ([]Rule, error) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("read plugin dir %q: %w", pluginDir, err)
	}

	var rules []Rule
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginDir, e.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath) //nolint:gosec // G304: path is within trusted plugin dir
		if err != nil {
			continue
		}
		var m pluginManifestModernize
		if err := json.Unmarshal(data, &m); err != nil {
			continue // malformed manifests are skipped
		}
		for i := range m.ModernizeRules {
			r, err := m.ModernizeRules[i].ToRule()
			if err != nil {
				continue // invalid rule descriptors are skipped
			}
			rules = append(rules, r)
		}
	}
	return rules, nil
}
