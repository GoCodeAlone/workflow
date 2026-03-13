package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// registerNewTools registers the additional schema and validation tools.
func (s *Server) registerNewTools() {
	// get_module_schema
	s.mcpServer.AddTool(
		mcp.NewTool("get_module_schema",
			mcp.WithDescription("Return the full configuration schema for a given module type. "+
				"Includes description, config fields (key, type, description, required, default, options), "+
				"inputs, outputs, and an example usage snippet."),
			mcp.WithString("module_type",
				mcp.Required(),
				mcp.Description("The module type string (e.g. 'http.server', 'messaging.broker')"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetModuleSchema,
	)

	// get_step_schema
	s.mcpServer.AddTool(
		mcp.NewTool("get_step_schema",
			mcp.WithDescription("Return the schema for a given pipeline step type. "+
				"Includes description, config keys with types and descriptions, and an example usage snippet."),
			mcp.WithString("step_type",
				mcp.Required(),
				mcp.Description("The step type string (e.g. 'step.set', 'step.http_call')"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetStepSchema,
	)

	// get_template_functions
	s.mcpServer.AddTool(
		mcp.NewTool("get_template_functions",
			mcp.WithDescription("Return the complete list of available Go template functions for pipeline templates. "+
				"Each function includes its name, signature, description, and an example usage."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetTemplateFunctions,
	)

	// validate_template_expressions
	s.mcpServer.AddTool(
		mcp.NewTool("validate_template_expressions",
			mcp.WithDescription("Validate template expressions ({{ ... }}) in a YAML pipeline config string. "+
				"Checks for forward references, self-references, undefined step references, "+
				"and hyphenated step name dot-access patterns that require index syntax."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the pipeline configuration to validate"),
			),
		),
		s.handleValidateTemplateExpressions,
	)

	// infer_pipeline_context
	s.mcpServer.AddTool(
		mcp.NewTool("infer_pipeline_context",
			mcp.WithDescription("Infer the available template variables at a specific point in a pipeline. "+
				"Analyzes preceding step outputs to tell you what .steps.* fields are available, "+
				"plus trigger, body, and meta context. Useful for authoring template expressions."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the workflow configuration"),
			),
			mcp.WithString("pipeline_name",
				mcp.Required(),
				mcp.Description("The name of the pipeline to analyze"),
			),
			mcp.WithString("after_step",
				mcp.Description("Step name to analyze context after. Omit to get context at the start of the pipeline."),
			),
			mcp.WithString("openapi_spec",
				mcp.Description("Optional OpenAPI 3.0 JSON/YAML spec to enrich request body schema context"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleInferPipelineContext,
	)

	// get_config_examples
	s.mcpServer.AddTool(
		mcp.NewTool("get_config_examples",
			mcp.WithDescription("List and optionally return example workflow config files from the example/ directory. "+
				"When called without a name, lists all available examples. "+
				"When called with a name, returns the full content of that example."),
			mcp.WithString("name",
				mcp.Description("Name of a specific example to fetch (e.g. 'api-server-config'). Omit to list all examples."),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetConfigExamples,
	)
}

// --- Tool Handlers for new tools ---

func (s *Server) handleGetModuleSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	moduleType := mcp.ParseString(req, "module_type", "")
	if moduleType == "" {
		return mcp.NewToolResultError("module_type is required"), nil
	}

	reg := schema.GetModuleSchemaRegistry()
	ms := reg.Get(moduleType)
	if ms == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown module type %q", moduleType)), nil
	}

	example := generateModuleExample(ms)

	result := map[string]any{
		"type":          ms.Type,
		"label":         ms.Label,
		"category":      ms.Category,
		"description":   ms.Description,
		"inputs":        ms.Inputs,
		"outputs":       ms.Outputs,
		"configFields":  ms.ConfigFields,
		"defaultConfig": ms.DefaultConfig,
		"example":       example,
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetStepSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stepType := mcp.ParseString(req, "step_type", "")
	if stepType == "" {
		return mcp.NewToolResultError("step_type is required"), nil
	}
	if !strings.HasPrefix(stepType, "step.") {
		return mcp.NewToolResultError(fmt.Sprintf("step type must begin with 'step.', got %q", stepType)), nil
	}

	// Try the step schema registry first (rich metadata with outputs).
	reg := schema.GetStepSchemaRegistry()
	ss := reg.Get(stepType)
	if ss != nil {
		configKeys := make([]string, 0, len(ss.ConfigFields))
		configDefs := make([]map[string]any, 0, len(ss.ConfigFields))
		for i := range ss.ConfigFields {
			cf := &ss.ConfigFields[i]
			configKeys = append(configKeys, cf.Key)
			def := map[string]any{
				"key":         cf.Key,
				"type":        string(cf.Type),
				"description": cf.Description,
				"required":    cf.Required,
			}
			if cf.DefaultValue != nil {
				def["default"] = cf.DefaultValue
			}
			if len(cf.Options) > 0 {
				def["options"] = cf.Options
			}
			configDefs = append(configDefs, def)
		}

		outputs := make([]map[string]any, 0, len(ss.Outputs))
		for _, o := range ss.Outputs {
			outputs = append(outputs, map[string]any{
				"key":         o.Key,
				"type":        o.Type,
				"description": o.Description,
			})
		}

		result := map[string]any{
			"type":        ss.Type,
			"description": ss.Description,
			"plugin":      ss.Plugin,
			"configKeys":  configKeys,
			"configDefs":  configDefs,
			"outputs":     outputs,
			"example":     generateStepExample(ss.Type, configKeys),
		}
		if len(ss.ReadKeys) > 0 {
			result["readKeys"] = ss.ReadKeys
		}
		return marshalToolResult(result)
	}

	// Fall back to checking if the type is in the known module types list.
	known := schema.KnownModuleTypes()
	found := false
	for _, t := range known {
		if t == stepType {
			found = true
			break
		}
	}
	if !found {
		return mcp.NewToolResultError(fmt.Sprintf("unknown step type %q", stepType)), nil
	}
	// Return minimal info for step types not in the schema registry.
	result := map[string]any{
		"type":       stepType,
		"configKeys": []string{},
		"example":    generateStepExample(stepType, []string{}),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetTemplateFunctions(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	funcs := module.TemplateFuncDescriptions()
	return marshalToolResult(map[string]any{
		"functions": funcs,
		"count":     len(funcs),
	})
}

func (s *Server) handleValidateTemplateExpressions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	// Parse config to get basic structure.
	_, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	// Re-parse into a generic map to access the pipelines section (which is map[string]any in config).
	var rawDoc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &rawDoc); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	// Extract pipelines section.
	pipelinesRaw, _ := rawDoc["pipelines"].(map[string]any)

	// Re-marshal pipelines back to YAML and parse as typed PipelineConfig.
	type minStep struct {
		Name   string         `yaml:"name"`
		Type   string         `yaml:"type"`
		Config map[string]any `yaml:"config"`
	}
	type minPipeline struct {
		Steps []minStep `yaml:"steps"`
	}

	var warnings []string

	pipelines := make(map[string]minPipeline, len(pipelinesRaw))
	for pName, pRaw := range pipelinesRaw {
		data, err := yaml.Marshal(pRaw)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("[pipeline=%s] could not re-marshal pipeline for analysis: %v", pName, err))
			continue
		}
		var p minPipeline
		if err := yaml.Unmarshal(data, &p); err != nil {
			warnings = append(warnings, fmt.Sprintf("[pipeline=%s] could not parse pipeline steps for analysis: %v", pName, err))
			continue
		}
		pipelines[pName] = p
	}

	// Regex patterns for template expression analysis.
	// templateRefRe is a heuristic matcher — it may not handle every edge case in Go templates.
	templateRefRe := regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	stepsRefRe := regexp.MustCompile(`\.steps\.([a-zA-Z0-9_-]+)`)
	hyphenStepRe := regexp.MustCompile(`\.steps\.([a-zA-Z0-9_]*-[a-zA-Z0-9_-]*)`)

	// Walk every pipeline step config looking for template patterns.
	for pName, p := range pipelines {
		// Build ordered step index map.
		stepIndexMap := make(map[string]int, len(p.Steps))
		for i, step := range p.Steps {
			stepIndexMap[step.Name] = i
		}

		for stepIdx, step := range p.Steps {
			// Compile self-reference regex once per step, outside the config/expression loops.
			var selfRefRe *regexp.Regexp
			if step.Name != "" {
				selfRefRe = regexp.MustCompile(`\.steps\.` + regexp.QuoteMeta(step.Name) + `\b`)
			}

			// Check all config values for template expressions.
			for configKey, configVal := range step.Config {
				valStr := fmt.Sprintf("%v", configVal)
				if !strings.Contains(valStr, "{{") {
					continue
				}
				exprs := templateRefRe.FindAllString(valStr, -1)
				for _, expr := range exprs {
					// Self-reference check.
					if selfRefRe != nil {
						if selfRefRe.MatchString(expr) {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] self-reference: step %q references itself in %s",
								pName, step.Name, configKey, step.Name, expr,
							))
						}
					}

					// warnedRefs tracks step names that have already received a forward/undefined warning
					// so the hyphen check below doesn't emit a redundant second warning for the same ref.
					warnedRefs := make(map[string]bool)
					refs := stepsRefRe.FindAllStringSubmatch(expr, -1)
					for _, ref := range refs {
						refName := ref[1]
						// Forward reference check.
						if refIdx, exists := stepIndexMap[refName]; exists && refIdx > stepIdx {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] forward reference: step %q (index %d) references step %q (index %d) which has not yet run",
								pName, step.Name, configKey, step.Name, stepIdx, refName, refIdx,
							))
							warnedRefs[refName] = true
						}
						// Undefined step reference check.
						if _, exists := stepIndexMap[refName]; !exists {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] undefined step reference: %q not found in pipeline",
								pName, step.Name, configKey, refName,
							))
							warnedRefs[refName] = true
						}
					}

					// Hyphenated dot-access check — skip refs already reported above to avoid duplicate warnings.
					hyphenRefs := hyphenStepRe.FindAllStringSubmatch(expr, -1)
					for _, href := range hyphenRefs {
						hyphenName := href[1]
						if !warnedRefs[hyphenName] {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] hyphenated step name %q uses dot-access; the engine auto-corrects this, but consider using {{ index .steps %q \"field\" }} for clarity",
								pName, step.Name, configKey, hyphenName, hyphenName,
							))
						}
					}
				}
			}
		}
	}

	sort.Strings(warnings)

	result := map[string]any{
		"warnings":          warnings,
		"warning_count":     len(warnings),
		"pipelines_checked": len(pipelines),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetConfigExamples(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(req, "name", "")

	exampleDir := "example"
	// Support absolute path from the server's working directory.
	// Only derive the root when pluginDir follows the expected "data/plugins" layout.
	if s.pluginDir != "" {
		// Validate expected layout: pluginDir must end with "data/plugins" (or "data"+sep+"plugins").
		pluginBase := filepath.Base(s.pluginDir)
		dataDir := filepath.Dir(s.pluginDir)
		dataBase := filepath.Base(dataDir)
		if pluginBase == "plugins" && dataBase == "data" {
			candidate := filepath.Join(filepath.Dir(dataDir), "example")
			if _, err := os.Stat(candidate); err == nil {
				exampleDir = candidate
			}
		}
	}

	if name != "" {
		// Return the content of the named example.
		content, filename, err := readExampleFile(exampleDir, name)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("example %q not found: %v", name, err)), nil
		}
		result := map[string]any{
			"name":     name,
			"filename": filename,
			"content":  content,
		}
		return marshalToolResult(result)
	}

	// List all .yaml files in the example directory.
	examples, err := listExamples(exampleDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list examples: %v", err)), nil
	}

	result := map[string]any{
		"examples": examples,
		"count":    len(examples),
	}
	return marshalToolResult(result)
}

