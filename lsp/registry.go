// Package lsp implements a Language Server Protocol server for workflow
// configuration files, providing completions, diagnostics, and hover info.
package lsp

import (
	"github.com/GoCodeAlone/workflow/schema"
)

// ModuleTypeInfo holds metadata about a known module type for the LSP.
type ModuleTypeInfo struct {
	Type        string
	Label       string
	Category    string
	Description string
	ConfigKeys  []string
}

// StepTypeInfo holds metadata about a known step type for the LSP.
type StepTypeInfo struct {
	Type        string
	Description string
	ConfigKeys  []string
}

// TriggerTypeInfo holds metadata about a known trigger type.
type TriggerTypeInfo struct {
	Type        string
	Description string
}

// Registry aggregates all known workflow types for LSP use.
type Registry struct {
	ModuleTypes  map[string]ModuleTypeInfo
	StepTypes    map[string]StepTypeInfo
	TriggerTypes map[string]TriggerTypeInfo
	WorkflowTypes []string
}

// NewRegistry builds a Registry from the schema package's known types and registry.
func NewRegistry() *Registry {
	r := &Registry{
		ModuleTypes:  make(map[string]ModuleTypeInfo),
		StepTypes:    make(map[string]StepTypeInfo),
		TriggerTypes: make(map[string]TriggerTypeInfo),
		WorkflowTypes: schema.KnownWorkflowTypes(),
	}

	// Build module type info from ModuleSchemaRegistry.
	reg := schema.NewModuleSchemaRegistry()
	for _, ms := range reg.All() {
		keys := make([]string, 0, len(ms.ConfigFields))
		for i := range ms.ConfigFields {
			keys = append(keys, ms.ConfigFields[i].Key)
		}
		r.ModuleTypes[ms.Type] = ModuleTypeInfo{
			Type:        ms.Type,
			Label:       ms.Label,
			Category:    ms.Category,
			Description: ms.Description,
			ConfigKeys:  keys,
		}
	}

	// Supplement with types from KnownModuleTypes that aren't in the schema registry.
	for _, t := range schema.KnownModuleTypes() {
		if _, exists := r.ModuleTypes[t]; !exists {
			r.ModuleTypes[t] = ModuleTypeInfo{
				Type:        t,
				Description: t + " module",
			}
		}
	}

	// Build step type info.
	for t := range schema.KnownStepTypes() {
		r.StepTypes[t] = StepTypeInfo{
			Type:        t,
			Description: "Pipeline step: " + t,
		}
	}

	// Build trigger type info.
	for _, t := range schema.KnownTriggerTypes() {
		desc := map[string]string{
			"http":     "HTTP trigger: fires on incoming HTTP requests",
			"schedule": "Schedule trigger: fires on a cron schedule",
			"event":    "Event trigger: fires when a message is received on a topic",
			"eventbus": "Event bus trigger: fires for events on the internal event bus",
		}
		d := desc[t]
		if d == "" {
			d = t + " trigger"
		}
		r.TriggerTypes[t] = TriggerTypeInfo{
			Type:        t,
			Description: d,
		}
	}

	return r
}

// templateFunctions returns the list of template functions available in pipeline templates.
func templateFunctions() []string {
	return []string{
		"uuidv4",
		"uuid",
		"now",
		"lower",
		"default",
		"trimPrefix",
		"trimSuffix",
		"json",
		"step",
		"trigger",
	}
}
