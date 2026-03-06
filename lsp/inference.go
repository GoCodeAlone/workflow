package lsp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/schema"
	"gopkg.in/yaml.v3"
)

// FieldSchema describes a single data field with rich metadata.
type FieldSchema struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Format      string         `json:"format,omitempty"`
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Children    []*FieldSchema `json:"children,omitempty"`
}

// StepOutputSchema holds the inferred outputs for a completed pipeline step.
type StepOutputSchema struct {
	StepName string                  `json:"stepName"`
	StepType string                  `json:"stepType"`
	Fields   []schema.InferredOutput `json:"fields"`
}

// TriggerSchema describes the data surfaced by the workflow trigger.
type TriggerSchema struct {
	Type        string         `json:"type"`
	PathParams  []*FieldSchema `json:"pathParams,omitempty"`
	QueryParams []*FieldSchema `json:"queryParams,omitempty"`
	BodyFields  []*FieldSchema `json:"bodyFields,omitempty"`
	Headers     []*FieldSchema `json:"headers,omitempty"`
}

// PipelineDataContext is the full set of context keys available at a given
// cursor position in a pipeline, combining trigger data with step outputs
// accumulated from all steps preceding the cursor.
type PipelineDataContext struct {
	PipelineName string                       `json:"pipelineName"`
	StepOrder    []string                     `json:"stepOrder"`
	Steps        map[string]*StepOutputSchema `json:"steps"`
	Trigger      *TriggerSchema               `json:"trigger,omitempty"`
}

// BuildPipelineContext builds a PipelineDataContext for the pipeline that
// contains cursorLine (0-based). It uses yaml.Node.Line (1-based) internally
// to locate the pipeline and identify which steps precede the cursor.
//
// OpenAPI trigger schema is auto-discovered from a module with type containing
// "openapi" and a config.spec_file key. Method and path are read from the
// pipeline's own trigger: section. Falls back to a generic {type: "http"}
// trigger when no OpenAPI spec is found or matched.
func BuildPipelineContext(reg *Registry, doc *Document, cursorLine int) *PipelineDataContext {
	ctx := &PipelineDataContext{
		StepOrder: []string{},
		Steps:     map[string]*StepOutputSchema{},
	}

	if doc == nil {
		return ctx
	}

	// Re-parse the YAML to get node line information.
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(doc.Content), &root); err != nil || len(root.Content) == 0 {
		return ctx
	}
	docNode := root.Content[0]

	cursor1based := cursorLine + 1

	// Find the pipeline containing the cursor.
	pipelineName, pipelineValNode, steps := findPipelineAtCursor(docNode, cursor1based)
	ctx.PipelineName = pipelineName

	if steps != nil {
		schemaReg := schema.GetStepSchemaRegistry()
		collectPrecedingSteps(ctx, steps, cursor1based, schemaReg)
	}

	// Auto-discover OpenAPI spec from modules section.
	specFile := discoverOpenAPISpec(docNode, doc.URI)
	if specFile != "" && pipelineValNode != nil {
		method, path := getPipelineTriggerInfo(pipelineValNode)
		if method != "" && path != "" {
			ctx.Trigger = parseOpenAPITriggerSchema(specFile, strings.ToLower(method), path)
		}
	}
	// Fall back to a generic HTTP trigger if none was discovered.
	if ctx.Trigger == nil {
		ctx.Trigger = &TriggerSchema{Type: "http"}
	}

	return ctx
}

// findPipelineAtCursor returns the pipeline name, its value node, and its steps
// for the pipeline whose key line is nearest to (and ≤) cursor1based.
func findPipelineAtCursor(docNode *yaml.Node, cursor1based int) (string, *yaml.Node, []*yaml.Node) {
	pipMap := findMapValue(docNode, "pipelines")
	if pipMap == nil || pipMap.Kind != yaml.MappingNode {
		return "", nil, nil
	}

	for i := 0; i+1 < len(pipMap.Content); i += 2 {
		keyNode := pipMap.Content[i]
		valNode := pipMap.Content[i+1]

		// Determine where the next pipeline starts.
		nextStart := 1<<31 - 1
		if i+2 < len(pipMap.Content) {
			nextStart = pipMap.Content[i+2].Line
		}

		if cursor1based >= keyNode.Line && cursor1based < nextStart {
			stepsNode := findMapValue(valNode, "steps")
			if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
				return keyNode.Value, valNode, nil
			}
			return keyNode.Value, valNode, stepsNode.Content
		}
	}
	return "", nil, nil
}

