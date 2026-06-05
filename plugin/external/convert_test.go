package external

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
)

// mustMapToStruct is a test helper that wraps mapToStruct and fails the test
// if the conversion errors. Use it for fixture data that is known to be
// representable by structpb.
func mustMapToStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := mapToStruct(m)
	if err != nil {
		t.Fatalf("mustMapToStruct: %v", err)
	}
	return s
}

// TestMapToStruct_PropagatesErrorOnUnrepresentableType verifies that
// mapToStruct surfaces structpb.NewStruct errors instead of silently
// dropping the entire map (workflow#537).
//
// A `chan` value is not representable in structpb (per
// google.golang.org/protobuf/types/known/structpb), so NewStruct returns an
// error. Prior to the fix, mapToStruct swallowed the error and returned an
// empty *structpb.Struct, hiding silent data loss in remote plugin calls.
func TestMapToStruct_PropagatesErrorOnUnrepresentableType(t *testing.T) {
	m := map[string]any{
		"ok_key":  "value",
		"bad_key": make(chan int),
	}
	s, err := mapToStruct(m)
	if err == nil {
		t.Fatal("expected error from structpb.NewStruct on chan, got nil")
	}
	if s != nil {
		t.Errorf("expected nil struct on error, got %v", s)
	}
}

// TestMapToStruct_NilInputReturnsNil documents the canonical nil-passthrough.
func TestMapToStruct_NilInputReturnsNil(t *testing.T) {
	s, err := mapToStruct(nil)
	if err != nil {
		t.Fatalf("expected no error on nil input, got %v", err)
	}
	if s != nil {
		t.Errorf("expected nil struct on nil input, got %v", s)
	}
}

// TestStripInternalKeys covers the engine-internal "_"-prefix key strip used
// before STRICT_PROTO / PROTO_WITH_LEGACY_STRUCT typed encode. Strip is the
// reserved namespace for engine internals (e.g. "_config_dir") which must not
// appear in plugin proto schemas — protojson with DiscardUnknown=false rejects
// them.
func TestStripInternalKeys(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{name: "nil input", in: nil, want: nil},
		{name: "no underscore keys", in: map[string]any{"a": 1, "b": "x"}, want: map[string]any{"a": 1, "b": "x"}},
		{name: "strips _config_dir", in: map[string]any{"_config_dir": "/etc", "name": "x"}, want: map[string]any{"name": "x"}},
		{name: "strips multiple", in: map[string]any{"_a": 1, "_b": 2, "c": 3}, want: map[string]any{"c": 3}},
		{name: "all stripped", in: map[string]any{"_a": 1, "_b": 2}, want: map[string]any{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripInternalKeys(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("stripInternalKeys(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestStripInternalKeysDoesNotMutateInput locks in the copy-on-clean contract:
// the caller's original map MUST retain "_"-prefix keys so the legacy
// *structpb.Struct path in createTypedConfigRequest can still consume them.
func TestStripInternalKeysDoesNotMutateInput(t *testing.T) {
	in := map[string]any{"_config_dir": "/etc", "name": "x"}
	_ = stripInternalKeys(in)
	if _, ok := in["_config_dir"]; !ok {
		t.Fatalf("stripInternalKeys mutated input — _config_dir removed from original")
	}
}

// TestMapToStruct_ValidMapRoundTrips verifies the happy path is preserved.
func TestMapToStruct_ValidMapRoundTrips(t *testing.T) {
	m := map[string]any{
		"name":    "test",
		"count":   float64(42), // structpb canonicalizes numbers as float64
		"enabled": true,
	}
	s, err := mapToStruct(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil struct")
	}
	got := s.AsMap()
	for k, v := range m {
		if got[k] != v {
			t.Errorf("key %q: expected %v, got %v", k, v, got[k])
		}
	}
}

func TestMapToStruct_NormalizesTypedSlices(t *testing.T) {
	s, err := mapToStruct(map[string]any{
		"ids": []string{"a", "b"},
		"rows": []map[string]any{
			{"id": "row-1", "amount": int64(100)},
		},
		"labels": []map[string]string{
			{"name": "urgent"},
		},
	})
	if err != nil {
		t.Fatalf("mapToStruct: %v", err)
	}
	got := s.AsMap()
	ids, ok := got["ids"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("ids = %#v, want []any{a,b}", got["ids"])
	}
	rows, ok := got["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("rows = %#v, want one row", got["rows"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["id"] != "row-1" || row["amount"] != float64(100) {
		t.Fatalf("row = %#v, want normalized map row", rows[0])
	}
	labels, ok := got["labels"].([]any)
	if !ok || len(labels) != 1 {
		t.Fatalf("labels = %#v, want one typed map label", got["labels"])
	}
	label, ok := labels[0].(map[string]any)
	if !ok || label["name"] != "urgent" {
		t.Fatalf("label = %#v, want normalized typed map", labels[0])
	}
}
