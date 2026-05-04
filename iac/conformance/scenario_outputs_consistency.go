package conformance

import (
	"context"
	"reflect"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioOutputsConsistencyAcrossReadCycles asserts the determinism
// contract on ResourceDriver.Read: two consecutive Read calls against
// the same ResourceRef MUST return Outputs that compare equal under
// reflect.DeepEqual when no apply or external mutation occurred
// between them. Catches a class of provider bugs where Read returns
// non-deterministic fields (request IDs, server-side timestamps,
// random ordering of slice elements) that masquerade as drift on the
// next refresh-outputs cycle.
//
// Smoke=false, RequiresCloud=true per design table row 10. Real
// provider plugins exercise the scenario against a live resource
// (Read-after-Read on the same ProviderID); the in-tree self-test
// uses a NoopDriver with a fixed ReadResult.
//
// Skip semantics: providers that don't expose a ResourceDriver for
// the well-known "infra.compute" probe type cause the scenario to
// t.Skipf — the contract is only meaningful for drivers that
// participate in the v2 read path.
func scenarioOutputsConsistencyAcrossReadCycles(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	// Two skip signals are honored: (a) ResourceDriver returns nil
	// without error (one provider idiom for "type unknown"), or
	// (b) returns an error (the canonical idiom — e.g.,
	// *platform.ResourceDriverNotFoundError). Either path is read as
	// "provider did not opt in" rather than a hard conformance failure.
	d, err := p.ResourceDriver("infra.compute")
	if err != nil {
		t.Skipf("provider %s does not expose a ResourceDriver for infra.compute "+
			"(read-consistency probe is opt-in for providers with a compute primitive): %v",
			p.Name(), err)
		return
	}
	if d == nil {
		t.Skipf("provider %s does not expose a ResourceDriver for infra.compute "+
			"(read-consistency contract is only meaningful for v2-dispatched drivers)",
			p.Name())
		return
	}

	ref := interfaces.ResourceRef{
		Name:       "vm",
		Type:       "infra.compute",
		ProviderID: "vm-id",
	}
	ctx := context.Background()

	first, err := d.Read(ctx, ref)
	if err != nil {
		t.Fatalf("first Read failed: %v", err)
	}
	second, err := d.Read(ctx, ref)
	if err != nil {
		t.Fatalf("second Read failed: %v", err)
	}

	// Both Reads against the same un-mutated resource must yield the
	// same Outputs map. Compare structural shapes including ProviderID
	// and Status — non-Outputs fields drifting also break refresh-
	// outputs reconciliation, so the contract is broader than
	// just .Outputs.
	if first == nil || second == nil {
		t.Fatalf("Read must return a non-nil ResourceOutput for an existing resource; "+
			"got first=%v second=%v", first, second)
	}
	if !reflect.DeepEqual(first.Outputs, second.Outputs) {
		t.Errorf("Outputs drift between consecutive Reads:\n  first:  %+v\n  second: %+v",
			first.Outputs, second.Outputs)
	}
	if first.ProviderID != second.ProviderID {
		t.Errorf("ProviderID drift between consecutive Reads: first=%q second=%q",
			first.ProviderID, second.ProviderID)
	}
	// Status must also be stable across consecutive reads — the comment
	// header advertises this contract, and a provider whose Status flips
	// between Reads (e.g., transient "syncing" → "ready" without state
	// mutation) would break Refresh's no-op detection downstream.
	if first.Status != second.Status {
		t.Errorf("Status drift between consecutive Reads: first=%q second=%q",
			first.Status, second.Status)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_OutputsConsistencyAcrossReadCycles",
		Smoke:         false,
		RequiresCloud: true,
		Run:           scenarioOutputsConsistencyAcrossReadCycles,
	})
}
