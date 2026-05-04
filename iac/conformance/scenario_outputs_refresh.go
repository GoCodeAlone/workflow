package conformance

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/refreshoutputs"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioOutputsRefreshDetectsNewFields asserts that a Refresh against a
// provider whose live Read returns Outputs with newly-added fields (e.g.,
// after a plugin upgrade widens the output map) reconciles those fields
// into persisted state. Closes the W-3a root-cause issue B (state outputs
// lag): without Refresh picking up the new keys, downstream consumers
// (cross-module substitution, drift detection) silently see the old
// shape forever.
//
// Smoke=false (not on every PR), RequiresCloud=true per design table
// row 4. The in-tree self-test installs a fake whose ResourceDriver.Read
// returns Outputs with one extra key relative to the persisted state;
// real provider plugins exercise the same scenario after a release that
// extends their output schema.
func scenarioOutputsRefreshDetectsNewFields(t *testing.T, cfg Config) {
	t.Helper()

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	// Persisted state — what's currently on disk before refresh.
	states := []interfaces.ResourceState{
		{
			Name:       "vm",
			Type:       "infra.compute",
			ProviderID: "vm-id",
			Outputs:    map[string]any{"ip": "1.2.3.4"},
		},
	}

	out, err := refreshoutputs.Refresh(context.Background(), p, states, refreshoutputs.Options{})
	if err != nil {
		t.Fatalf("Refresh failed: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 refreshed state, got %d", len(out))
	}

	// Live driver Read returned Outputs with an additional "endpoint"
	// key; Refresh must reconcile that into the returned state.
	got := out[0].Outputs
	if got["ip"] != "1.2.3.4" {
		t.Errorf("ip preserved-from-live: got %v, want %q", got["ip"], "1.2.3.4")
	}
	if _, ok := got["endpoint"]; !ok {
		t.Errorf("Refresh did not pick up newly-added \"endpoint\" key; got Outputs=%+v", got)
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_OutputsRefreshDetectsNewFields",
		Smoke:         false,
		RequiresCloud: true,
		Run:           scenarioOutputsRefreshDetectsNewFields,
	})
}
