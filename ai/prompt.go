package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SystemPrompt returns the system prompt describing the workflow engine capabilities.
func SystemPrompt() string {
	return `You are an expert workflow engine architect. You generate YAML workflow configurations
and Go component code for the GoCodeAlone/workflow engine.

## Workflow Engine Overview

The engine builds workflows from YAML config with three top-level sections:
- modules: list of named, typed components with optional config and dependencies
- workflows: workflow definitions keyed by type (http, messaging, statemachine, event)
- triggers: trigger definitions keyed by type (http, schedule, event)

## Available Module Types

### HTTP Infrastructure
- http.server: HTTP server with address config
- http.router: HTTP router (depends on server)
- http.handler: Generic HTTP handler with contentType config
- api.handler: REST API handler with resourceName config
- http.middleware.auth: Auth middleware with secretKey/authType config
- http.middleware.logging: Logging middleware with logLevel config
- http.middleware.ratelimit: Rate limiter with requestsPerMinute and burstSize
- http.middleware.cors: CORS middleware with allowedOrigins and allowedMethods
- chimux.router: Chi-based HTTP router
- httpserver.modular: Modular HTTP server module
- httpclient.modular: HTTP client module for outbound requests

### Messaging
- messaging.broker: In-memory message broker
- messaging.handler: Message handler for topic subscriptions

### State Machine
- statemachine.engine: State machine engine
- state.tracker: State tracker for resources
- state.connector: Connects state machines to resources

### Events
- event.processor: Complex event pattern matching with bufferSize and cleanupInterval

### Infrastructure
- scheduler.modular: Cron-based scheduler
- eventbus.modular: Event bus for pub/sub
- eventlogger.modular: Event logger
- cache.modular: Cache module
- database.modular: Database module
- auth.modular: Auth module
- jsonschema.modular: JSON schema validation
- reverseproxy / http.proxy: Reverse proxy

## Workflow Types

### HTTP Workflow
Defines HTTP routes with handlers and optional middleware:
` + "```yaml" + `
workflows:
  http:
    routes:
      - method: GET
        path: /api/resource
        handler: handlerName
        middlewares:
          - authMiddleware
` + "```" + `

### Messaging Workflow
Defines topic subscriptions with handlers:
` + "```yaml" + `
workflows:
  messaging:
    subscriptions:
      - topic: events-topic
        handler: eventHandler
` + "```" + `

### State Machine Workflow
Defines states, transitions, and hooks:
` + "```yaml" + `
workflows:
  statemachine:
    engine: engineName
    definitions:
      - name: workflow-name
        initialState: "new"
        states:
          new:
            description: "Initial state"
            isFinal: false
        transitions:
          advance:
            fromState: "new"
            toState: "processing"
    hooks:
      - workflowType: "workflow-name"
        transitions: ["advance"]
        handler: "handlerName"
` + "```" + `

### Event Workflow
Defines complex event patterns and handlers:
` + "```yaml" + `
workflows:
  event:
    processor: processorName
    patterns:
      - patternId: "pattern-name"
        eventTypes: ["event.type.a", "event.type.b"]
        windowTime: "5m"
        condition: "count"  # or "sequence"
        minOccurs: 3
    handlers:
      - patternId: "pattern-name"
        handler: handlerName
` + "```" + `

## Trigger Types

### HTTP Trigger
Maps HTTP endpoints to workflow actions:
` + "```yaml" + `
triggers:
  http:
    routes:
      - path: "/api/workflows/action"
        method: "POST"
        workflow: "workflow-name"
        action: "action-name"
` + "```" + `

### Schedule Trigger
Cron-based workflow triggers:
` + "```yaml" + `
triggers:
  schedule:
    jobs:
      - cron: "0 * * * *"
        workflow: "workflow-name"
        action: "action-name"
        params:
          key: value
` + "```" + `

### Event Trigger
Event-based workflow triggers:
` + "```yaml" + `
triggers:
  event:
    subscriptions:
      - topic: "events-topic"
        event: "event.type"
        workflow: "workflow-name"
        action: "action-name"
` + "```" + `

## Go Component Interfaces

When generating custom components, implement these interfaces:

### modular.Module
` + "```go" + `
type Module interface {
    Name() string
    RegisterConfig(app Application)
    Init(app Application) error
}
` + "```" + `

### workflow.WorkflowHandler
` + "```go" + `
type WorkflowHandler interface {
    CanHandle(workflowType string) bool
    ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error
    ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error)
}
` + "```" + `

## Rules
1. Always output valid YAML for workflow configs.
2. Ensure module dependencies are declared with dependsOn.
3. Use existing module types when possible.
4. When a module type doesn't exist, specify it as a ComponentSpec so it can be generated.
5. All generated Go code must compile and implement the required interfaces.
6. Keep workflows focused and composable.`
}

// GeneratePrompt builds a workflow generation prompt from the request.
func GeneratePrompt(req GenerateRequest) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Generate a workflow configuration for the following request:\n\n"))
	b.WriteString(fmt.Sprintf("Intent: %s\n\n", req.Intent))

	if len(req.Context) > 0 {
		b.WriteString("Additional context:\n")
		for k, v := range req.Context {
			b.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
		}
		b.WriteString("\n")
	}

	if len(req.Constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, c := range req.Constraints {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
		b.WriteString("\n")
	}

	b.WriteString(`Respond with a JSON object containing:
1. "workflow": a complete WorkflowConfig with modules, workflows, and triggers sections
2. "components": an array of ComponentSpec objects for any modules that don't exist as built-in types
3. "explanation": a brief explanation of how the workflow operates

For each component in "components", include:
- "name": the module name
- "type": the module type string
- "description": what the component does
- "interface": which Go interface it implements (e.g., "modular.Module")
- "goCode": compilable Go source code for the component
`)
	return b.String()
}