// exampleInfo describes an available config example.
type exampleInfo struct {
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Description string `json:"description,omitempty"`
}

// listExamples lists all YAML example files in the given directory.
func listExamples(exampleDir string) ([]exampleInfo, error) {
	entries, err := os.ReadDir(exampleDir)
	if os.IsNotExist(err) {
		return []exampleInfo{}, nil
	}
	if err != nil {
		return nil, err
	}

	var examples []exampleInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		// Strip leading numbers (e.g. "01-foo" -> "01-foo" kept as-is).
		examples = append(examples, exampleInfo{
			Name:     name,
			Filename: e.Name(),
		})
	}
	return examples, nil
}

// readExampleFile reads an example YAML file by name.
// name can be the base name with or without the .yaml extension.
func readExampleFile(exampleDir, name string) (string, string, error) {
	// Path traversal protection: reject names containing ".." or path separators.
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return "", "", fmt.Errorf("invalid example name %q", name)
	}

	// Normalize name.
	if !strings.HasSuffix(name, ".yaml") {
		name += ".yaml"
	}

	absDir, err := filepath.Abs(exampleDir)
	if err != nil {
		return "", "", fmt.Errorf("invalid example directory: %w", err)
	}

	resolved := filepath.Join(absDir, name)
	// Verify the resolved path stays within exampleDir.
	if !strings.HasPrefix(resolved, absDir+string(filepath.Separator)) {
		return "", "", fmt.Errorf("invalid example name %q", name)
	}

	data, err := os.ReadFile(resolved) //nolint:gosec // G304: path is validated to be within absDir
	if err == nil {
		return string(data), filepath.Base(resolved), nil
	}
	return "", "", fmt.Errorf("file not found")
}

