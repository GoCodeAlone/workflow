package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"text/template"

	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

// templateValidationResult holds the outcome of validating a single template.
type templateValidationResult struct {
	Name         string
	ModuleCount  int
	ModuleValid  int
	StepCount    int
	StepValid    int
	DepCount     int
	DepValid     int
	TriggerCount int
	TriggerValid int
	Warnings     []string
	Errors       []string
}

// pass returns true if there are no errors.
func (r *templateValidationResult) pass() bool {
	return len(r.Errors) == 0
}

// templateValidationSummary holds overall validation output.
type templateValidationSummary struct {
	Results  []templateValidationResult `json:"results"`
	Total    int                        `json:"total"`
	Passed   int                        `json:"passed"`
	Failed   int                        `json:"failed"`
	Warnings int                        `json:"warnings"`
}

// templateVarRegex matches Go template variables like {{.VarName}}.
var templateVarRegex = regexp.MustCompile(`\{\{\.([A-Za-z][A-Za-z0-9_]*)\}\}`)

// runTemplate dispatches template subcommands.
func runTemplate(args []string) error {
	if len(args) < 1 {
		return templateUsage()
	}
	switch args[0] {
	case "validate":
		return runTemplateValidate(args[1:])
	default:
		return templateUsage()
	}
}

func templateUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl template <subcommand> [options]

Subcommands:
  validate   Validate project templates or a specific config file

Run 'wfctl template validate -h' for details.
`)
	return fmt.Errorf("template subcommand is required")
}

// runTemplateValidate validates project templates or an explicit config file.
func runTemplateValidate(args []string) error {
	fs2 := flag.NewFlagSet("template validate", flag.ContinueOnError)
	templateName := fs2.String("template", "", "Validate a specific template (default: all)")
	configFile := fs2.String("config", "", "Validate a specific config file instead of templates")
	strict := fs2.Bool("strict", false, "Fail on warnings (not just errors)")
	format := fs2.String("format", "text", "Output format: text or json")
	fs2.Usage = func() {
		fmt.Fprintf(fs2.Output(), `Usage: wfctl template validate [options]

Validate project templates against the engine's known module and step types.

