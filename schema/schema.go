// Package schema provides JSON Schema generation and validation for workflow
// configuration files. It generates a JSON Schema from the known config
// structure and module types, and validates parsed configs against it.
package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// dynamicModuleTypes holds module types registered at runtime by plugins.
var (
	dynamicMu          sync.RWMutex
	dynamicModuleTypes = make(map[string]bool)
)

// dynamicTriggerTypes holds trigger types registered at runtime by plugins.
var (
	dynamicTriggerMu    sync.RWMutex
	dynamicTriggerTypes = make(map[string]bool)
)

// dynamicWorkflowTypes holds workflow types registered at runtime by plugins.
var (
	dynamicWorkflowMu    sync.RWMutex
	dynamicWorkflowTypes = make(map[string]bool)
)

// RegisterModuleType registers a module type so it is recognized by KnownModuleTypes.
func RegisterModuleType(moduleType string) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	dynamicModuleTypes[moduleType] = true
}

// UnregisterModuleType removes a dynamically registered module type. Intended for testing.
func UnregisterModuleType(moduleType string) {
	dynamicMu.Lock()
	defer dynamicMu.Unlock()
	delete(dynamicModuleTypes, moduleType)
}

// RegisterTriggerType registers a trigger type so it is recognized by KnownTriggerTypes.
func RegisterTriggerType(triggerType string) {
	dynamicTriggerMu.Lock()
	defer dynamicTriggerMu.Unlock()
	dynamicTriggerTypes[triggerType] = true
}

// UnregisterTriggerType removes a dynamically registered trigger type. Intended for testing.
func UnregisterTriggerType(triggerType string) {
	dynamicTriggerMu.Lock()
	defer dynamicTriggerMu.Unlock()
	delete(dynamicTriggerTypes, triggerType)
}

// RegisterWorkflowType registers a workflow type so it is recognized by KnownWorkflowTypes.
func RegisterWorkflowType(workflowType string) {
	dynamicWorkflowMu.Lock()
	defer dynamicWorkflowMu.Unlock()
	dynamicWorkflowTypes[workflowType] = true
}

// UnregisterWorkflowType removes a dynamically registered workflow type. Intended for testing.
func UnregisterWorkflowType(workflowType string) {
	dynamicWorkflowMu.Lock()
	defer dynamicWorkflowMu.Unlock()
	delete(dynamicWorkflowTypes, workflowType)
}

// Schema represents a JSON Schema document.
type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	Title                string             `json:"title,omitempty"`
	Description          string             `json:"description,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	AdditionalProperties json.RawMessage    `json:"additionalProperties,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"`
	If                   *Schema            `json:"if,omitempty"`
	Then                 *Schema            `json:"then,omitempty"`
	Default              any                `json:"default,omitempty"`
	MinItems             *int               `json:"minItems,omitempty"`
	Minimum              *float64           `json:"minimum,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Definitions          map[string]*Schema `json:"$defs,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
}

// setAdditionalPropertiesBool sets additionalProperties to a boolean value.
func (s *Schema) setAdditionalPropertiesBool(v bool) {
	if v {
		s.AdditionalProperties = json.RawMessage(`true`)
	} else {
		s.AdditionalProperties = json.RawMessage(`false`)
	}
}

// configFieldDefToSchema converts a ConfigFieldDef to a JSON Schema property.
func configFieldDefToSchema(f ConfigFieldDef) *Schema {
	s := &Schema{
		Description: f.Description,
	}
	if f.DefaultValue != nil {
		s.Default = f.DefaultValue
	}
	switch f.Type {
	case FieldTypeString, FieldTypeDuration, FieldTypeFilePath, FieldTypeSQL:
		s.Type = "string"
	case FieldTypeNumber:
		s.Type = "number"
	case FieldTypeBool:
		s.Type = "boolean"
	case FieldTypeSelect:
		s.Type = "string"
		if len(f.Options) > 0 {
			s.Enum = f.Options
		}
	case FieldTypeArray:
		s.Type = "array"
		if f.ArrayItemType != "" {
			s.Items = &Schema{Type: f.ArrayItemType}
		} else {
			s.Items = &Schema{Type: "string"}
		}
	case FieldTypeMap, FieldTypeJSON:
		s.Type = "object"
	default:
		s.Type = "string"
	}
	return s
}

