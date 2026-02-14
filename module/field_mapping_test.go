package module

import (
	"encoding/json"
	"testing"
)

func TestFieldMapping_Set_And_Resolve(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body", "content")

	data := map[string]any{"Body": "hello"}

	val, ok := fm.Resolve(data, "body")
	if !ok {
		t.Fatal("expected Resolve to find 'body' via fallback 'Body'")
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestFieldMapping_Resolve_PrimaryFirst(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body", "content")

	data := map[string]any{
		"body":    "primary",
		"Body":    "secondary",
		"content": "tertiary",
	}

	val, ok := fm.Resolve(data, "body")
	if !ok {
		t.Fatal("expected Resolve to succeed")
	}
	if val != "primary" {
		t.Errorf("expected primary value 'primary', got %v", val)
	}
}

func TestFieldMapping_Resolve_NotFound(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body")

	data := map[string]any{"content": "hello"}

	_, ok := fm.Resolve(data, "body")
	if ok {
		t.Error("expected Resolve to return false when no match")
	}
}

func TestFieldMapping_Resolve_NoMapping(t *testing.T) {
	fm := NewFieldMapping()
	// No mapping set for "body" — should fall back to using "body" as literal key

	data := map[string]any{"body": "hello"}

	val, ok := fm.Resolve(data, "body")
	if !ok {
		t.Fatal("expected Resolve to fall back to literal logical name")
	}
	if val != "hello" {
		t.Errorf("expected 'hello', got %v", val)
	}
}

func TestFieldMapping_ResolveString(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("state", "state")

	data := map[string]any{"state": "active"}
	got := fm.ResolveString(data, "state")
	if got != "active" {
		t.Errorf("expected 'active', got %q", got)
	}

	// Non-string value
	data["state"] = 42
	got = fm.ResolveString(data, "state")
	if got != "" {
		t.Errorf("expected empty string for non-string value, got %q", got)
	}

	// Missing value
	got = fm.ResolveString(data, "missing")
	if got != "" {
		t.Errorf("expected empty string for missing field, got %q", got)
	}
}

func TestFieldMapping_ResolveSlice(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("tags", "tags")

	tags := []any{"a", "b"}
	data := map[string]any{"tags": tags}

	got := fm.ResolveSlice(data, "tags")
	if len(got) != 2 {
		t.Errorf("expected 2 tags, got %d", len(got))
	}

	// Missing
	got = fm.ResolveSlice(data, "missing")
	if got != nil {
		t.Errorf("expected nil for missing field, got %v", got)
	}
}

func TestFieldMapping_SetValue(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body")

	data := make(map[string]any)
	fm.SetValue(data, "body", "hello")

	if data["body"] != "hello" {
		t.Errorf("expected SetValue to use primary name 'body', got %v", data)
	}
	if _, exists := data["Body"]; exists {
		t.Error("SetValue should only set the primary field name")
	}
}

func TestFieldMapping_SetValue_NoMapping(t *testing.T) {
	fm := NewFieldMapping()

	data := make(map[string]any)
	fm.SetValue(data, "unmapped", "value")

	if data["unmapped"] != "value" {
		t.Error("expected SetValue to use logical name when no mapping defined")
	}
}

func TestFieldMapping_Primary(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body", "content")

	if fm.Primary("body") != "body" {
		t.Errorf("expected Primary to return 'body', got %q", fm.Primary("body"))
	}

	// Unmapped returns logical name
	if fm.Primary("unmapped") != "unmapped" {
		t.Errorf("expected Primary to return logical name for unmapped, got %q", fm.Primary("unmapped"))
	}
}

func TestFieldMapping_Has(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("state", "state")

	if !fm.Has("state") {
		t.Error("expected Has('state') to return true")
	}
	if fm.Has("missing") {
		t.Error("expected Has('missing') to return false")
	}
}

func TestFieldMapping_Merge(t *testing.T) {
	fm1 := NewFieldMapping()
	fm1.Set("state", "state")
	fm1.Set("body", "body")

	fm2 := NewFieldMapping()
	fm2.Set("body", "content", "Body") // override
	fm2.Set("tags", "tags")            // new

	fm1.Merge(fm2)

	// body should be overridden
	if fm1.Primary("body") != "content" {
		t.Errorf("expected merged body primary to be 'content', got %q", fm1.Primary("body"))
	}
	// state should remain
	if !fm1.Has("state") {
		t.Error("expected state to survive merge")
	}
	// tags should be added
	if !fm1.Has("tags") {
		t.Error("expected tags to be added from merge")
	}
}

func TestFieldMapping_Clone(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("body", "body", "Body")

	clone := fm.Clone()
	clone.Set("body", "content") // mutate clone

	// Original should be unaffected
	if fm.Primary("body") != "body" {
		t.Errorf("expected original to be unaffected by clone mutation, got %q", fm.Primary("body"))
	}
	if clone.Primary("body") != "content" {
		t.Errorf("expected clone mutation to take effect, got %q", clone.Primary("body"))
	}
}

func TestFieldMappingFromConfig(t *testing.T) {
	cfg := map[string]any{
		"state": "status",                            // single string
		"body":  []any{"content", "Body", "message"}, // string slice
		"tags":  []string{"labels", "tags"},          // native string slice
	}

	fm := FieldMappingFromConfig(cfg)

	if fm.Primary("state") != "status" {
		t.Errorf("expected state primary 'status', got %q", fm.Primary("state"))
	}
	if fm.Primary("body") != "content" {
		t.Errorf("expected body primary 'content', got %q", fm.Primary("body"))
	}
	if fm.Primary("tags") != "labels" {
		t.Errorf("expected tags primary 'labels', got %q", fm.Primary("tags"))
	}
}

func TestFieldMappingFromConfig_Nil(t *testing.T) {
	fm := FieldMappingFromConfig(nil)
	if fm == nil {
		t.Fatal("expected non-nil FieldMapping from nil config")
	}
	// Should work as pass-through
	data := map[string]any{"foo": "bar"}
	val, ok := fm.Resolve(data, "foo")
	if !ok || val != "bar" {
		t.Error("expected pass-through resolution for empty mapping")
	}
}

func TestFieldMapping_JSON_RoundTrip(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("state", "state")
	fm.Set("body", "body", "Body", "content")

	data, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	fm2 := NewFieldMapping()
	if err := json.Unmarshal(data, fm2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if fm2.Primary("state") != "state" {
		t.Error("state mapping lost in roundtrip")
	}
	if fm2.Primary("body") != "body" {
		t.Error("body mapping lost in roundtrip")
	}

	// Verify fallback resolution works after roundtrip
	testData := map[string]any{"Body": "hello"}
	val, ok := fm2.Resolve(testData, "body")
	if !ok || val != "hello" {
		t.Error("fallback resolution broken after JSON roundtrip")
	}
}

func TestDefaultRESTFieldMapping(t *testing.T) {
	fm := DefaultRESTFieldMapping()

	// Verify key defaults
	if fm.Primary("state") != "state" {
		t.Errorf("expected state default 'state', got %q", fm.Primary("state"))
	}
	if fm.Primary("body") != "body" {
		t.Errorf("expected body default 'body', got %q", fm.Primary("body"))
	}
	if fm.Primary("riskLevel") != "riskLevel" {
		t.Errorf("expected riskLevel default 'riskLevel', got %q", fm.Primary("riskLevel"))
	}

	// Verify body fallback chain works
	data := map[string]any{"content": "hello"}
	val, ok := fm.Resolve(data, "body")
	if !ok || val != "hello" {
		t.Error("expected body fallback chain to resolve 'content'")
	}
}

func TestDefaultTransitionMap(t *testing.T) {
	tm := DefaultTransitionMap()

	expected := map[string]string{
		"assign":    "assign_responder",
		"transfer":  "transfer_to_responder",
		"escalate":  "escalate_to_medical",
		"wrap-up":   "begin_wrap_up",
		"close":     "close_from_active",
		"follow-up": "schedule_follow_up",
		"survey":    "send_entry_survey",
	}

	for action, transition := range expected {
		if tm[action] != transition {
			t.Errorf("expected %s -> %s, got %s", action, transition, tm[action])
		}
	}
}

func TestDefaultSummaryFields(t *testing.T) {
	fields := DefaultSummaryFields()
	if len(fields) != 10 {
		t.Errorf("expected 10 default summary fields, got %d", len(fields))
	}

	// Check a few expected fields
	found := make(map[string]bool)
	for _, f := range fields {
		found[f] = true
	}
	for _, expected := range []string{"programId", "riskLevel", "tags", "responderId"} {
		if !found[expected] {
			t.Errorf("expected default summary fields to include %q", expected)
		}
	}
}

func TestFieldMapping_Set_Empty(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("empty") // no actual names
	if fm.Has("empty") {
		t.Error("expected Set with no actual names to be a no-op")
	}
}

func TestFieldMapping_Merge_Nil(t *testing.T) {
	fm := NewFieldMapping()
	fm.Set("state", "state")
	fm.Merge(nil) // should not panic
	if !fm.Has("state") {
		t.Error("expected state to survive nil merge")
	}
}

func TestFieldMapping_String(t *testing.T) {
	fm := NewFieldMapping()
	if fm.String() != "FieldMapping{}" {
		t.Errorf("expected empty string repr, got %q", fm.String())
	}

	fm.Set("state", "state")
	s := fm.String()
	if s == "FieldMapping{}" {
		t.Error("expected non-empty string repr after Set")
	}
}
