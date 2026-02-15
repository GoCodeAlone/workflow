package module

import (
	"context"
	"fmt"
	"testing"
)

// BenchmarkJQTransform measures JQ transform throughput.
// Target: 10,000 transforms/sec (from PLATFORM_ROADMAP.md Phase 2).
func BenchmarkJQTransform_Simple(b *testing.B) {
	factory := NewJQStepFactory()
	step, err := factory("jq-bench", map[string]any{
		"expression": ".name",
	}, nil)
	if err != nil {
		b.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "Alice",
		"age":   30,
		"email": "alice@example.com",
	}, nil)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := step.Execute(ctx, pc)
		if err != nil {
			b.Fatalf("execute error: %v", err)
		}
	}
}

func BenchmarkJQTransform_ObjectConstruction(b *testing.B) {
	factory := NewJQStepFactory()
	step, err := factory("jq-bench-obj", map[string]any{
		"expression": `{user_name: .name, user_email: .email}`,
	}, nil)
	if err != nil {
		b.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "Charlie",
		"email": "charlie@example.com",
		"role":  "admin",
	}, nil)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := step.Execute(ctx, pc)
		if err != nil {
			b.Fatalf("execute error: %v", err)
		}
	}
}

func BenchmarkJQTransform_ArraySelect(b *testing.B) {
	factory := NewJQStepFactory()
	step, err := factory("jq-bench-select", map[string]any{
		"expression": `[.items[] | select(.price > 1)]`,
	}, nil)
	if err != nil {
		b.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "apple", "price": 1.5},
			map[string]any{"name": "banana", "price": 0.75},
			map[string]any{"name": "cherry", "price": 3.0},
		},
	}, nil)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := step.Execute(ctx, pc)
		if err != nil {
			b.Fatalf("execute error: %v", err)
		}
	}
}

func BenchmarkJQTransform_Complex(b *testing.B) {
	factory := NewJQStepFactory()
	step, err := factory("jq-bench-complex", map[string]any{
		"expression": `{
			total_items: (.orders | length),
			total_value: ([.orders[].amount] | add),
			high_value: [.orders[] | select(.amount > 100) | .id]
		}`,
	}, nil)
	if err != nil {
		b.Fatalf("factory error: %v", err)
	}

	orders := make([]any, 50)
	for i := range orders {
		orders[i] = map[string]any{
			"id":     fmt.Sprintf("order-%d", i),
			"amount": float64(i * 10),
		}
	}

	pc := NewPipelineContext(map[string]any{
		"orders": orders,
	}, nil)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := step.Execute(ctx, pc)
		if err != nil {
			b.Fatalf("execute error: %v", err)
		}
	}
}

// BenchmarkJQTransform_Throughput runs the JQ step in a tight loop and measures ops/sec.
func BenchmarkJQTransform_Throughput(b *testing.B) {
	factory := NewJQStepFactory()
	step, err := factory("jq-throughput", map[string]any{
		"expression": `{name: .name, upper: .name, count: (.items | length)}`,
	}, nil)
	if err != nil {
		b.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "benchmark",
		"items": []any{"a", "b", "c", "d", "e"},
	}, nil)

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := step.Execute(ctx, pc)
		if err != nil {
			b.Fatalf("execute error: %v", err)
		}
	}
}