// coreModuleTypes is the hardcoded list of built-in module type identifiers
// recognized by the workflow engine's BuildFromConfig.
var coreModuleTypes = []string{
	"api.command",
	"api.gateway",
	"api.handler",
	"api.query",
	"auth.jwt",
	"auth.user-store",
	"cache.modular",
	"data.transformer",
	"database.workflow",
	"dlq.service",
	"dynamic.component",
	"eventstore.service",
	"featureflag.service",
	"health.checker",
	"http.handler",
	"http.middleware.auth",
	"http.middleware.cors",
	"http.middleware.logging",
	"http.middleware.ratelimit",
	"http.middleware.requestid",
	"http.middleware.securityheaders",
	"http.proxy",
	"http.router",
	"http.server",
	"http.simple_proxy",
	"jsonschema.modular",
	"license.validator",
	"log.collector",
	"messaging.broker",
	"messaging.broker.eventbus",
	"messaging.handler",
	"messaging.kafka",
	"messaging.nats",
	"metrics.collector",
	"notification.slack",
	"observability.otel",
	"openapi",
	"openapi.consumer",
	"openapi.generator",
	"persistence.store",
	"platform.context",
	"platform.provider",
	"platform.resource",
	"processing.step",
	"reverseproxy",
	"scheduler.modular",
	"secrets.aws",
	"secrets.vault",
	"state.connector",
	"state.tracker",
	"statemachine.engine",
	"static.fileserver",
	"step.ai_classify",
	"step.ai_complete",
	"step.ai_extract",
	"step.artifact_pull",
	"step.artifact_push",
	"step.base64_decode",
	"step.build_ui",
	"step.cache_delete",
	"step.cache_get",
	"step.cache_set",
	"step.circuit_breaker",
	"step.conditional",
	"step.constraint_check",
	"step.db_exec",
	"step.db_query",
	"step.delegate",
	"step.deploy",
	"step.dlq_replay",
	"step.dlq_send",
	"step.docker_build",
	"step.docker_push",
	"step.docker_run",
	"step.drift_check",
	"step.event_publish",
	"step.feature_flag",
	"step.ff_gate",
	"step.foreach",
	"step.gate",
	"step.http_call",
	"step.jq",
	"step.json_response",
	"step.log",
	"step.platform_apply",
	"step.platform_destroy",
	"step.platform_plan",
	"step.platform_template",
	"step.publish",
	"step.rate_limit",
	"step.request_parse",
	"step.resilient_circuit_breaker",
	"step.retry_with_backoff",
	"step.s3_upload",
	"step.scan_container",
	"step.scan_deps",
	"step.scan_sast",
	"step.set",
	"step.shell_exec",
	"step.sub_workflow",
	"step.transform",
	"step.ui_scaffold",
	"step.ui_scaffold_analyze",
	"step.validate",
	"step.validate_pagination",
	"step.validate_path_param",
	"step.validate_request_body",
	"step.webhook_verify",
	"step.workflow_call",
	"storage.gcs",
	"storage.local",
	"storage.s3",
	"storage.sqlite",
	"timeline.service",
	"webhook.sender",
	"workflow.registry",
}

// CoreModuleTypes returns only the hardcoded built-in module type identifiers.
// Use this when you need the original list without any plugin-provided types.
func CoreModuleTypes() []string {
	out := make([]string, len(coreModuleTypes))
	copy(out, coreModuleTypes)
	return out
}

