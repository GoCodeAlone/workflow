// Package validation provides shared pipeline configuration validation utilities
// that are used by both the workflow engine (at startup) and the wfctl CLI tool
// (as static analysis). This avoids duplicating logic between the two consumers.
package validation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/GoCodeAlone/workflow/schema"
)

// RefValidationResult holds the outcome of pipeline template reference validation.
// Warnings are suspicious but non-fatal references; Errors are definitively wrong.
type RefValidationResult struct {
	Warnings []string
	Errors   []string
}

// HasIssues returns true when there are any warnings or errors.
func (r *RefValidationResult) HasIssues() bool {
	return len(r.Warnings) > 0 || len(r.Errors) > 0
}

// templateExprRe matches template actions {{ ... }}.
var templateExprRe = regexp.MustCompile(`\{\{(.*?)\}\}`)

// stepRefDotRe matches .steps.STEP_NAME and captures an optional field path.
// Group 1: step name (may contain hyphens).
// Group 2: remaining dot-path (e.g. ".row.auth_token"), field names without hyphens.
var stepRefDotRe = regexp.MustCompile(`\.steps\.([a-zA-Z_][a-zA-Z0-9_-]*)((?:\.[a-zA-Z_][a-zA-Z0-9_]*)*)`)

// stepFieldDotRe matches .steps.STEP_NAME.FIELD_NAME (captures step and first field).
var stepFieldDotRe = regexp.MustCompile(`\.steps\.([a-zA-Z_][a-zA-Z0-9_-]*)\.([a-zA-Z_][a-zA-Z0-9_-]*)`)

// stepRefIndexRe matches index .steps "STEP_NAME" patterns.
var stepRefIndexRe = regexp.MustCompile(`index\s+\.steps\s+"([^"]+)"`)

// stepRefFuncRe matches step "STEP_NAME" function calls at the start of an
// action, after a pipe, or after an opening parenthesis.
var stepRefFuncRe = regexp.MustCompile(`(?:^|\||\()\s*step\s+"([^"]+)"`)

// stepFuncFieldRe matches step "STEP_NAME" "FIELD_NAME" capturing both arguments,
// when used as a function call at the start of an action, after a pipe, or after
// an opening parenthesis.
var stepFuncFieldRe = regexp.MustCompile(`(?:^|\||\()\s*step\s+"([^"]+)"\s+"([^"]+)"`)

// hyphenDotRe matches dot-access chains with hyphens (e.g., .steps.my-step.field),
// including continuation segments after the hyphenated part.
var hyphenDotRe = regexp.MustCompile(`\.[a-zA-Z_][a-zA-Z0-9_]*-[a-zA-Z0-9_-]*(?:\.[a-zA-Z_][a-zA-Z0-9_-]*)*`)

// plainStepPathRe matches bare step context-key references such as
// "steps.STEP_NAME.field.subfield" used in plain-string config values (no {{ }}).
var plainStepPathRe = regexp.MustCompile(`^steps\.([a-zA-Z_][a-zA-Z0-9_-]*)((?:\.[a-zA-Z_][a-zA-Z0-9_]*)*)`)

// pipelineStepMeta holds the type and config of a pipeline step for static analysis.
type pipelineStepMeta struct {
	typ    string
	config map[string]any
}

// stepBuildInfo holds the type and config of a pipeline step, used for output field validation.
type stepBuildInfo struct {
	stepType   string
	stepConfig map[string]any
}

// dbQueryStepTypes is the set of step types that produce a "row" or "rows" output
// from a SQL query and support SQL alias extraction.
var dbQueryStepTypes = map[string]bool{
	"step.db_query":        true,
	"step.db_query_cached": true,
}

// isDBQueryStep reports whether a step type is a DB query step.
func isDBQueryStep(t string) bool { return dbQueryStepTypes[t] }

// ValidatePipelineTemplateRefs validates all pipeline step template expressions in the
// given pipelines map for dangling step references and output field mismatches.
// It performs the same checks as `wfctl template validate` at the pipeline template level.
//
// The pipelines parameter is expected to be a map[string]any where each value is a
// pipeline config map containing a "steps" field (as parsed from YAML).
//
// An optional *schema.StepSchemaRegistry may be provided to supply plugin-registered
// step schemas. When absent, a default built-in registry is created once and reused
// across all pipelines.
func ValidatePipelineTemplateRefs(pipelines map[string]any, reg ...*schema.StepSchemaRegistry) *RefValidationResult {
	var r *schema.StepSchemaRegistry
	if len(reg) > 0 && reg[0] != nil {
		r = reg[0]
	} else {
		r = schema.NewStepSchemaRegistry()
	}
	result := &RefValidationResult{}
	for pipelineName, pipelineRaw := range pipelines {
		pipelineMap, ok := pipelineRaw.(map[string]any)
		if !ok {
			continue
		}
		stepsRaw, _ := pipelineMap["steps"].([]any)
		if len(stepsRaw) == 0 {
			continue
		}
		validatePipelineTemplateRefs(pipelineName, stepsRaw, r, result)
	}
	return result
}

