package dynamic

import (
	"context"
	"sort"
	"sync"
	"testing"
)

// --- FieldContract unit tests ---

func TestNewFieldContract(t *testing.T) {
	c := NewFieldContract()
	if c == nil {
		t.Fatal("expected non-nil contract")
	}
	if len(c.RequiredInputs) != 0 {
		t.Errorf("expected empty required inputs, got %d", len(c.RequiredInputs))
	}
	if len(c.OptionalInputs) != 0 {
		t.Errorf("expected empty optional inputs, got %d", len(c.OptionalInputs))
	}
	if len(c.Outputs) != 0 {
		t.Errorf("expected empty outputs, got %d", len(c.Outputs))
	}
}

func TestValidateInputs_NilContract(t *testing.T) {
	err := ValidateInputs(nil, map[string]any{"x": 1})
	if err != nil {
		t.Errorf("expected nil error for nil contract, got: %v", err)
	}
}

func TestValidateInputs_RequiredFieldPresent(t *testing.T) {
	c := NewFieldContract()
	c.RequiredInputs["name"] = FieldSpec{Type: FieldTypeString, Description: "user name"}

	err := ValidateInputs(c, map[string]any{"name": "Alice"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
}

func TestValidateInputs_RequiredFieldMissing(t *testing.T) {
	c := NewFieldContract()
	c.RequiredInputs["name"] = FieldSpec{Type: FieldTypeString}

	err := ValidateInputs(c, map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if got := err.Error(); !contains(got, "missing required field \"name\"") {
		t.Errorf("expected missing field error, got: %s", got)
	}
}

func TestValidateInputs_RequiredFieldNil(t *testing.T) {
	c := NewFieldContract()
	c.RequiredInputs["name"] = FieldSpec{Type: FieldTypeString}

	err := ValidateInputs(c, map[string]any{"name": nil})
	if err == nil {
		t.Fatal("expected error for nil required field")
	}
}

func TestValidateInputs_TypeMismatch(t *testing.T) {
	c := NewFieldContract()
	c.RequiredInputs["count"] = FieldSpec{Type: FieldTypeInt}

	err := ValidateInputs(c, map[string]any{"count": "not-a-number"})
	if err == nil {
		t.Fatal("expected type mismatch error")
	}
	if got := err.Error(); !contains(got, "expected int") {
		t.Errorf("expected type mismatch message, got: %s", got)
	}
}

func TestValidateInputs_OptionalFieldMissing(t *testing.T) {
	c := NewFieldContract()
	c.OptionalInputs["tag"] = FieldSpec{Type: FieldTypeString}

	err := ValidateInputs(c, map[string]any{})
	if err != nil {
		t.Errorf("expected no error for missing optional field, got: %v", err)
	}
}

func TestValidateInputs_OptionalFieldTypeMismatch(t *testing.T) {
	c := NewFieldContract()
	c.OptionalInputs["tag"] = FieldSpec{Type: FieldTypeString}

	err := ValidateInputs(c, map[string]any{"tag": 42})
	if err == nil {
		t.Fatal("expected type mismatch for optional field")
	}
}

func TestValidateInputs_MultipleErrors(t *testing.T) {
	c := NewFieldContract()
	c.RequiredInputs["a"] = FieldSpec{Type: FieldTypeString}
	c.RequiredInputs["b"] = FieldSpec{Type: FieldTypeInt}

	err := ValidateInputs(c, map[string]any{})
	if err == nil {
		t.Fatal("expected error")
	}
	// Should contain both missing field messages
	got := err.Error()
	if !contains(got, "missing required field \"a\"") || !contains(got, "missing required field \"b\"") {
		t.Errorf("expected both fields mentioned, got: %s", got)
	}
}

// --- Type checking tests ---

func TestCheckType_String(t *testing.T) {
	if err := checkType("f", "hello", FieldTypeString); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", 42, FieldTypeString); err == nil {
		t.Error("expected error for int as string")
	}
}

func TestCheckType_Int(t *testing.T) {
	if err := checkType("f", 42, FieldTypeInt); err != nil {
		t.Errorf("expected no error for int: %v", err)
	}
	if err := checkType("f", int64(42), FieldTypeInt); err != nil {
		t.Errorf("expected no error for int64: %v", err)
	}
	// JSON numbers come as float64
	if err := checkType("f", float64(42), FieldTypeInt); err != nil {
		t.Errorf("expected no error for float64 as int: %v", err)
	}
	if err := checkType("f", "42", FieldTypeInt); err == nil {
		t.Error("expected error for string as int")
	}
}

func TestCheckType_Float(t *testing.T) {
	if err := checkType("f", float64(3.14), FieldTypeFloat); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", 42, FieldTypeFloat); err != nil {
		t.Errorf("expected no error for int as float: %v", err)
	}
	if err := checkType("f", "3.14", FieldTypeFloat); err == nil {
		t.Error("expected error for string as float")
	}
}

func TestCheckType_Bool(t *testing.T) {
	if err := checkType("f", true, FieldTypeBool); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", "true", FieldTypeBool); err == nil {
		t.Error("expected error for string as bool")
	}
}