Options:
`)
		fs2.PrintDefaults()
	}
	if err := fs2.Parse(args); err != nil {
		return err
	}

	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	var results []templateValidationResult

	if *configFile != "" {
		// Validate a specific config file
		cfg, err := config.LoadFromFile(*configFile)
		if err != nil {
			return fmt.Errorf("failed to load config %s: %w", *configFile, err)
		}
		result := validateWorkflowConfig(*configFile, cfg, knownModules, knownSteps, knownTriggers)
		results = append(results, result)
	} else {
		// Validate project templates
		var templatesToCheck []string
		if *templateName != "" {
			templatesToCheck = []string{*templateName}
		} else {
			templatesToCheck = []string{"api-service", "event-processor", "full-stack", "plugin", "ui-plugin"}
		}

		for _, tmplName := range templatesToCheck {
			result := validateProjectTemplate(tmplName, knownModules, knownSteps, knownTriggers)
			results = append(results, result)
		}
	}

	// Build summary
	summary := templateValidationSummary{
		Results: results,
		Total:   len(results),
	}
	totalWarnings := 0
	for i := range results {
		if results[i].pass() {
			summary.Passed++
		} else {
			summary.Failed++
		}
		totalWarnings += len(results[i].Warnings)
	}
	summary.Warnings = totalWarnings

	// Output
	switch strings.ToLower(*format) {
	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	default:
		printTemplateValidationResults(results, summary)
	}

	if summary.Failed > 0 {
		return fmt.Errorf("%d/%d templates failed validation", summary.Failed, summary.Total)
	}
	if *strict && totalWarnings > 0 {
		return fmt.Errorf("%d warning(s) in strict mode", totalWarnings)
	}
	return nil
}

// validateProjectTemplate validates a named project template by rendering its workflow.yaml.tmpl
// with placeholder values and checking the resulting config.
func validateProjectTemplate(name string, knownModules map[string]ModuleTypeInfo, knownSteps map[string]StepTypeInfo, knownTriggers map[string]bool) templateValidationResult {
	tmplPath := fmt.Sprintf("templates/%s/workflow.yaml.tmpl", name)

	data, err := templateFS.ReadFile(tmplPath)
	if err != nil {
		// Template may not have a workflow.yaml (e.g., plugin template)
		return templateValidationResult{
			Name:     name,
			Warnings: []string{"no workflow.yaml.tmpl found (skipping)"},
		}
	}

	// Check template variable completeness
	warnings := checkTemplateVars(string(data), name)

	// Render with placeholder values
	rendered, err := renderTemplateWithPlaceholders(string(data), name)
	if err != nil {
		return templateValidationResult{
			Name:   name,
			Errors: []string{fmt.Sprintf("failed to render template: %v", err)},
		}
	}

	// Parse the rendered YAML into a raw map to handle flexible trigger formats
	// (templates may use sequence format for triggers which WorkflowConfig doesn't support)
	rawCfg, err := parseRawYAML(rendered)
	if err != nil {
		return templateValidationResult{
			Name:   name,
			Errors: []string{fmt.Sprintf("failed to parse rendered template: %v", err)},
		}
	}

	cfg := rawConfigToWorkflowConfig(rawCfg)
	result := validateWorkflowConfig(name, cfg, knownModules, knownSteps, knownTriggers)
	result.Warnings = append(warnings, result.Warnings...)
	return result
}

// parseRawYAML parses a YAML string into a generic map.
func parseRawYAML(content string) (map[string]any, error) {
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// rawConfigToWorkflowConfig converts a raw YAML map into a WorkflowConfig,
// handling both map and sequence formats for triggers.
func rawConfigToWorkflowConfig(raw map[string]any) *config.WorkflowConfig {
	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: make(map[string]any),
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}

	// Extract modules
	if modulesRaw, ok := raw["modules"]; ok {
		if modulesList, ok := modulesRaw.([]any); ok {
			for _, modRaw := range modulesList {
				if modMap, ok := modRaw.(map[string]any); ok {
					mod := config.ModuleConfig{}
					if n, ok := modMap["name"].(string); ok {
						mod.Name = n
					}
					if t, ok := modMap["type"].(string); ok {
						mod.Type = t
					}
					if cfg2, ok := modMap["config"].(map[string]any); ok {
						mod.Config = cfg2
					}
					if deps, ok := modMap["dependsOn"].([]any); ok {
						for _, d := range deps {
							if s, ok := d.(string); ok {
								mod.DependsOn = append(mod.DependsOn, s)
							}
						}
					}
					cfg.Modules = append(cfg.Modules, mod)
				}
			}
		}
	}

	// Extract workflows
	if workflowsRaw, ok := raw["workflows"]; ok {
		if workflowsMap, ok := workflowsRaw.(map[string]any); ok {
			cfg.Workflows = workflowsMap
		}
	}

	// Extract triggers — support both map and sequence formats
	if triggersRaw, ok := raw["triggers"]; ok {
		switch t := triggersRaw.(type) {
		case map[string]any:
			cfg.Triggers = t
		case []any:
			// Convert sequence format to map using name as key
			for _, trigRaw := range t {
				if trigMap, ok := trigRaw.(map[string]any); ok {
					trigName, _ := trigMap["name"].(string)
					if trigName == "" {
						trigType, _ := trigMap["type"].(string)
						trigName = trigType
					}
					if trigName != "" {
						cfg.Triggers[trigName] = trigMap
					}
				}
			}
		}
	}

	// Extract pipelines
	if pipelinesRaw, ok := raw["pipelines"]; ok {
		if pipelinesMap, ok := pipelinesRaw.(map[string]any); ok {
			cfg.Pipelines = pipelinesMap
		}
	}

	return cfg
}

// renderTemplateWithPlaceholders renders a Go template with sample data.
func renderTemplateWithPlaceholders(tmplContent, name string) (string, error) {
	data := map[string]string{
		"Name":        "sample",
		"NameCamel":   "Sample",
		"Author":      "sample-author",
		"Description": "sample description",
	}

	tmpl, err := template.New(name).Parse(tmplContent)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// checkTemplateVars checks that all {{.Variable}} placeholders have corresponding data fields.
func checkTemplateVars(content, templateName string) []string {
	knownVars := map[string]bool{
		"Name":        true,
		"NameCamel":   true,
		"Author":      true,
		"Description": true,
	}

	var warnings []string
	matches := templateVarRegex.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		varName := m[1]
		if seen[varName] {
			continue
		}
		seen[varName] = true
		if !knownVars[varName] {
			warnings = append(warnings, fmt.Sprintf("template variable {{.%s}} has no corresponding data field", varName))
		}
	}
	return warnings
}

// validateWorkflowConfig checks a workflow config against known types.
func validateWorkflowConfig(name string, cfg *config.WorkflowConfig, knownModules map[string]ModuleTypeInfo, knownSteps map[string]StepTypeInfo, knownTriggers map[string]bool) templateValidationResult {
	result := templateValidationResult{Name: name}

	// Build module name set for dependency checking
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
		info, ok := knownModules[mod.Type]
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("module %q uses unknown type %q", mod.Name, mod.Type))
		} else {
			result.ModuleValid++
			// 5. Warn on unknown config fields
			if mod.Config != nil && len(info.ConfigKeys) > 0 {
				knownKeys := make(map[string]bool)
				for _, k := range info.ConfigKeys {
					knownKeys[k] = true
				}
				for key := range mod.Config {
					if !knownKeys[key] {
						result.Warnings = append(result.Warnings, fmt.Sprintf("module %q (%s) config field %q not in known fields", mod.Name, mod.Type, key))
					}
				}
			}
		}

		// 3. Validate dependencies
		for _, dep := range mod.DependsOn {
			result.DepCount++
			if !moduleNames[dep] {
				result.Errors = append(result.Errors, fmt.Sprintf("module %q depends on unknown module %q", mod.Name, dep))
			} else {
				result.DepValid++
			}
		}
	}

	// 2. Validate step types in pipelines
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
			stepInfo, ok := knownSteps[stepType]
			if !ok {
				result.Errors = append(result.Errors, fmt.Sprintf("pipeline %q step uses unknown type %q", pipelineName, stepType))
			} else {
				result.StepValid++
				// Config key warnings
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

		// 6. Validate template expressions in pipeline steps
		validatePipelineTemplates(pipelineName, stepsRaw, &result)

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

	// Also validate triggers in the top-level triggers map
	for triggerName, triggerRaw := range cfg.Triggers {
		triggerMap, ok := triggerRaw.(map[string]any)
		if !ok {
			continue
		}
		triggerType, _ := triggerMap["type"].(string)
		if triggerType == "" {
			// Some triggers embed their type as the key name
			triggerType = triggerName
		}
		if triggerType != "" && !strings.HasPrefix(triggerType, "#") {
			result.TriggerCount++
			if knownTriggers[triggerType] || knownTriggers[triggerName] {
				result.TriggerValid++
			} else {
				// Not an error since triggers can be dynamic; warn instead
				result.Warnings = append(result.Warnings, fmt.Sprintf("trigger %q may use unknown type %q", triggerName, triggerType))
				result.TriggerValid++ // still counted valid to avoid false errors
			}
		}
	}

	return result
}

// printTemplateValidationResults prints results in human-readable text format.
func printTemplateValidationResults(results []templateValidationResult, summary templateValidationSummary) {
	for i := range results {
		r := &results[i]
		fmt.Printf("Validating template: %s\n", r.Name)

		if r.ModuleCount > 0 {
			if r.ModuleValid == r.ModuleCount {
				fmt.Printf("  + Module types: %d/%d valid\n", r.ModuleValid, r.ModuleCount)
			} else {
				fmt.Printf("  x Module types: %d/%d valid\n", r.ModuleValid, r.ModuleCount)
			}
		}

		if r.StepCount > 0 {
			if r.StepValid == r.StepCount {
				fmt.Printf("  + Step types: %d/%d valid\n", r.StepValid, r.StepCount)
			} else {
				fmt.Printf("  x Step types: %d/%d valid\n", r.StepValid, r.StepCount)
			}
		}

		if r.DepCount > 0 {
			if r.DepValid == r.DepCount {
				fmt.Printf("  + Dependencies: all resolved\n")
			} else {
				fmt.Printf("  x Dependencies: %d/%d resolved\n", r.DepValid, r.DepCount)
			}
		}

		if r.TriggerCount > 0 {
			if r.TriggerValid == r.TriggerCount {
				fmt.Printf("  + Triggers: %d/%d valid\n", r.TriggerValid, r.TriggerCount)
			} else {
				fmt.Printf("  x Triggers: %d/%d valid\n", r.TriggerValid, r.TriggerCount)
			}
		}

		for _, w := range r.Warnings {
			fmt.Printf("  ! Config warning: %s\n", w)
		}
		for _, e := range r.Errors {
			fmt.Printf("  ERROR: %s\n", e)
		}

		if r.pass() {
			if len(r.Warnings) > 0 {
				fmt.Printf("  Result: PASS (%d warning(s))\n", len(r.Warnings))
			} else {
				fmt.Printf("  Result: PASS\n")
			}
		} else {
			fmt.Printf("  Result: FAIL (%d error(s))\n", len(r.Errors))
		}
		fmt.Println()
	}

	totalWarn := 0
	for i := range results {
		totalWarn += len(results[i].Warnings)
	}

	if totalWarn > 0 {
		fmt.Printf("Summary: %d/%d templates valid (%d warning(s))\n", summary.Passed, summary.Total, totalWarn)
	} else {
		fmt.Printf("Summary: %d/%d templates valid\n", summary.Passed, summary.Total)
	}
}

// templateFSReader allows reading from the embedded templateFS for validation.
// It wraps around the existing templateFS embed.FS.
var _ fs.FS = templateFS

// --- Pipeline template expression linting ---

// templateExprRe matches template actions {{ ... }}.
var templateExprRe = regexp.MustCompile(`\{\{(.*?)\}\}`)

// stepRefDotRe matches .steps.STEP_NAME patterns (dot access).
var stepRefDotRe = regexp.MustCompile(`\.steps\.([a-zA-Z_][a-zA-Z0-9_-]*)`)

// stepRefIndexRe matches index .steps "STEP_NAME" patterns.
var stepRefIndexRe = regexp.MustCompile(`index\s+\.steps\s+"([^"]+)"`)

