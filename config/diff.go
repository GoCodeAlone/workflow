package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ModuleConfigDiff represents what changed between two configs.
type ModuleConfigDiff struct {
	Added     []ModuleConfig       // modules in new but not old
	Removed   []ModuleConfig       // modules in old but not new
	Modified  []ModuleConfigChange // modules present in both with different config
	Unchanged []string             // module names with no config change
}

// ModuleConfigChange represents a change to a single module's config.
type ModuleConfigChange struct {
	Name      string
	OldConfig map[string]any
	NewConfig map[string]any
}

// DiffModuleConfigs compares two configs and identifies module-level changes.
func DiffModuleConfigs(old, new *WorkflowConfig) *ModuleConfigDiff {
	diff := &ModuleConfigDiff{}

	oldMap := make(map[string]ModuleConfig)
	for _, m := range old.Modules {
		oldMap[m.Name] = m
	}
	newMap := make(map[string]ModuleConfig)
	for _, m := range new.Modules {
		newMap[m.Name] = m
	}

	for name, newMod := range newMap {
		oldMod, exists := oldMap[name]
		if !exists {
			diff.Added = append(diff.Added, newMod)
			continue
		}
		if hashModuleConfig(oldMod) != hashModuleConfig(newMod) {
			diff.Modified = append(diff.Modified, ModuleConfigChange{
				Name:      name,
				OldConfig: oldMod.Config,
				NewConfig: newMod.Config,
			})
		} else {
			diff.Unchanged = append(diff.Unchanged, name)
		}
	}

	for name, oldMod := range oldMap {
		if _, exists := newMap[name]; !exists {
			diff.Removed = append(diff.Removed, oldMod)
		}
	}

	return diff
}

// HasNonModuleChanges returns true if workflows, triggers, pipelines, or
// platform config changed between old and new (requiring full reload).
func HasNonModuleChanges(old, new *WorkflowConfig) bool {
	return hashAny(old.Workflows) != hashAny(new.Workflows) ||
		hashAny(old.Triggers) != hashAny(new.Triggers) ||
		hashAny(old.Pipelines) != hashAny(new.Pipelines) ||
		hashAny(old.Platform) != hashAny(new.Platform)
}

func hashModuleConfig(m ModuleConfig) string {
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Sprintf("error:%v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashAny(v any) string {
	if v == nil {
		return "nil"
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Sprintf("error:%v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