// validatePipelineTemplateRefs checks template expressions in pipeline step configs for
// references to nonexistent or forward-declared steps and common template pitfalls.
// It also warns when a template references a field that is not in the step type's
// declared output schema (Phase 1 static analysis).
func validatePipelineTemplateRefs(pipelineName string, stepsRaw []any, reg *schema.StepSchemaRegistry, result *RefValidationResult) {
	// Build ordered step name list and step metadata for schema validation.
	stepNames := make(map[string]int)             // step name -> index in pipeline
	stepMeta := make(map[string]pipelineStepMeta) // step name -> type+config (used by validateStepOutputField)
	stepInfos := make(map[string]stepBuildInfo)   // step name -> type and config (used by validateStepRef)

	for i, stepRaw := range stepsRaw {
		stepMap, ok := stepRaw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := stepMap["name"].(string)
		if name == "" {
			continue
		}

		stepNames[name] = i
		typ, _ := stepMap["type"].(string)
		cfg, _ := stepMap["config"].(map[string]any)
		if cfg == nil {
			cfg = map[string]any{}
		}

		stepInfos[name] = stepBuildInfo{stepType: typ, stepConfig: cfg}
		stepMeta[name] = pipelineStepMeta{typ: typ, config: cfg}
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

				// Check for step name references via dot-access (captures optional field path)
				dotMatches := stepRefDotRe.FindAllStringSubmatch(actionContent, -1)
				hasHyphen := hyphenDotRe.MatchString(actionContent)
				for _, m := range dotMatches {
					refName := m[1]
					fieldPath := ""
					// When the action contains a hyphenated dot-access, skip field-path
					// validation to avoid spurious output-field or SQL-column warnings
					// (a dedicated hyphen warning is emitted separately below).
					if !hasHyphen && len(m) > 2 {
						fieldPath = m[2]
					}
					validateStepRef(pipelineName, stepName, refName, fieldPath, i, stepNames, stepInfos, reg, result)
				}

				// Check for step output field references via dot-access (.steps.NAME.FIELD)
				// Skip when the action contains hyphenated dot-access, which is not valid
				// Go-template syntax and is already flagged separately by hyphenDotRe.
				if !hyphenDotRe.MatchString(actionContent) {
					fieldDotMatches := stepFieldDotRe.FindAllStringSubmatch(actionContent, -1)
					for _, m := range fieldDotMatches {
						refStepName, refField := m[1], m[2]
						validateStepOutputField(pipelineName, stepName, refStepName, refField, stepMeta, reg, result)
					}
				}

				// Check for step name references via index (no field path resolvable)
				indexMatches := stepRefIndexRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range indexMatches {
					refName := m[1]
					validateStepRef(pipelineName, stepName, refName, "", i, stepNames, stepInfos, reg, result)
				}

				// Check for step name references via step function (no field path resolvable)
				funcMatches := stepRefFuncRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range funcMatches {
					refName := m[1]
					validateStepRef(pipelineName, stepName, refName, "", i, stepNames, stepInfos, reg, result)
				}

				// Check for step output field references via step function (step "NAME" "FIELD")
				funcFieldMatches := stepFuncFieldRe.FindAllStringSubmatch(actionContent, -1)
				for _, m := range funcFieldMatches {
					refStepName, refField := m[1], m[2]
					validateStepOutputField(pipelineName, stepName, refStepName, refField, stepMeta, reg, result)
				}

				// Warn on hyphenated dot-access (auto-fixed but suggest preferred syntax)
				if hyphenDotRe.MatchString(actionContent) {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("pipeline %q step %q: template uses hyphenated dot-access which is auto-fixed; prefer step \"name\" \"field\" syntax", pipelineName, stepName))
				}
			}
		}

		// Validate plain-string step references in specific config fields
		// (e.g. secret_from, backend_url_key, field in conditional/branch).
		if stepCfg, ok := stepMap["config"].(map[string]any); ok {
			validatePlainStepRefs(pipelineName, stepName, i, stepCfg, stepNames, stepInfos, reg, result)
		}
	}
}