func TestCheckType_Map(t *testing.T) {
	if err := checkType("f", map[string]any{"k": "v"}, FieldTypeMap); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", "not-a-map", FieldTypeMap); err == nil {
		t.Error("expected error for string as map")
	}
}

func TestCheckType_Slice(t *testing.T) {
	if err := checkType("f", []any{1, 2}, FieldTypeSlice); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", "not-a-slice", FieldTypeSlice); err == nil {
		t.Error("expected error for string as slice")
	}
}

func TestCheckType_Any(t *testing.T) {
	if err := checkType("f", "anything", FieldTypeAny); err != nil {
		t.Errorf("expected no error: %v", err)
	}
	if err := checkType("f", 42, FieldTypeAny); err != nil {
		t.Errorf("expected no error: %v", err)
	}
}

// --- ApplyDefaults tests ---

func TestApplyDefaults_NilContract(t *testing.T) {
	params := map[string]any{"x": 1}
	result := ApplyDefaults(nil, params)
	if result["x"] != 1 {
		t.Errorf("expected original params returned")
	}
}

func TestApplyDefaults_FillsMissing(t *testing.T) {
	c := NewFieldContract()
	c.OptionalInputs["urgency"] = FieldSpec{Type: FieldTypeString, Default: "standard"}
	c.OptionalInputs["count"] = FieldSpec{Type: FieldTypeInt, Default: 10}

	params := map[string]any{"name": "test"}
	result := ApplyDefaults(c, params)

	if result["urgency"] != "standard" {
		t.Errorf("expected default urgency, got %v", result["urgency"])
	}
	if result["count"] != 10 {
		t.Errorf("expected default count, got %v", result["count"])
	}
	if result["name"] != "test" {
		t.Error("original param should be preserved")
	}
}

func TestApplyDefaults_DoesNotOverwrite(t *testing.T) {
	c := NewFieldContract()
	c.OptionalInputs["urgency"] = FieldSpec{Type: FieldTypeString, Default: "standard"}

	params := map[string]any{"urgency": "critical"}
	result := ApplyDefaults(c, params)

	if result["urgency"] != "critical" {
		t.Errorf("expected existing value preserved, got %v", result["urgency"])
	}
}

func TestApplyDefaults_DoesNotMutateOriginal(t *testing.T) {
	c := NewFieldContract()
	c.OptionalInputs["urgency"] = FieldSpec{Type: FieldTypeString, Default: "standard"}

	params := map[string]any{"name": "test"}
	_ = ApplyDefaults(c, params)

	if _, ok := params["urgency"]; ok {
		t.Error("original params should not be mutated")
	}
}

// --- ContractRegistry tests ---

func TestContractRegistry_RegisterAndGet(t *testing.T) {
	cr := NewContractRegistry()
	c := NewFieldContract()
	c.RequiredInputs["body"] = FieldSpec{Type: FieldTypeString}

	cr.Register("keyword-matcher", c)

	got, ok := cr.Get("keyword-matcher")
	if !ok {
		t.Fatal("expected contract to be found")
	}
	if _, exists := got.RequiredInputs["body"]; !exists {
		t.Error("expected required input 'body' in contract")
	}
}

func TestContractRegistry_GetMissing(t *testing.T) {
	cr := NewContractRegistry()
	_, ok := cr.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestContractRegistry_Unregister(t *testing.T) {
	cr := NewContractRegistry()
	cr.Register("test", NewFieldContract())
	cr.Unregister("test")
	if _, ok := cr.Get("test"); ok {
		t.Error("expected contract to be removed")
	}
}

func TestContractRegistry_RegisterNil(t *testing.T) {
	cr := NewContractRegistry()
	cr.Register("test", nil)
	if _, ok := cr.Get("test"); ok {
		t.Error("nil contract should not be registered")
	}
}

func TestContractRegistry_List(t *testing.T) {
	cr := NewContractRegistry()
	cr.Register("a", NewFieldContract())
	cr.Register("b", NewFieldContract())

	names := cr.List()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("expected [a b], got %v", names)
	}
}

func TestContractRegistry_ConcurrentAccess(t *testing.T) {
	cr := NewContractRegistry()
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			c := NewFieldContract()
			cr.Register("comp", c)
		}(i)
	}
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cr.Get("comp")
			cr.List()
		}()
	}
	wg.Wait()
}

// --- parseContractMap tests ---

func TestParseContractMap_Nil(t *testing.T) {
	c := parseContractMap(nil)
	if c != nil {
		t.Error("expected nil for nil input")
	}
}

