package main

import (
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestBuildAppliedSpecMap_OmitsAdoptionAndLegacy(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "apply-resource", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "apply"},
		{Name: "adoption-resource", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "adoption"},
		{Name: "legacy-resource", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: ""},
		{Name: "nil-config-resource", AppliedConfig: nil, AppliedConfigSource: "apply"},
		{Name: "empty-map-config-resource", AppliedConfig: map[string]any{}, AppliedConfigSource: "apply"},
	}
	refs := []interfaces.ResourceRef{
		{Name: "apply-resource"},
		{Name: "adoption-resource"},
		{Name: "legacy-resource"},
		{Name: "nil-config-resource"},
		{Name: "empty-map-config-resource"},
		{Name: "missing-from-state"},
	}

	got := buildAppliedSpecMap(states, refs)
	// adoption-resource: omitted (refuse false-positive on adoption)
	// legacy-resource: omitted (legacy default-to-adoption)
	// nil-config-resource: omitted (nil AppliedConfig)
	// empty-map-config-resource: omitted (len 0 — same branch as nil)
	// missing-from-state: omitted (no state)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry in result, got %d: %v", len(got), got)
	}
	spec, ok := got["apply-resource"]
	if !ok {
		t.Fatalf("expected 'apply-resource' in result; got %v", got)
	}
	if spec.Name != "apply-resource" {
		t.Errorf("spec.Name: got %q, want %q", spec.Name, "apply-resource")
	}
	if !reflect.DeepEqual(spec.Config, map[string]any{"k": "v"}) {
		t.Errorf("spec.Config: got %v, want %v", spec.Config, map[string]any{"k": "v"})
	}
}

// TestBuildAppliedSpecMap_PrefersStateTypeOverRefType verifies that when
// ResourceState.Type is set, it takes precedence over ref.Type in the
// ResourceSpec output (state is canonical; ref may be a lightweight lookup).
func TestBuildAppliedSpecMap_PrefersStateTypeOverRefType(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "x", Type: "infra.database", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "apply"},
	}
	refs := []interfaces.ResourceRef{{Name: "x", Type: "infra.other"}} // ref has wrong type

	got := buildAppliedSpecMap(states, refs)
	spec, ok := got["x"]
	if !ok {
		t.Fatalf("expected 'x' in result")
	}
	if spec.Type != "infra.database" {
		t.Errorf("spec.Type: got %q, want %q (state type should win)", spec.Type, "infra.database")
	}
}

func TestBuildAppliedSpecMap_NilStatesReturnsNil(t *testing.T) {
	refs := []interfaces.ResourceRef{{Name: "x"}}
	got := buildAppliedSpecMap(nil, refs)
	if got != nil {
		t.Errorf("expected nil for empty states, got %v", got)
	}
}

func TestBuildAppliedSpecMap_NilRefsReturnsNil(t *testing.T) {
	states := []interfaces.ResourceState{{Name: "x", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "apply"}}
	got := buildAppliedSpecMap(states, nil)
	if got != nil {
		t.Errorf("expected nil for nil refs, got %v", got)
	}
}

func TestBuildAppliedSpecMap_NoSafeEntriesReturnsNil(t *testing.T) {
	// When all entries are adoption/legacy, result is nil (not empty map).
	// Callers may use nil check to short-circuit the type-assertion.
	states := []interfaces.ResourceState{
		{Name: "x", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "adoption"},
	}
	refs := []interfaces.ResourceRef{{Name: "x"}}
	got := buildAppliedSpecMap(states, refs)
	if got != nil {
		t.Errorf("expected nil when no safe entries; got %v", got)
	}
}

func TestBuildAppliedSpecMap_ShallowCopyPreventsCallerMutation(t *testing.T) {
	states := []interfaces.ResourceState{
		{Name: "x", AppliedConfig: map[string]any{"k": "v"}, AppliedConfigSource: "apply"},
	}
	refs := []interfaces.ResourceRef{{Name: "x"}}

	got := buildAppliedSpecMap(states, refs)
	// Mutate the returned spec's Config; verify the source state is not affected.
	spec := got["x"]
	spec.Config["k"] = "mutated"
	if states[0].AppliedConfig["k"] == "mutated" {
		t.Errorf("buildAppliedSpecMap must return a shallow copy; source state was mutated")
	}
}
