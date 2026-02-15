package module

import (
	"context"
	"testing"
)

func TestJQStepFieldAccess(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-field", map[string]any{
		"expression": ".name",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if step.Name() != "jq-field" {
		t.Errorf("expected step name 'jq-field', got %q", step.Name())
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "Alice",
		"age":   30,
		"email": "alice@example.com",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["result"] != "Alice" {
		t.Errorf("expected result='Alice', got %v", result.Output["result"])
	}
}

func TestJQStepNestedAccess(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-nested", map[string]any{
		"expression": ".address.city",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name": "Bob",
		"address": map[string]any{
			"city":  "Portland",
			"state": "OR",
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["result"] != "Portland" {
		t.Errorf("expected result='Portland', got %v", result.Output["result"])
	}
}

func TestJQStepObjectConstruction(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-obj", map[string]any{
		"expression": `{user_name: .name, user_email: .email}`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "Charlie",
		"email": "charlie@example.com",
		"role":  "admin",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// The single-result map should be merged into output.
	if result.Output["user_name"] != "Charlie" {
		t.Errorf("expected user_name='Charlie', got %v", result.Output["user_name"])
	}
	if result.Output["user_email"] != "charlie@example.com" {
		t.Errorf("expected user_email='charlie@example.com', got %v", result.Output["user_email"])
	}
}

func TestJQStepArrayMap(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-map", map[string]any{
		"expression": `[.items[] | {name: .name, upper_name: .name}]`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "apple", "price": 1.5},
			map[string]any{"name": "banana", "price": 0.75},
			map[string]any{"name": "cherry", "price": 3.0},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.Output["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be []any, got %T", result.Output["result"])
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 items, got %d", len(arr))
	}

	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element to be map, got %T", arr[0])
	}
	if first["name"] != "apple" {
		t.Errorf("expected first name='apple', got %v", first["name"])
	}
}

func TestJQStepArraySelect(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-select", map[string]any{
		"expression": `[.items[] | select(.price > 1)]`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "apple", "price": 1.5},
			map[string]any{"name": "banana", "price": 0.75},
			map[string]any{"name": "cherry", "price": 3.0},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.Output["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be []any, got %T", result.Output["result"])
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 items (price > 1), got %d", len(arr))
	}
}

func TestJQStepPipe(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-pipe", map[string]any{
		"expression": `.items | map(select(.active)) | length`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "a", "active": true},
			map[string]any{"name": "b", "active": false},
			map[string]any{"name": "c", "active": true},
			map[string]any{"name": "d", "active": true},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// gojq returns numbers as int or float64 depending on the operation.
	// length returns int.
	count, ok := result.Output["result"].(int)
	if !ok {
		t.Fatalf("expected result to be int, got %T (%v)", result.Output["result"], result.Output["result"])
	}
	if count != 3 {
		t.Errorf("expected 3 active items, got %d", count)
	}
}

func TestJQStepArithmetic(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-arith", map[string]any{
		"expression": `.price * .quantity`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"price":    9.99,
		"quantity": 3,
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	total, ok := result.Output["result"].(float64)
	if !ok {
		t.Fatalf("expected result to be float64, got %T (%v)", result.Output["result"], result.Output["result"])
	}
	expected := 9.99 * 3
	if total != expected {
		t.Errorf("expected total=%f, got %f", expected, total)
	}
}

func TestJQStepConditional(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-cond", map[string]any{
		"expression": `if .score >= 90 then "A" elif .score >= 80 then "B" else "C" end`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	tests := []struct {
		score    int
		expected string
	}{
		{95, "A"},
		{85, "B"},
		{70, "C"},
	}

	for _, tt := range tests {
		pc := NewPipelineContext(map[string]any{"score": tt.score}, nil)
		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("execute error for score=%d: %v", tt.score, err)
		}
		if result.Output["result"] != tt.expected {
			t.Errorf("score=%d: expected grade=%q, got %v", tt.score, tt.expected, result.Output["result"])
		}
	}
}

