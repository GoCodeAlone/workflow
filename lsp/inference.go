package lsp

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/schema"
	"gopkg.in/yaml.v3"
)

// FieldSchema describes a single data field in the pipeline context.
type FieldSchema struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// StepOutputSchema holds the inferred outputs for a completed pipeline step.
type StepOutputSchema struct {
	StepName string                 `json:"stepName"`
	StepType string                 `json:"stepType"`
	Outputs  map[string]FieldSchema `json:"outputs"`
}

// TriggerSchema describes the data surfaced by the workflow trigger.
type TriggerSchema struct {
	Type        string                 `json:"type"`
	PathParams  map[string]FieldSchema `json:"pathParams,omitempty"`
	QueryParams map[string]FieldSchema `json:"queryParams,omitempty"`
	BodyFields  map[string]FieldSchema `json:"bodyFields,omitempty"`
}

// PipelineDataContext is the full set of context keys available at a given
// point in a pipeline, combining trigger data with step outputs accumulated
// from all preceding steps.
type PipelineDataContext struct {
	PipelineName string             `json:"pipelineName"`
	Trigger      *TriggerSchema     `json:"trigger,omitempty"`
	StepOutputs  []StepOutputSchema `json:"stepOutputs"`
}

// BuildPipelineContext builds a PipelineDataContext for the named pipeline.
// It walks the YAML steps in order, inferring outputs for each step via
// InferStepOutputs. Steps are included up to (but not including) upToStepName;
// pass "" to include all steps.
//
// If openAPISpecPath is non-empty the file is read and parsed to populate the
// trigger schema with HTTP path/query params and request body fields.
// httpMethod and httpPath identify which OpenAPI operation to use (e.g.
// "POST", "/pets").  Either can be empty to skip OpenAPI parsing.
func BuildPipelineContext(content string, pipelineName string, upToStepName string,
	openAPISpecPath string, httpMethod string, httpPath string) *PipelineDataContext {

	ctx := &PipelineDataContext{PipelineName: pipelineName}

	// Parse YAML.
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil || len(root.Content) == 0 {
		return ctx
	}
	doc := root.Content[0]

	// Find the pipeline's steps.
	steps := findPipelineSteps(doc, pipelineName)
	if steps == nil {
		return ctx
	}

	reg := schema.GetStepSchemaRegistry()
	for _, step := range steps {
		name, stepType, cfg := parseStep(step)
		if upToStepName != "" && name == upToStepName {
			break
		}
		inferred := reg.InferStepOutputs(stepType, cfg)
		outputs := make(map[string]FieldSchema, len(inferred))
		for _, o := range inferred {
			outputs[o.Key] = FieldSchema{Type: o.Type, Description: o.Description}
		}
		ctx.StepOutputs = append(ctx.StepOutputs, StepOutputSchema{
			StepName: name,
			StepType: stepType,
			Outputs:  outputs,
		})
	}

	// Parse OpenAPI spec if provided.
	if openAPISpecPath != "" && httpMethod != "" && httpPath != "" {
		ctx.Trigger = parseOpenAPITrigger(openAPISpecPath, strings.ToLower(httpMethod), httpPath)
	}

	return ctx
}

// findPipelineSteps returns the sequence of step YAML nodes for pipelineName.
func findPipelineSteps(doc *yaml.Node, pipelineName string) []*yaml.Node {
	// Walk doc looking for "pipelines" key.
	pipMap := findMapValue(doc, "pipelines")
	if pipMap == nil {
		return nil
	}
	pipBody := findMapValue(pipMap, pipelineName)
	if pipBody == nil {
		return nil
	}
	stepsNode := findMapValue(pipBody, "steps")
	if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
		return nil
	}
	return stepsNode.Content
}