// generateModuleExample creates an example YAML snippet for a module schema.
func generateModuleExample(ms *schema.ModuleSchema) string {
	var b strings.Builder
	name := strings.ReplaceAll(ms.Type, ".", "-")
	fmt.Fprintf(&b, "modules:\n  - name: my-%s\n    type: %s\n", name, ms.Type)
	if len(ms.ConfigFields) > 0 {
		b.WriteString("    config:\n")
		for i := range ms.ConfigFields {
			f := &ms.ConfigFields[i]
			val := f.DefaultValue
			if val == nil {
				val = exampleValue(*f)
			}
			fmt.Fprintf(&b, "      %s: %v\n", f.Key, val)
		}
	}
	return b.String()
}

// exampleValue returns a placeholder value for a config field.
func exampleValue(f schema.ConfigFieldDef) any {
	switch f.Type {
	case schema.FieldTypeString, schema.FieldTypeFilePath, schema.FieldTypeSQL:
		if f.Placeholder != "" {
			return f.Placeholder
		}
		return ""
	case schema.FieldTypeNumber:
		return 0
	case schema.FieldTypeBool:
		return false
	case schema.FieldTypeDuration:
		return "30s"
	case schema.FieldTypeSelect:
		if len(f.Options) > 0 {
			return f.Options[0]
		}
		return ""
	case schema.FieldTypeArray:
		return "[]"
	case schema.FieldTypeMap, schema.FieldTypeJSON:
		return "{}"
	default:
		return ""
	}
}

