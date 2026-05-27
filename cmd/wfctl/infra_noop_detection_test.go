package main

import (
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
)

// TestIsNoopStateStore_RecognisesBothConcreteTypes guards the
// post-Task-1-lift invariant that isNoopStateStore detects both the
// legacy cmd/wfctl-internal *noopStateStore and the new
// *wfctlhelpers.NoopStateStore. The check feeds the post-apply
// "skip metadata persist when store is no-op" short-circuit in
// infra.go:1605 — if a concrete type goes unrecognised, real state is
// silently corrupted with a metadata.json from a discarded apply.
//
// Per code-reviewer I-2.2 on commit 7a064b824.
func TestIsNoopStateStore_RecognisesBothConcreteTypes(t *testing.T) {
	cases := []struct {
		name  string
		store infraStateStore
		want  bool
	}{
		{"legacy cmd/wfctl noopStateStore", &noopStateStore{}, true},
		{"new wfctlhelpers.NoopStateStore", &wfctlhelpers.NoopStateStore{}, true},
		{"fsWfctlStateStore is not a noop", &fsWfctlStateStore{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isNoopStateStore(c.store)
			if got != c.want {
				t.Errorf("isNoopStateStore(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}
