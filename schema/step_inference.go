package schema

import "sort"

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

func inferDBQueryCachedOutputs(cfg map[string]any) []InferredOutput {
	base := inferDBQueryOutputs(cfg)
	out := make([]InferredOutput, 0, len(base)+1)
	out = append(out, InferredOutput{Key: "cache_hit", Type: "boolean", Description: "Whether the result came from cache"})
	out = append(out, base...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
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
		out = append(out, InferredOutput{
			Key:         o.Key,
			Type:        o.Type,
			Description: o.Description,
		})
	}
	return out
}
