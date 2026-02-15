package module

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateEngine resolves {{ .field }} expressions against a PipelineContext.
type TemplateEngine struct{}

// NewTemplateEngine creates a new TemplateEngine.
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{}
}

// templateData builds the data map that Go templates see.
func (te *TemplateEngine) templateData(pc *PipelineContext) map[string]any {
	data := make(map[string]any)

	// Current values are top-level
	for k, v := range pc.Current {
		data[k] = v
	}

	// Step outputs accessible under "steps"
	data["steps"] = pc.StepOutputs

	// Trigger data accessible under "trigger"
	data["trigger"] = pc.TriggerData

	// Metadata accessible under "meta"
	data["meta"] = pc.Metadata

	return data
}

// Resolve evaluates a template string against a PipelineContext.
// If the string does not contain {{ }}, it is returned as-is.
func (te *TemplateEngine) Resolve(tmplStr string, pc *PipelineContext) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}

	t, err := template.New("").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("template parse error: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, te.templateData(pc)); err != nil {
		return "", fmt.Errorf("template exec error: %w", err)
	}
	return buf.String(), nil
}

// ResolveMap evaluates all string values in a map that contain {{ }} expressions.
// Non-string values and nested maps/slices are processed recursively.
func (te *TemplateEngine) ResolveMap(data map[string]any, pc *PipelineContext) (map[string]any, error) {
	result := make(map[string]any, len(data))
	for k, v := range data {
		resolved, err := te.resolveValue(v, pc)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		result[k] = resolved
	}
	return result, nil
}

func (te *TemplateEngine) resolveValue(v any, pc *PipelineContext) (any, error) {
	switch val := v.(type) {
	case string:
		return te.Resolve(val, pc)
	case map[string]any:
		return te.ResolveMap(val, pc)
	case []any:
		resolved := make([]any, len(val))
		for i, item := range val {
			r, err := te.resolveValue(item, pc)
			if err != nil {
				return nil, err
			}
			resolved[i] = r
		}
		return resolved, nil
	default:
		return v, nil
	}
}
