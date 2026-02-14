package module

import (
	"context"
	"testing"
)

func TestNewDataTransformer(t *testing.T) {
	dt := NewDataTransformer("my-transformer")
	if dt.Name() != "my-transformer" {
		t.Errorf("expected name 'my-transformer', got %q", dt.Name())
	}
}

func TestDataTransformer_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	dt := NewDataTransformer("transformer")
	if err := dt.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
}

func TestDataTransformer_RegisterAndTransform(t *testing.T) {
	dt := NewDataTransformer("transformer")
	pipeline := &TransformPipeline{
		Name: "test-pipeline",
		Operations: []TransformOperation{
			{
				Type:   "extract",
				Config: map[string]any{"path": "user.name"},
			},
		},
	}
	dt.RegisterPipeline(pipeline)

	data := map[string]any{
		"user": map[string]any{
			"name": "Alice",
			"age":  30,
		},
	}

	result, err := dt.Transform(context.Background(), "test-pipeline", data)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected 'Alice', got %v", result)
	}
}

func TestDataTransformer_TransformPipelineNotFound(t *testing.T) {
	dt := NewDataTransformer("transformer")
	_, err := dt.Transform(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent pipeline")
	}
}

// Extract operation tests

func TestDataTransformer_ExtractSimple(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "name"}},
	}

	data := map[string]any{"name": "Bob", "age": 25}
	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}
	if result != "Bob" {
		t.Errorf("expected 'Bob', got %v", result)
	}
}

func TestDataTransformer_ExtractNested(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "user.address.city"}},
	}

	data := map[string]any{
		"user": map[string]any{
			"address": map[string]any{
				"city":  "Portland",
				"state": "OR",
			},
		},
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}
	if result != "Portland" {
		t.Errorf("expected 'Portland', got %v", result)
	}
}

func TestDataTransformer_ExtractArrayIndex(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "items.1.name"}},
	}

	data := map[string]any{
		"items": []any{
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
			map[string]any{"name": "third"},
		},
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}
	if result != "second" {
		t.Errorf("expected 'second', got %v", result)
	}
}

func TestDataTransformer_ExtractMissingKey(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "nonexistent"}},
	}

	data := map[string]any{"name": "Alice"}
	_, err := dt.TransformWithOps(context.Background(), ops, data)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestDataTransformer_ExtractOutOfBounds(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "items.5"}},
	}

	data := map[string]any{
		"items": []any{"a", "b"},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, data)
	if err == nil {
		t.Fatal("expected error for out-of-bounds index")
	}
}

func TestDataTransformer_ExtractNoPath(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path config")
	}
}

func TestDataTransformer_ExtractNonNavigable(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "name.sub"}},
	}

	data := map[string]any{"name": "Alice"}
	_, err := dt.TransformWithOps(context.Background(), ops, data)
	if err == nil {
		t.Fatal("expected error when navigating into non-map/slice type")
	}
}

// Map operation tests

func TestDataTransformer_MapRenameFields(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "map",
			Config: map[string]any{
				"mappings": map[string]any{
					"first_name": "firstName",
					"last_name":  "lastName",
				},
			},
		},
	}

	data := map[string]any{
		"first_name": "Alice",
		"last_name":  "Smith",
		"age":        30,
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["firstName"] != "Alice" {
		t.Errorf("expected firstName 'Alice', got %v", resultMap["firstName"])
	}
	if resultMap["lastName"] != "Smith" {
		t.Errorf("expected lastName 'Smith', got %v", resultMap["lastName"])
	}
	if resultMap["age"] != 30 {
		t.Errorf("expected age 30, got %v", resultMap["age"])
	}
	// Old names should be removed
	if _, exists := resultMap["first_name"]; exists {
		t.Error("expected 'first_name' to be removed after mapping")
	}
}

func TestDataTransformer_MapNoMappings(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "map", Config: map[string]any{}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing mappings config")
	}
}

func TestDataTransformer_MapNonMapInput(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "map", Config: map[string]any{
			"mappings": map[string]any{"a": "b"},
		}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "not a map")
	if err == nil {
		t.Fatal("expected error for non-map input")
	}
}

// Filter operation tests

