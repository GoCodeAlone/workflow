package schema

import (
	"sort"
	"strings"
)

// InferredOutput describes a single inferred output key for a step instance.
type InferredOutput struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// InferStepOutputs computes output fields for a step given its type and config.
// For config-dependent steps (step.set, step.db_query), it produces precise outputs
// based on the actual config values. Falls back to static schema outputs for all other types.
func (r *StepSchemaRegistry) InferStepOutputs(stepType string, stepConfig map[string]any) []InferredOutput {
	switch stepType {
	case "step.set":
		return inferSetOutputs(stepConfig)
	case "step.db_query":
		return inferDBQueryOutputs(stepConfig)
	case "step.db_exec":
		return inferDBExecOutputs(stepConfig)
	case "step.db_query_cached":
		return inferDBQueryCachedOutputs(stepConfig)
	case "step.request_parse":
		return inferRequestParseOutputs(stepConfig)
	case "step.validate":
		return r.inferValidateOutputs(stepConfig)
	case "step.nosql_get":
		return inferNoSQLGetOutputs(stepConfig)
	case "step.nosql_query":
		return inferNoSQLQueryOutputs(stepConfig)
	case "step.nosql_put":
		return inferNoSQLPutOutputs()
	case "step.parallel":
		return inferParallelOutputs(stepConfig)
	}

	return r.staticOutputs(stepType)
}

func inferSetOutputs(cfg map[string]any) []InferredOutput {
	values, _ := cfg["values"].(map[string]any)
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]InferredOutput, 0, len(keys))
	for _, k := range keys {
		out = append(out, InferredOutput{Key: k, Type: "any"})
	}
	return out
}

func inferDBQueryOutputs(cfg map[string]any) []InferredOutput {
	mode, _ := cfg["mode"].(string)
	if mode == "list" {
		return []InferredOutput{
			{Key: "count", Type: "number", Description: "Number of rows returned"},
			{Key: "rows", Type: "array", Description: "All result rows"},
		}
	}
	// default: single
	return []InferredOutput{
		{Key: "found", Type: "boolean", Description: "Whether a row was found"},
		{Key: "row", Type: "map", Description: "First result row as key-value map"},
	}
}

func inferDBExecOutputs(cfg map[string]any) []InferredOutput {
	returning, _ := cfg["returning"].(bool)
	if returning {
		return inferDBQueryOutputs(cfg)
	}
	return []InferredOutput{
		{Key: "affected_rows", Type: "number", Description: "Number of rows affected by the statement"},
		{Key: "ignored_error", Type: "string", Description: "Error text when ignore_error is enabled and execution fails"},
		{Key: "last_id", Type: "string", Description: "Last inserted row ID as a string"},
	}
}

