// Package schema provides JSON Schema generation and validation for workflow
// configuration files. It generates a JSON Schema from the known config
// structure and module types, and validates parsed configs against it.
package schema

import (
	"sort"
	"sync"
)

// dynamicModuleTypes holds module types registered at runtime by plugins.
var (
	dynamicMu          sync.RWMutex
	dynamicModuleTypes = make(map[string]bool)
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

// Schema represents a JSON Schema document.
type Schema struct {
	Schema      string             `json:"$schema"`
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	Type        string             `json:"type"`
	Required    []string           `json:"required,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	AdditionalP *bool              `json:"additionalProperties,omitempty"`
	AnyOf       []*Schema          `json:"anyOf,omitempty"`
	Default     any                `json:"default,omitempty"`
	MinItems    *int               `json:"minItems,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`
	Definitions map[string]*Schema `json:"$defs,omitempty"`
	Ref         string             `json:"$ref,omitempty"`
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
	"step.build_ui",
	"step.circuit_breaker",
	"step.conditional",
	"step.constraint_check",
	"step.db_exec",
	"step.db_query",
	"step.delegate",
	"step.deploy",
	"step.docker_build",
	"step.docker_push",
	"step.docker_run",
	"step.drift_check",
	"step.feature_flag",
	"step.ff_gate",
	"step.gate",
	"step.http_call",
	"step.jq",
	"step.json_response",
	"step.base64_decode",
	"step.log",
	"step.platform_apply",
	"step.platform_destroy",
	"step.platform_plan",
	"step.platform_template",
	"step.publish",
	"step.rate_limit",
	"step.request_parse",
	"step.scan_container",
	"step.scan_deps",
	"step.scan_sast",
	"step.set",
	"step.shell_exec",
	"step.sub_workflow",
	"step.transform",
	"step.validate",
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

// KnownTriggerTypes returns all built-in trigger type identifiers.
func KnownTriggerTypes() []string {
	return []string{
		"http",
		"schedule",
		"event",
		"eventbus",
	}
}

// KnownWorkflowTypes returns all built-in workflow handler type identifiers.
func KnownWorkflowTypes() []string {
	return []string{
		"event",
		"http",
		"messaging",
		"statemachine",
		"scheduler",
		"integration",
	}
}

// GenerateWorkflowSchema produces the full JSON Schema describing a valid
// WorkflowConfig YAML file.
func GenerateWorkflowSchema() *Schema {
	f := false
	one := 1

	moduleConfigSchema := &Schema{
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
				Enum:        NewModuleSchemaRegistry().Types(),
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
		AdditionalP: &f,
	}

	return &Schema{
		Schema:      "https://json-schema.org/draft/2020-12/schema",
		Title:       "Workflow Configuration",
		Description: "Schema for GoCodeAlone/workflow engine YAML configuration files",
		Type:        "object",
		Required:    []string{"modules"},
		Properties: map[string]*Schema{
			"modules": {
				Type:        "array",
				Description: "List of module definitions to instantiate",
				Items:       moduleConfigSchema,
				MinItems:    &one,
			},
			"workflows": {
				Type:        "object",
				Description: "Workflow handler configurations keyed by workflow type (e.g. http, messaging, statemachine, scheduler, integration)",
			},
			"triggers": {
				Type:        "object",
				Description: "Trigger configurations keyed by trigger type (e.g. http, schedule, event, eventbus)",
			},
		},
	}
}