func TestParseContractMap_FullStructure(t *testing.T) {
	m := map[string]any{
		"required_inputs": map[string]any{
			"body": map[string]any{
				"type":        "string",
				"description": "message body",
			},
		},
		"optional_inputs": map[string]any{
			"urgency": map[string]any{
				"type":        "string",
				"description": "urgency level",
				"default":     "standard",
			},
		},
		"outputs": map[string]any{
			"matched": map[string]any{
				"type":        "bool",
				"description": "whether a keyword matched",
			},
		},
	}

	c := parseContractMap(m)
	if c == nil {
		t.Fatal("expected non-nil contract")
	}
	if len(c.RequiredInputs) != 1 {
		t.Errorf("expected 1 required input, got %d", len(c.RequiredInputs))
	}
	if c.RequiredInputs["body"].Type != FieldTypeString {
		t.Errorf("expected string type, got %v", c.RequiredInputs["body"].Type)
	}
	if c.OptionalInputs["urgency"].Default != "standard" {
		t.Errorf("expected default 'standard', got %v", c.OptionalInputs["urgency"].Default)
	}
	if c.Outputs["matched"].Type != FieldTypeBool {
		t.Errorf("expected bool type, got %v", c.Outputs["matched"].Type)
	}
}

func TestParseContractMap_SimpleTypeString(t *testing.T) {
	m := map[string]any{
		"required_inputs": map[string]any{
			"body": "string",
		},
	}
	c := parseContractMap(m)
	if c.RequiredInputs["body"].Type != FieldTypeString {
		t.Errorf("expected string type from shorthand, got %v", c.RequiredInputs["body"].Type)
	}
}

// --- Dynamic component contract integration ---

// Component source that declares a Contract() function.
const contractComponentSource = `package component

import (
	"context"
	"fmt"
)

func Name() string {
	return "contract-test"
}

func Contract() map[string]interface{} {
	return map[string]interface{}{
		"required_inputs": map[string]interface{}{
			"name": map[string]interface{}{
				"type":        "string",
				"description": "user name",
			},
		},
		"optional_inputs": map[string]interface{}{
			"greeting": map[string]interface{}{
				"type":        "string",
				"description": "greeting prefix",
				"default":     "hello",
			},
		},
		"outputs": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "greeting message",
			},
		},
	}
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	greeting, _ := params["greeting"].(string)
	if greeting == "" {
		greeting = "hello"
	}
	return map[string]interface{}{
		"message": fmt.Sprintf("%s %s", greeting, name),
	}, nil
}
`

func TestDynamicComponent_ContractExtracted(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(contractComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	if comp.Contract == nil {
		t.Fatal("expected contract to be extracted")
	}
	if _, ok := comp.Contract.RequiredInputs["name"]; !ok {
		t.Error("expected required input 'name'")
	}
	if _, ok := comp.Contract.OptionalInputs["greeting"]; !ok {
		t.Error("expected optional input 'greeting'")
	}
	if _, ok := comp.Contract.Outputs["message"]; !ok {
		t.Error("expected output 'message'")
	}
}

func TestDynamicComponent_ContractValidation_MissingRequired(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(contractComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	// Execute without required "name" field
	_, err := comp.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected validation error for missing required field")
	}
	if !contains(err.Error(), "missing required field") {
		t.Errorf("expected missing field error, got: %v", err)
	}
}

func TestDynamicComponent_ContractValidation_TypeMismatch(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(contractComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	// Execute with wrong type for "name"
	_, err := comp.Execute(context.Background(), map[string]any{"name": 42})
	if err == nil {
		t.Fatal("expected validation error for type mismatch")
	}
	if !contains(err.Error(), "expected string") {
		t.Errorf("expected type error, got: %v", err)
	}
}

func TestDynamicComponent_ContractValidation_Success(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(contractComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	result, err := comp.Execute(context.Background(), map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["message"] != "hello World" {
		t.Errorf("expected 'hello World', got %v", result["message"])
	}
}

func TestDynamicComponent_ContractDefaults(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(contractComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	// greeting should default to "hello"
	result, err := comp.Execute(context.Background(), map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["message"] != "hello World" {
		t.Errorf("expected default greeting, got %v", result["message"])
	}

	// Override greeting
	result, err = comp.Execute(context.Background(), map[string]any{"name": "World", "greeting": "hi"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["message"] != "hi World" {
		t.Errorf("expected 'hi World', got %v", result["message"])
	}
}

func TestDynamicComponent_NoContract(t *testing.T) {
	// Existing components without Contract() should still work
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test", pool)

	if err := comp.LoadFromSource(simpleComponentSource); err != nil {
		t.Fatalf("LoadFromSource failed: %v", err)
	}

	if comp.Contract != nil {
		t.Error("expected nil contract for component without Contract()")
	}

	// Execute should work normally
	result, err := comp.Execute(context.Background(), map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["greeting"] != "hello World" {
		t.Errorf("expected 'hello World', got %v", result["greeting"])
	}
}

// --- Loader integration with contracts ---

func TestLoader_ContractRegisteredAfterLoad(t *testing.T) {
	pool := NewInterpreterPool()
	reg := NewComponentRegistry()
	loader := NewLoader(pool, reg)

	comp, err := loader.LoadFromString("ctest", contractComponentSource)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}

	if comp.Contract == nil {
		t.Fatal("expected contract on loaded component")
	}
	if len(comp.Contract.RequiredInputs) != 1 {
		t.Errorf("expected 1 required input, got %d", len(comp.Contract.RequiredInputs))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
