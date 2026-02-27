// Package mcp provides a Model Context Protocol (MCP) server that exposes
// workflow engine functionality to AI assistants. The server dynamically
// reflects available module types, step types, trigger types, plugin
// information, and configuration validation so that AI tools can author
// and validate workflow YAML files with accurate, up-to-date knowledge.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Version is the MCP server version, set at build time.
var Version = "dev"

// EngineProvider is the interface that the MCP server requires from the
// workflow engine. It is kept intentionally narrow so that the mcp package
// does not import the root workflow package directly.
type EngineProvider interface {
	// BuildFromConfig builds the engine from a parsed workflow config.
	BuildFromConfig(cfg *config.WorkflowConfig) error
	// Start starts the engine and all registered triggers.
	Start(ctx context.Context) error
	// Stop gracefully shuts down the engine.
	Stop(ctx context.Context) error
	// TriggerWorkflow dispatches a workflow execution.
	TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
}

// ServerOption configures optional Server behaviour.
type ServerOption func(*Server)

// WithEngine attaches a pre-built workflow engine to the MCP server,
// enabling the run_workflow tool for AI-driven workflow execution.
func WithEngine(engine EngineProvider) ServerOption {
	return func(s *Server) {
		s.engine = engine
	}
}

// Server wraps an MCP server instance and provides workflow-engine-specific
// tools and resources.
type Server struct {
	mcpServer *server.MCPServer
	pluginDir string
	engine    EngineProvider // optional; enables execution tools when set
}

// NewServer creates a new MCP server with all workflow engine tools and
// resources registered. pluginDir is the directory where installed plugins
// reside (e.g., "data/plugins"). If set, the server will read plugin manifests
// from this directory and include plugin-provided types in all type listings.
// Optional ServerOption values can be provided to attach an engine, etc.
func NewServer(pluginDir string, opts ...ServerOption) *Server {
	s := &Server{
		pluginDir: pluginDir,
	}

	for _, opt := range opts {
		opt(s)
	}

	s.mcpServer = server.NewMCPServer(
		"workflow-mcp-server",
		Version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(false, true),
		server.WithInstructions("This MCP server exposes the GoCodeAlone/workflow engine. "+
			"Use the provided tools to list available module types, step types, trigger types, "+
			"workflow types, generate JSON schemas, validate YAML configurations, inspect configs, "+
			"and manage plugins. Resources provide documentation and example configurations."),
	)

	// Load types from installed plugin manifests so that plugin-provided types
	// appear in all type listings (list_module_types, list_step_types, etc.).
	if pluginDir != "" {
		s.loadInstalledPluginTypes(pluginDir)
	}

	s.registerTools()
	s.registerNewTools()
	s.registerResources()

	return s
}

// MCPServer returns the underlying mcp-go server instance (useful for testing).
func (s *Server) MCPServer() *server.MCPServer {
	return s.mcpServer
}

// ServeStdio starts the MCP server over standard input/output.
func (s *Server) ServeStdio() error {
	return server.ServeStdio(s.mcpServer)
}