// KnownModuleTypes returns all built-in module type identifiers plus any types
// registered at runtime by plugins. The result is sorted and deduplicated.
func KnownModuleTypes() []string {
	dynamicMu.RLock()
	defer dynamicMu.RUnlock()

	if len(dynamicModuleTypes) == 0 {
		out := make([]string, len(coreModuleTypes))
		copy(out, coreModuleTypes)
		return out
	}

	seen := make(map[string]bool, len(coreModuleTypes)+len(dynamicModuleTypes))
	for _, t := range coreModuleTypes {
		seen[t] = true
	}
	for t := range dynamicModuleTypes {
		seen[t] = true
	}

	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// KnownTriggerTypes returns all built-in trigger type identifiers plus any types
// registered at runtime by plugins. The result is sorted and deduplicated.
func KnownTriggerTypes() []string {
	core := []string{
		"http",
		"schedule",
		"event",
		"eventbus",
	}

	dynamicTriggerMu.RLock()
	defer dynamicTriggerMu.RUnlock()

	if len(dynamicTriggerTypes) == 0 {
		out := make([]string, len(core))
		copy(out, core)
		sort.Strings(out)
		return out
	}

	seen := make(map[string]bool, len(core)+len(dynamicTriggerTypes))
	for _, t := range core {
		seen[t] = true
	}
	for t := range dynamicTriggerTypes {
		seen[t] = true
	}

	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// KnownWorkflowTypes returns all built-in workflow handler type identifiers plus any types
// registered at runtime by plugins. The result is sorted and deduplicated.
func KnownWorkflowTypes() []string {
	core := []string{
		"event",
		"http",
		"messaging",
		"statemachine",
		"scheduler",
		"integration",
	}

	dynamicWorkflowMu.RLock()
	defer dynamicWorkflowMu.RUnlock()

	if len(dynamicWorkflowTypes) == 0 {
		out := make([]string, len(core))
		copy(out, core)
		sort.Strings(out)
		return out
	}

	seen := make(map[string]bool, len(core)+len(dynamicWorkflowTypes))
	for _, t := range core {
		seen[t] = true
	}
	for t := range dynamicWorkflowTypes {
		seen[t] = true
	}

	result := make([]string, 0, len(seen))
	for t := range seen {
		result = append(result, t)
	}
	sort.Strings(result)
	return result
}

// pluginManifestTypes holds the type declarations from a plugin.json manifest.
// This is a minimal subset of the full plugin manifest to avoid import cycles.
type pluginManifestTypes struct {
	ModuleTypes   []string `json:"moduleTypes"`
	StepTypes     []string `json:"stepTypes"`
	TriggerTypes  []string `json:"triggerTypes"`
	WorkflowTypes []string `json:"workflowTypes"`
}

// LoadPluginTypesFromDir scans pluginDir for subdirectories containing a
// plugin.json manifest, reads each manifest's type declarations, and registers
// them with the schema package so that they appear in all type listings and
// pass validation. Unknown or malformed manifests are silently skipped.
// Returns an error only if pluginDir cannot be read at all.
func LoadPluginTypesFromDir(pluginDir string) error {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return fmt.Errorf("read plugin dir %q: %w", pluginDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginDir, e.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath) //nolint:gosec // G304: path is within the trusted plugins directory
		if err != nil {
			continue
		}
		var m pluginManifestTypes
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		for _, t := range m.ModuleTypes {
			RegisterModuleType(t)
		}
		for _, t := range m.StepTypes {
			// Step types share the module type registry (identified by "step." prefix).
			RegisterModuleType(t)
		}
		for _, t := range m.TriggerTypes {
			RegisterTriggerType(t)
		}
		for _, t := range m.WorkflowTypes {
			RegisterWorkflowType(t)
		}
	}
	return nil
}

// moduleIfThen builds an if/then conditional schema for a specific module type
// that adds per-type config property validation.
func moduleIfThen(moduleType string, ms *ModuleSchema) *Schema {
	props := make(map[string]*Schema, len(ms.ConfigFields))
	required := make([]string, 0)
	for i := range ms.ConfigFields {
		f := &ms.ConfigFields[i]
		props[f.Key] = configFieldDefToSchema(*f)
		if f.Required {
			required = append(required, f.Key)
		}
	}
	configSchema := &Schema{
		Type:       "object",
		Properties: props,
	}
	configSchema.setAdditionalPropertiesBool(false)
	if len(required) > 0 {
		configSchema.Required = required
	}
	then := &Schema{
		Properties: map[string]*Schema{
			"config": configSchema,
		},
	}
	return &Schema{
		If: &Schema{
			Required: []string{"type"},
			Properties: map[string]*Schema{
				"type": {Enum: []string{moduleType}},
			},
		},
		Then: then,
	}
}

// GenerateWorkflowSchema produces the full JSON Schema describing a valid
// WorkflowConfig YAML file.
func GenerateWorkflowSchema() *Schema {
	one := 1
	reg := NewModuleSchemaRegistry()

	moduleBase := &Schema{
		Type:     "object",
		Required: []string{"name", "type"},
		Properties: map[string]*Schema{
			"name": {
				Type:        "string",
				Description: "Unique name for this module instance",
				Pattern:     "^[a-zA-Z][a-zA-Z0-9._-]*$",
			},
			"type": {
				Type:        "string",
				Description: "Module type identifier (built-in or plugin-provided)",
				Enum:        reg.Types(),
			},
			"config": {
				Type:        "object",
				Description: "Module-specific configuration key/value pairs",
			},
			"dependsOn": {
				Type:        "array",
				Description: "List of module names this module depends on",
				Items:       &Schema{Type: "string"},
			},
			"branches": {
				Type:        "object",
				Description: "Branch configuration for conditional routing",
			},
		},
	}
	moduleBase.setAdditionalPropertiesBool(false)

	// Build if/then conditionals per registered module type.
	allOf := make([]*Schema, 0, len(reg.schemas))
	types := reg.Types()
	for _, t := range types {
		ms := reg.Get(t)
		if ms == nil || len(ms.ConfigFields) == 0 {
			continue
		}
		allOf = append(allOf, moduleIfThen(t, ms))
	}
	if len(allOf) > 0 {
		moduleBase.AllOf = allOf
	}

	// Step schema — type enum built from KnownStepTypes.
	stepTypes := KnownStepTypes()
	stepTypeEnum := make([]string, 0, len(stepTypes))
	for t := range stepTypes {
		stepTypeEnum = append(stepTypeEnum, t)
	}
	sort.Strings(stepTypeEnum)

	stepSchema := &Schema{
		Type:     "object",
		Required: []string{"type"},
		Properties: map[string]*Schema{
			"type": {
				Type:        "string",
				Description: "Step type identifier",
				Enum:        stepTypeEnum,
			},
			"name": {Type: "string", Description: "Step name (used to reference output in later steps)"},
			"config": {
				Type:        "object",
				Description: "Step-specific configuration",
			},
			"dependsOn": {
				Type:  "array",
				Items: &Schema{Type: "string"},
			},
		},
	}

	// Build per-step if/then config conditionals from the registry.
	// TODO: register step config field schemas in ModuleSchemaRegistry so these
	// conditionals can enforce per-step config shapes (similar to module types).
	stepAllOf := make([]*Schema, 0)
	for _, t := range stepTypeEnum {
		ms := reg.Get(t)
		if ms == nil || len(ms.ConfigFields) == 0 {
			continue
		}
		stepAllOf = append(stepAllOf, moduleIfThen(t, ms))
	}
	if len(stepAllOf) > 0 {
		stepSchema.AllOf = stepAllOf
	}

	// Trigger schema — KnownTriggerTypes() returns a sorted []string.
	triggerEnum := KnownTriggerTypes()

	triggerSchema := &Schema{
		Type:        "object",
		Description: "Trigger configurations keyed by trigger type",
		Properties:  map[string]*Schema{},
	}
	for _, t := range triggerEnum {
		triggerSchema.Properties[t] = &Schema{
			Type:        "object",
			Description: "Configuration for the " + t + " trigger",
		}
	}
	triggerSchema.setAdditionalPropertiesBool(false)

	// Pipeline schema.
	pipelineSchema := &Schema{
		Type:        "object",
		Description: "Named pipeline definitions",
		Properties: map[string]*Schema{
			"trigger": {
				Type:        "object",
				Description: "Inline trigger definition for this pipeline",
				Properties: map[string]*Schema{
					"type": {
						Type:        "string",
						Description: "Trigger type",
						Enum:        triggerEnum,
					},
					"config": {Type: "object", Description: "Trigger-specific configuration"},
				},
			},
			"steps": {
				Type:        "array",
				Description: "Ordered list of pipeline steps",
				Items:       stepSchema,
			},
		},
	}

	root := &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		Title:       "Workflow Configuration",
		Description: "Schema for GoCodeAlone/workflow engine YAML configuration files",
		Type:        "object",
		Required:    []string{"modules"},
		Properties: map[string]*Schema{
			"modules": {
				Type:        "array",
				Description: "List of module definitions to instantiate",
				Items:       moduleBase,
				MinItems:    &one,
			},
			"workflows": {
				Type:        "object",
				Description: "Workflow handler configurations keyed by workflow type (e.g. http, messaging, statemachine, scheduler, integration)",
			},
			"triggers": triggerSchema,
			"pipelines": buildPipelinesSchema(pipelineSchema),
			"imports": {
				Type:        "array",
				Description: "List of external config files to import",
				Items:       &Schema{Type: "string"},
			},
			"requires": {
				Type:        "object",
				Description: "Plugin dependency declarations",
				Properties: map[string]*Schema{
					"plugins": {
						Type:  "array",
						Items: &Schema{Type: "string"},
					},
					"version": {Type: "string", Description: "Minimum engine version"},
				},
			},
			"platform": {
				Type:        "object",
				Description: "Platform-level configuration (kubernetes, cloud, etc.)",
			},
		},
	}

	return root
}