// validateStepOutputField checks that a referenced output field exists in the
// step type's declared output schema. It emits a warning when the step type is
// known, has declared outputs, and none of them match the referenced field.
func validateStepOutputField(pipelineName, currentStep, refStepName, refField string, stepMeta map[string]pipelineStepMeta, reg *schema.StepSchemaRegistry, result *RefValidationResult) {
	meta, ok := stepMeta[refStepName]
	if !ok || meta.typ == "" {
		return // step name unknown or no type — already caught by validateStepRef
	}

	outputs := reg.InferStepOutputs(meta.typ, meta.config)
	if len(outputs) == 0 {
		return // no declared outputs for this step type — nothing to check
	}

	// If any output key is a placeholder (wrapped in parentheses), the step
	// has dynamic/config-dependent outputs and we cannot validate statically.
	for _, o := range outputs {
		if len(o.Key) > 1 && o.Key[0] == '(' && o.Key[len(o.Key)-1] == ')' {
			return
		}
	}

	for _, o := range outputs {
		if o.Key == refField {
			return // field found — all good
		}
	}

	// Build a suggestion list from declared outputs
	keys := make([]string, 0, len(outputs))
	for _, o := range outputs {
		keys = append(keys, o.Key)
	}
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("pipeline %q step %q: references %s.%s but step %q (%s) declares outputs: %s",
			pipelineName, currentStep, refStepName, refField, refStepName, meta.typ, strings.Join(keys, ", ")))
}

// validateStepRef checks that a referenced step name exists and appears before the
// current step in the pipeline execution order.  When fieldPath is non-empty it
// also validates the first output field name against the step's known outputs, and
// for db_query steps it performs best-effort SQL alias checking for "row.<col>" paths.
func validateStepRef(pipelineName, currentStep, refName, fieldPath string, currentIdx int, stepNames map[string]int, stepInfos map[string]stepBuildInfo, reg *schema.StepSchemaRegistry, result *RefValidationResult) {
	refIdx, exists := stepNames[refName]
	switch {
	case !exists:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references step %q which does not exist in this pipeline", pipelineName, currentStep, refName))
		return
	case refIdx == currentIdx:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references itself; a step cannot use its own outputs because they are not available until after execution", pipelineName, currentStep))
		return
	case refIdx > currentIdx:
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references step %q which has not executed yet (appears later in pipeline)", pipelineName, currentStep, refName))
		return
	}

	// Step exists and precedes the current step — validate the output field path.
	if fieldPath == "" {
		return
	}

	info, ok := stepInfos[refName]
	if !ok || info.stepType == "" {
		return
	}

	outputs := reg.InferStepOutputs(info.stepType, info.stepConfig)
	if len(outputs) == 0 {
		return // no schema information available; skip
	}

	// If any output key is a placeholder (e.g. "(key)", "(dynamic)", "(nested)"),
	// the step emits dynamic fields whose names cannot be statically determined.
	// Skip field-path validation for such steps to avoid false positives.
	if hasDynamicOutputs(outputs) {
		return
	}

	// Split ".row.auth_token" → ["row", "auth_token"]
	parts := strings.Split(strings.TrimPrefix(fieldPath, "."), ".")
	if len(parts) == 0 || parts[0] == "" {
		return
	}
	firstField := parts[0]

	// Check the first field against known output keys.
	var matchedOutput *schema.InferredOutput
	for i := range outputs {
		if outputs[i].Key == firstField {
			matchedOutput = &outputs[i]
			break
		}
	}
	if matchedOutput == nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("pipeline %q step %q: references step %q output field %q which is not a known output of step type %q (known outputs: %s)",
				pipelineName, currentStep, refName, firstField, info.stepType, joinOutputKeys(outputs)))
		return
	}

	// For db_query/db_query_cached steps, try SQL alias validation on "row.<col>" paths.
	if firstField == "row" && len(parts) > 1 && isDBQueryStep(info.stepType) {
		columnName := parts[1]
		query, _ := info.stepConfig["query"].(string)
		if query != "" {
			sqlCols := ExtractSQLColumns(query)
			if len(sqlCols) > 0 {
				found := false
				for _, col := range sqlCols {
					if col == columnName {
						found = true
						break
					}
				}
				if !found {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("pipeline %q step %q: references step %q output field \"row.%s\" but the SQL query does not select column %q (available: %s)",
							pipelineName, currentStep, refName, columnName, columnName, strings.Join(sqlCols, ", ")))
				}
			}
		}
	}
}

