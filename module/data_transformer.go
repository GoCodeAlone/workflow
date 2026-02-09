package module

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// TransformOperation defines a single transformation step
type TransformOperation struct {
	Type   string                 `json:"type" yaml:"type"`     // "extract", "map", "convert", "filter"
	Config map[string]interface{} `json:"config" yaml:"config"`
}

// TransformPipeline is a named sequence of operations
type TransformPipeline struct {
	Name       string               `json:"name" yaml:"name"`
	Operations []TransformOperation `json:"operations" yaml:"operations"`
}

// DataTransformer provides named data transformation pipelines
type DataTransformer struct {
	name      string
	pipelines map[string]*TransformPipeline
	mu        sync.RWMutex
}

// NewDataTransformer creates a new DataTransformer module
func NewDataTransformer(name string) *DataTransformer {
	return &DataTransformer{
		name:      name,
		pipelines: make(map[string]*TransformPipeline),
	}
}

// Name returns the module name
func (dt *DataTransformer) Name() string {
	return dt.name
}

// Init registers the data transformer as a service
func (dt *DataTransformer) Init(app modular.Application) error {
	return app.RegisterService("data.transformer", dt)
}

// RegisterPipeline registers a named transformation pipeline
func (dt *DataTransformer) RegisterPipeline(pipeline *TransformPipeline) {
	dt.mu.Lock()
	defer dt.mu.Unlock()
	dt.pipelines[pipeline.Name] = pipeline
}

// Transform runs a named pipeline on the given data
func (dt *DataTransformer) Transform(ctx context.Context, pipelineName string, data interface{}) (interface{}, error) {
	dt.mu.RLock()
	pipeline, exists := dt.pipelines[pipelineName]
	dt.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("pipeline '%s' not found", pipelineName)
	}

	return dt.TransformWithOps(ctx, pipeline.Operations, data)
}

// TransformWithOps runs a sequence of operations on the given data
func (dt *DataTransformer) TransformWithOps(ctx context.Context, ops []TransformOperation, data interface{}) (interface{}, error) {
	current := data
	for i, op := range ops {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var err error
		current, err = dt.applyOperation(op, current)
		if err != nil {
			return nil, fmt.Errorf("operation %d (%s) failed: %w", i, op.Type, err)
		}
	}
	return current, nil
}

// applyOperation applies a single transformation operation
func (dt *DataTransformer) applyOperation(op TransformOperation, data interface{}) (interface{}, error) {
	switch op.Type {
	case "extract":
		return dt.opExtract(op.Config, data)
	case "map":
		return dt.opMap(op.Config, data)
	case "filter":
		return dt.opFilter(op.Config, data)
	case "convert":
		return dt.opConvert(op.Config, data)
	default:
		return nil, fmt.Errorf("unknown operation type: %s", op.Type)
	}
}

// opExtract extracts a value using dot-notation path
func (dt *DataTransformer) opExtract(config map[string]interface{}, data interface{}) (interface{}, error) {
	path, _ := config["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("extract requires 'path' config")
	}

	return extractByPath(data, path)
}

// extractByPath navigates a nested structure using dot notation
func extractByPath(data interface{}, path string) (interface{}, error) {
	parts := strings.Split(path, ".")
	current := data

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, exists := v[part]
			if !exists {
				return nil, fmt.Errorf("key '%s' not found in path '%s'", part, path)
			}
			current = val
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("expected numeric index for array, got '%s'", part)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("index %d out of bounds (len=%d)", idx, len(v))
			}
			current = v[idx]
		default:
			return nil, fmt.Errorf("cannot navigate into %T at '%s'", current, part)
		}
	}

	return current, nil
}

// opMap renames fields in a map
func (dt *DataTransformer) opMap(config map[string]interface{}, data interface{}) (interface{}, error) {
	mappingsRaw, _ := config["mappings"].(map[string]interface{})
	if len(mappingsRaw) == 0 {
		return nil, fmt.Errorf("map requires 'mappings' config")
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("map operation requires map[string]interface{} input, got %T", data)
	}

	result := make(map[string]interface{})
	// Copy all existing fields
	for k, v := range dataMap {
		result[k] = v
	}

	// Apply mappings: rename oldName -> newName
	for oldName, newNameRaw := range mappingsRaw {
		newName, ok := newNameRaw.(string)
		if !ok {
			continue
		}
		if val, exists := result[oldName]; exists {
			result[newName] = val
			delete(result, oldName)
		}
	}

	return result, nil
}

// opFilter keeps only specified fields
func (dt *DataTransformer) opFilter(config map[string]interface{}, data interface{}) (interface{}, error) {
	fieldsRaw, _ := config["fields"].([]interface{})
	if len(fieldsRaw) == 0 {
		return nil, fmt.Errorf("filter requires 'fields' config")
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("filter operation requires map[string]interface{} input, got %T", data)
	}

	fields := make(map[string]bool)
	for _, f := range fieldsRaw {
		if s, ok := f.(string); ok {
			fields[s] = true
		}
	}

	result := make(map[string]interface{})
	for k, v := range dataMap {
		if fields[k] {
			result[k] = v
		}
	}

	return result, nil
}

// opConvert converts between formats (json marshaling/unmarshaling)
func (dt *DataTransformer) opConvert(config map[string]interface{}, data interface{}) (interface{}, error) {
	from, _ := config["from"].(string)
	to, _ := config["to"].(string)

	if from == "" || to == "" {
		return nil, fmt.Errorf("convert requires 'from' and 'to' config")
	}

	switch {
	case from == "json" && to == "json":
		// Re-serialize through JSON (normalizes types, e.g., int -> float64)
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("json marshal failed: %w", err)
		}
		var result interface{}
		if err := json.Unmarshal(jsonBytes, &result); err != nil {
			return nil, fmt.Errorf("json unmarshal failed: %w", err)
		}
		return result, nil
	case from == "json" && to == "string":
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("json marshal failed: %w", err)
		}
		return string(jsonBytes), nil
	case from == "string" && to == "json":
		str, ok := data.(string)
		if !ok {
			return nil, fmt.Errorf("expected string input for string->json conversion")
		}
		var result interface{}
		if err := json.Unmarshal([]byte(str), &result); err != nil {
			return nil, fmt.Errorf("json unmarshal failed: %w", err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported conversion: %s -> %s", from, to)
	}
}