// generateStepExample creates an example YAML snippet for a pipeline step type.
func generateStepExample(stepType string, configKeys []string) string {
	var b strings.Builder
	name := strings.TrimPrefix(stepType, "step.")
	name = strings.ReplaceAll(name, "_", "-")
	fmt.Fprintf(&b, "pipelines:\n  my-pipeline:\n    steps:\n")
	fmt.Fprintf(&b, "      - name: %s-step\n        type: %s\n", name, stepType)
	if len(configKeys) > 0 {
		b.WriteString("        config:\n")
		for _, k := range configKeys {
			fmt.Fprintf(&b, "          %s: \"\"\n", k)
		}
	}
	return b.String()
}

// handleInferPipelineContext analyzes a pipeline and returns the available
// template variables at the specified position (after a given step).
func (s *Server) handleInferPipelineContext(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}
	pipelineName := mcp.ParseString(req, "pipeline_name", "")
	if pipelineName == "" {
		return mcp.NewToolResultError("pipeline_name is required"), nil
	}
	afterStep := mcp.ParseString(req, "after_step", "")

	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	rawPipeline, ok := cfg.Pipelines[pipelineName]
	if !ok {
		names := make([]string, 0, len(cfg.Pipelines))
		for k := range cfg.Pipelines {
			names = append(names, k)
		}
		sort.Strings(names)
		return mcp.NewToolResultError(fmt.Sprintf("pipeline %q not found; available: %v", pipelineName, names)), nil
	}

	// Decode the pipeline raw value into PipelineConfig.
	pipeline, err := decodePipelineConfig(rawPipeline)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to decode pipeline %q: %v", pipelineName, err)), nil
	}

	// Collect steps preceding the target position.
	precedingSteps := pipeline.Steps
	if afterStep != "" {
		idx := -1
		for i, step := range pipeline.Steps {
			if step.Name == afterStep {
				idx = i
				break
			}
		}
		if idx < 0 {
			stepNames := make([]string, 0, len(pipeline.Steps))
			for _, st := range pipeline.Steps {
				stepNames = append(stepNames, st.Name)
			}
			return mcp.NewToolResultError(fmt.Sprintf("step %q not found in pipeline %q; steps: %v", afterStep, pipelineName, stepNames)), nil
		}
		precedingSteps = pipeline.Steps[:idx+1]
	}

	// Infer outputs for each preceding step.
	stepReg := schema.GetStepSchemaRegistry()
	type stepContext struct {
		Name    string                  `json:"name"`
		Type    string                  `json:"type"`
		Outputs []schema.InferredOutput `json:"outputs"`
	}
	stepsCtx := make([]stepContext, 0, len(precedingSteps))
	for _, step := range precedingSteps {
		outputs := stepReg.InferStepOutputs(step.Type, step.Config)
		stepsCtx = append(stepsCtx, stepContext{
			Name:    step.Name,
			Type:    step.Type,
			Outputs: outputs,
		})
	}

	// Build trigger context info.
	triggerCtx := map[string]any{
		"type":        pipeline.Trigger.Type,
		"description": "Trigger data available via .trigger.*",
		"fields": []string{
			"path_params", "query", "headers", "body",
		},
	}

	// Meta context.
	metaCtx := map[string]any{
		"description": "Pipeline metadata available via .meta.*",
		"fields": []string{
			"pipeline_name", "trigger_type", "timestamp",
		},
	}

	result := map[string]any{
		"pipeline_name": pipelineName,
		"after_step":    afterStep,
		"steps":         stepsCtx,
		"trigger":       triggerCtx,
		"meta":          metaCtx,
		"summary": fmt.Sprintf("%d steps analyzed, %d preceding steps included",
			len(pipeline.Steps), len(precedingSteps)),
	}
	return marshalToolResult(result)
}

// decodePipelineConfig converts a raw map (from YAML unmarshalling) to a PipelineConfig.
func decodePipelineConfig(raw any) (*config.PipelineConfig, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var pc config.PipelineConfig
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return nil, err
	}
	return &pc, nil
}