// validatePlainStepRefs checks plain-string config values that contain bare step
// context-key references (e.g. "steps.STEP_NAME.field") in config fields known to
// accept such paths: secret_from, backend_url_key, and field (conditional/branch).
func validatePlainStepRefs(pipelineName, stepName string, stepIdx int, stepCfg map[string]any, stepNames map[string]int, stepInfos map[string]stepBuildInfo, reg *schema.StepSchemaRegistry, result *RefValidationResult) {
	// Config keys that are documented to accept a bare "steps.X.y" context path.
	plainRefKeys := []string{"secret_from", "backend_url_key", "field"}
	for _, key := range plainRefKeys {
		val, ok := stepCfg[key].(string)
		if !ok || val == "" {
			continue
		}
		m := plainStepPathRe.FindStringSubmatch(val)
		if m == nil {
			continue
		}
		refName := m[1]
		fieldPath := m[2] // already in ".field.subfield" form
		validateStepRef(pipelineName, stepName, refName, fieldPath, stepIdx, stepNames, stepInfos, reg, result)
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

// joinOutputKeys returns a comma-joined list of output key names for error messages,
// omitting placeholder/wildcard entries like "(key)", "(dynamic)", "(nested)".
func joinOutputKeys(outputs []schema.InferredOutput) string {
	keys := make([]string, 0, len(outputs))
	for _, o := range outputs {
		if !isPlaceholderOutputKey(o.Key) {
			keys = append(keys, o.Key)
		}
	}
	return strings.Join(keys, ", ")
}

// isPlaceholderOutputKey reports whether an output key is a dynamic/wildcard
// placeholder (e.g. "(key)", "(dynamic)", "(nested)").  Steps that expose
// such placeholders produce outputs whose field names cannot be statically
// determined, so field-path validation should be skipped for them.
func isPlaceholderOutputKey(key string) bool {
	return len(key) >= 2 && key[0] == '(' && key[len(key)-1] == ')'
}

// hasDynamicOutputs reports whether any output in the list is a wildcard
// placeholder, meaning the step emits fields that are not statically known.
func hasDynamicOutputs(outputs []schema.InferredOutput) bool {
	for _, o := range outputs {
		if isPlaceholderOutputKey(o.Key) {
			return true
		}
	}
	return false
}

// ExtractSQLColumns parses a SQL SELECT statement and returns the column names
// (or aliases) from the SELECT clause.
func ExtractSQLColumns(query string) []string {
	// Normalize whitespace
	query = strings.Join(strings.Fields(query), " ")

	// Find SELECT ... FROM
	upper := strings.ToUpper(query)
	selectIdx := strings.Index(upper, "SELECT ")
	fromIdx := strings.Index(upper, " FROM ")
	if selectIdx < 0 || fromIdx < 0 || fromIdx <= selectIdx {
		return nil
	}

	selectClause := query[selectIdx+7 : fromIdx]

	// Handle DISTINCT
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(selectClause)), "DISTINCT ") {
		selectClause = strings.TrimSpace(selectClause)[9:]
	}

	// Split by comma, handling parenthesized subexpressions
	var columns []string
	depth := 0
	current := ""
	for _, ch := range selectClause {
		switch ch {
		case '(':
			depth++
			current += string(ch)
		case ')':
			depth--
			current += string(ch)
		case ',':
			if depth == 0 {
				if col := extractColumnName(strings.TrimSpace(current)); col != "" {
					columns = append(columns, col)
				}
				current = ""
			} else {
				current += string(ch)
			}
		default:
			current += string(ch)
		}
	}
	if col := extractColumnName(strings.TrimSpace(current)); col != "" {
		columns = append(columns, col)
	}
	return columns
}

// extractColumnName extracts the effective column name from a SELECT expression.
// Handles: "col", "table.col", "expr AS alias", "COALESCE(...) AS alias".
func extractColumnName(expr string) string {
	if expr == "" || expr == "*" {
		return ""
	}
	// Check for AS alias (case-insensitive)
	upper := strings.ToUpper(expr)
	if asIdx := strings.LastIndex(upper, " AS "); asIdx >= 0 {
		alias := strings.TrimSpace(expr[asIdx+4:])
		// Remove quotes if present
		alias = strings.Trim(alias, "\"'`")
		return alias
	}
	// Check for table.column
	if dotIdx := strings.LastIndex(expr, "."); dotIdx >= 0 {
		return strings.TrimSpace(expr[dotIdx+1:])
	}
	// Simple column name
	return strings.TrimSpace(expr)
}