func TestJQStepWithPipelineContext(t *testing.T) {
	// Simulate a pipeline where a previous step produced data,
	// and the JQ step uses input_from to target that step's output.
	factory := NewJQStepFactory()
	step, err := factory("jq-ctx", map[string]any{
		"expression": `[.[] | select(.status == "completed")]`,
		"input_from": "steps.fetch-orders.orders",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("fetch-orders", map[string]any{
		"orders": []any{
			map[string]any{"id": "1", "status": "completed", "total": 100},
			map[string]any{"id": "2", "status": "pending", "total": 50},
			map[string]any{"id": "3", "status": "completed", "total": 200},
		},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.Output["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be []any, got %T", result.Output["result"])
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 completed orders, got %d", len(arr))
	}

	first, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element to be map, got %T", arr[0])
	}
	if first["id"] != "1" {
		t.Errorf("expected first completed order id='1', got %v", first["id"])
	}
}

func TestJQStepInvalidExpression(t *testing.T) {
	factory := NewJQStepFactory()

	// Unparseable expression
	_, err := factory("jq-bad", map[string]any{
		"expression": ".foo ||| bar",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid expression, got nil")
	}
	t.Logf("Got expected parse error: %v", err)
}

func TestJQStepMissingExpression(t *testing.T) {
	factory := NewJQStepFactory()

	_, err := factory("jq-empty", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing expression, got nil")
	}

	_, err = factory("jq-empty2", map[string]any{"expression": ""}, nil)
	if err == nil {
		t.Fatal("expected error for empty expression, got nil")
	}
}

func TestJQStepKeys(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-keys", map[string]any{
		"expression": `keys`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"alpha": 1,
		"beta":  2,
		"gamma": 3,
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.Output["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be []any, got %T", result.Output["result"])
	}
	if len(arr) != 3 {
		t.Errorf("expected 3 keys, got %d", len(arr))
	}
}

func TestJQStepLength(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-len", map[string]any{
		"expression": `.items | length`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c", "d", "e"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	length, ok := result.Output["result"].(int)
	if !ok {
		t.Fatalf("expected result to be int, got %T (%v)", result.Output["result"], result.Output["result"])
	}
	if length != 5 {
		t.Errorf("expected length=5, got %d", length)
	}
}

func TestJQStepAdd(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-add", map[string]any{
		"expression": `.values | add`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"values": []any{10, 20, 30},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// JSON numbers unmarshalled to float64 after normalization, then added.
	sum, ok := result.Output["result"].(float64)
	if !ok {
		t.Fatalf("expected result to be float64, got %T (%v)", result.Output["result"], result.Output["result"])
	}
	if sum != 60 {
		t.Errorf("expected sum=60, got %f", sum)
	}
}

func TestJQStepStringInterpolation(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-interp", map[string]any{
		"expression": `"Hello, \(.name)! You are \(.age) years old."`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name": "Dana",
		"age":  28,
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	expected := "Hello, Dana! You are 28 years old."
	if result.Output["result"] != expected {
		t.Errorf("expected %q, got %v", expected, result.Output["result"])
	}
}

func TestJQStepComplexPipeline(t *testing.T) {
	// A complex pipeline that demonstrates multiple JQ features combined:
	// group items by category, sum totals per group, build a summary.
	factory := NewJQStepFactory()
	step, err := factory("jq-complex", map[string]any{
		"expression": `{
			total_items: (.orders | length),
			total_value: ([.orders[].amount] | add),
			high_value: [.orders[] | select(.amount > 100) | .id]
		}`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"orders": []any{
			map[string]any{"id": "A1", "amount": 50},
			map[string]any{"id": "A2", "amount": 150},
			map[string]any{"id": "A3", "amount": 75},
			map[string]any{"id": "A4", "amount": 200},
		},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// total_items — gojq's length returns int
	totalItems, ok := result.Output["total_items"].(int)
	if !ok {
		t.Fatalf("expected total_items to be int, got %T", result.Output["total_items"])
	}
	if totalItems != 4 {
		t.Errorf("expected total_items=4, got %v", totalItems)
	}

	// total_value
	totalValue, ok := result.Output["total_value"].(float64)
	if !ok {
		t.Fatalf("expected total_value to be float64, got %T", result.Output["total_value"])
	}
	if totalValue != 475 {
		t.Errorf("expected total_value=475, got %v", totalValue)
	}

	// high_value IDs
	highValue, ok := result.Output["high_value"].([]any)
	if !ok {
		t.Fatalf("expected high_value to be []any, got %T", result.Output["high_value"])
	}
	if len(highValue) != 2 {
		t.Fatalf("expected 2 high value orders, got %d", len(highValue))
	}
	if highValue[0] != "A2" || highValue[1] != "A4" {
		t.Errorf("expected high_value=['A2','A4'], got %v", highValue)
	}
}

func TestJQStepIdentity(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-identity", map[string]any{
		"expression": ".",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"x": 1,
		"y": "two",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Identity returns the full input map. Since it's a map result,
	// its keys should be merged into output.
	if result.Output["x"] != float64(1) {
		t.Errorf("expected x=1 (float64 after JSON roundtrip), got %v (%T)", result.Output["x"], result.Output["x"])
	}
	if result.Output["y"] != "two" {
		t.Errorf("expected y='two', got %v", result.Output["y"])
	}
}

func TestJQStepFirstLast(t *testing.T) {
	factory := NewJQStepFactory()

	// first
	stepFirst, err := factory("jq-first", map[string]any{
		"expression": `.items | first`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// last
	stepLast, err := factory("jq-last", map[string]any{
		"expression": `.items | last`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"alpha", "beta", "gamma"},
	}, nil)

	resultFirst, err := stepFirst.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	if resultFirst.Output["result"] != "alpha" {
		t.Errorf("expected first='alpha', got %v", resultFirst.Output["result"])
	}

	resultLast, err := stepLast.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("last execute error: %v", err)
	}
	if resultLast.Output["result"] != "gamma" {
		t.Errorf("expected last='gamma', got %v", resultLast.Output["result"])
	}
}

func TestJQStepArrayIndex(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-idx", map[string]any{
		"expression": `.items[1]`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["result"] != "b" {
		t.Errorf("expected result='b', got %v", result.Output["result"])
	}
}

func TestJQStepInputFrom(t *testing.T) {
	factory := NewJQStepFactory()
	step, err := factory("jq-input-from", map[string]any{
		"expression": `[.[] | .name]`,
		"input_from": "users",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"users": []any{
			map[string]any{"name": "Alice", "role": "admin"},
			map[string]any{"name": "Bob", "role": "user"},
		},
		"unrelated": "data",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	arr, ok := result.Output["result"].([]any)
	if !ok {
		t.Fatalf("expected result to be []any, got %T", result.Output["result"])
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 names, got %d", len(arr))
	}
	if arr[0] != "Alice" || arr[1] != "Bob" {
		t.Errorf("expected ['Alice','Bob'], got %v", arr)
	}
}
