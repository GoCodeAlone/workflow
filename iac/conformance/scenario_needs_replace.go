package conformance

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// scenarioNeedsReplaceTriggersReplaceAction asserts the v2 IaC contract:
// when a provider's ResourceDriver.Diff returns NeedsReplace=true,
// platform.ComputePlan must emit Action="replace" rather than coercing
// to update or skip. This is the foundational replace-classification
// guarantee every IaC provider plugin MUST satisfy.
//
// The scenario is portable: it crafts a desired/current pair where a
// field marked ForceNew (region) differs, then dispatches ComputePlan
// against cfg.Provider() and asserts the resulting plan has exactly one
// action with Action="replace". Real provider plugins (DO, AWS, …)
// satisfy this by their Diff implementations recognising standard
// force-new fields; the in-tree fake satisfies it via configurable
// DiffResult.NeedsReplace=true.
//
// Smoke=true so this runs on every PR for active providers.
// RequiresCloud=true gates the run on cfg.LiveCloud — real provider
// plugins exercise their cloud Diff path; the in-tree self-test sets
// LiveCloud=true so the scenario fires against the configured fake.
func scenarioNeedsReplaceTriggersReplaceAction(t *testing.T, cfg Config) {
	t.Helper()
	// Conformance always tests the LIVE driver contract; install a
	// fresh no-op diff cache so a stale entry from a prior run can't
	// make the scenario pass for the wrong reason. SetDiffCacheForTest
	// Stores into platform's atomic.Pointer directly, bypassing the
	// sync.Once-sealed env-var path that the prior t.Setenv workaround
	// relied on. t.Cleanup restores the prior cache when the scenario
	// subtest returns, so consecutive Runs in the same process get
	// independent cache state.
	platform.SetDiffCacheForTest(t, diffcache.NewNoop())

	p := cfg.Provider()
	defer func() { _ = p.Close() }()

	desired := []interfaces.ResourceSpec{
		{Name: "vpc", Type: "infra.vpc", Config: map[string]any{"region": "nyc3"}},
	}
	current := []interfaces.ResourceState{
		{
			Name:          "vpc",
			Type:          "infra.vpc",
			ProviderID:    "existing-id",
			AppliedConfig: map[string]any{"region": "nyc1"},
		},
	}

	plan, err := platform.ComputePlan(context.Background(), p, desired, current)
	if err != nil {
		t.Fatalf("ComputePlan failed: %v", err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected exactly 1 action, got %d: %+v", len(plan.Actions), plan.Actions)
	}
	if plan.Actions[0].Action != "replace" {
		t.Errorf("expected Action=\"replace\" when Diff.NeedsReplace=true, got %q (full action: %+v)",
			plan.Actions[0].Action, plan.Actions[0])
	}
	if plan.Actions[0].Resource.Name != "vpc" {
		t.Errorf("Resource.Name = %q, want %q", plan.Actions[0].Resource.Name, "vpc")
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_NeedsReplaceTriggersReplaceAction",
		Smoke:         true,
		RequiresCloud: true,
		Run:           scenarioNeedsReplaceTriggersReplaceAction,
	})
}
