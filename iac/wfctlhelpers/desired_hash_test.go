package wfctlhelpers_test

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestDesiredStateHash_Determinism asserts that calling DesiredStateHash twice
// with the same inputs returns the same value (the in-process plan/re-plan
// invariant).
func TestDesiredStateHash_Determinism(t *testing.T) {
	desired := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
		{Name: "db1", Type: "infra.database", Config: map[string]any{"size": "s"}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc1", ProviderID: "do-vpc-111"},
	}

	h1 := wfctlhelpers.DesiredStateHash(nil, desired, current, "staging")
	h2 := wfctlhelpers.DesiredStateHash(nil, desired, current, "staging")
	if h1 == "" {
		t.Fatal("DesiredStateHash returned empty string")
	}
	if h1 != h2 {
		t.Errorf("DesiredStateHash is not deterministic: %q != %q", h1, h2)
	}
}

// TestDesiredStateHash_ModuleRefCollapses asserts that a ${MODULE.id} ref in a
// spec's config is resolved to the ProviderID from current state before hashing
// (matching the CLI resolve+hash path).
func TestDesiredStateHash_ModuleRefCollapses(t *testing.T) {
	// desired spec references vpc1's id — must collapse to "do-vpc-111"
	desired := []interfaces.ResourceSpec{
		{Name: "db1", Type: "infra.database", Config: map[string]any{
			"vpc_id": "${vpc1.id}",
		}},
	}
	current := []interfaces.ResourceState{
		{Name: "vpc1", ProviderID: "do-vpc-111"},
	}

	// Hash with the state that can resolve ${vpc1.id}
	h := wfctlhelpers.DesiredStateHash(nil, desired, current, "staging")
	// Hash with the ref pre-resolved to prove it produces the same digest
	desiredResolved := []interfaces.ResourceSpec{
		{Name: "db1", Type: "infra.database", Config: map[string]any{
			"vpc_id": "do-vpc-111",
		}},
	}
	hResolved := wfctlhelpers.DesiredStateHash(nil, desiredResolved, nil, "staging")

	if h == "" || hResolved == "" {
		t.Fatal("DesiredStateHash returned empty string")
	}
	if h != hResolved {
		t.Errorf("${vpc1.id} not collapsed: hash with ref=%q, hash pre-resolved=%q", h, hResolved)
	}
}

// TestDesiredStateHash_ChangesOnFieldChange asserts that the hash changes when
// a spec field changes.
func TestDesiredStateHash_ChangesOnFieldChange(t *testing.T) {
	base := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
	}
	modified := []interfaces.ResourceSpec{
		{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "sfo3"}},
	}

	h1 := wfctlhelpers.DesiredStateHash(nil, base, nil, "staging")
	h2 := wfctlhelpers.DesiredStateHash(nil, modified, nil, "staging")
	if h1 == "" || h2 == "" {
		t.Fatal("DesiredStateHash returned empty string")
	}
	if h1 == h2 {
		t.Error("DesiredStateHash did not change when spec field changed")
	}
}

// TestDesiredStateHash_EmptySpecsIsStable asserts that an empty desired set
// returns a non-empty, stable hash (not "" sentinel — the delete-all case).
func TestDesiredStateHash_EmptySpecsIsStable(t *testing.T) {
	h := wfctlhelpers.DesiredStateHash(nil, nil, nil, "")
	if h == "" {
		t.Error("DesiredStateHash(empty) returned empty string — should be sha256([])")
	}
	h2 := wfctlhelpers.DesiredStateHash(nil, nil, nil, "")
	if h != h2 {
		t.Errorf("empty hash not deterministic: %q != %q", h, h2)
	}
}

// TestDesiredStateHash_SortOrderIndependent asserts that specs in different
// orders produce the same hash.
func TestDesiredStateHash_SortOrderIndependent(t *testing.T) {
	a := []interfaces.ResourceSpec{
		{Name: "aaa", Type: "infra.vpc"},
		{Name: "bbb", Type: "infra.database"},
	}
	b := []interfaces.ResourceSpec{
		{Name: "bbb", Type: "infra.database"},
		{Name: "aaa", Type: "infra.vpc"},
	}
	h1 := wfctlhelpers.DesiredStateHash(nil, a, nil, "")
	h2 := wfctlhelpers.DesiredStateHash(nil, b, nil, "")
	if h1 != h2 {
		t.Errorf("hash is order-dependent: a=%q b=%q", h1, h2)
	}
}

// TestDesiredStateHash_MatchesHandlerInlined is the divergence-guard for
// the inlined copy in iac/admin/handler (handler.DesiredHash). Both
// implementations must produce byte-identical digests for the same inputs,
// preventing silent copy-drift after future refactors of either function.
//
// handler.DesiredHash is exported specifically for this cross-package test;
// iac/admin/handler does NOT import iac/wfctlhelpers (no cycle).
func TestDesiredStateHash_MatchesHandlerInlined(t *testing.T) {
	cases := []struct {
		name    string
		desired []interfaces.ResourceSpec
		current []interfaces.ResourceState
	}{
		{
			name:    "empty",
			desired: nil,
			current: nil,
		},
		{
			name: "create-only (no current)",
			desired: []interfaces.ResourceSpec{
				{Name: "vpc1", Type: "infra.vpc", Config: map[string]any{"region": "nyc1"}},
			},
			current: nil,
		},
		{
			name: "module-ref collapsed",
			desired: []interfaces.ResourceSpec{
				{Name: "db1", Type: "infra.database", Config: map[string]any{"vpc_id": "${vpc1.id}"}},
			},
			current: []interfaces.ResourceState{
				{Name: "vpc1", ProviderID: "do-vpc-111"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h1 := wfctlhelpers.DesiredStateHash(nil, tc.desired, tc.current, "")
			h2 := handler.DesiredHash(nil, tc.desired, tc.current)
			if h1 != h2 {
				t.Errorf("divergence between wfctlhelpers.DesiredStateHash and handler.DesiredHash:\n  wfctlhelpers=%q\n  handler=     %q", h1, h2)
			}
		})
	}
}