// ComponentFormat specifies the target format for generated component code.
type ComponentFormat string

const (
	// FormatModule generates code implementing the modular.Module interface (struct-based).
	FormatModule ComponentFormat = "module"
	// FormatDynamic generates code as a flat package with exported functions
	// compatible with the Yaegi dynamic interpreter.
	FormatDynamic ComponentFormat = "dynamic"
)

// ComponentPrompt builds a prompt for generating a single component.
func ComponentPrompt(spec ComponentSpec) string {
	return fmt.Sprintf(`Generate Go source code for a workflow component with the following specification:

Name: %s
Type: %s
Description: %s
Interface: %s

Requirements:
1. The code must compile and implement the %s interface.
2. Include the package declaration.
3. Include all necessary imports.
4. Add meaningful error handling.
5. Follow Go conventions and best practices.

Return only the Go source code, no explanation.`, spec.Name, spec.Type, spec.Description, spec.Interface, spec.Interface)
}

// DynamicComponentPrompt builds a prompt for generating a component in dynamic format.
// Dynamic components are flat packages with exported functions that can be loaded
// by the Yaegi interpreter at runtime.
func DynamicComponentPrompt(spec ComponentSpec) string {
	return fmt.Sprintf(`Generate Go source code for a dynamic workflow component with the following specification:

Name: %s
Type: %s
Description: %s

The code MUST follow this exact format for the Yaegi dynamic interpreter:

1. Package must be "package component"
2. Only use standard library imports from this allowed list:
   fmt, strings, strconv, encoding/json, encoding/xml, encoding/csv,
   encoding/base64, context, time, math, math/rand, sort, sync,
   sync/atomic, errors, io, bytes, bufio, unicode, unicode/utf8,
   regexp, path, net/url, net/http, log, maps, slices,
   crypto/sha256, crypto/hmac, crypto/md5, hash, html,
   html/template, text/template
3. NO third-party imports (no github.com/... packages)
4. Must have these exported functions:
   - Name() string - returns %q
   - Init(map[string]interface{}) error - initialization with service map
   - Execute(context.Context, map[string]interface{}) (map[string]interface{}, error) - main logic
5. Optionally include:
   - Start(context.Context) error
   - Stop(context.Context) error

Example structure:
` + "```go" + `
package component

import (
    "context"
    "fmt"
)

func Name() string {
    return "component-name"
}

func Init(services map[string]interface{}) error {
    return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
    // Component logic here
    return map[string]interface{}{"result": "value"}, nil
}
` + "```" + `

Return only the Go source code, no explanation.`, spec.Name, spec.Type, spec.Description, spec.Name)
}

// SuggestPrompt builds a prompt for workflow suggestions.
func SuggestPrompt(useCase string) string {
	return fmt.Sprintf(`Suggest workflow configurations for the following use case:

%s

Return a JSON array of workflow suggestions, each containing:
- "name": a short name for the workflow
- "description": what the workflow does
- "config": a complete WorkflowConfig (with modules, workflows, triggers)
- "confidence": a number between 0 and 1 indicating how well this matches the use case

Provide 1-3 suggestions, ordered by confidence (highest first).`, useCase)
}

// MissingComponentsPrompt builds a prompt for identifying missing components.
func MissingComponentsPrompt(moduleTypes []string) string {
	return fmt.Sprintf(`Given a workflow config that references the following module types:

%s

Identify which module types are NOT built-in and would need custom Go implementations.

The built-in module types are:
- http.server, http.router, http.handler, api.handler
- http.middleware.auth, http.middleware.logging, http.middleware.ratelimit, http.middleware.cors
- messaging.broker, messaging.handler
- statemachine.engine, state.tracker, state.connector
- event.processor
- httpserver.modular, httpclient.modular, chimux.router
- scheduler.modular, eventbus.modular, eventlogger.modular
- cache.modular, database.modular, auth.modular, jsonschema.modular
- reverseproxy, http.proxy

For each non-built-in type, return a JSON array of ComponentSpec objects with name, type, description, and interface fields.
Leave goCode empty - it will be generated separately.`, strings.Join(moduleTypes, "\n- "))
}

// LoadExampleConfigs reads example YAML files from the given directory
// and returns them as a map of filename to content.
func LoadExampleConfigs(exampleDir string) (map[string]string, error) {
	examples := make(map[string]string)

	entries, err := os.ReadDir(exampleDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read example directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(exampleDir, entry.Name()))
		if err != nil {
			continue // skip unreadable files
		}
		examples[entry.Name()] = string(data)
	}
	return examples, nil
}

// ExamplePromptSection builds a prompt section with example configs.
func ExamplePromptSection(examples map[string]string) string {
	if len(examples) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n## Example Configurations\n\n")
	for name, content := range examples {
		b.WriteString(fmt.Sprintf("### %s\n```yaml\n%s\n```\n\n", name, content))
	}
	return b.String()
}