// parseStep extracts name, type, and config map from a step YAML node.
func parseStep(node *yaml.Node) (name, stepType string, cfg map[string]any) {
	if node.Kind != yaml.MappingNode {
		return
	}
	name = scalarValue(node, "name")
	stepType = scalarValue(node, "type")
	cfgNode := findMapValue(node, "config")
	if cfgNode != nil {
		if err := cfgNode.Decode(&cfg); err != nil {
			cfg = nil
		}
	}
	return
}

// findMapValue searches a MappingNode for key and returns its value node.
func findMapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	// DocumentNode — unwrap.
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return findMapValue(node.Content[0], key)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// scalarValue returns the string value of a named key in a MappingNode.
func scalarValue(node *yaml.Node, key string) string {
	v := findMapValue(node, key)
	if v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}
	return ""
}

// ── OpenAPI spec parsing ────────────────────────────────────────────────────

// openAPISpec is a minimal representation of an OpenAPI 3.0 document.
type openAPISpec struct {
	Paths map[string]map[string]*openAPIOperation `yaml:"paths" json:"paths"`
}

type openAPIOperation struct {
	Parameters  []openAPIParameter  `yaml:"parameters" json:"parameters"`
	RequestBody *openAPIRequestBody `yaml:"requestBody" json:"requestBody"`
}

type openAPIParameter struct {
	Name   string          `yaml:"name" json:"name"`
	In     string          `yaml:"in" json:"in"` // "path", "query", "header"
	Schema openAPIParamSch `yaml:"schema" json:"schema"`
}

type openAPIParamSch struct {
	Type string `yaml:"type" json:"type"`
}

type openAPIRequestBody struct {
	Content map[string]openAPIMediaType `yaml:"content" json:"content"`
}

type openAPIMediaType struct {
	Schema openAPIBodySchema `yaml:"schema" json:"schema"`
}

type openAPIBodySchema struct {
	Properties map[string]openAPIProperty `yaml:"properties" json:"properties"`
}

type openAPIProperty struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description"`
}

// parseOpenAPITrigger reads an OpenAPI spec file and builds a TriggerSchema
// for the given HTTP method and path.
func parseOpenAPITrigger(specPath, method, path string) *TriggerSchema {
	data, err := os.ReadFile(specPath) //nolint:gosec // G304: caller-supplied trusted path
	if err != nil {
		return nil
	}

	var spec openAPISpec
	// Try YAML first; fall back to JSON.
	if err := yaml.Unmarshal(data, &spec); err != nil || spec.Paths == nil {
		if err2 := json.Unmarshal(data, &spec); err2 != nil || spec.Paths == nil {
			return nil
		}
	}

	pathItem, ok := spec.Paths[path]
	if !ok {
		return nil
	}
	op, ok := pathItem[method]
	if !ok || op == nil {
		return nil
	}

	trigger := &TriggerSchema{Type: "http"}

	for _, p := range op.Parameters {
		fs := FieldSchema{Type: p.Schema.Type}
		if fs.Type == "" {
			fs.Type = "string"
		}
		switch p.In {
		case "path":
			if trigger.PathParams == nil {
				trigger.PathParams = make(map[string]FieldSchema)
			}
			trigger.PathParams[p.Name] = fs
		case "query":
			if trigger.QueryParams == nil {
				trigger.QueryParams = make(map[string]FieldSchema)
			}
			trigger.QueryParams[p.Name] = fs
		}
	}

	if op.RequestBody != nil {
		for _, mt := range op.RequestBody.Content {
			if len(mt.Schema.Properties) == 0 {
				continue
			}
			if trigger.BodyFields == nil {
				trigger.BodyFields = make(map[string]FieldSchema)
			}
			keys := make([]string, 0, len(mt.Schema.Properties))
			for k := range mt.Schema.Properties {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				prop := mt.Schema.Properties[k]
				t := prop.Type
				if t == "" {
					t = "any"
				}
				trigger.BodyFields[k] = FieldSchema{Type: t, Description: prop.Description}
			}
			break // use first media type
		}
	}

	return trigger
}