// collectPrecedingSteps adds steps that precede the cursor line to ctx.
// The "current step" is the last step whose yaml.Node.Line ≤ cursor1based;
// all steps before it are included.
func collectPrecedingSteps(ctx *PipelineDataContext, steps []*yaml.Node, cursor1based int, reg *schema.StepSchemaRegistry) {
	type stepRec struct {
		name, stepType string
		cfg            map[string]any
		line           int
	}

	all := make([]stepRec, 0, len(steps))
	for _, step := range steps {
		name, stepType, cfg, line := parseStepWithLine(step)
		if name == "" && stepType == "" {
			continue
		}
		all = append(all, stepRec{name, stepType, cfg, line})
	}

	// Find the index of the "current step": last step with line <= cursor.
	currentIdx := -1
	for i, s := range all {
		if s.line <= cursor1based {
			currentIdx = i
		}
	}

	// Include all steps before the current step.
	for i := 0; i < currentIdx; i++ {
		s := all[i]
		inferred := reg.InferStepOutputs(s.stepType, s.cfg)
		ctx.StepOrder = append(ctx.StepOrder, s.name)
		ctx.Steps[s.name] = &StepOutputSchema{
			StepName: s.name,
			StepType: s.stepType,
			Fields:   inferred,
		}
	}
}

// parseStepWithLine extracts name, type, config, and yaml line from a step node.
func parseStepWithLine(node *yaml.Node) (name, stepType string, cfg map[string]any, line int) {
	if node.Kind != yaml.MappingNode {
		return
	}
	line = node.Line
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

// getPipelineTriggerInfo extracts method and path from the pipeline's trigger section.
func getPipelineTriggerInfo(pipelineValNode *yaml.Node) (method, path string) {
	triggerNode := findMapValue(pipelineValNode, "trigger")
	if triggerNode == nil {
		return "", ""
	}
	method = scalarValue(triggerNode, "method")
	path = scalarValue(triggerNode, "path")
	return
}

// discoverOpenAPISpec walks the modules: section to find an openapi module
// and returns its resolved spec_file path, or "" if none found.
func discoverOpenAPISpec(docNode *yaml.Node, docURI string) string {
	modulesNode := findMapValue(docNode, "modules")
	if modulesNode == nil || modulesNode.Kind != yaml.SequenceNode {
		return ""
	}

	for _, modNode := range modulesNode.Content {
		if modNode.Kind != yaml.MappingNode {
			continue
		}
		modType := scalarValue(modNode, "type")
		if !strings.Contains(strings.ToLower(modType), "openapi") {
			continue
		}
		cfgNode := findMapValue(modNode, "config")
		if cfgNode == nil {
			continue
		}
		specFile := scalarValue(cfgNode, "spec_file")
		if specFile == "" {
			continue
		}
		// Resolve relative paths against the document's directory.
		if !filepath.IsAbs(specFile) && docURI != "" {
			docPath := strings.TrimPrefix(docURI, "file://")
			dir := filepath.Dir(docPath)
			specFile = filepath.Join(dir, specFile)
		}
		return specFile
	}
	return ""
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

// parseOpenAPITriggerSchema reads an OpenAPI spec file and builds a TriggerSchema
// for the given HTTP method and path.
func parseOpenAPITriggerSchema(specPath, method, path string) *TriggerSchema {
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
		t := p.Schema.Type
		if t == "" {
			t = "string"
		}
		fs := &FieldSchema{Name: p.Name, Type: t}
		switch p.In {
		case "path":
			trigger.PathParams = append(trigger.PathParams, fs)
		case "query":
			trigger.QueryParams = append(trigger.QueryParams, fs)
		case "header":
			trigger.Headers = append(trigger.Headers, fs)
		}
	}

	// Sort slices for deterministic output.
	sort.Slice(trigger.PathParams, func(i, j int) bool { return trigger.PathParams[i].Name < trigger.PathParams[j].Name })
	sort.Slice(trigger.QueryParams, func(i, j int) bool { return trigger.QueryParams[i].Name < trigger.QueryParams[j].Name })
	sort.Slice(trigger.Headers, func(i, j int) bool { return trigger.Headers[i].Name < trigger.Headers[j].Name })

	if op.RequestBody != nil {
		for _, mt := range op.RequestBody.Content {
			if len(mt.Schema.Properties) == 0 {
				continue
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
				trigger.BodyFields = append(trigger.BodyFields, &FieldSchema{
					Name:        k,
					Type:        t,
					Description: prop.Description,
				})
			}
			break // use first media type
		}
	}

	return trigger
}
