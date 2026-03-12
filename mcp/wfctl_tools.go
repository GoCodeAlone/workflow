package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/manifest"
	"github.com/GoCodeAlone/workflow/modernize"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// registerWfctlTools registers the wfctl CLI-equivalent tools.
// All tools are read-only — they analyze config, never modify anything.
func (s *Server) registerWfctlTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("api_extract",
			mcp.WithDescription("Extract an OpenAPI 3.0 specification from a workflow YAML config. "+
				"Parses HTTP-triggered workflows and pipelines to produce a complete API spec "+
				"with inferred request/response schemas."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithString("title",
				mcp.Description("API title (default: extracted from config or \"Workflow API\")"),
			),
			mcp.WithString("version",
				mcp.Description("API version (default: \"1.0.0\")"),
			),
			mcp.WithBoolean("include_schemas",
				mcp.Description("Attempt to infer request/response schemas from step types (default: true)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleAPIExtract,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("diff_configs",
			mcp.WithDescription("Compare two workflow YAML configurations and report what changed. "+
				"Detects added/removed/changed modules and pipelines, flags stateful module changes, "+
				"and identifies breaking changes."),
			mcp.WithString("old_yaml",
				mcp.Required(),
				mcp.Description("The YAML content of the old/baseline configuration"),
			),
			mcp.WithString("new_yaml",
				mcp.Required(),
				mcp.Description("The YAML content of the new configuration"),
			),
			mcp.WithBoolean("check_breaking",
				mcp.Description("Include breaking change detection for stateful modules (default: true)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleDiffConfigs,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("manifest_analyze",
			mcp.WithDescription("Analyze a workflow YAML config and produce an infrastructure requirements manifest. "+
				"Reports databases, services, event buses, storage, external APIs, ports, sidecars, "+
				"and resource estimates (CPU, memory, disk)."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithString("name",
				mcp.Description("Override the manifest name (default: derived from config)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleManifestAnalyze,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("contract_generate",
			mcp.WithDescription("Generate an API contract snapshot from a workflow config. "+
				"Extracts endpoints, modules, step types, and event topics. "+
				"Optionally compares against a baseline contract JSON to detect breaking changes."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithString("baseline_json",
				mcp.Description("Previous contract JSON for comparison (optional)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleContractGenerate,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("compat_check",
			mcp.WithDescription("Check whether a workflow config is compatible with the current engine version. "+
				"Reports which module and step types are available in the engine."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleCompatCheck,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("template_validate_config",
			mcp.WithDescription("Deep validation of a workflow config against known module types, step types, "+
				"trigger types, dependencies, and template expressions. More comprehensive than validate_config."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithBoolean("strict",
				mcp.Description("Fail on warnings in addition to errors (default: false)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleTemplateValidateConfig,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("generate_github_actions",
			mcp.WithDescription("Generate GitHub Actions CI/CD workflow YAML files based on analysis of a workflow config. "+
				"Detects features (UI, auth, database, plugins, HTTP) and generates appropriate CI and CD workflows."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithString("registry",
				mcp.Description("Container registry for Docker images (default: \"ghcr.io\")"),
			),
			mcp.WithString("platforms",
				mcp.Description("Platforms to build for (default: \"linux/amd64,linux/arm64\")"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGenerateGithubActions,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("detect_project_features",
			mcp.WithDescription("Analyze a workflow config to detect what features the project uses. "+
				"Reports presence of UI, auth, database, plugins, HTTP, and lists all module types."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleDetectProjectFeatures,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("modernize",
			mcp.WithDescription("Detect and optionally fix known YAML config anti-patterns. "+
				"Reports issues like hyphenated step names, template syntax in conditional fields, "+
				"missing db_query mode, dot-access patterns, absolute dbPaths, empty routes, "+
				"and snake_case config keys. When the server was started with a plugin directory, "+
				"plugin-declared modernize rules are automatically included. "+
				"Returns findings with line numbers and fix suggestions."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration to analyze"),
			),
			mcp.WithBoolean("apply",
				mcp.Description("Apply fixes to the YAML and return the modified content (default: false, dry-run only)"),
			),
			mcp.WithString("rules",
				mcp.Description("Comma-separated list of rule IDs to run (default: all). Available: hyphen-steps, conditional-field, db-query-mode, db-query-index, absolute-dbpath, empty-routes, camelcase-config"),
			),
			mcp.WithString("exclude_rules",
				mcp.Description("Comma-separated list of rule IDs to skip"),
			),
			mcp.WithReadOnlyHintAnnotation(false),
		),
		s.handleModernize,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("registry_search",
			mcp.WithDescription("Search the workflow plugin registry for available plugins. "+
				"Returns plugin manifests matching the query, with capabilities, versions, and download information. "+
				"Queries the local clone of the GoCodeAlone/workflow-registry repository."),
			mcp.WithString("query",
				mcp.Description("Search term to filter plugins by name, description, or keywords"),
			),
			mcp.WithString("type",
				mcp.Description("Filter by plugin type: builtin, external, internal"),
			),
			mcp.WithString("tier",
				mcp.Description("Filter by tier: core, community, premium"),
			),
			mcp.WithBoolean("include_private",
				mcp.Description("Include private/proprietary plugins (default: false)"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleRegistrySearch,
	)
}

// --- Tool Handlers ---

func (s *Server) handleAPIExtract(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	title := mcp.ParseString(req, "title", "")
	if title == "" {
		title = mcpExtractTitleFromConfig(cfg)
	}
	if title == "" {
		title = "Workflow API"
	}

	version := mcp.ParseString(req, "version", "1.0.0")
	includeSchemas := mcp.ParseBoolean(req, "include_schemas", true)

	genCfg := module.OpenAPIGeneratorConfig{
		Title:   title,
		Version: version,
	}
	gen := module.NewOpenAPIGenerator("mcp-api-extract", genCfg)

	gen.BuildSpec(cfg.Workflows)

	if len(cfg.Pipelines) > 0 {
		pipelineRoutes := mcpExtractPipelineRoutes(cfg.Pipelines, includeSchemas, gen)
		if len(pipelineRoutes) > 0 {
			gen.BuildSpecFromRoutes(mcpAppendToExistingSpec(gen, pipelineRoutes))
		}
	}

	if includeSchemas {
		gen.ApplySchemas()
	}

	spec := gen.GetSpec()
	if spec == nil {
		spec = &module.OpenAPISpec{
			OpenAPI: "3.0.3",
			Info: module.OpenAPIInfo{
				Title:   title,
				Version: version,
			},
			Paths: make(map[string]*module.OpenAPIPath),
		}
	}

	return marshalToolResult(spec)
}

func (s *Server) handleDiffConfigs(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	oldYAML := mcp.ParseString(req, "old_yaml", "")
	if oldYAML == "" {
		return mcp.NewToolResultError("old_yaml is required"), nil
	}
	newYAML := mcp.ParseString(req, "new_yaml", "")
	if newYAML == "" {
		return mcp.NewToolResultError("new_yaml is required"), nil
	}

	oldCfg, err := config.LoadFromString(oldYAML)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("old config YAML parse error: %v", err)), nil
	}
	newCfg, err := config.LoadFromString(newYAML)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("new config YAML parse error: %v", err)), nil
	}

	result := mcpDiffConfigs(oldCfg, newCfg)
	return marshalToolResult(result)
}

func (s *Server) handleManifestAnalyze(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	name := mcp.ParseString(req, "name", "")
	m := manifest.AnalyzeWithName(cfg, name)
	return marshalToolResult(m)
}

func (s *Server) handleContractGenerate(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	contract := mcpGenerateContract(cfg)

	baselineJSON := mcp.ParseString(req, "baseline_json", "")
	if baselineJSON != "" {
		var baseline mcpContract
		if err := json.Unmarshal([]byte(baselineJSON), &baseline); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse baseline_json: %v", err)), nil
		}
		comparison := mcpCompareContracts(&baseline, contract)
		return marshalToolResult(comparison)
	}

	return marshalToolResult(contract)
}

func (s *Server) handleCompatCheck(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	result := mcpCheckCompatibility(cfg)
	return marshalToolResult(result)
}

func (s *Server) handleTemplateValidateConfig(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	strict := mcp.ParseBoolean(req, "strict", false)
	result := mcpValidateWorkflowConfig(cfg)

	pass := len(result.Errors) == 0
	if strict && len(result.Warnings) > 0 {
		pass = false
	}

	out := map[string]any{
		"valid":         pass,
		"name":          result.Name,
		"module_count":  result.ModuleCount,
		"module_valid":  result.ModuleValid,
		"step_count":    result.StepCount,
		"step_valid":    result.StepValid,
		"dep_count":     result.DepCount,
		"dep_valid":     result.DepValid,
		"trigger_count": result.TriggerCount,
		"trigger_valid": result.TriggerValid,
		"warnings":      result.Warnings,
		"errors":        result.Errors,
	}
	return marshalToolResult(out)
}

func (s *Server) handleGenerateGithubActions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	registry := mcp.ParseString(req, "registry", "ghcr.io")
	platforms := mcp.ParseString(req, "platforms", "linux/amd64,linux/arm64")

	features := mcpDetectFeatures(cfg)
	ciYAML := mcpGenerateCIWorkflow(features)
	cdYAML := mcpGenerateCDWorkflow(features, registry, platforms)

	result := map[string]any{
		"features": features,
		"ci_yaml":  ciYAML,
		"cd_yaml":  cdYAML,
	}

	if features.HasPlugin {
		result["release_yaml"] = mcpGenerateReleaseWorkflow()
	}

	return marshalToolResult(result)
}

func (s *Server) handleDetectProjectFeatures(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	features := mcpDetectFeatures(cfg)
	return marshalToolResult(features)
}

// --- Types ---

type mcpDiffResult struct {
	Modules         []mcpModuleDiff            `json:"modules"`
	Pipelines       []mcpPipelineDiff          `json:"pipelines"`
	BreakingChanges []mcpBreakingChangeSummary `json:"breakingChanges,omitempty"`
}

type mcpModuleDiff struct {
	Name            string              `json:"name"`
	Status          string              `json:"status"`
	Type            string              `json:"type,omitempty"`
	Stateful        bool                `json:"stateful"`
	Detail          string              `json:"detail,omitempty"`
	BreakingChanges []mcpBreakingChange `json:"breakingChanges,omitempty"`
}

type mcpPipelineDiff struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Trigger string `json:"trigger,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type mcpBreakingChangeSummary struct {
	ModuleName string              `json:"moduleName"`
	Changes    []mcpBreakingChange `json:"changes"`
}

type mcpBreakingChange struct {
	Field    string `json:"field"`
	OldValue string `json:"oldValue"`
	NewValue string `json:"newValue"`
	Message  string `json:"message"`
}

type mcpContract struct {
	Version     string              `json:"version"`
	ConfigHash  string              `json:"configHash"`
	GeneratedAt string              `json:"generatedAt"`
	Endpoints   []mcpEndpoint       `json:"endpoints"`
	Modules     []mcpModuleContract `json:"modules"`
	Steps       []string            `json:"steps"`
	Events      []mcpEventContract  `json:"events"`
}

type mcpEndpoint struct {
	Method       string `json:"method"`
	Path         string `json:"path"`
	AuthRequired bool   `json:"authRequired"`
	Pipeline     string `json:"pipeline"`
}

type mcpModuleContract struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Stateful bool   `json:"stateful"`
}

type mcpEventContract struct {
	Topic     string `json:"topic"`
	Direction string `json:"direction"`
	Pipeline  string `json:"pipeline"`
}

type mcpContractComparison struct {
	BaseVersion    string              `json:"baseVersion"`
	CurrentVersion string              `json:"currentVersion"`
	Endpoints      []mcpEndpointChange `json:"endpoints"`
	Modules        []mcpModuleChange   `json:"modules"`
	Events         []mcpEventChange    `json:"events"`
	BreakingCount  int                 `json:"breakingCount"`
}

type mcpEndpointChange struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Pipeline   string `json:"pipeline"`
	Change     string `json:"change"`
	Detail     string `json:"detail,omitempty"`
	IsBreaking bool   `json:"isBreaking"`
}

type mcpModuleChange struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Change string `json:"change"`
}

type mcpEventChange struct {
	Topic     string `json:"topic"`
	Direction string `json:"direction"`
	Pipeline  string `json:"pipeline"`
	Change    string `json:"change"`
}

type mcpCompatResult struct {
	EngineVersion   string          `json:"engineVersion"`
	RequiredModules []mcpCompatItem `json:"requiredModules"`
	RequiredSteps   []mcpCompatItem `json:"requiredSteps"`
	Compatible      bool            `json:"compatible"`
	Issues          []string        `json:"issues,omitempty"`
}

type mcpCompatItem struct {
	Type      string `json:"type"`
	Available bool   `json:"available"`
}

type mcpValidationResult struct {
	Name         string   `json:"name"`
	ModuleCount  int      `json:"moduleCount"`
	ModuleValid  int      `json:"moduleValid"`
	StepCount    int      `json:"stepCount"`
	StepValid    int      `json:"stepValid"`
	DepCount     int      `json:"depCount"`
	DepValid     int      `json:"depValid"`
	TriggerCount int      `json:"triggerCount"`
	TriggerValid int      `json:"triggerValid"`
	Warnings     []string `json:"warnings"`
	Errors       []string `json:"errors"`
}

type mcpProjectFeatures struct {
	HasUI       bool     `json:"hasUI"`
	HasAuth     bool     `json:"hasAuth"`
	HasDatabase bool     `json:"hasDatabase"`
	HasPlugin   bool     `json:"hasPlugin"`
	HasHTTP     bool     `json:"hasHTTP"`
	ModuleTypes []string `json:"moduleTypes"`
}

// --- Implementation helpers ---

// mcpExtractTitleFromConfig derives a title from the config's http.server module name.
func mcpExtractTitleFromConfig(cfg *config.WorkflowConfig) string {
	for _, mod := range cfg.Modules {
		if mod.Type == "http.server" && mod.Name != "server" && mod.Name != "" {
			return strings.Title(strings.ReplaceAll(mod.Name, "-", " ")) //nolint:staticcheck
		}
	}
	return ""
}

// mcpExtractPipelineRoutes scans pipelines for HTTP triggers and returns route definitions.
func mcpExtractPipelineRoutes(pipelines map[string]any, includeSchemas bool, gen *module.OpenAPIGenerator) []module.RouteDefinition {
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	var routes []module.RouteDefinition
	for _, name := range names {
		raw := pipelines[name]
		pipelineMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		triggerRaw, ok := pipelineMap["trigger"]
		if !ok {
			continue
		}
		triggerMap, ok := triggerRaw.(map[string]any)
		if !ok {
			continue
		}
		triggerType, _ := triggerMap["type"].(string)
		if triggerType != "http" {
			continue
		}
		triggerConfig, ok := triggerMap["config"].(map[string]any)
		if !ok {
			continue
		}
		path, _ := triggerConfig["path"].(string)
		method, _ := triggerConfig["method"].(string)
		if path == "" || method == "" {
			continue
		}

		route := module.RouteDefinition{
			Method:  strings.ToUpper(method),
			Path:    path,
			Handler: name,
			Tags:    []string{"pipelines"},
			Summary: fmt.Sprintf("%s %s (pipeline: %s)", strings.ToUpper(method), path, name),
		}

		if includeSchemas {
			if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
				mcpApplyPipelineSchemas(gen, name, method, path, stepsRaw)
			}
		}

		routes = append(routes, route)
	}
	return routes
}

// mcpApplyPipelineSchemas infers schemas from pipeline steps and sets them on the generator.
func mcpApplyPipelineSchemas(gen *module.OpenAPIGenerator, _ string, method, path string, stepsRaw []any) {
	var reqSchema *module.OpenAPISchema
	var respSchema *module.OpenAPISchema
	hasAuthRequired := false

	for _, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		stepType, _ := stepMap["type"].(string)
		stepCfg, _ := stepMap["config"].(map[string]any)

		switch stepType {
		case "step.validate":
			if stepCfg != nil && reqSchema == nil {
				reqSchema = &module.OpenAPISchema{
					Type:       "object",
					Properties: make(map[string]*module.OpenAPISchema),
				}
				if rules, ok := stepCfg["rules"].(map[string]any); ok {
					for field, ruleRaw := range rules {
						ruleStr, _ := ruleRaw.(string)
						parts := strings.Split(ruleStr, ",")
						fieldSchema := &module.OpenAPISchema{Type: "string"}
						isRequired := false
						for _, part := range parts {
							part = strings.TrimSpace(part)
							switch part {
							case "required":
								isRequired = true
							case "email":
								fieldSchema.Format = "email"
							case "numeric", "number":
								fieldSchema.Type = "number"
							case "boolean", "bool":
								fieldSchema.Type = "boolean"
							}
						}
						reqSchema.Properties[field] = fieldSchema
						if isRequired {
							reqSchema.Required = append(reqSchema.Required, field)
						}
					}
					sort.Strings(reqSchema.Required)
				}
			}
		case "step.auth_required":
			hasAuthRequired = true
		case "step.json_response":
			if respSchema == nil {
				respSchema = &module.OpenAPISchema{Type: "object"}
			}
		}
	}

	gen.SetOperationSchema(method, path, reqSchema, respSchema)

	if hasAuthRequired {
		gen.RegisterComponentSchema("UnauthorizedError", &module.OpenAPISchema{
			Type: "object",
			Properties: map[string]*module.OpenAPISchema{
				"error": {Type: "string", Example: "Unauthorized"},
			},
		})
	}
}

// mcpAppendToExistingSpec combines existing spec routes with new pipeline routes.
func mcpAppendToExistingSpec(gen *module.OpenAPIGenerator, pipelineRoutes []module.RouteDefinition) []module.RouteDefinition {
	spec := gen.GetSpec()
	if spec == nil {
		return pipelineRoutes
	}

	var existing []module.RouteDefinition
	paths := gen.SortedPaths()
	for _, path := range paths {
		pathItem := spec.Paths[path]
		if pathItem == nil {
			continue
		}
		ops := []struct {
			method string
			op     *module.OpenAPIOperation
		}{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"DELETE", pathItem.Delete},
			{"PATCH", pathItem.Patch},
			{"OPTIONS", pathItem.Options},
		}
		for _, entry := range ops {
			if entry.op == nil {
				continue
			}
			route := module.RouteDefinition{
				Method:  entry.method,
				Path:    path,
				Summary: entry.op.Summary,
				Tags:    entry.op.Tags,
			}
			if len(entry.op.Tags) > 0 {
				route.Handler = entry.op.Tags[0]
			}
			if _, hasAuth := entry.op.Responses["401"]; hasAuth {
				route.Middlewares = []string{"auth"}
			}
			existing = append(existing, route)
		}
	}

	return append(existing, pipelineRoutes...)
}

// --- Diff logic ---

func mcpDiffConfigs(oldCfg, newCfg *config.WorkflowConfig) mcpDiffResult {
	result := mcpDiffResult{}

	oldModules := make(map[string]*config.ModuleConfig, len(oldCfg.Modules))
	for i := range oldCfg.Modules {
		m := &oldCfg.Modules[i]
		oldModules[m.Name] = m
	}
	newModules := make(map[string]*config.ModuleConfig, len(newCfg.Modules))
	for i := range newCfg.Modules {
		m := &newCfg.Modules[i]
		newModules[m.Name] = m
	}

	allNames := mcpUnionKeys(oldModules, newModules)
	sort.Strings(allNames)

	for _, name := range allNames {
		oldMod := oldModules[name]
		newMod := newModules[name]
		diff := mcpDiffModule(name, oldMod, newMod)
		result.Modules = append(result.Modules, diff)
		if len(diff.BreakingChanges) > 0 {
			result.BreakingChanges = append(result.BreakingChanges, mcpBreakingChangeSummary{
				ModuleName: name,
				Changes:    diff.BreakingChanges,
			})
		}
	}

	oldPipelines := mcpNormalisePipelines(oldCfg.Pipelines)
	newPipelines := mcpNormalisePipelines(newCfg.Pipelines)
	allPNames := mcpUnionStringKeys(oldPipelines, newPipelines)
	sort.Strings(allPNames)

	for _, name := range allPNames {
		oldP, hasOld := oldPipelines[name]
		newP, hasNew := newPipelines[name]
		result.Pipelines = append(result.Pipelines, mcpDiffPipeline(name, oldP, hasOld, newP, hasNew))
	}

	return result
}

func mcpDiffModule(name string, oldMod, newMod *config.ModuleConfig) mcpModuleDiff {
	d := mcpModuleDiff{Name: name}

	switch {
	case oldMod == nil && newMod != nil:
		d.Status = "added"
		d.Type = newMod.Type
		d.Stateful = mcpIsStateful(newMod.Type)
		d.Detail = "NEW"
	case oldMod != nil && newMod == nil:
		d.Status = "removed"
		d.Type = oldMod.Type
		d.Stateful = mcpIsStateful(oldMod.Type)
		if d.Stateful {
			d.Detail = "REMOVED — WARNING: stateful resource may still hold data"
		} else {
			d.Detail = "REMOVED (stateless, safe to remove)"
		}
	default:
		d.Type = newMod.Type
		d.Stateful = mcpIsStateful(newMod.Type)
		breaking := mcpDetectBreakingChanges(oldMod, newMod)
		configChanged := mcpIsConfigChanged(oldMod.Config, newMod.Config)
		typeChanged := oldMod.Type != newMod.Type

		switch {
		case typeChanged:
			d.Status = "changed"
			d.Detail = fmt.Sprintf("TYPE CHANGED: %s → %s", oldMod.Type, newMod.Type)
			d.BreakingChanges = breaking
		case len(breaking) > 0:
			d.Status = "changed"
			parts := make([]string, 0, len(breaking))
			for _, bc := range breaking {
				parts = append(parts, fmt.Sprintf("%s: %s → %s", bc.Field, mcpDescribeValue(bc.OldValue), mcpDescribeValue(bc.NewValue)))
			}
			d.Detail = "CONFIG CHANGED: " + strings.Join(parts, "; ")
			d.BreakingChanges = breaking
		case configChanged:
			d.Status = "changed"
			d.Detail = "CONFIG CHANGED"
		default:
			d.Status = "unchanged"
			d.Detail = "UNCHANGED"
		}
	}
	return d
}

func mcpDiffPipeline(name string, oldP map[string]any, hasOld bool, newP map[string]any, hasNew bool) mcpPipelineDiff {
	d := mcpPipelineDiff{Name: name}
	switch {
	case !hasOld && hasNew:
		d.Status = "added"
		d.Trigger = mcpDescribePipelineTrigger(newP)
		d.Detail = "NEW"
	case hasOld && !hasNew:
		d.Status = "removed"
		d.Trigger = mcpDescribePipelineTrigger(oldP)
		d.Detail = "REMOVED"
	default:
		d.Trigger = mcpDescribePipelineTrigger(newP)
		oldTrigger := mcpDescribePipelineTrigger(oldP)
		newTrigger := d.Trigger
		oldSteps := mcpCountSteps(oldP)
		newSteps := mcpCountSteps(newP)

		switch {
		case oldTrigger != newTrigger:
			d.Status = "changed"
			d.Detail = fmt.Sprintf("TRIGGER CHANGED: %s → %s", oldTrigger, newTrigger)
		case oldSteps != newSteps:
			d.Status = "changed"
			d.Detail = fmt.Sprintf("STEPS CHANGED: %d → %d steps", oldSteps, newSteps)
		default:
			d.Status = "unchanged"
			d.Detail = "UNCHANGED"
		}
	}
	return d
}

// mcpIsStateful checks if a module type manages persistent state.
func mcpIsStateful(moduleType string) bool {
	switch moduleType {
	case "storage.sqlite", "database.workflow", "persistence.store",
		"messaging.broker", "messaging.nats", "messaging.kafka", "messaging.broker.eventbus",
		"static.fileserver":
		return true
	default:
		return false
	}
}

// mcpStatefulBreakingKeys returns the config keys that are breaking for a stateful module type.
func mcpStatefulBreakingKeys(moduleType string) []string {
	switch moduleType {
	case "storage.sqlite":
		return []string{"dbPath", "path"}
	case "database.workflow":
		return []string{"dsn", "driver", "host", "port", "database", "dbname"}
	case "persistence.store":
		return []string{"database"}
	case "messaging.broker", "messaging.broker.eventbus":
		return []string{"persistence", "dataDir"}
	case "messaging.nats":
		return []string{"url", "clusterID"}
	case "messaging.kafka":
		return []string{"brokers", "topic"}
	case "static.fileserver":
		return []string{"rootDir", "dir"}
	default:
		return nil
	}
}

func mcpDetectBreakingChanges(oldMod, newMod *config.ModuleConfig) []mcpBreakingChange {
	if oldMod == nil || newMod == nil {
		return nil
	}
	var changes []mcpBreakingChange
	if oldMod.Type != newMod.Type {
		changes = append(changes, mcpBreakingChange{
			Field:    "type",
			OldValue: oldMod.Type,
			NewValue: newMod.Type,
			Message:  fmt.Sprintf("module type changed from %q to %q", oldMod.Type, newMod.Type),
		})
		return changes
	}
	if !mcpIsStateful(oldMod.Type) {
		return nil
	}
	for _, key := range mcpStatefulBreakingKeys(oldMod.Type) {
		oldVal := mcpConfigValueStr(oldMod.Config, key)
		newVal := mcpConfigValueStr(newMod.Config, key)
		if oldVal != newVal {
			changes = append(changes, mcpBreakingChange{
				Field:    key,
				OldValue: oldVal,
				NewValue: newVal,
				Message:  fmt.Sprintf("config key %q changed: %s → %s", key, mcpDescribeValue(oldVal), mcpDescribeValue(newVal)),
			})
		}
	}
	return changes
}

func mcpConfigValueStr(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func mcpDescribeValue(v string) string {
	if v == "" {
		return "(unset)"
	}
	return v
}

func mcpIsConfigChanged(oldCfg, newCfg map[string]any) bool {
	if len(oldCfg) != len(newCfg) {
		return true
	}
	for k, ov := range oldCfg {
		nv, ok := newCfg[k]
		if !ok {
			return true
		}
		if fmt.Sprintf("%v", ov) != fmt.Sprintf("%v", nv) {
			return true
		}
	}
	return false
}

func mcpDescribePipelineTrigger(p map[string]any) string {
	if p == nil {
		return "unknown"
	}
	triggerRaw, ok := p["trigger"]
	if !ok {
		return "unknown"
	}
	triggerMap, ok := triggerRaw.(map[string]any)
	if !ok {
		return "unknown"
	}
	triggerType, _ := triggerMap["type"].(string)
	cfgRaw, ok := triggerMap["config"]
	if !ok {
		return triggerType
	}
	triggerCfg, ok := cfgRaw.(map[string]any)
	if !ok {
		return triggerType
	}
	method, _ := triggerCfg["method"].(string)
	path, _ := triggerCfg["path"].(string)
	if method != "" && path != "" {
		return fmt.Sprintf("%s %s %s", triggerType, method, path)
	}
	if path != "" {
		return fmt.Sprintf("%s %s", triggerType, path)
	}
	return triggerType
}

func mcpCountSteps(p map[string]any) int {
	if p == nil {
		return 0
	}
	stepsRaw, ok := p["steps"]
	if !ok {
		return 0
	}
	steps, ok := stepsRaw.([]any)
	if !ok {
		return 0
	}
	return len(steps)
}

func mcpNormalisePipelines(raw map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(raw))
	for name, v := range raw {
		if m, ok := v.(map[string]any); ok {
			out[name] = m
		} else {
			out[name] = nil
		}
	}
	return out
}

func mcpUnionKeys(a, b map[string]*config.ModuleConfig) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

func mcpUnionStringKeys(a, b map[string]map[string]any) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}

// --- Contract logic ---

func mcpGenerateContract(cfg *config.WorkflowConfig) *mcpContract {
	cfgData, _ := json.Marshal(cfg)
	hash := fmt.Sprintf("%x", sha256.Sum256(cfgData))[:16]

	contract := &mcpContract{
		Version:     "1.0",
		ConfigHash:  hash,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	for _, mod := range cfg.Modules {
		mc := mcpModuleContract{
			Name:     mod.Name,
			Type:     mod.Type,
			Stateful: mcpIsStateful(mod.Type),
		}
		contract.Modules = append(contract.Modules, mc)
	}
	sort.Slice(contract.Modules, func(i, j int) bool {
		return contract.Modules[i].Name < contract.Modules[j].Name
	})

	stepSet := make(map[string]bool)
	for pipelineName, pipelineRaw := range cfg.Pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}

		// Extract HTTP endpoints
		if triggerRaw, ok := pipelineMap["trigger"]; ok {
			if triggerMap, ok := triggerRaw.(map[string]any); ok {
				triggerType, _ := triggerMap["type"].(string)
				if triggerType == "http" {
					triggerCfg, _ := triggerMap["config"].(map[string]any)
					if triggerCfg != nil {
						path, _ := triggerCfg["path"].(string)
						method, _ := triggerCfg["method"].(string)
						if path != "" && method != "" {
							ep := mcpEndpoint{
								Method:   strings.ToUpper(method),
								Path:     path,
								Pipeline: pipelineName,
							}
							if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
								for _, stepRaw := range stepsRaw {
									if stepMap, ok := stepRaw.(map[string]any); ok {
										if st, _ := stepMap["type"].(string); st == "step.auth_required" {
											ep.AuthRequired = true
										}
									}
								}
							}
							contract.Endpoints = append(contract.Endpoints, ep)
						}
					}
				}
			}
		}

		// Extract steps and events
		if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
			for _, stepRaw := range stepsRaw {
				if stepMap, ok := stepRaw.(map[string]any); ok {
					stepType, _ := stepMap["type"].(string)
					if stepType != "" {
						stepSet[stepType] = true
					}
					if stepType == "step.publish" {
						if stepCfg, ok := stepMap["config"].(map[string]any); ok {
							if topic, ok := stepCfg["topic"].(string); ok && topic != "" {
								contract.Events = append(contract.Events, mcpEventContract{
									Topic:     topic,
									Direction: "publish",
									Pipeline:  pipelineName,
								})
							}
						}
					}
				}
			}
		}

		// Extract event subscriptions
		if triggerRaw, ok := pipelineMap["trigger"]; ok {
			if triggerMap, ok := triggerRaw.(map[string]any); ok {
				triggerType, _ := triggerMap["type"].(string)
				if triggerType == "event" {
					if triggerCfg, ok := triggerMap["config"].(map[string]any); ok {
						if topic, ok := triggerCfg["topic"].(string); ok && topic != "" {
							contract.Events = append(contract.Events, mcpEventContract{
								Topic:     topic,
								Direction: "subscribe",
								Pipeline:  pipelineName,
							})
						}
					}
				}
			}
		}
	}

	sort.Slice(contract.Endpoints, func(i, j int) bool {
		if contract.Endpoints[i].Path != contract.Endpoints[j].Path {
			return contract.Endpoints[i].Path < contract.Endpoints[j].Path
		}
		return contract.Endpoints[i].Method < contract.Endpoints[j].Method
	})

	for st := range stepSet {
		contract.Steps = append(contract.Steps, st)
	}
	sort.Strings(contract.Steps)

	sort.Slice(contract.Events, func(i, j int) bool {
		if contract.Events[i].Topic != contract.Events[j].Topic {
			return contract.Events[i].Topic < contract.Events[j].Topic
		}
		return contract.Events[i].Direction < contract.Events[j].Direction
	})

	return contract
}

func mcpCompareContracts(base, current *mcpContract) *mcpContractComparison {
	comp := &mcpContractComparison{
		BaseVersion:    base.Version,
		CurrentVersion: current.Version,
	}

	baseEPs := make(map[string]mcpEndpoint)
	for _, ep := range base.Endpoints {
		baseEPs[ep.Method+" "+ep.Path] = ep
	}
	currentEPs := make(map[string]mcpEndpoint)
	for _, ep := range current.Endpoints {
		currentEPs[ep.Method+" "+ep.Path] = ep
	}

	for key, baseEP := range baseEPs {
		if currentEP, exists := currentEPs[key]; exists {
			if baseEP.AuthRequired != currentEP.AuthRequired && !baseEP.AuthRequired {
				comp.Endpoints = append(comp.Endpoints, mcpEndpointChange{
					Method:     baseEP.Method,
					Path:       baseEP.Path,
					Pipeline:   currentEP.Pipeline,
					Change:     "CHANGED",
					Detail:     "auth requirement added (clients without tokens will get 401)",
					IsBreaking: true,
				})
				comp.BreakingCount++
			} else {
				comp.Endpoints = append(comp.Endpoints, mcpEndpointChange{
					Method:   baseEP.Method,
					Path:     baseEP.Path,
					Pipeline: currentEP.Pipeline,
					Change:   "UNCHANGED",
				})
			}
		} else {
			comp.Endpoints = append(comp.Endpoints, mcpEndpointChange{
				Method:     baseEP.Method,
				Path:       baseEP.Path,
				Pipeline:   baseEP.Pipeline,
				Change:     "REMOVED",
				Detail:     "endpoint removed (clients calling this will get 404)",
				IsBreaking: true,
			})
			comp.BreakingCount++
		}
		delete(currentEPs, key)
	}

	for _, ep := range currentEPs {
		comp.Endpoints = append(comp.Endpoints, mcpEndpointChange{
			Method:   ep.Method,
			Path:     ep.Path,
			Pipeline: ep.Pipeline,
			Change:   "ADDED",
		})
	}

	sort.Slice(comp.Endpoints, func(i, j int) bool {
		if comp.Endpoints[i].Path != comp.Endpoints[j].Path {
			return comp.Endpoints[i].Path < comp.Endpoints[j].Path
		}
		return comp.Endpoints[i].Method < comp.Endpoints[j].Method
	})

	// Compare modules
	baseMods := make(map[string]mcpModuleContract)
	for _, m := range base.Modules {
		baseMods[m.Name] = m
	}
	currentMods := make(map[string]mcpModuleContract)
	for _, m := range current.Modules {
		currentMods[m.Name] = m
	}

	for name, baseMod := range baseMods {
		if _, exists := currentMods[name]; exists {
			comp.Modules = append(comp.Modules, mcpModuleChange{Name: name, Type: baseMod.Type, Change: "UNCHANGED"})
		} else {
			comp.Modules = append(comp.Modules, mcpModuleChange{Name: name, Type: baseMod.Type, Change: "REMOVED"})
		}
		delete(currentMods, name)
	}
	for name, currentMod := range currentMods {
		comp.Modules = append(comp.Modules, mcpModuleChange{Name: name, Type: currentMod.Type, Change: "ADDED"})
	}
	sort.Slice(comp.Modules, func(i, j int) bool {
		return comp.Modules[i].Name < comp.Modules[j].Name
	})

	// Compare events
	baseEvents := make(map[string]mcpEventContract)
	for _, e := range base.Events {
		baseEvents[e.Direction+":"+e.Topic] = e
	}
	currentEvents := make(map[string]mcpEventContract)
	for _, e := range current.Events {
		currentEvents[e.Direction+":"+e.Topic] = e
	}

	for key, baseEv := range baseEvents {
		if _, exists := currentEvents[key]; exists {
			comp.Events = append(comp.Events, mcpEventChange{Topic: baseEv.Topic, Direction: baseEv.Direction, Pipeline: baseEv.Pipeline, Change: "UNCHANGED"})
		} else {
			comp.Events = append(comp.Events, mcpEventChange{Topic: baseEv.Topic, Direction: baseEv.Direction, Pipeline: baseEv.Pipeline, Change: "REMOVED"})
		}
		delete(currentEvents, key)
	}
	for _, ev := range currentEvents {
		comp.Events = append(comp.Events, mcpEventChange{Topic: ev.Topic, Direction: ev.Direction, Pipeline: ev.Pipeline, Change: "ADDED"})
	}
	sort.Slice(comp.Events, func(i, j int) bool {
		return comp.Events[i].Topic < comp.Events[j].Topic
	})

	return comp
}

// --- Compat check logic ---

func mcpCheckCompatibility(cfg *config.WorkflowConfig) *mcpCompatResult {
	knownModules := make(map[string]bool)
	for _, t := range schema.KnownModuleTypes() {
		knownModules[t] = true
	}

	// Also check step types from the knownStepTypeDescriptions registry
	knownSteps := make(map[string]bool)
	for t := range knownStepTypeDescriptions() {
		knownSteps[t] = true
	}
	// Merge with schema-registered step types
	for _, t := range schema.KnownModuleTypes() {
		if strings.HasPrefix(t, "step.") {
			knownSteps[t] = true
		}
	}

	result := &mcpCompatResult{
		EngineVersion: Version,
		Compatible:    true,
	}

	// Check module types
	seen := make(map[string]bool)
	for _, mod := range cfg.Modules {
		if seen[mod.Type] {
			continue
		}
		seen[mod.Type] = true
		item := mcpCompatItem{Type: mod.Type}
		if knownModules[mod.Type] {
			item.Available = true
		} else {
			item.Available = false
			result.Compatible = false
			result.Issues = append(result.Issues, fmt.Sprintf("module type %q is not available in this engine version", mod.Type))
		}
		result.RequiredModules = append(result.RequiredModules, item)
	}

	// Check step types from pipelines
	stepSeen := make(map[string]bool)
	for _, pipelineRaw := range cfg.Pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}
		if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
			for _, stepRaw := range stepsRaw {
				if stepMap, ok := stepRaw.(map[string]any); ok {
					if stepType, ok := stepMap["type"].(string); ok && stepType != "" && !stepSeen[stepType] {
						stepSeen[stepType] = true
						item := mcpCompatItem{Type: stepType}
						if knownSteps[stepType] {
							item.Available = true
						} else {
							item.Available = false
							result.Compatible = false
							result.Issues = append(result.Issues, fmt.Sprintf("step type %q is not available in this engine version", stepType))
						}
						result.RequiredSteps = append(result.RequiredSteps, item)
					}
				}
			}
		}
	}

	sort.Slice(result.RequiredModules, func(i, j int) bool {
		return result.RequiredModules[i].Type < result.RequiredModules[j].Type
	})
	sort.Slice(result.RequiredSteps, func(i, j int) bool {
		return result.RequiredSteps[i].Type < result.RequiredSteps[j].Type
	})

	return result
}

// --- Template validate config logic ---

func mcpValidateWorkflowConfig(cfg *config.WorkflowConfig) mcpValidationResult {
	result := mcpValidationResult{Name: "config"}

	knownModuleSet := make(map[string]bool)
	for _, t := range schema.KnownModuleTypes() {
		knownModuleSet[t] = true
	}

	knownSteps := knownStepTypeDescriptions()
	knownStepSet := make(map[string]bool)
	for t := range knownSteps {
		knownStepSet[t] = true
	}
	for _, t := range schema.KnownModuleTypes() {
		if strings.HasPrefix(t, "step.") {
			knownStepSet[t] = true
		}
	}

	knownTriggers := make(map[string]bool)
	for _, t := range schema.KnownTriggerTypes() {
		knownTriggers[t] = true
	}
	// Add well-known defaults
	for _, t := range []string{"http", "event", "eventbus", "schedule", "reconciliation"} {
		knownTriggers[t] = true
	}

	// Build module name set
	moduleNames := make(map[string]bool)
	for _, mod := range cfg.Modules {
		moduleNames[mod.Name] = true
	}

	// 1. Validate module types
	result.ModuleCount = len(cfg.Modules)
	for _, mod := range cfg.Modules {
		if mod.Type == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("module %q has no type", mod.Name))
			continue
		}
		if !knownModuleSet[mod.Type] {
			result.Errors = append(result.Errors, fmt.Sprintf("module %q uses unknown type %q", mod.Name, mod.Type))
		} else {
			result.ModuleValid++
			// Config field hints from schema registry
			reg := schema.GetModuleSchemaRegistry()
			if ms := reg.Get(mod.Type); ms != nil && mod.Config != nil && len(ms.ConfigFields) > 0 {
				knownKeys := make(map[string]bool)
				snakeToCamel := make(map[string]string)
				for i := range ms.ConfigFields {
					f := &ms.ConfigFields[i]
					knownKeys[f.Key] = true
					if snake := schema.CamelToSnake(f.Key); snake != f.Key {
						snakeToCamel[snake] = f.Key
					}
				}
				for key := range mod.Config {
					if !knownKeys[key] {
						if camel, ok := snakeToCamel[key]; ok {
							result.Warnings = append(result.Warnings, fmt.Sprintf("module %q (%s) config field %q uses snake_case; use camelCase %q instead", mod.Name, mod.Type, key, camel))
						} else {
							result.Warnings = append(result.Warnings, fmt.Sprintf("module %q (%s) config field %q not in known fields", mod.Name, mod.Type, key))
						}
					}
				}
			}
		}

		// 2. Validate dependencies
		for _, dep := range mod.DependsOn {
			result.DepCount++
			if !moduleNames[dep] {
				result.Errors = append(result.Errors, fmt.Sprintf("module %q depends on unknown module %q", mod.Name, dep))
			} else {
				result.DepValid++
			}
		}
	}

	// 3. Validate step types in pipelines
	for pipelineName, pipelineRaw := range cfg.Pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}
		stepsRaw, _ := pipelineMap["steps"].([]any)
		for _, stepRaw := range stepsRaw {
			stepMap, ok := stepRaw.(map[string]any)
			if !ok {
				continue
			}
			result.StepCount++
			stepType, _ := stepMap["type"].(string)
			if stepType == "" {
				result.Errors = append(result.Errors, fmt.Sprintf("pipeline %q has a step with no type", pipelineName))
				continue
			}
			if !knownStepSet[stepType] {
				result.Errors = append(result.Errors, fmt.Sprintf("pipeline %q step uses unknown type %q", pipelineName, stepType))
			} else {
				result.StepValid++
				// Config key warnings from step descriptions
				if stepInfo, ok := knownSteps[stepType]; ok {
					if stepCfg, ok := stepMap["config"].(map[string]any); ok && len(stepInfo.ConfigKeys) > 0 {
						knownKeys := make(map[string]bool)
						for _, k := range stepInfo.ConfigKeys {
							knownKeys[k] = true
						}
						for key := range stepCfg {
							if !knownKeys[key] {
								result.Warnings = append(result.Warnings, fmt.Sprintf("pipeline %q step %q (%s) config field %q not in known fields", pipelineName, stepMap["name"], stepType, key))
							}
						}
					}
				}
			}
		}

		// 4. Validate trigger types
		if triggerRaw, ok := pipelineMap["trigger"]; ok {
			if triggerMap, ok := triggerRaw.(map[string]any); ok {
				triggerType, _ := triggerMap["type"].(string)
				if triggerType != "" {
					result.TriggerCount++
					if !knownTriggers[triggerType] {
						result.Errors = append(result.Errors, fmt.Sprintf("pipeline %q uses unknown trigger type %q", pipelineName, triggerType))
					} else {
						result.TriggerValid++
					}
				}
			}
		}
	}

	// 5. Validate top-level triggers
	for triggerName, triggerRaw := range cfg.Triggers {
		triggerMap, ok := triggerRaw.(map[string]any)
		if !ok {
			continue
		}
		triggerType, _ := triggerMap["type"].(string)
		if triggerType == "" {
			triggerType = triggerName
		}
		if triggerType != "" && !strings.HasPrefix(triggerType, "#") {
			result.TriggerCount++
			if knownTriggers[triggerType] || knownTriggers[triggerName] {
				result.TriggerValid++
			} else {
				result.Warnings = append(result.Warnings, fmt.Sprintf("trigger %q may use unknown type %q", triggerName, triggerType))
				result.TriggerValid++
			}
		}
	}

	return result
}

// --- Feature detection and GitHub Actions generation ---

func mcpDetectFeatures(cfg *config.WorkflowConfig) *mcpProjectFeatures {
	features := &mcpProjectFeatures{}
	typeSet := make(map[string]bool)

	for _, mod := range cfg.Modules {
		t := strings.ToLower(mod.Type)
		typeSet[mod.Type] = true

		switch {
		case strings.HasPrefix(t, "static.") || t == "static.fileserver":
			features.HasUI = true
		case strings.HasPrefix(t, "auth.") || strings.Contains(t, "jwt") || strings.Contains(t, "auth"):
			features.HasAuth = true
		case strings.HasPrefix(t, "storage.") || strings.HasPrefix(t, "database.") ||
			strings.Contains(t, "sqlite") || strings.Contains(t, "postgres") || strings.Contains(t, "mysql"):
			features.HasDatabase = true
		case strings.HasPrefix(t, "http.server") || strings.HasPrefix(t, "http.router"):
			features.HasHTTP = true
		}
	}

	for t := range typeSet {
		features.ModuleTypes = append(features.ModuleTypes, t)
	}
	sort.Strings(features.ModuleTypes)

	return features
}

func mcpGenerateCIWorkflow(features *mcpProjectFeatures) string {
	var b strings.Builder

	b.WriteString("name: CI\n")
	b.WriteString("on:\n")
	b.WriteString("  pull_request:\n")
	b.WriteString("    branches: [main]\n")
	b.WriteString("  push:\n")
	b.WriteString("    branches: [main]\n")
	b.WriteString("\n")
	b.WriteString("jobs:\n")
	b.WriteString("  validate:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.22'\n")
	b.WriteString("      - name: Install wfctl\n")
	b.WriteString("        run: go install github.com/GoCodeAlone/workflow/cmd/wfctl@latest\n")
	b.WriteString("      - name: Validate config\n")
	b.WriteString("        run: wfctl validate config.yaml\n")
	b.WriteString("      - name: Inspect config\n")
	b.WriteString("        run: wfctl inspect config.yaml\n")

	if features.HasUI {
		b.WriteString("      - uses: actions/setup-node@v4\n")
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '22'\n")
		b.WriteString("      - name: Build UI\n")
		b.WriteString("        run: wfctl build-ui --ui-dir ui\n")
	}

	if features.HasAuth {
		b.WriteString("      - name: Verify secrets setup\n")
		b.WriteString("        run: echo \"Secrets configured for auth modules\"\n")
		b.WriteString("        env:\n")
		b.WriteString("          JWT_SECRET: ${{ secrets.JWT_SECRET }}\n")
	}

	if features.HasDatabase {
		b.WriteString("      - name: Run migrations\n")
		b.WriteString("        run: wfctl migrate --config config.yaml\n")
		b.WriteString("        continue-on-error: true\n")
	}

	return b.String()
}

func mcpGenerateCDWorkflow(features *mcpProjectFeatures, registry, platforms string) string {
	var b strings.Builder

	b.WriteString("name: CD\n")
	b.WriteString("on:\n")
	b.WriteString("  push:\n")
	b.WriteString("    tags: ['v*']\n")
	b.WriteString("\n")
	b.WriteString("env:\n")
	fmt.Fprintf(&b, "  REGISTRY: %s\n", registry)
	b.WriteString("\n")
	b.WriteString("jobs:\n")
	b.WriteString("  build:\n")
	b.WriteString("    runs-on: ubuntu-latest\n")
	b.WriteString("    permissions:\n")
	b.WriteString("      contents: read\n")
	b.WriteString("      packages: write\n")
	b.WriteString("    steps:\n")
	b.WriteString("      - uses: actions/checkout@v4\n")
	b.WriteString("      - uses: actions/setup-go@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          go-version: '1.22'\n")

	if features.HasUI {
		b.WriteString("      - uses: actions/setup-node@v4\n")
		b.WriteString("        with:\n")
		b.WriteString("          node-version: '22'\n")
		b.WriteString("      - name: Build UI\n")
		b.WriteString("        run: |\n")
		b.WriteString("          cd ui && npm ci && npm run build && cd ..\n")
	}

	b.WriteString("      - name: Build binary\n")
	b.WriteString("        run: |\n")
	b.WriteString("          GOOS=linux GOARCH=amd64 go build -o bin/server ./cmd/server/\n")
	b.WriteString("      - name: Log in to registry\n")
	b.WriteString("        uses: docker/login-action@v3\n")
	b.WriteString("        with:\n")
	b.WriteString("          registry: ${{ env.REGISTRY }}\n")
	b.WriteString("          username: ${{ github.actor }}\n")
	b.WriteString("          password: ${{ secrets.GITHUB_TOKEN }}\n")
	b.WriteString("      - name: Set up Docker Buildx\n")
	b.WriteString("        uses: docker/setup-buildx-action@v3\n")
	b.WriteString("      - name: Build and push Docker image\n")
	b.WriteString("        uses: docker/build-push-action@v5\n")
	b.WriteString("        with:\n")
	b.WriteString("          context: .\n")
	b.WriteString("          push: true\n")
	fmt.Fprintf(&b, "          platforms: %s\n", platforms)
	b.WriteString("          tags: |\n")
	b.WriteString("            ${{ env.REGISTRY }}/${{ github.repository }}:${{ github.ref_name }}\n")
	b.WriteString("            ${{ env.REGISTRY }}/${{ github.repository }}:latest\n")

	return b.String()
}

func (s *Server) handleModernize(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	apply := mcp.ParseBoolean(req, "apply", false)
	rulesFilter := mcp.ParseString(req, "rules", "")
	excludeFilter := mcp.ParseString(req, "exclude_rules", "")

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(yamlContent), &doc); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	rules := modernize.AllRules()

	// Include modernize rules from installed external plugins when a plugin
	// directory was configured at server startup.
	if s.pluginDir != "" {
		pluginRules, err := modernize.LoadRulesFromDir(s.pluginDir)
		if err == nil {
			rules = append(rules, pluginRules...)
		}
	}

	rules = modernize.FilterRules(rules, rulesFilter, excludeFilter)

	// Check phase
	var allFindings []modernize.Finding
	for _, r := range rules {
		allFindings = append(allFindings, r.Check(&doc, []byte(yamlContent))...)
	}

	result := map[string]any{
		"findings": allFindings,
		"count":    len(allFindings),
	}

	if apply && len(allFindings) > 0 {
		var changes []modernize.Change
		for _, r := range rules {
			if r.Fix == nil {
				continue
			}
			changes = append(changes, r.Fix(&doc)...)
		}
		out, err := yaml.Marshal(&doc)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("marshal error: %v", err)), nil
		}
		result["changes"] = changes
		result["change_count"] = len(changes)
		result["fixed_yaml"] = string(out)
	}

	return marshalToolResult(result)
}

// registryManifest is a subset of fields from a plugin registry manifest.json.
type registryManifest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Type        string   `json:"type"`
	Tier        string   `json:"tier"`
	License     string   `json:"license"`
	Private     bool     `json:"private"`
	Keywords    []string `json:"keywords"`
	Repository  string   `json:"repository"`
}

func (s *Server) handleRegistrySearch(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s.registryDir == "" {
		return mcp.NewToolResultError("no registry directory configured; start the server with --registry-dir or WithRegistryDir option"), nil
	}

	query := strings.ToLower(mcp.ParseString(req, "query", ""))
	typeFilter := strings.ToLower(mcp.ParseString(req, "type", ""))
	tierFilter := strings.ToLower(mcp.ParseString(req, "tier", ""))
	includePrivate := mcp.ParseBoolean(req, "include_private", false)

	pluginsDir := filepath.Join(s.registryDir, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read registry plugins directory %s: %v", pluginsDir, err)), nil
	}

	var matches []registryManifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginsDir, e.Name(), "manifest.json")
		data, err := os.ReadFile(manifestPath) //nolint:gosec // G304: path is within registry directory
		if err != nil {
			continue
		}
		var m registryManifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}

		// Filter private plugins
		if m.Private && !includePrivate {
			continue
		}

		// Filter by type
		if typeFilter != "" && !strings.EqualFold(m.Type, typeFilter) {
			continue
		}

		// Filter by tier
		if tierFilter != "" && !strings.EqualFold(m.Tier, tierFilter) {
			continue
		}

		// Filter by query (match name, description, or keywords)
		if query != "" {
			matched := strings.Contains(strings.ToLower(m.Name), query) ||
				strings.Contains(strings.ToLower(m.Description), query)
			if !matched {
				for _, kw := range m.Keywords {
					if strings.Contains(strings.ToLower(kw), query) {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}

		matches = append(matches, m)
	}

	result := map[string]any{
		"plugins": matches,
		"count":   len(matches),
	}
	return marshalToolResult(result)
}

func mcpGenerateReleaseWorkflow() string {
	return `name: Release
on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.22'
      - name: Build plugin binaries
        run: |
          mkdir -p dist
          for GOOS in linux darwin; do
            for GOARCH in amd64 arm64; do
              GOOS=$GOOS GOARCH=$GOARCH go build -o dist/plugin-$GOOS-$GOARCH ./cmd/*/
            done
          done
      - name: Create release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/*
`
}