func inferDBQueryCachedOutputs(cfg map[string]any) []InferredOutput {
	mode, _ := cfg["mode"].(string)
	out := []InferredOutput{{Key: "cache_hit", Type: "boolean", Description: "Whether the result came from cache"}}
	if mode == "list" {
		out = append(out,
			InferredOutput{Key: "count", Type: "number", Description: "Number of rows returned"},
			InferredOutput{Key: "rows", Type: "array", Description: "All result rows"},
		)
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		return out
	}

	query, _ := cfg["query"].(string)
	cols := extractSQLColumnsForOutputs(query)
	if len(cols) == 0 {
		out = append(out, InferredOutput{Key: "(query-column)", Type: "any", Description: "Selected column emitted as a top-level field"})
		sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
		return out
	}
	for _, col := range cols {
		out = append(out, InferredOutput{Key: col, Type: "any", Description: "Selected column emitted as a top-level field"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func extractSQLColumnsForOutputs(query string) []string {
	query = strings.Join(strings.Fields(query), " ")
	upper := strings.ToUpper(query)
	selectIdx := strings.Index(upper, "SELECT ")
	if selectIdx < 0 {
		return nil
	}

	selectStart := selectIdx + len("SELECT ")
	fromIdx := topLevelSQLKeywordIndex(query, selectStart, " FROM ")
	selectClause := query[selectStart:]
	if fromIdx > selectStart {
		selectClause = query[selectStart:fromIdx]
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(selectClause)), "DISTINCT ") {
		selectClause = strings.TrimSpace(selectClause)[9:]
	}

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
				if col := extractSQLColumnNameForOutputs(strings.TrimSpace(current)); col != "" {
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
	if col := extractSQLColumnNameForOutputs(strings.TrimSpace(current)); col != "" {
		columns = append(columns, col)
	}
	return columns
}

func topLevelSQLKeywordIndex(query string, start int, keyword string) int {
	upper := strings.ToUpper(query)
	depth := 0
	var quote byte
	for i := start; i <= len(query)-len(keyword); i++ {
		ch := query[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.HasPrefix(upper[i:], keyword) {
				return i
			}
		}
	}
	return -1
}

func extractSQLColumnNameForOutputs(expr string) string {
	if expr == "" || expr == "*" {
		return ""
	}
	upper := strings.ToUpper(expr)
	if asIdx := strings.LastIndex(upper, " AS "); asIdx >= 0 {
		return strings.Trim(strings.TrimSpace(expr[asIdx+4:]), "\"'`")
	}
	if dotIdx := strings.LastIndex(expr, "."); dotIdx >= 0 {
		return strings.Trim(strings.TrimSpace(expr[dotIdx+1:]), "\"'`")
	}
	return strings.Trim(strings.TrimSpace(expr), "\"'`")
}

func inferRequestParseOutputs(_ map[string]any) []InferredOutput {
	return []InferredOutput{
		{Key: "body", Type: "any", Description: "Parsed request body"},
		{Key: "headers", Type: "map", Description: "Parsed request headers"},
		{Key: "path_params", Type: "map", Description: "URL path parameters"},
		{Key: "query", Type: "map", Description: "Parsed query parameters"},
	}
}

func (r *StepSchemaRegistry) inferValidateOutputs(cfg map[string]any) []InferredOutput {
	out := []InferredOutput{
		{Key: "valid", Type: "boolean", Description: "Whether validation passed"},
	}

	// If strategy is json_schema and schema.properties is defined, list the validated fields.
	strategy, _ := cfg["strategy"].(string)
	if strategy != "json_schema" {
		return out
	}
	schemaMap, _ := cfg["schema"].(map[string]any)
	if schemaMap == nil {
		return out
	}
	props, _ := schemaMap["properties"].(map[string]any)
	if len(props) == 0 {
		return out
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		propType := "any"
		if pm, ok := props[k].(map[string]any); ok {
			if t, ok := pm["type"].(string); ok {
				propType = t
			}
		}
		out = append(out, InferredOutput{Key: k, Type: propType, Description: "Validated field"})
	}
	return out
}

func inferNoSQLGetOutputs(_ map[string]any) []InferredOutput {
	return []InferredOutput{
		{Key: "found", Type: "boolean", Description: "Whether the key was found"},
		{Key: "item", Type: "any", Description: "The retrieved document"},
	}
}

func inferNoSQLQueryOutputs(_ map[string]any) []InferredOutput {
	return []InferredOutput{
		{Key: "count", Type: "number", Description: "Number of matching documents"},
		{Key: "items", Type: "array", Description: "Matching documents"},
	}
}

func inferNoSQLPutOutputs() []InferredOutput {
	return []InferredOutput{
		{Key: "key", Type: "string", Description: "The document key that was stored"},
		{Key: "stored", Type: "boolean", Description: "Whether the document was stored successfully"},
	}
}

func inferParallelOutputs(stepConfig map[string]any) []InferredOutput {
	outputs := []InferredOutput{
		{Key: "results", Type: "map", Description: "Map of branch_name → branch output"},
		{Key: "errors", Type: "map", Description: "Map of branch_name → error string"},
		{Key: "completed", Type: "integer", Description: "Count of successful branches"},
		{Key: "failed", Type: "integer", Description: "Count of failed branches"},
	}
	// If steps are provided in config, list branch names
	if stepsRaw, ok := stepConfig["steps"].([]any); ok {
		for _, raw := range stepsRaw {
			if stepCfg, ok := raw.(map[string]any); ok {
				if name, ok := stepCfg["name"].(string); ok {
					outputs = append(outputs, InferredOutput{
						Key:         "results." + name,
						Type:        "(dynamic)",
						Description: "Output from branch " + name,
					})
				}
			}
		}
	}
	return outputs
}

// staticOutputs converts a step's registered schema outputs to InferredOutputs,
// skipping dynamic placeholder entries.
func (r *StepSchemaRegistry) staticOutputs(stepType string) []InferredOutput {
	s := r.Get(stepType)
	if s == nil {
		return nil
	}
	var out []InferredOutput
	for _, o := range s.Outputs {
		if o.Key == "(dynamic)" || o.Key == "(nested)" {
			continue
		}
		out = append(out, InferredOutput(o))
	}
	return out
}