// stepRefFuncRe matches step "STEP_NAME" function calls at the start of an
// action, after a pipe, or after an opening parenthesis.
var stepRefFuncRe = regexp.MustCompile(`(?:^|\||\()\s*step\s+"([^"]+)"`)

// hyphenDotRe matches dot-access chains with hyphens (e.g., .steps.my-step.field),
// including continuation segments after the hyphenated part.
var hyphenDotRe = regexp.MustCompile(`\.[a-zA-Z_][a-zA-Z0-9_]*-[a-zA-Z0-9_-]*(?:\.[a-zA-Z_][a-zA-Z0-9_-]*)*`)

// validatePipelineTemplates checks template expressions in pipeline step configs for
// references to nonexistent or forward-declared steps and common template pitfalls.
func validatePipelineTemplates(pipelineName string, stepsRaw []any, result *templateValidationResult) {
	// Build ordered step name list
	stepNames := make(map[string]int) // step name -> index in pipeline
	for i, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := stepMap["name"].(string)
		if name != "" {
			stepNames[name] = i
		}
	}

	// Check each step's config for template expressions
	for i, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		stepName, _ := stepMap["name"].(string)
		if stepName == "" {
			stepName = fmt.Sprintf("step[%d]", i)
		}

		// Collect all string values from the step config recursively
		templates := collectTemplateStrings(stepMap)

		for _, tmpl := range templates {
			// Find all template actions
			actions := templateExprRe.FindAllStringSubmatch(tmpl, -1)
			for _, action := range actions {
				if len(action) < 2 {
					continue
				}
				actionContent := action[1]

				// Skip comments
				trimmed := strings.TrimSpace(actionContent)
				if strings.HasPrefix(trimmed, "/*") {
					continue
				}

				// Check for step name references via dot-access
				dotMatches := stepRefDotRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range dotMatches {
					refName := m[1]
					validateStepRef(pipelineName, stepName, refName, i, stepNames, result)
				}

				// Check for step name references via index
				indexMatches := stepRefIndexRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range indexMatches {
					refName := m[1]
					validateStepRef(pipelineName, stepName, refName, i, stepNames, result)
				}

				// Check for step name references via step function
				funcMatches := stepRefFuncRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range funcMatches {
					refName := m[1]
					validateStepRef(pipelineName, stepName, refName, i, stepNames, result)
				}

				// Warn on hyphenated dot-access (auto-fixed but suggest preferred syntax)
				if hyphenDotRe.MatchString(actionContent) {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("pipeline %q step %q: template uses hyphenated dot-access which is auto-fixed; prefer step \"name\" \"field\" syntax", pipelineName, stepName))
				}
			}
		}
	}
}

// validateStepRef checks that a referenced step name exists and appears before the
// current step in the pipeline execution order.
func validateStepRef(pipelineName, currentStep, refName string, currentIdx int, stepNames map[string]int, result *templateValidationResult) {
	refIdx, exists := stepNames[refName]
	switch {
	case !exists:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references step %q which does not exist in this pipeline", pipelineName, currentStep, refName))
	case refIdx == currentIdx:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references itself; a step cannot use its own outputs because they are not available until after execution", pipelineName, currentStep))
	case refIdx > currentIdx:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references step %q which has not executed yet (appears later in pipeline)", pipelineName, currentStep, refName))
	}
}

// collectTemplateStrings recursively finds all strings containing {{ in a value tree.
// This intentionally scans all fields (not just "config") because template expressions
// can appear in conditions, names, and other step fields.
func collectTemplateStrings(data any) []string {
	var results []string
	switch v := data.(type) {
	case string:
		if strings.Contains(v, "{{") {
			results = append(results, v)
		}
	case map[string]any:
		for _, val := range v {
			results = append(results, collectTemplateStrings(val)...)
		}
	case []any:
		for _, item := range v {
			results = append(results, collectTemplateStrings(item)...)
		}
	}
	return results
}
