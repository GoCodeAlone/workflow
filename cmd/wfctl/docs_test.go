package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

// writeJSON writes an indented JSON representation of v to the given path.
// Used only in tests to create plugin.json fixtures.
func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// --- test config strings ---

const docsMinimalConfig = `
modules:
  - name: api-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
    dependsOn: [api-server]

workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: users-handler
      - method: POST
        path: /api/users
        handler: users-handler

triggers:
  http:
    server: api-server
`

const docsFullConfig = `
requires:
  plugins:
    - name: workflow-plugin-http
    - name: workflow-plugin-authz
      version: ">=1.0.0"
  capabilities:
    - authorization
    - http-serving

modules:
  - name: api-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
    dependsOn: [api-server]
  - name: auth-middleware
    type: auth.jwt
    dependsOn: [api-router]
    config:
      issuer: "https://auth.example.com"
  - name: order-handler
    type: http.handler
    dependsOn: [api-router]
    config:
      contentType: application/json
  - name: order-broker
    type: messaging.broker
    dependsOn: [order-handler]
  - name: order-state
    type: statemachine.engine
    dependsOn: [order-broker]
  - name: order-metrics
    type: metrics.collector
  - name: order-db
    type: storage.sqlite
    config:
      path: data/orders.db

workflows:
  http:
    routes:
      - method: GET
        path: /api/orders
        handler: order-handler
        middlewares:
          - auth-middleware
      - method: POST
        path: /api/orders
        handler: order-handler
        middlewares:
          - auth-middleware
      - method: GET
        path: /health
        handler: health-handler

  messaging:
    subscriptions:
      - topic: order.created
        handler: order-handler
      - topic: order.completed
        handler: notification-handler
    producers:
      - name: order-handler
        forwardTo:
          - order.created
          - order.updated

  statemachine:
    engine: order-state
    definitions:
      - name: order-lifecycle
        description: "Manages order state transitions"
        initialState: pending
        states:
          pending:
            description: "Order received"
            isFinal: false
            isError: false
          confirmed:
            description: "Order confirmed"
            isFinal: false
            isError: false
          shipped:
            description: "Order shipped"
            isFinal: false
            isError: false
          delivered:
            description: "Order delivered"
            isFinal: true
            isError: false
          cancelled:
            description: "Order cancelled"
            isFinal: true
            isError: true
        transitions:
          confirm:
            fromState: pending
            toState: confirmed
          ship:
            fromState: confirmed
            toState: shipped
          deliver:
            fromState: shipped
            toState: delivered
          cancel:
            fromState: pending
            toState: cancelled

triggers:
  http:
    server: api-server

pipelines:
  validate-order:
    trigger:
      type: http
      config:
        path: /api/orders/validate
        method: POST
    steps:
      - name: validate-payload
        type: step.validate
        config:
          strategy: required_fields
          required_fields:
            - customer_id
            - items
      - name: check-inventory
        type: step.http_call
        config:
          url: "http://inventory-service/check"
      - name: respond
        type: step.json_response
        config:
          status: 200
    on_error: stop
    timeout: 30s

  process-payment:
    trigger:
      type: http
      config:
        path: /api/payments
        method: POST
    steps:
      - name: validate
        type: step.validate
        config:
          strategy: json_schema
      - name: charge
        type: step.http_call
        config:
          url: "http://payment-gateway/charge"
      - name: record
        type: step.log
        config:
          level: info
          message: "Payment processed"
    compensation:
      - name: refund
        type: step.http_call
        config:
          url: "http://payment-gateway/refund"
    timeout: 60s

sidecars:
  - name: redis-cache
    type: redis
    config:
      port: 6379
  - name: jaeger-agent
    type: jaeger
    config:
      port: 6831
`

func writeDocsConfigFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
	return path
}

func writePluginJSON(t *testing.T, dir string, manifest *plugin.PluginManifest) string {
	t.Helper()
	pDir := filepath.Join(dir, manifest.Name)
	if err := os.MkdirAll(pDir, 0750); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	path := filepath.Join(pDir, "plugin.json")
	if err := writeJSON(path, manifest); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}
	return pDir
}

// --- Command-level tests ---