// registerTools registers all workflow engine tools with the MCP server.
func (s *Server) registerTools() {
	// list_module_types
	s.mcpServer.AddTool(
		mcp.NewTool("list_module_types",
			mcp.WithDescription("List all available workflow module types that can be used in the 'modules' section of a workflow YAML config. Returns built-in types plus types from installed plugins (loaded from plugin_dir at server startup)."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleListModuleTypes,
	)

	// list_step_types
	s.mcpServer.AddTool(
		mcp.NewTool("list_step_types",
			mcp.WithDescription("List all available pipeline step types that can be used in pipeline definitions. Returns built-in steps plus steps from installed plugins (loaded from plugin_dir at server startup)."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleListStepTypes,
	)

	// list_trigger_types
	s.mcpServer.AddTool(
		mcp.NewTool("list_trigger_types",
			mcp.WithDescription("List all available trigger types (e.g., http, schedule, event, eventbus) that can start workflow execution. Includes types from installed plugins."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleListTriggerTypes,
	)

	// list_workflow_types
	s.mcpServer.AddTool(
		mcp.NewTool("list_workflow_types",
			mcp.WithDescription("List all available workflow handler types (e.g., http, messaging, statemachine, scheduler, integration, event, pipeline) that define how workflows process work. Includes types from installed plugins."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleListWorkflowTypes,
	)

	// generate_schema
	s.mcpServer.AddTool(
		mcp.NewTool("generate_schema",
			mcp.WithDescription("Generate the JSON Schema for workflow configuration YAML files. This schema describes all valid fields, module types, and structure for authoring workflow configs."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGenerateSchema,
	)

	// validate_config
	s.mcpServer.AddTool(
		mcp.NewTool("validate_config",
			mcp.WithDescription("Validate a workflow YAML configuration string. Returns validation results including any errors found. Use this to check if a YAML config is well-formed and uses valid module types."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration to validate"),
			),
			mcp.WithBoolean("strict",
				mcp.Description("Enable strict validation (no empty modules allowed). Default: false"),
			),
			mcp.WithBoolean("skip_unknown_types",
				mcp.Description("Skip unknown module/workflow/trigger type checks. Default: false"),
			),
		),
		s.handleValidateConfig,
	)

	// inspect_config
	s.mcpServer.AddTool(
		mcp.NewTool("inspect_config",
			mcp.WithDescription("Inspect a workflow YAML configuration and return a structured summary of its modules, workflows, triggers, pipelines, and dependency graph."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration to inspect"),
			),
		),
		s.handleInspectConfig,
	)

	// list_plugins
	s.mcpServer.AddTool(
		mcp.NewTool("list_plugins",
			mcp.WithDescription("List installed external plugins from the plugin directory."),
			mcp.WithString("data_dir",
				mcp.Description("Plugin data directory. Defaults to 'data/plugins'"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleListPlugins,
	)

	// get_config_skeleton
	s.mcpServer.AddTool(
		mcp.NewTool("get_config_skeleton",
			mcp.WithDescription("Generate a skeleton/template workflow YAML configuration for a given set of module types. Useful for bootstrapping new workflow configs."),
			mcp.WithArray("module_types",
				mcp.Required(),
				mcp.Description("List of module type strings to include in the skeleton config (e.g., ['http.server', 'http.router', 'http.handler'])"),
			),
		),
		s.handleGetConfigSkeleton,
	)

	// run_workflow - only available when an engine is attached
	if s.engine != nil {
		s.mcpServer.AddTool(
			mcp.NewTool("run_workflow",
				mcp.WithDescription("Trigger a workflow execution on the attached engine. Requires the server to be started with an engine (WithEngine option). "+
					"Provide the workflow type, action, and optional data payload."),
				mcp.WithString("workflow_type",
					mcp.Required(),
					mcp.Description("The workflow type to trigger (e.g., 'http', 'messaging', 'pipeline')"),
				),
				mcp.WithString("action",
					mcp.Required(),
					mcp.Description("The action to perform within the workflow"),
				),
				mcp.WithObject("data",
					mcp.Description("Key-value data payload to pass to the workflow"),
				),
			),
			s.handleRunWorkflow,
		)
	}
}

// registerResources registers documentation and example resources.
func (s *Server) registerResources() {
	s.mcpServer.AddResource(
		mcp.NewResource(
			"workflow://docs/overview",
			"Workflow Engine Overview",
			mcp.WithResourceDescription("Overview documentation for the GoCodeAlone/workflow engine, including architecture, key concepts, and configuration patterns."),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleDocsOverview,
	)

	s.mcpServer.AddResource(
		mcp.NewResource(
			"workflow://docs/yaml-syntax",
			"YAML Configuration Syntax Guide",
			mcp.WithResourceDescription("Detailed guide on workflow YAML configuration file syntax, including modules, workflows, triggers, and pipelines."),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleDocsYAMLSyntax,
	)

	s.mcpServer.AddResource(
		mcp.NewResource(
			"workflow://docs/module-reference",
			"Module Type Reference",
			mcp.WithResourceDescription("Reference documentation for all available module types with their configuration options."),
			mcp.WithMIMEType("text/markdown"),
		),
		s.handleDocsModuleReference,
	)
}

// --- Tool Handlers ---

func (s *Server) handleListModuleTypes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types := schema.KnownModuleTypes()
	result := map[string]any{
		"module_types": types,
		"count":        len(types),
	}
	return marshalToolResult(result)
}

func (s *Server) handleListStepTypes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types := schema.KnownModuleTypes()
	var stepTypes []string
	for _, t := range types {
		if strings.HasPrefix(t, "step.") {
			stepTypes = append(stepTypes, t)
		}
	}
	result := map[string]any{
		"step_types": stepTypes,
		"count":      len(stepTypes),
	}
	return marshalToolResult(result)
}

func (s *Server) handleListTriggerTypes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types := schema.KnownTriggerTypes()
	result := map[string]any{
		"trigger_types": types,
		"count":         len(types),
	}
	return marshalToolResult(result)
}

func (s *Server) handleListWorkflowTypes(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	types := schema.KnownWorkflowTypes()
	result := map[string]any{
		"workflow_types": types,
		"count":          len(types),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGenerateSchema(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	sch := schema.GenerateWorkflowSchema()
	data, err := json.MarshalIndent(sch, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate schema: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (s *Server) handleValidateConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	strict := mcp.ParseBoolean(req, "strict", false)
	skipUnknown := mcp.ParseBoolean(req, "skip_unknown_types", false)

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	var opts []schema.ValidationOption
	if !strict {
		opts = append(opts, schema.WithAllowEmptyModules())
	}
	if skipUnknown {
		opts = append(opts, schema.WithSkipModuleTypeCheck(), schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	} else {
		opts = append(opts, schema.WithSkipWorkflowTypeCheck(), schema.WithSkipTriggerTypeCheck())
	}
	opts = append(opts, schema.WithAllowNoEntryPoints())

	if err := schema.ValidateConfig(cfg, opts...); err != nil {
		result := map[string]any{
			"valid":   false,
			"errors":  err.Error(),
			"summary": fmt.Sprintf("%d modules parsed", len(cfg.Modules)),
		}
		return marshalToolResult(result)
	}

	result := map[string]any{
		"valid":   true,
		"summary": fmt.Sprintf("%d modules, %d workflows, %d triggers", len(cfg.Modules), len(cfg.Workflows), len(cfg.Triggers)),
	}
	return marshalToolResult(result)
}

func (s *Server) handleInspectConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	// Build modules summary
	type moduleInfo struct {
		Name      string   `json:"name"`
		Type      string   `json:"type"`
		DependsOn []string `json:"depends_on,omitempty"`
	}
	modules := make([]moduleInfo, 0, len(cfg.Modules))
	typeCount := make(map[string]int)
	for _, mod := range cfg.Modules {
		modules = append(modules, moduleInfo{
			Name:      mod.Name,
			Type:      mod.Type,
			DependsOn: mod.DependsOn,
		})
		typeCount[mod.Type]++
	}

	// Module type distribution
	types := make([]string, 0, len(typeCount))
	for t := range typeCount {
		types = append(types, t)
	}
	sort.Strings(types)
	typeDist := make(map[string]int)
	for _, t := range types {
		typeDist[t] = typeCount[t]
	}

	// Workflow and trigger names
	workflowNames := make([]string, 0, len(cfg.Workflows))
	for name := range cfg.Workflows {
		workflowNames = append(workflowNames, name)
	}
	sort.Strings(workflowNames)

	triggerNames := make([]string, 0, len(cfg.Triggers))
	for name := range cfg.Triggers {
		triggerNames = append(triggerNames, name)
	}
	sort.Strings(triggerNames)

	pipelineNames := make([]string, 0, len(cfg.Pipelines))
	for name := range cfg.Pipelines {
		pipelineNames = append(pipelineNames, name)
	}
	sort.Strings(pipelineNames)

	// Dependency graph
	var depEdges []string
	for _, mod := range cfg.Modules {
		for _, dep := range mod.DependsOn {
			depEdges = append(depEdges, fmt.Sprintf("%s -> %s", mod.Name, dep))
		}
	}

	result := map[string]any{
		"modules":          modules,
		"module_count":     len(modules),
		"module_types":     typeDist,
		"workflows":        workflowNames,
		"triggers":         triggerNames,
		"pipelines":        pipelineNames,
		"dependency_graph": depEdges,
	}
	return marshalToolResult(result)
}

func (s *Server) handleListPlugins(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	dataDir := mcp.ParseString(req, "data_dir", s.pluginDir)
	if dataDir == "" {
		dataDir = "data/plugins"
	}

	entries, err := os.ReadDir(dataDir)
	if os.IsNotExist(err) {
		result := map[string]any{
			"plugins": []any{},
			"count":   0,
			"message": "No plugins installed (directory does not exist)",
		}
		return marshalToolResult(result)
	}
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read plugin directory %s: %v", dataDir, err)), nil
	}

	type pluginInfo struct {
		Name          string   `json:"name"`
		Version       string   `json:"version"`
		ModuleTypes   []string `json:"module_types,omitempty"`
		StepTypes     []string `json:"step_types,omitempty"`
		TriggerTypes  []string `json:"trigger_types,omitempty"`
		WorkflowTypes []string `json:"workflow_types,omitempty"`
	}
	var plugins []pluginInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(dataDir, e.Name())
		ver := readPluginVersion(dir)
		info := pluginInfo{Name: e.Name(), Version: ver}
		// Enrich with type declarations from the plugin manifest.
		if data, err := os.ReadFile(filepath.Join(dir, "plugin.json")); err == nil { //nolint:gosec // G304: path is within the trusted plugins directory
			var m pluginManifestTypes
			if json.Unmarshal(data, &m) == nil {
				info.ModuleTypes = m.ModuleTypes
				info.StepTypes = m.StepTypes
				info.TriggerTypes = m.TriggerTypes
				info.WorkflowTypes = m.WorkflowTypes
			}
		}
		plugins = append(plugins, info)
	}

	result := map[string]any{
		"plugins": plugins,
		"count":   len(plugins),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetConfigSkeleton(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rawTypes := req.Params.Arguments["module_types"]
	if rawTypes == nil {
		return mcp.NewToolResultError("module_types is required"), nil
	}

	typesSlice, ok := rawTypes.([]any)
	if !ok {
		return mcp.NewToolResultError("module_types must be an array of strings"), nil
	}

	var moduleTypes []string
	for _, t := range typesSlice {
		if s, ok := t.(string); ok {
			moduleTypes = append(moduleTypes, s)
		}
	}

	if len(moduleTypes) == 0 {
		return mcp.NewToolResultError("at least one module type is required"), nil
	}

	yaml := generateConfigSkeleton(moduleTypes)
	return mcp.NewToolResultText(yaml), nil
}

func (s *Server) handleRunWorkflow(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.engine == nil {
		return mcp.NewToolResultError("no engine attached; start the server with WithEngine option"), nil
	}

	workflowType := mcp.ParseString(req, "workflow_type", "")
	if workflowType == "" {
		return mcp.NewToolResultError("workflow_type is required"), nil
	}

	action := mcp.ParseString(req, "action", "")
	if action == "" {
		return mcp.NewToolResultError("action is required"), nil
	}

	var data map[string]any
	if rawData, ok := req.Params.Arguments["data"]; ok && rawData != nil {
		if d, ok := rawData.(map[string]any); ok {
			data = d
		}
	}
	if data == nil {
		data = make(map[string]any)
	}

	if err := s.engine.TriggerWorkflow(ctx, workflowType, action, data); err != nil {
		result := map[string]any{
			"success": false,
			"error":   err.Error(),
		}
		return marshalToolResult(result)
	}

	result := map[string]any{
		"success":       true,
		"workflow_type": workflowType,
		"action":        action,
	}
	return marshalToolResult(result)
}

// --- Resource Handlers ---

func (s *Server) handleDocsOverview(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "workflow://docs/overview",
			MIMEType: "text/markdown",
			Text:     docsOverview,
		},
	}, nil
}

func (s *Server) handleDocsYAMLSyntax(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "workflow://docs/yaml-syntax",
			MIMEType: "text/markdown",
			Text:     docsYAMLSyntax,
		},
	}, nil
}

func (s *Server) handleDocsModuleReference(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	// Dynamically generate module reference from known types
	doc := generateModuleReference()
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      "workflow://docs/module-reference",
			MIMEType: "text/markdown",
			Text:     doc,
		},
	}, nil
}

// --- Helpers ---

func marshalToolResult(v any) (*mcp.CallToolResult, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("internal error: %v", err)), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// pluginManifestTypes holds the type declarations from a plugin.json manifest file.
// This is a minimal subset of plugin.PluginManifest used to avoid a package dependency.
type pluginManifestTypes struct {
	ModuleTypes   []string `json:"moduleTypes"`
	StepTypes     []string `json:"stepTypes"`
	TriggerTypes  []string `json:"triggerTypes"`
	WorkflowTypes []string `json:"workflowTypes"`
}

// loadInstalledPluginTypes scans pluginDir for subdirectories containing a
// plugin.json manifest, reads each manifest's type declarations, and registers
// them with the schema package so that they appear in all type listings.
// Unknown or malformed manifests are silently skipped.
func (s *Server) loadInstalledPluginTypes(pluginDir string) {
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return
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
			schema.RegisterModuleType(t)
		}
		for _, t := range m.StepTypes {
			// Step types are also surfaced as module types in the MCP server view
			// (they share the same registry and are identified by the "step." prefix).
			schema.RegisterModuleType(t)
		}
		for _, t := range m.TriggerTypes {
			schema.RegisterTriggerType(t)
		}
		for _, t := range m.WorkflowTypes {
			schema.RegisterWorkflowType(t)
		}
	}
}

func readPluginVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return "unknown"
	}
	var m struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Version == "" {
		return "unknown"
	}
	return m.Version
}

func generateConfigSkeleton(moduleTypes []string) string {
	var b strings.Builder
	b.WriteString("# Workflow configuration skeleton\n")
	b.WriteString("# Generated by workflow MCP server\n\n")
	b.WriteString("modules:\n")

	for i, mt := range moduleTypes {
		name := strings.ReplaceAll(mt, ".", "-")
		fmt.Fprintf(&b, "  - name: %s-%d\n", name, i+1)
		fmt.Fprintf(&b, "    type: %s\n", mt)
		b.WriteString("    config: {}\n")
		if i < len(moduleTypes)-1 {
			b.WriteString("\n")
		}
	}

	b.WriteString("\nworkflows: {}\n")
	b.WriteString("\ntriggers: {}\n")

	return b.String()
}

func generateModuleReference() string {
	types := schema.KnownModuleTypes()

	// Group by prefix
	groups := make(map[string][]string)
	for _, t := range types {
		parts := strings.SplitN(t, ".", 2)
		prefix := parts[0]
		groups[prefix] = append(groups[prefix], t)
	}

	prefixes := make([]string, 0, len(groups))
	for p := range groups {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)

	var b strings.Builder
	b.WriteString("# Module Type Reference\n\n")
	b.WriteString("This reference lists all available module types grouped by category.\n\n")

	for _, prefix := range prefixes {
		fmt.Fprintf(&b, "## %s\n\n", cases.Title(language.English).String(prefix))
		for _, t := range groups[prefix] {
			fmt.Fprintf(&b, "- `%s`\n", t)
		}
		b.WriteString("\n")
	}

	// Add step types separately
	b.WriteString("## Pipeline Steps\n\n")
	b.WriteString("Pipeline steps are used in the `pipelines` section and execute sequentially.\n\n")
	for _, t := range types {
		if strings.HasPrefix(t, "step.") {
			fmt.Fprintf(&b, "- `%s`\n", t)
		}
	}
	b.WriteString("\n")

	return b.String()
}
