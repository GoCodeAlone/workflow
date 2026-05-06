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
	want := map[string]map[string]any{
		"apply-resource": {"k": "v"},
		// adoption-resource: omitted (refuse false-positive on adoption)
		// legacy-resource: omitted (legacy default-to-adoption)
		// nil-config-resource: omitted (nil AppliedConfig)
		// empty-map-config-resource: omitted (len 0 — same branch as nil)
		// missing-from-state: omitted (no state)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
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
	// Mutate the returned map; verify the source state is not affected.
	got["x"]["k"] = "mutated"
	if states[0].AppliedConfig["k"] == "mutated" {
		t.Errorf("buildAppliedSpecMap must return a shallow copy; source state was mutated")
	}
}