func TestRunDocsNoSubcommand(t *testing.T) {
	err := runDocs([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunDocsUnknownSubcommand(t *testing.T) {
	err := runDocs([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunDocsGenerateNoConfig(t *testing.T) {
	err := runDocsGenerate([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunDocsGenerateMinimal(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsMinimalConfig)
	outDir := filepath.Join(dir, "docs")

	err := runDocsGenerate([]string{"-output", outDir, cfgPath})
	if err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	// Should create README.md, modules.md, workflows.md, architecture.md
	for _, f := range []string{"README.md", "modules.md", "workflows.md", "architecture.md"} {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}

	// pipelines.md should NOT be created (no pipelines in config)
	if _, err := os.Stat(filepath.Join(outDir, "pipelines.md")); err == nil {
		t.Error("pipelines.md should not be created when no pipelines exist")
	}

	// plugins.md should NOT be created (no plugin-dir)
	if _, err := os.Stat(filepath.Join(outDir, "plugins.md")); err == nil {
		t.Error("plugins.md should not be created without -plugin-dir")
	}
}

func TestRunDocsGenerateFull(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	// Create plugin manifests
	pluginDir := filepath.Join(dir, "plugins")
	writePluginJSON(t, pluginDir, &plugin.PluginManifest{
		Name:        "workflow-plugin-authz",
		Version:     "1.2.0",
		Author:      "GoCodeAlone",
		Description: "Authorization plugin for workflow engine",
		License:     "MIT",
		Repository:  "https://github.com/GoCodeAlone/workflow-plugin-authz",
		Tier:        plugin.TierCommunity,
		ModuleTypes: []string{"authz.policy", "authz.enforcer"},
		StepTypes:   []string{"step.authz_check", "step.authz_grant"},
		Tags:        []string{"authorization", "rbac", "security"},
		Dependencies: []plugin.Dependency{
			{Name: "workflow-plugin-http", Constraint: ">=1.0.0"},
		},
		Capabilities: []plugin.CapabilityDecl{
			{Name: "authorization", Role: "provider", Priority: 10},
		},
	})

	err := runDocsGenerate([]string{"-output", outDir, "-plugin-dir", pluginDir, cfgPath})
	if err != nil {
		t.Fatalf("docs generate (full) failed: %v", err)
	}

	// All files should be created
	for _, f := range []string{"README.md", "modules.md", "pipelines.md", "workflows.md", "plugins.md", "architecture.md"} {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}
}

func TestDocsReadmeContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	if err := runDocsGenerate([]string{"-output", outDir, "-title", "My Order Service", cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	content := string(data)

	checks := []string{
		"# My Order Service",
		"Modules",
		"Workflows",
		"Pipelines",
		"workflow-plugin-http",
		"workflow-plugin-authz",
		"modules.md",
		"pipelines.md",
		"workflows.md",
		"architecture.md",
		"authorization",
		"Sidecars",
		"redis-cache",
		"jaeger-agent",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("README.md should contain %q", check)
		}
	}
}

func TestDocsModulesContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	if err := runDocsGenerate([]string{"-output", outDir, cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "modules.md"))
	if err != nil {
		t.Fatalf("failed to read modules.md: %v", err)
	}
	content := string(data)

	// Check module inventory
	for _, mod := range []string{"api-server", "api-router", "auth-middleware", "order-handler", "order-broker", "order-state", "order-metrics", "order-db"} {
		if !strings.Contains(content, mod) {
			t.Errorf("modules.md should contain module %q", mod)
		}
	}

	// Check dependency graph with mermaid
	if !strings.Contains(content, "```mermaid") {
		t.Error("modules.md should contain a mermaid diagram")
	}
	if !strings.Contains(content, "graph LR") {
		t.Error("modules.md should contain a graph LR diagram")
	}
}

func TestDocsPipelinesContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	if err := runDocsGenerate([]string{"-output", outDir, cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "pipelines.md"))
	if err != nil {
		t.Fatalf("failed to read pipelines.md: %v", err)
	}
	content := string(data)

	// Check pipeline names
	if !strings.Contains(content, "validate-order") {
		t.Error("pipelines.md should contain validate-order pipeline")
	}
	if !strings.Contains(content, "process-payment") {
		t.Error("pipelines.md should contain process-payment pipeline")
	}

	// Check mermaid workflow diagram
	if !strings.Contains(content, "```mermaid") {
		t.Error("pipelines.md should contain mermaid diagrams")
	}
	if !strings.Contains(content, "graph TD") {
		t.Error("pipelines.md should contain workflow diagrams")
	}

	// Check steps
	if !strings.Contains(content, "validate-payload") {
		t.Error("pipelines.md should list step names")
	}
	if !strings.Contains(content, "step.validate") {
		t.Error("pipelines.md should list step types")
	}

	// Check compensation steps
	if !strings.Contains(content, "Compensation") {
		t.Error("pipelines.md should document compensation steps")
	}
}

func TestDocsWorkflowsContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	if err := runDocsGenerate([]string{"-output", outDir, cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "workflows.md"))
	if err != nil {
		t.Fatalf("failed to read workflows.md: %v", err)
	}
	content := string(data)

	// HTTP routes
	if !strings.Contains(content, "/api/orders") {
		t.Error("workflows.md should contain HTTP routes")
	}
	if !strings.Contains(content, "GET") {
		t.Error("workflows.md should contain HTTP methods")
	}
	if !strings.Contains(content, "auth-middleware") {
		t.Error("workflows.md should show middlewares")
	}

	// Messaging
	if !strings.Contains(content, "order.created") {
		t.Error("workflows.md should contain messaging topics")
	}

	// State machine
	if !strings.Contains(content, "stateDiagram-v2") {
		t.Error("workflows.md should contain state diagram")
	}
	if !strings.Contains(content, "pending") {
		t.Error("workflows.md should show state machine states")
	}
	if !strings.Contains(content, "delivered") {
		t.Error("workflows.md should show final states")
	}
}

func TestDocsPluginsContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsMinimalConfig)
	outDir := filepath.Join(dir, "docs")
	pluginDir := filepath.Join(dir, "plugins")

	writePluginJSON(t, pluginDir, &plugin.PluginManifest{
		Name:        "workflow-plugin-authz",
		Version:     "1.2.0",
		Author:      "GoCodeAlone",
		Description: "Authorization plugin",
		License:     "MIT",
		Repository:  "https://github.com/GoCodeAlone/workflow-plugin-authz",
		Tier:        plugin.TierCommunity,
		ModuleTypes: []string{"authz.policy"},
		StepTypes:   []string{"step.authz_check"},
		Tags:        []string{"authorization"},
	})

	if err := runDocsGenerate([]string{"-output", outDir, "-plugin-dir", pluginDir, cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "plugins.md"))
	if err != nil {
		t.Fatalf("failed to read plugins.md: %v", err)
	}
	content := string(data)

	checks := []string{
		"workflow-plugin-authz",
		"1.2.0",
		"GoCodeAlone",
		"Authorization plugin",
		"MIT",
		"authz.policy",
		"step.authz_check",
		"authorization",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("plugins.md should contain %q", check)
		}
	}
}

func TestDocsArchitectureContent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsFullConfig)
	outDir := filepath.Join(dir, "docs")

	pluginDir := filepath.Join(dir, "plugins")
	writePluginJSON(t, pluginDir, &plugin.PluginManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "test",
		Description: "test plugin",
		ModuleTypes: []string{"test.module"},
		StepTypes:   []string{"step.test"},
	})

	if err := runDocsGenerate([]string{"-output", outDir, "-plugin-dir", pluginDir, cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "architecture.md"))
	if err != nil {
		t.Fatalf("failed to read architecture.md: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "```mermaid") {
		t.Error("architecture.md should contain mermaid diagrams")
	}
	if !strings.Contains(content, "graph TB") {
		t.Error("architecture.md should contain architecture diagram")
	}
	if !strings.Contains(content, "subgraph") {
		t.Error("architecture.md should use subgraphs for layers")
	}
	if !strings.Contains(content, "Plugin Architecture") {
		t.Error("architecture.md should contain plugin architecture section when plugins loaded")
	}
}

// --- Mermaid quoting tests ---

func TestMermaidQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"has space", `"has space"`},
		{"has-dash", `"has-dash"`},
		{"has.dot", `"has.dot"`},
		{"has/slash", `"has/slash"`},
		{"(parens)", `"(parens)"`},
		{"special#chars", `"special#chars"`},
		{`has"quote`, `"has#quot;quote"`},
		{"", `""`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mermaidQuote(tt.input)
			if got != tt.expected {
				t.Errorf("mermaidQuote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMermaidID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"has-dash", "has_dash"},
		{"has.dot", "has_dot"},
		{"has space", "has_space"},
		{"CamelCase", "CamelCase"},
		{"with_underscore", "with_underscore"},
		{"123numeric", "123numeric"},
		{"", "_empty"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mermaidID(tt.input)
			if got != tt.expected {
				t.Errorf("mermaidID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// --- Helper tests ---

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"api-server-config.yaml", "Api Server Config"},
		{"simple_workflow.yml", "Simple Workflow"},
		{"config.yaml", "Config"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := deriveTitle(tt.input)
			if got != tt.expected {
				t.Errorf("deriveTitle(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestClassifyModuleLayer(t *testing.T) {
	tests := []struct {
		modType  string
		expected string
	}{
		{"http.server", "HTTP"},
		{"http.router", "HTTP"},
		{"http.handler", "HTTP"},
		{"messaging.broker", "Messaging"},
		{"event.processor", "Messaging"},
		{"statemachine.engine", "State Management"},
		{"state.tracker", "State Management"},
		{"storage.sqlite", "Storage"},
		{"metrics.collector", "Observability"},
		{"health.checker", "Observability"},
		{"data.transformer", "Processing"},
		{"auth.jwt", "Processing"},
		{"unknown.type", "Other"},
	}

	for _, tt := range tests {
		t.Run(tt.modType, func(t *testing.T) {
			got := classifyModuleLayer(tt.modType)
			if got != tt.expected {
				t.Errorf("classifyModuleLayer(%q) = %q, want %q", tt.modType, got, tt.expected)
			}
		})
	}
}

func TestExtractSteps(t *testing.T) {
	pMap := map[string]any{
		"steps": []any{
			map[string]any{"name": "step1", "type": "step.validate"},
			map[string]any{"name": "step2", "type": "step.log"},
		},
	}

	steps := extractSteps(pMap, "steps")
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].name != "step1" || steps[0].typ != "step.validate" {
		t.Errorf("unexpected step[0]: %+v", steps[0])
	}
	if steps[1].name != "step2" || steps[1].typ != "step.log" {
		t.Errorf("unexpected step[1]: %+v", steps[1])
	}
}

func TestExtractStepsMissing(t *testing.T) {
	pMap := map[string]any{}
	steps := extractSteps(pMap, "steps")
	if len(steps) != 0 {
		t.Errorf("expected 0 steps from empty map, got %d", len(steps))
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"TRUE", true},
		{"false", false},
		{nil, false},
		{42, false},
	}

	for _, tt := range tests {
		got := toBool(tt.input)
		if got != tt.expected {
			t.Errorf("toBool(%v) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestLoadPluginManifestsEmptyDir(t *testing.T) {
	dir := t.TempDir()
	manifests, err := loadPluginManifests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("expected 0 manifests from empty dir, got %d", len(manifests))
	}
}

func TestLoadPluginManifestsWithPlugins(t *testing.T) {
	dir := t.TempDir()

	writePluginJSON(t, dir, &plugin.PluginManifest{
		Name:    "plugin-a",
		Version: "1.0.0",
		Author:  "test",
	})
	writePluginJSON(t, dir, &plugin.PluginManifest{
		Name:    "plugin-b",
		Version: "2.0.0",
		Author:  "test",
	})

	manifests, err := loadPluginManifests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 2 {
		t.Errorf("expected 2 manifests, got %d", len(manifests))
	}
}

func TestDocsCustomTitle(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeDocsConfigFile(t, dir, docsMinimalConfig)
	outDir := filepath.Join(dir, "docs")

	if err := runDocsGenerate([]string{"-output", outDir, "-title", "Custom App", cfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	if !strings.Contains(string(data), "# Custom App") {
		t.Error("README.md should use custom title")
	}
}

// --- ApplicationConfig (multi-workflow) tests ---

// docsAPIWorkflowConfig is a workflow config for the "api" service.
const docsAPIWorkflowConfig = `
modules:
  - name: api-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
    dependsOn: [api-server]
  - name: auth-middleware
    type: auth.jwt
    dependsOn: [api-router]
    config:
      issuer: "https://auth.example.com"
  - name: user-handler
    type: http.handler
    dependsOn: [api-router]
    config:
      contentType: application/json

workflows:
  http:
    routes:
      - method: GET
        path: /api/users
        handler: user-handler
        middlewares:
          - auth-middleware
      - method: POST
        path: /api/users
        handler: user-handler

triggers:
  http:
    server: api-server

pipelines:
  create-user:
    trigger:
      type: http
      config:
        path: /api/users
        method: POST
    steps:
      - name: validate-input
        type: step.validate
        config:
          strategy: required_fields
          required_fields:
            - username
            - email
      - name: save-user
        type: step.http_call
        config:
          url: "http://user-store/save"
      - name: respond
        type: step.json_response
        config:
          status: 201
    timeout: 30s
`

// docsJobsWorkflowConfig is a workflow config for the "jobs" service.
const docsJobsWorkflowConfig = `
modules:
  - name: job-broker
    type: messaging.broker
  - name: job-processor
    type: messaging.handler
    dependsOn: [job-broker]
  - name: job-state
    type: statemachine.engine
    dependsOn: [job-broker]

workflows:
  messaging:
    subscriptions:
      - topic: job.submitted
        handler: job-processor
      - topic: job.completed
        handler: job-processor
    producers:
      - name: job-processor
        forwardTo:
          - job.started
          - job.completed

  statemachine:
    engine: job-state
    definitions:
      - name: job-lifecycle
        description: "Manages job state transitions"
        initialState: submitted
        states:
          submitted:
            description: "Job submitted"
            isFinal: false
            isError: false
          running:
            description: "Job running"
            isFinal: false
            isError: false
          completed:
            description: "Job completed"
            isFinal: true
            isError: false
          failed:
            description: "Job failed"
            isFinal: true
            isError: true
        transitions:
          start:
            fromState: submitted
            toState: running
          complete:
            fromState: running
            toState: completed
          fail:
            fromState: running
            toState: failed

pipelines:
  process-job:
    trigger:
      type: messaging
      config:
        topic: job.submitted
    steps:
      - name: validate-job
        type: step.validate
        config:
          strategy: required_fields
          required_fields:
            - job_id
            - payload
      - name: run-job
        type: step.http_call
        config:
          url: "http://job-runner/execute"
      - name: notify
        type: step.log
        config:
          level: info
          message: "Job processed"
    compensation:
      - name: requeue-job
        type: step.http_call
        config:
          url: "http://job-runner/requeue"
    timeout: 120s
`

// writeTempWorkflowFile writes a named workflow YAML file into dir and returns its path.
func writeTempWorkflowFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0640); err != nil {
		t.Fatalf("failed to write workflow file %s: %v", name, err)
	}
	return path
}

// appConfigFixture holds paths set up by buildAppConfigFixture.
type appConfigFixture struct {
	appCfgPath string
	outDir     string
}

// buildAppConfigFixture creates a temp directory tree with the chimera-platform
// ApplicationConfig fixture (api + jobs workflow files) and returns the paths
// needed by individual test cases.
func buildAppConfigFixture(t *testing.T) appConfigFixture {
	t.Helper()
	dir := t.TempDir()

	apiDir := filepath.Join(dir, "api")
	jobsDir := filepath.Join(dir, "jobs")
	if err := os.MkdirAll(apiDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobsDir, 0750); err != nil {
		t.Fatal(err)
	}
	writeTempWorkflowFile(t, apiDir, "api.yaml", docsAPIWorkflowConfig)
	writeTempWorkflowFile(t, jobsDir, "application.yaml", docsJobsWorkflowConfig)

	const appConfig = `
application:
  name: chimera-platform
  workflows:
    - file: ./api/api.yaml
      name: api
    - file: ./jobs/application.yaml
      name: jobs
`
	appCfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(appCfgPath, []byte(appConfig), 0640); err != nil {
		t.Fatal(err)
	}

	return appConfigFixture{
		appCfgPath: appCfgPath,
		outDir:     filepath.Join(dir, "docs"),
	}
}

// TestDocsApplicationConfig verifies that docs can be generated from an
// ApplicationConfig that embeds multiple workflow YAML files, and that all
// content from those files appears in the generated documentation.
func TestDocsApplicationConfig(t *testing.T) {
	f := buildAppConfigFixture(t)

	if err := runDocsGenerate([]string{"-output", f.outDir, f.appCfgPath}); err != nil {
		t.Fatalf("docs generate failed for ApplicationConfig: %v", err)
	}

	// All main doc files should be created
	for _, name := range []string{"README.md", "modules.md", "pipelines.md", "workflows.md", "architecture.md"} {
		path := filepath.Join(f.outDir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to be created for ApplicationConfig", name)
		}
	}
}

// TestDocsApplicationConfigReadme checks that the README lists the application
// name and the embedded workflow files.
func TestDocsApplicationConfigReadme(t *testing.T) {
	f := buildAppConfigFixture(t)

	if err := runDocsGenerate([]string{"-output", f.outDir, f.appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(f.outDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	content := string(data)

	// Title should be the application name
	if !strings.Contains(content, "# chimera-platform") {
		t.Error("README.md should use application name as title")
	}
	// Should list workflow sources
	if !strings.Contains(content, "Application Workflows") {
		t.Error("README.md should have Application Workflows section")
	}
	if !strings.Contains(content, "api") {
		t.Error("README.md should list the 'api' workflow")
	}
	if !strings.Contains(content, "jobs") {
		t.Error("README.md should list the 'jobs' workflow")
	}
	if !strings.Contains(content, "./api/api.yaml") {
		t.Error("README.md should list the api workflow file path")
	}
	if !strings.Contains(content, "./jobs/application.yaml") {
		t.Error("README.md should list the jobs workflow file path")
	}
}

// TestDocsApplicationConfigModules checks that modules from all embedded
// workflow files appear in modules.md.
func TestDocsApplicationConfigModules(t *testing.T) {
	f := buildAppConfigFixture(t)

	if err := runDocsGenerate([]string{"-output", f.outDir, f.appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(f.outDir, "modules.md"))
	if err != nil {
		t.Fatalf("failed to read modules.md: %v", err)
	}
	content := string(data)

	// Modules from api workflow
	for _, mod := range []string{"api-server", "api-router", "auth-middleware", "user-handler"} {
		if !strings.Contains(content, mod) {
			t.Errorf("modules.md should contain module %q from api workflow", mod)
		}
	}
	// Modules from jobs workflow
	for _, mod := range []string{"job-broker", "job-processor", "job-state"} {
		if !strings.Contains(content, mod) {
			t.Errorf("modules.md should contain module %q from jobs workflow", mod)
		}
	}
}

// TestDocsApplicationConfigWorkflows checks that workflows from all embedded
// files appear in workflows.md.
func TestDocsApplicationConfigWorkflows(t *testing.T) {
	f := buildAppConfigFixture(t)

	if err := runDocsGenerate([]string{"-output", f.outDir, f.appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(f.outDir, "workflows.md"))
	if err != nil {
		t.Fatalf("failed to read workflows.md: %v", err)
	}
	content := string(data)

	// HTTP workflow from api
	if !strings.Contains(content, "/api/users") {
		t.Error("workflows.md should contain HTTP routes from api workflow")
	}
	if !strings.Contains(content, "auth-middleware") {
		t.Error("workflows.md should show middlewares from api workflow")
	}

	// Messaging workflow from jobs
	if !strings.Contains(content, "job.submitted") {
		t.Error("workflows.md should contain messaging topics from jobs workflow")
	}

	// State machine from jobs
	if !strings.Contains(content, "stateDiagram-v2") {
		t.Error("workflows.md should contain state machine diagram from jobs workflow")
	}
	if !strings.Contains(content, "submitted") {
		t.Error("workflows.md should show state machine states from jobs workflow")
	}
}

// TestDocsApplicationConfigPipelines checks that pipelines from all embedded
// files appear in pipelines.md.
func TestDocsApplicationConfigPipelines(t *testing.T) {
	f := buildAppConfigFixture(t)

	if err := runDocsGenerate([]string{"-output", f.outDir, f.appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(f.outDir, "pipelines.md"))
	if err != nil {
		t.Fatalf("failed to read pipelines.md: %v", err)
	}
	content := string(data)

	// Pipeline from api workflow
	if !strings.Contains(content, "create-user") {
		t.Error("pipelines.md should contain create-user pipeline from api workflow")
	}
	if !strings.Contains(content, "validate-input") {
		t.Error("pipelines.md should list steps from api workflow pipeline")
	}

	// Pipeline from jobs workflow
	if !strings.Contains(content, "process-job") {
		t.Error("pipelines.md should contain process-job pipeline from jobs workflow")
	}
	if !strings.Contains(content, "Compensation") {
		t.Error("pipelines.md should document compensation steps from jobs workflow pipeline")
	}
}

// TestDocsApplicationConfigTitleOverride verifies that -title flag overrides
// the application name when both are present.
func TestDocsApplicationConfigTitleOverride(t *testing.T) {
	dir := t.TempDir()

	apiDir := filepath.Join(dir, "api")
	if err := os.MkdirAll(apiDir, 0750); err != nil {
		t.Fatal(err)
	}
	writeTempWorkflowFile(t, apiDir, "api.yaml", docsAPIWorkflowConfig)

	const appConfig = `
application:
  name: chimera-platform
  workflows:
    - file: ./api/api.yaml
      name: api
`
	appCfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(appCfgPath, []byte(appConfig), 0640); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "docs")
	if err := runDocsGenerate([]string{"-output", outDir, "-title", "Override Title", appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	if !strings.Contains(string(data), "# Override Title") {
		t.Error("README.md should use the -title flag value when specified")
	}
	if strings.Contains(string(data), "# chimera-platform") {
		t.Error("README.md should NOT use the application name when -title is specified")
	}
}

// TestDocsApplicationConfigDuplicateWorkflowKey verifies that when two embedded
// workflow files both define the same workflow key (e.g. both have workflows.http),
// their list-bearing fields (routes, subscriptions, producers, definitions) are
// merged/appended rather than the second file silently overwriting the first.
func TestDocsApplicationConfigDuplicateWorkflowKey(t *testing.T) {
	dir := t.TempDir()

	// Two workflow files that both contribute http routes
	const apiV1Config = `
modules:
  - name: api-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
    dependsOn: [api-server]
  - name: v1-handler
    type: http.handler
    dependsOn: [api-router]

workflows:
  http:
    routes:
      - method: GET
        path: /v1/resource
        handler: v1-handler

triggers:
  http:
    server: api-server
`

	const apiV2Config = `
modules:
  - name: v2-handler
    type: http.handler

workflows:
  http:
    routes:
      - method: GET
        path: /v2/resource
        handler: v2-handler
`

	v1Dir := filepath.Join(dir, "v1")
	v2Dir := filepath.Join(dir, "v2")
	if err := os.MkdirAll(v1Dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(v2Dir, 0750); err != nil {
		t.Fatal(err)
	}
	writeTempWorkflowFile(t, v1Dir, "v1.yaml", apiV1Config)
	writeTempWorkflowFile(t, v2Dir, "v2.yaml", apiV2Config)

	const appConfig = `
application:
  name: multi-version-api
  workflows:
    - file: ./v1/v1.yaml
      name: v1
    - file: ./v2/v2.yaml
      name: v2
`
	appCfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(appCfgPath, []byte(appConfig), 0640); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "docs")
	if err := runDocsGenerate([]string{"-output", outDir, appCfgPath}); err != nil {
		t.Fatalf("docs generate failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "workflows.md"))
	if err != nil {
		t.Fatalf("failed to read workflows.md: %v", err)
	}
	content := string(data)

	// Routes from BOTH workflow files must be present
	if !strings.Contains(content, "/v1/resource") {
		t.Error("workflows.md should contain /v1/resource route from v1 workflow file")
	}
	if !strings.Contains(content, "/v2/resource") {
		t.Error("workflows.md should contain /v2/resource route from v2 workflow file (deep merge)")
	}
}
