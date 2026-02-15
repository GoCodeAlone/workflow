// Package schema provides JSON Schema generation and validation for workflow
// configuration files. It generates a JSON Schema from the known config
// structure and module types, and validates parsed configs against it.
package schema

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

// KnownModuleTypes returns all built-in module type identifiers recognized by
// the workflow engine's BuildFromConfig.
func KnownModuleTypes() []string {
	return []string{
		"api.command",
		"api.handler",
		"api.query",
		"auth.jwt",
		"auth.modular",
		"auth.user-store",
		"cache.modular",
		"chimux.router",
		"data.transformer",
		"database.modular",
		"database.workflow",
		"dynamic.component",
		"eventbus.modular",
		"eventlogger.modular",
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
		"httpclient.modular",
		"httpserver.modular",
		"jsonschema.modular",
		"log.collector",
		"messaging.broker",
		"messaging.broker.eventbus",
		"messaging.handler",
		"messaging.kafka",
		"messaging.nats",
		"metrics.collector",
		"notification.slack",
		"observability.otel",
		"openapi.consumer",
		"openapi.generator",
		"persistence.store",
		"processing.step",
		"reverseproxy",
		"scheduler.modular",
		"secrets.aws",
		"secrets.vault",
		"state.connector",
		"state.tracker",
		"statemachine.engine",
		"static.fileserver",
		"step.conditional",
		"step.http_call",
		"step.log",
		"step.publish",
		"step.set",
		"step.transform",
		"step.validate",
		"storage.gcs",
		"storage.local",
		"storage.s3",
		"storage.sqlite",
		"webhook.sender",
		"workflow.registry",
	}
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
				Description: "Module type identifier",
				Enum:        KnownModuleTypes(),
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
