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
	return collectConfigVariables(cfg)
}

func collectConfigVariables(cfg *config.WorkflowConfig) ([]configVariableEntry, []string, error) {
	if cfg == nil {
		return nil, nil, nil
	}
	entriesByName := map[string]configVariableEntry{}
	sensitiveByName := map[string]struct{}{}
	addVariableConfigEntries(entriesByName, cfg.Vars)
	addVariableConfigEntries(entriesByName, cfg.Variables)
	for _, mod := range cfg.Modules {
		if mod.Type != "config.provider" || mod.Config == nil {
			continue
		}
		schema, err := schemaEntriesFromConfigProvider(mod)
		if err != nil {
			return nil, nil, err
		}
		prefixes := envSourcePrefixes(mod.Config)
		if len(prefixes) == 0 {
			continue
		}
		for key, entry := range schema {
			if strings.TrimSpace(entry.Env) == "" {
				continue
			}
			for _, prefix := range prefixes {
				name := prefix + strings.TrimSpace(entry.Env)
				if name == "" {
					continue
				}
				if entry.Sensitive {
					sensitiveByName[name] = struct{}{}
					continue
				}
				if _, exists := entriesByName[name]; exists {
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
	return sortedConfigVariableEntries(entriesByName), sortedStringSet(sensitiveByName), nil
}

func addVariableConfigEntries(entries map[string]configVariableEntry, vars *config.VariablesConfig) {
	if vars == nil {
		return
	}
	for _, entry := range vars.Entries {
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			continue
		}
		if _, exists := entries[name]; exists {
			continue
		}
		entries[name] = configVariableEntry{
			Name:        name,
			Key:         "vars." + name,
			Required:    entry.Required,
			Default:     entry.Default,
			Description: entry.Description,
		}
	}
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
		return nil
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

func sortedStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
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