func TestDataTransformer_FilterFields(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "filter",
			Config: map[string]any{
				"fields": []any{"name", "email"},
			},
		},
	}

	data := map[string]any{
		"name":     "Alice",
		"email":    "alice@example.com",
		"password": "secret",
		"age":      30,
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if len(resultMap) != 2 {
		t.Errorf("expected 2 fields, got %d", len(resultMap))
	}
	if resultMap["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", resultMap["name"])
	}
	if resultMap["email"] != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %v", resultMap["email"])
	}
	if _, exists := resultMap["password"]; exists {
		t.Error("expected 'password' to be filtered out")
	}
}

func TestDataTransformer_FilterNoFields(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "filter", Config: map[string]any{}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing fields config")
	}
}

func TestDataTransformer_FilterNonMapInput(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "filter", Config: map[string]any{
			"fields": []any{"name"},
		}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "not a map")
	if err == nil {
		t.Fatal("expected error for non-map input")
	}
}

// Convert operation tests

func TestDataTransformer_ConvertJsonToJson(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "json",
				"to":   "json",
			},
		},
	}

	data := map[string]any{
		"count": 42,
		"name":  "test",
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	// After json round-trip, int becomes float64
	if resultMap["count"] != float64(42) {
		t.Errorf("expected count 42.0, got %v (%T)", resultMap["count"], resultMap["count"])
	}
	if resultMap["name"] != "test" {
		t.Errorf("expected name 'test', got %v", resultMap["name"])
	}
}

func TestDataTransformer_ConvertJsonToString(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "json",
				"to":   "string",
			},
		},
	}

	data := map[string]any{"key": "value"}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}
	if str != `{"key":"value"}` {
		t.Errorf("expected JSON string, got %q", str)
	}
}

func TestDataTransformer_ConvertStringToJson(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "string",
				"to":   "json",
			},
		},
	}

	result, err := dt.TransformWithOps(context.Background(), ops, `{"name":"Alice","age":30}`)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	if resultMap["name"] != "Alice" {
		t.Errorf("expected name 'Alice', got %v", resultMap["name"])
	}
}

func TestDataTransformer_ConvertStringToJsonInvalid(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "string",
				"to":   "json",
			},
		},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "not json {{{")
	if err == nil {
		t.Fatal("expected error for invalid JSON string")
	}
}

func TestDataTransformer_ConvertStringToJsonNotString(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "string",
				"to":   "json",
			},
		},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, 42)
	if err == nil {
		t.Fatal("expected error for non-string input")
	}
}

func TestDataTransformer_ConvertUnsupported(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{
			Type: "convert",
			Config: map[string]any{
				"from": "xml",
				"to":   "csv",
			},
		},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "data")
	if err == nil {
		t.Fatal("expected error for unsupported conversion")
	}
}

func TestDataTransformer_ConvertMissingConfig(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "convert", Config: map[string]any{}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "data")
	if err == nil {
		t.Fatal("expected error for missing from/to config")
	}
}

// Unknown operation type

func TestDataTransformer_UnknownOperation(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "unknown_op", Config: map[string]any{}},
	}

	_, err := dt.TransformWithOps(context.Background(), ops, "data")
	if err == nil {
		t.Fatal("expected error for unknown operation type")
	}
}

// Pipeline with multiple operations

func TestDataTransformer_MultiStepPipeline(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		// Step 1: Filter to only user and metadata
		{
			Type: "filter",
			Config: map[string]any{
				"fields": []any{"user", "metadata"},
			},
		},
		// Step 2: Extract user data
		{
			Type:   "extract",
			Config: map[string]any{"path": "user"},
		},
		// Step 3: Rename fields
		{
			Type: "map",
			Config: map[string]any{
				"mappings": map[string]any{
					"first_name": "firstName",
				},
			},
		},
	}

	data := map[string]any{
		"user": map[string]any{
			"first_name": "Alice",
			"email":      "alice@example.com",
		},
		"metadata": map[string]any{"version": "1.0"},
		"internal": "should be filtered",
	}

	result, err := dt.TransformWithOps(context.Background(), ops, data)
	if err != nil {
		t.Fatalf("TransformWithOps failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}

	if resultMap["firstName"] != "Alice" {
		t.Errorf("expected firstName 'Alice', got %v", resultMap["firstName"])
	}
	if resultMap["email"] != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %v", resultMap["email"])
	}
}

func TestDataTransformer_ContextCancellation(t *testing.T) {
	dt := NewDataTransformer("t")
	ops := []TransformOperation{
		{Type: "extract", Config: map[string]any{"path": "name"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := dt.TransformWithOps(ctx, ops, map[string]any{"name": "Alice"})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
