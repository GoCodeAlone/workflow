package schema

import (
	"fmt"
	"sync"
	"testing"
)

// newOrderSchema is a helper that returns a well-formed order.created schema.
func newOrderSchema(version string) *EventSchema {
	return &EventSchema{
		Type:        "order.created",
		Version:     version,
		Description: "Fired when a new order is created",
		Fields: map[string]FieldDef{
			"order_id":       {Type: "string", Description: "Order identifier", Format: "uuid"},
			"customer_email": {Type: "string", Description: "Customer email", Format: "email"},
			"amount":         {Type: "number", Description: "Order total"},
			"currency":       {Type: "string", Description: "ISO 4217 currency code", Enum: []string{"USD", "EUR", "GBP"}},
			"items":          {Type: "array", Description: "Line items"},
			"metadata":       {Type: "object", Description: "Arbitrary metadata"},
			"confirmed":      {Type: "boolean", Description: "Whether order is confirmed"},
			"created_at":     {Type: "string", Description: "Creation timestamp", Format: "date-time"},
			"callback_url":   {Type: "string", Description: "Webhook callback", Format: "uri"},
		},
		Required: []string{"order_id", "customer_email", "amount", "currency"},
	}
}

func TestRegisterAndGet(t *testing.T) {
	reg := NewEventSchemaRegistry()
	schema := newOrderSchema("1.0.0")

	if err := reg.Register(schema); err != nil {
		t.Fatalf("Register() unexpected error: %v", err)
	}

	got, ok := reg.Get("order.created", "1.0.0")
	if !ok {
		t.Fatal("Get() returned false, expected true")
	}
	if got.Type != "order.created" {
		t.Errorf("Get().Type = %q, want %q", got.Type, "order.created")
	}
	if got.Version != "1.0.0" {
		t.Errorf("Get().Version = %q, want %q", got.Version, "1.0.0")
	}
	if len(got.Fields) != 9 {
		t.Errorf("Get().Fields has %d entries, want 9", len(got.Fields))
	}

	// Not found
	_, ok = reg.Get("order.created", "2.0.0")
	if ok {
		t.Error("Get() for missing version returned true, expected false")
	}
	_, ok = reg.Get("nonexistent", "1.0.0")
	if ok {
		t.Error("Get() for missing type returned true, expected false")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	reg := NewEventSchemaRegistry()
	schema := newOrderSchema("1.0.0")

	if err := reg.Register(schema); err != nil {
		t.Fatalf("First Register() unexpected error: %v", err)
	}

	// Same type + version should fail
	dup := newOrderSchema("1.0.0")
	err := reg.Register(dup)
	if err == nil {
		t.Fatal("Register() of duplicate should return error, got nil")
	}
	if got := err.Error(); got == "" {
		t.Error("error message should not be empty")
	}

	// Different version should succeed
	v2 := newOrderSchema("2.0.0")
	if err := reg.Register(v2); err != nil {
		t.Fatalf("Register() different version unexpected error: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	reg := NewEventSchemaRegistry()

	// Nil schema
	if err := reg.Register(nil); err == nil {
		t.Error("Register(nil) should return error")
	}

	// Empty type
	if err := reg.Register(&EventSchema{Version: "1.0.0"}); err == nil {
		t.Error("Register() with empty type should return error")
	}

	// Empty version
	if err := reg.Register(&EventSchema{Type: "test"}); err == nil {
		t.Error("Register() with empty version should return error")
	}

	// Required field not in fields map
	if err := reg.Register(&EventSchema{
		Type:     "test",
		Version:  "1.0.0",
		Fields:   map[string]FieldDef{"name": {Type: "string"}},
		Required: []string{"name", "missing"},
	}); err == nil {
		t.Error("Register() with required field not in fields should return error")
	}

	// Invalid field type
	if err := reg.Register(&EventSchema{
		Type:    "test",
		Version: "1.0.0",
		Fields:  map[string]FieldDef{"bad": {Type: "invalid"}},
	}); err == nil {
		t.Error("Register() with invalid field type should return error")
	}
}

func TestGetLatestVersion(t *testing.T) {
	reg := NewEventSchemaRegistry()

	// Register versions out of order
	for _, v := range []string{"1.0.0", "1.2.0", "1.1.0", "2.0.0", "1.10.0"} {
		s := newOrderSchema(v)
		if err := reg.Register(s); err != nil {
			t.Fatalf("Register(%s) unexpected error: %v", v, err)
		}
	}

	latest, ok := reg.GetLatest("order.created")
	if !ok {
		t.Fatal("GetLatest() returned false, expected true")
	}
	if latest.Version != "2.0.0" {
		t.Errorf("GetLatest().Version = %q, want %q", latest.Version, "2.0.0")
	}

	// Non-existent type
	_, ok = reg.GetLatest("nonexistent")
	if ok {
		t.Error("GetLatest() for missing type returned true, expected false")
	}
}

func TestGetLatestVersionSemverOrdering(t *testing.T) {
	reg := NewEventSchemaRegistry()

	// Test that 1.10.0 > 1.9.0 (numeric, not lexicographic)
	for _, v := range []string{"1.9.0", "1.10.0"} {
		s := &EventSchema{
			Type:    "test.semver",
			Version: v,
			Fields:  map[string]FieldDef{},
		}
		if err := reg.Register(s); err != nil {
			t.Fatalf("Register(%s) unexpected error: %v", v, err)
		}
	}

	latest, ok := reg.GetLatest("test.semver")
	if !ok {
		t.Fatal("GetLatest() returned false")
	}
	if latest.Version != "1.10.0" {
		t.Errorf("GetLatest().Version = %q, want %q (numeric semver comparison)", latest.Version, "1.10.0")
	}
}

func TestValidateSuccess(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
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

	if err := reg.Validate("order.created", data); err != nil {
		t.Errorf("Validate() unexpected error: %v", err)
	}
}

func TestValidateWithExtraFields(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}

	// Extra fields not in schema should be allowed (open content model)
	data := map[string]any{
		"order_id":       "550e8400-e29b-41d4-a716-446655440000",
		"customer_email": "test@example.com",
		"amount":         42.50,
		"currency":       "USD",
		"extra_field":    "should be allowed",
	}

	if err := reg.Validate("order.created", data); err != nil {
		t.Errorf("Validate() with extra fields unexpected error: %v", err)
	}
}

func TestValidateRequiredFields(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}

	// Missing all required fields
	data := map[string]any{
		"items": []any{"item1"},
	}

	err := reg.Validate("order.created", data)
	if err == nil {
		t.Fatal("Validate() with missing required fields should return error")
	}

	errs, ok := err.(EventValidationErrors)
	if !ok {
		t.Fatalf("expected EventValidationErrors, got %T", err)
	}

	// Should have 4 errors for 4 missing required fields
	if len(errs) != 4 {
		t.Errorf("expected 4 validation errors, got %d: %v", len(errs), err)
	}

	// Check that each required field is reported
	reported := make(map[string]bool)
	for _, e := range errs {
		reported[e.Field] = true
	}
	for _, req := range []string{"order_id", "customer_email", "amount", "currency"} {
		if !reported[req] {
			t.Errorf("expected validation error for missing required field %q", req)
		}
	}
}

func TestValidateFieldTypes(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}

	validUUID := "550e8400-e29b-41d4-a716-446655440000"
	tests := []struct {
		name    string
		data    map[string]any
		wantErr bool
		field   string
	}{
		{
			name: "string field with wrong type",
			data: map[string]any{
				"order_id": 123, "customer_email": "a@b.com", "amount": 10.0, "currency": "USD",
			},
			wantErr: true,
			field:   "order_id",
		},
		{
			name: "number field with string",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": "not-a-number", "currency": "USD",
			},
			wantErr: true,
			field:   "amount",
		},
		{
			name: "boolean field with string",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": 10.0, "currency": "USD",
				"confirmed": "yes",
			},
			wantErr: true,
			field:   "confirmed",
		},
		{
			name: "object field with string",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": 10.0, "currency": "USD",
				"metadata": "not-an-object",
			},
			wantErr: true,
			field:   "metadata",
		},
		{
			name: "array field with string",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": 10.0, "currency": "USD",
				"items": "not-an-array",
			},
			wantErr: true,
			field:   "items",
		},
		{
			name: "number field with int",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": 10, "currency": "USD",
			},
			wantErr: false,
		},
		{
			name: "number field with int64",
			data: map[string]any{
				"order_id": validUUID, "customer_email": "a@b.com", "amount": int64(100), "currency": "USD",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reg.Validate("order.created", tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}
				errs, ok := err.(EventValidationErrors)
				if !ok {
					t.Fatalf("expected EventValidationErrors, got %T", err)
				}
				found := false
				for _, e := range errs {
					if e.Field == tt.field {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error for field %q, errors: %v", tt.field, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateEnumValues(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}

	// Valid enum value
	data := map[string]any{
		"order_id":       "550e8400-e29b-41d4-a716-446655440000",
		"customer_email": "a@b.com",
		"amount":         10.0,
		"currency":       "USD",
	}
	if err := reg.Validate("order.created", data); err != nil {
		t.Errorf("Validate() with valid enum value unexpected error: %v", err)
	}

	// Invalid enum value
	data["currency"] = "JPY"
	err := reg.Validate("order.created", data)
	if err == nil {
		t.Fatal("Validate() with invalid enum value should return error")
	}
	errs, ok := err.(EventValidationErrors)
	if !ok {
		t.Fatalf("expected EventValidationErrors, got %T", err)
	}
	found := false
	for _, e := range errs {
		if e.Field == "currency" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected error for field 'currency', got: %v", err)
	}
}

func TestValidateFormats(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}

	baseData := func() map[string]any {
		return map[string]any{
			"order_id":       "550e8400-e29b-41d4-a716-446655440000",
			"customer_email": "test@example.com",
			"amount":         42.50,
			"currency":       "USD",
			"created_at":     "2025-01-15T10:30:00Z",
			"callback_url":   "https://example.com/webhook",
		}
	}

	tests := []struct {
		name    string
		field   string
		value   string
		wantErr bool
	}{
		// Email format
		{"valid email", "customer_email", "user@example.com", false},
		{"valid email with plus", "customer_email", "user+tag@example.com", false},
		{"invalid email no at", "customer_email", "notanemail", true},
		{"invalid email no domain", "customer_email", "user@", true},

		// UUID format
		{"valid uuid", "order_id", "550e8400-e29b-41d4-a716-446655440000", false},
		{"valid uuid uppercase", "order_id", "550E8400-E29B-41D4-A716-446655440000", false},
		{"invalid uuid", "order_id", "not-a-uuid", true},
		{"invalid uuid short", "order_id", "550e8400-e29b-41d4", true},

		// Date-time format
		{"valid date-time", "created_at", "2025-01-15T10:30:00Z", false},
		{"valid date-time with offset", "created_at", "2025-01-15T10:30:00+05:00", false},
		{"invalid date-time", "created_at", "January 15, 2025", true},
		{"invalid date-time partial", "created_at", "2025-01-15", true},

		// URI format
		{"valid uri https", "callback_url", "https://example.com/path", false},
		{"valid uri http", "callback_url", "http://example.com", false},
		{"valid uri custom scheme", "callback_url", "custom+scheme://data", false},
		{"invalid uri no scheme", "callback_url", "example.com/path", true},
		{"invalid uri bare path", "callback_url", "/just/a/path", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := baseData()
			data[tt.field] = tt.value
			err := reg.Validate("order.created", data)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected validation error for %s=%q, got nil", tt.field, tt.value)
				}
				errs, ok := err.(EventValidationErrors)
				if !ok {
					t.Fatalf("expected EventValidationErrors, got %T", err)
				}
				found := false
				for _, e := range errs {
					if e.Field == tt.field {
						found = true
					}
				}
				if !found {
					t.Errorf("expected error for field %q, errors: %v", tt.field, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateNoSchemaRegistered(t *testing.T) {
	reg := NewEventSchemaRegistry()

	err := reg.Validate("nonexistent", map[string]any{"foo": "bar"})
	if err == nil {
		t.Fatal("Validate() for unregistered type should return error")
	}
}

func TestValidateVersionSpecific(t *testing.T) {
	reg := NewEventSchemaRegistry()

	v1 := &EventSchema{
		Type:     "user.updated",
		Version:  "1.0.0",
		Fields:   map[string]FieldDef{"name": {Type: "string"}},
		Required: []string{"name"},
	}
	v2 := &EventSchema{
		Type:     "user.updated",
		Version:  "2.0.0",
		Fields:   map[string]FieldDef{"name": {Type: "string"}, "email": {Type: "string"}},
		Required: []string{"name", "email"},
	}
	if err := reg.Register(v1); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(v2); err != nil {
		t.Fatal(err)
	}

	// Valid against v1 (only name required)
	data := map[string]any{"name": "Alice"}
	if err := reg.ValidateVersion("user.updated", "1.0.0", data); err != nil {
		t.Errorf("ValidateVersion v1 unexpected error: %v", err)
	}

	// Invalid against v2 (email also required)
	err := reg.ValidateVersion("user.updated", "2.0.0", data)
	if err == nil {
		t.Fatal("ValidateVersion v2 should fail without email")
	}

	// Non-existent version
	err = reg.ValidateVersion("user.updated", "3.0.0", data)
	if err == nil {
		t.Fatal("ValidateVersion for missing version should return error")
	}
}

func TestRemove(t *testing.T) {
	reg := NewEventSchemaRegistry()
	if err := reg.Register(newOrderSchema("1.0.0")); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(newOrderSchema("2.0.0")); err != nil {
		t.Fatal(err)
	}

	// Remove existing
	if !reg.Remove("order.created", "1.0.0") {
		t.Error("Remove() returned false for existing schema")
	}

	// Verify it's gone
	_, ok := reg.Get("order.created", "1.0.0")
	if ok {
		t.Error("Get() still finds removed schema")
	}

	// v2 should still be there
	_, ok = reg.Get("order.created", "2.0.0")
	if !ok {
		t.Error("Get() cannot find schema that should still exist")
	}

	// Remove non-existent
	if reg.Remove("order.created", "1.0.0") {
		t.Error("Remove() returned true for already-removed schema")
	}
	if reg.Remove("nonexistent", "1.0.0") {
		t.Error("Remove() returned true for non-existent type")
	}
}

func TestListTypes(t *testing.T) {
	reg := NewEventSchemaRegistry()

	schemas := []*EventSchema{
		{Type: "order.created", Version: "1.0.0", Fields: map[string]FieldDef{}},
		{Type: "order.created", Version: "2.0.0", Fields: map[string]FieldDef{}},
		{Type: "order.shipped", Version: "1.0.0", Fields: map[string]FieldDef{}},
		{Type: "user.registered", Version: "1.0.0", Fields: map[string]FieldDef{}},
	}
	for _, s := range schemas {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	types := reg.ListTypes()
	expected := []string{"order.created", "order.shipped", "user.registered"}

	if len(types) != len(expected) {
		t.Fatalf("ListTypes() returned %d types, want %d", len(types), len(expected))
	}
	for i, typ := range types {
		if typ != expected[i] {
			t.Errorf("ListTypes()[%d] = %q, want %q", i, typ, expected[i])
		}
	}
}

func TestList(t *testing.T) {
	reg := NewEventSchemaRegistry()

	schemas := []*EventSchema{
		{Type: "b.event", Version: "2.0.0", Fields: map[string]FieldDef{}},
		{Type: "a.event", Version: "1.0.0", Fields: map[string]FieldDef{}},
		{Type: "b.event", Version: "1.0.0", Fields: map[string]FieldDef{}},
	}
	for _, s := range schemas {
		if err := reg.Register(s); err != nil {
			t.Fatal(err)
		}
	}

	list := reg.List()
	if len(list) != 3 {
		t.Fatalf("List() returned %d schemas, want 3", len(list))
	}

	// Should be sorted by type, then version
	expectedOrder := []struct{ typ, ver string }{
		{"a.event", "1.0.0"},
		{"b.event", "1.0.0"},
		{"b.event", "2.0.0"},
	}
	for i, exp := range expectedOrder {
		if list[i].Type != exp.typ || list[i].Version != exp.ver {
			t.Errorf("List()[%d] = {%q, %q}, want {%q, %q}",
				i, list[i].Type, list[i].Version, exp.typ, exp.ver)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	reg := NewEventSchemaRegistry()

	// Pre-register a schema for concurrent validation
	base := &EventSchema{
		Type:     "concurrent.test",
		Version:  "1.0.0",
		Fields:   map[string]FieldDef{"name": {Type: "string"}},
		Required: []string{"name"},
	}
	if err := reg.Register(base); err != nil {
		t.Fatal(err)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*3)

	// Concurrent registrations (each with unique version)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s := &EventSchema{
				Type:     "concurrent.test",
				Version:  fmt.Sprintf("2.%d.0", n),
				Fields:   map[string]FieldDef{"name": {Type: "string"}},
				Required: []string{"name"},
			}
			if err := reg.Register(s); err != nil {
				errs <- fmt.Errorf("register goroutine %d: %w", n, err)
			}
		}(i)
	}

	// Concurrent validations
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			data := map[string]any{"name": fmt.Sprintf("user-%d", n)}
			if err := reg.Validate("concurrent.test", data); err != nil {
				errs <- fmt.Errorf("validate goroutine %d: %w", n, err)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = reg.ListTypes()
			_ = reg.List()
			reg.Get("concurrent.test", "1.0.0")
			reg.GetLatest("concurrent.test")
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Verify all registrations succeeded
	types := reg.ListTypes()
	found := false
	for _, typ := range types {
		if typ == "concurrent.test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("concurrent.test type not found after concurrent registrations")
	}

	// We should have 1 (base) + goroutines versions
	list := reg.List()
	concurrentCount := 0
	for _, s := range list {
		if s.Type == "concurrent.test" {
			concurrentCount++
		}
	}
	if concurrentCount != goroutines+1 {
		t.Errorf("expected %d concurrent.test schemas, got %d", goroutines+1, concurrentCount)
	}
}

func TestValidateMultipleErrors(t *testing.T) {
	reg := NewEventSchemaRegistry()
	schema := &EventSchema{
		Type:    "multi.error",
		Version: "1.0.0",
		Fields: map[string]FieldDef{
			"name":   {Type: "string"},
			"age":    {Type: "number"},
			"active": {Type: "boolean"},
			"status": {Type: "string", Enum: []string{"active", "inactive"}},
		},
		Required: []string{"name", "age"},
	}
	if err := reg.Register(schema); err != nil {
		t.Fatal(err)
	}

	// Data with multiple problems: missing required 'age', wrong type for 'active', bad enum
	data := map[string]any{
		"name":   "test",
		"active": "not-a-bool",
		"status": "deleted",
	}

	err := reg.Validate("multi.error", data)
	if err == nil {
		t.Fatal("expected multiple validation errors, got nil")
	}

	errs, ok := err.(EventValidationErrors)
	if !ok {
		t.Fatalf("expected EventValidationErrors, got %T", err)
	}

	// Should have at least 3 errors: missing age, wrong type active, bad enum status
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), err)
	}

	// Verify the Error() string contains all the errors
	errStr := errs.Error()
	if !contains(errStr, "3 error(s)") && !contains(errStr, "error(s)") {
		t.Errorf("Error() string doesn't mention error count: %s", errStr)
	}
}

func TestEventValidationErrorString(t *testing.T) {
	e := &EventValidationError{Field: "email", Message: "required field is missing"}
	got := e.Error()
	expected := `field "email": required field is missing`
	if got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}

	e2 := &EventValidationError{Message: "general error"}
	if e2.Error() != "general error" {
		t.Errorf("Error() without field = %q, want %q", e2.Error(), "general error")
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.9.0", "1.10.0", -1}, // numeric, not lexicographic
		{"1.10.0", "1.9.0", 1},
		{"10.0.0", "2.0.0", 1},
	}

	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidateNilValueForRequiredField(t *testing.T) {
	reg := NewEventSchemaRegistry()
	schema := &EventSchema{
		Type:     "nil.test",
		Version:  "1.0.0",
		Fields:   map[string]FieldDef{"name": {Type: "string"}},
		Required: []string{"name"},
	}
	if err := reg.Register(schema); err != nil {
		t.Fatal(err)
	}

	// Field is present but nil -- should still pass (presence is checked, not nil-ness for type)
	data := map[string]any{"name": nil}
	if err := reg.Validate("nil.test", data); err != nil {
		t.Errorf("Validate() with nil value for present key should pass: %v", err)
	}
}

// contains is a helper for checking substring presence.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
