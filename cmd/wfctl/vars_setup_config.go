package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
)

type configVariableEntry struct {
	Name        string
	Key         string
	Required    bool
	Default     string
	Description string
}

func collectConfigVariablesFromFile(path string) ([]configVariableEntry, []string, error) {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return nil, nil, err
	}
	return collectConfigVariables(cfg), collectSensitiveConfigVariables(cfg), nil
}

func collectConfigVariables(cfg *config.WorkflowConfig) []configVariableEntry {
	if cfg == nil {
		return nil
	}
	entriesByName := map[string]configVariableEntry{}
	for _, mod := range cfg.Modules {
		if mod.Type != "config.provider" || mod.Config == nil {
			continue
		}
		schema, err := schemaEntriesFromConfigProvider(mod)
		if err != nil {
			continue
		}
		prefixes := envSourcePrefixes(mod.Config)
		if len(prefixes) == 0 {
			continue
		}
		for key, entry := range schema {
			if entry.Sensitive || strings.TrimSpace(entry.Env) == "" {
				continue
			}
			for _, prefix := range prefixes {
				name := prefix + strings.TrimSpace(entry.Env)
				if name == "" {
					continue
				}
				entriesByName[name] = configVariableEntry{
					Name:        name,
					Key:         mod.Name + "." + key,
					Required:    entry.Required,
					Default:     entry.Default,
					Description: entry.Desc,
				}
			}
		}
	}
	return sortedConfigVariableEntries(entriesByName)
}

func collectSensitiveConfigVariables(cfg *config.WorkflowConfig) []string {
	if cfg == nil {
		return nil
	}
	names := map[string]struct{}{}
	for _, mod := range cfg.Modules {
		if mod.Type != "config.provider" || mod.Config == nil {
			continue
		}
		schema, err := schemaEntriesFromConfigProvider(mod)
		if err != nil {
			continue
		}
		prefixes := envSourcePrefixes(mod.Config)
		if len(prefixes) == 0 {
			continue
		}
		for _, entry := range schema {
			if !entry.Sensitive || strings.TrimSpace(entry.Env) == "" {
				continue
			}
			for _, prefix := range prefixes {
				names[prefix+strings.TrimSpace(entry.Env)] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func schemaEntriesFromConfigProvider(mod config.ModuleConfig) (map[string]module.SchemaEntry, error) {
	raw, ok := mod.Config["schema"].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("config.provider %q has no schema map", mod.Name)
	}
	return module.ParseSchema(raw)
}

func envSourcePrefixes(cfg map[string]any) []string {
	rawSources, ok := cfg["sources"].([]any)
	if !ok || len(rawSources) == 0 {
		return []string{""}
	}
	prefixes := map[string]struct{}{}
	hasEnvSource := false
	for _, raw := range rawSources {
		src, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		srcType, _ := src["type"].(string)
		if srcType != "env" {
			continue
		}
		hasEnvSource = true
		prefix, _ := src["prefix"].(string)
		prefixes[prefix] = struct{}{}
	}
	if !hasEnvSource {
		return nil
	}
	out := make([]string, 0, len(prefixes))
	for prefix := range prefixes {
		out = append(out, prefix)
	}
	sort.Strings(out)
	return out
}

func sortedConfigVariableEntries(entries map[string]configVariableEntry) []configVariableEntry {
	out := make([]configVariableEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}