// KnownStepTypes returns all step type identifiers derived from KnownModuleTypes
// by filtering for types with the "step." prefix. This ensures the set is always
// complete and consistent with the module type registry.
func KnownStepTypes() map[string]bool {
	all := KnownModuleTypes()
	result := make(map[string]bool, 64)
	for _, t := range all {
		if len(t) > 5 && t[:5] == "step." {
			result[t] = true
		}
	}
	return result
}

// buildPipelinesSchema constructs the pipelines object schema using
// AdditionalProperties so that any pipeline name (arbitrary string key) is
// validated against pipelineSchema rather than creating a literal "*" property.
func buildPipelinesSchema(pipelineSchema *Schema) *Schema {
	raw, err := json.Marshal(pipelineSchema)
	if err != nil {
		// Fallback: allow any object if marshal fails (should never happen).
		s := &Schema{
			Type:        "object",
			Description: "Named pipeline definitions",
		}
		s.setAdditionalPropertiesBool(true)
		return s
	}
	return &Schema{
		Type:                 "object",
		Description:          "Named pipeline definitions",
		AdditionalProperties: json.RawMessage(raw),
	}
}

// GenerateApplicationSchema produces a JSON Schema for application-level configs.
func GenerateApplicationSchema() *Schema {
	workflowSchema := GenerateWorkflowSchema()
	return &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		Title:       "Application Configuration",
		Description: "Schema for GoCodeAlone/workflow application-level YAML configuration files",
		Type:        "object",
		Properties: map[string]*Schema{
			"name":    {Type: "string", Description: "Application name"},
			"version": {Type: "string", Description: "Application version"},
			"engine":  workflowSchema,
			"services": {
				Type:        "object",
				Description: "Named service configurations",
			},
		},
	}
}
