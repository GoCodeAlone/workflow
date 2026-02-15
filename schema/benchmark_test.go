package schema

import (
	"fmt"
	"testing"
)

// BenchmarkSchemaValidation measures event schema validation overhead.
// Target: <1ms per validation (from PLATFORM_ROADMAP.md Phase 2).
func BenchmarkSchemaValidation_Simple(b *testing.B) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		b.Fatal(err)
	}

	data := map[string]any{
		"order_id":       "550e8400-e29b-41d4-a716-446655440000",
		"customer_email": "test@example.com",
		"amount":         42.50,
		"currency":       "USD",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := reg.Validate("order.created", data); err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}

func BenchmarkSchemaValidation_AllFields(b *testing.B) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		b.Fatal(err)
	}

	data := map[string]any{
		"order_id":       "550e8400-e29b-41d4-a716-446655440000",
		"customer_email": "test@example.com",
		"amount":         42.50,
		"currency":       "USD",
		"items":          []any{"item1", "item2"},
		"metadata":       map[string]any{"source": "web"},
		"confirmed":      true,
		"created_at":     "2025-01-15T10:30:00Z",
		"callback_url":   "https://example.com/webhook",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := reg.Validate("order.created", data); err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}

func BenchmarkSchemaValidation_FormatValidation(b *testing.B) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		b.Fatal(err)
	}

	data := map[string]any{
		"order_id":       "550e8400-e29b-41d4-a716-446655440000",
		"customer_email": "user+tag@example.com",
		"amount":         99.99,
		"currency":       "EUR",
		"created_at":     "2025-06-15T10:30:00+05:00",
		"callback_url":   "https://example.com/webhook/v2/callback?token=abc",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := reg.Validate("order.created", data); err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}

func BenchmarkSchemaValidation_ManySchemas(b *testing.B) {
	reg := NewEventSchemaRegistry()

	// Register 100 different schemas to simulate a loaded registry
	for i := 0; i < 100; i++ {
		schema := &EventSchema{
			Type:    fmt.Sprintf("event.type.%d", i),
			Version: "1.0.0",
			Fields: map[string]FieldDef{
				"id":   {Type: "string", Format: "uuid"},
				"name": {Type: "string"},
				"val":  {Type: "number"},
			},
			Required: []string{"id", "name"},
		}
		if err := reg.Register(schema); err != nil {
			b.Fatal(err)
		}
	}

	data := map[string]any{
		"id":   "550e8400-e29b-41d4-a716-446655440000",
		"name": "test",
		"val":  42.0,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Validate against the 50th schema to ensure lookup in a populated registry
		if err := reg.Validate("event.type.50", data); err != nil {
			b.Fatalf("Validate error: %v", err)
		}
	}
}
