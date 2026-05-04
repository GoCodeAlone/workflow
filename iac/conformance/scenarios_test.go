package conformance

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/diffcache"
	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/platform"
)

// fakeProvider is the in-tree fake used by the conformance self-tests.
// It composes iactest.NoopProvider so every IaCProvider method is
// satisfied by a zero-value default; later T7.x scenarios add overrides.
type fakeProvider struct {
	iactest.NoopProvider
}

func newFakeProvider() interfaces.IaCProvider { return &fakeProvider{} }

func defaultCfg() Config {
	return Config{Provider: newFakeProvider}
}

// TestRun_AllScenariosRunByDefault verifies the dispatcher invokes every
// scenario when no filters are set.
func TestRun_AllScenariosRunByDefault(t *testing.T) {
	var ran []string
	scenarios := []Scenario{
		{Name: "alpha", Run: func(t *testing.T, _ Config) { ran = append(ran, "alpha") }},
		{Name: "beta", Run: func(t *testing.T, _ Config) { ran = append(ran, "beta") }},
	}
	cfg := defaultCfg()
	cfg.LiveCloud = true
	runWithScenarios(t, cfg, scenarios)
	if len(ran) != 2 || ran[0] != "alpha" || ran[1] != "beta" {
		t.Errorf("expected [alpha beta], got %v", ran)
	}
}

// TestRun_SmokeOnlyFiltersNonSmoke verifies SmokeOnly=true skips scenarios
// where Smoke=false.
func TestRun_SmokeOnlyFiltersNonSmoke(t *testing.T) {
	var ran []string
	scenarios := []Scenario{
		{Name: "smoke-yes", Smoke: true, Run: func(t *testing.T, _ Config) { ran = append(ran, "smoke-yes") }},
		{Name: "smoke-no", Smoke: false, Run: func(t *testing.T, _ Config) { ran = append(ran, "smoke-no") }},
	}
	cfg := defaultCfg()
	cfg.SmokeOnly = true
	cfg.LiveCloud = true
	runWithScenarios(t, cfg, scenarios)
	if len(ran) != 1 || ran[0] != "smoke-yes" {
		t.Errorf("expected only smoke-yes to run, got %v", ran)
	}
}

// TestRun_LiveCloudFalseFiltersRequiresCloud verifies that
// scenarios marked RequiresCloud=true are skipped when LiveCloud=false.
func TestRun_LiveCloudFalseFiltersRequiresCloud(t *testing.T) {
	var ran []string
	scenarios := []Scenario{
		{Name: "local", RequiresCloud: false, Run: func(t *testing.T, _ Config) { ran = append(ran, "local") }},
		{Name: "cloud", RequiresCloud: true, Run: func(t *testing.T, _ Config) { ran = append(ran, "cloud") }},
	}
	cfg := defaultCfg() // LiveCloud false by default
	runWithScenarios(t, cfg, scenarios)
	if len(ran) != 1 || ran[0] != "local" {
		t.Errorf("expected only local to run when LiveCloud=false, got %v", ran)
	}
}

// TestRun_SkipScenariosByName verifies SkipScenarios skips the named scenario
// (its body must not execute) but lets the others run.
func TestRun_SkipScenariosByName(t *testing.T) {
	var ran []string
	scenarios := []Scenario{
		{Name: "ok", Run: func(t *testing.T, _ Config) { ran = append(ran, "ok") }},
		{Name: "skipme", Run: func(t *testing.T, _ Config) { ran = append(ran, "skipme") }},
		{Name: "also-ok", Run: func(t *testing.T, _ Config) { ran = append(ran, "also-ok") }},
	}
	cfg := defaultCfg()
	cfg.LiveCloud = true
	cfg.SkipScenarios = map[string]string{"skipme": "needs new wfctl flag"}
	runWithScenarios(t, cfg, scenarios)
	if len(ran) != 2 || ran[0] != "ok" || ran[1] != "also-ok" {
		t.Errorf("expected [ok also-ok] (skipme skipped), got %v", ran)
	}
}

// TestRun_SkipShownAsSkippedSubtest verifies the named-skip path produces
// a t.Skipf-flagged subtest rather than silently dropping the scenario.
// This regression-guards the plan-literal defect where `t.Skipf` on the
// outer `t` would Goexit the dispatcher entirely.
func TestRun_SkipShownAsSkippedSubtest(t *testing.T) {
	scenarios := []Scenario{
		{Name: "first-skipped", Run: func(t *testing.T, _ Config) { t.Fatal("body must not run when skipped") }},
		{Name: "second-runs", Run: func(t *testing.T, _ Config) { /* must run */ }},
	}
	cfg := defaultCfg()
	cfg.LiveCloud = true
	cfg.SkipScenarios = map[string]string{"first-skipped": "intentional"}
	// runWithScenarios issues two t.Run subtests; if the dispatcher Goexits on
	// the first skip the second never runs and the test framework reports zero
	// subtests for "second-runs". We verify by counting completed subtests via
	// a sentinel.
	var secondRan bool
	scenarios[1].Run = func(t *testing.T, _ Config) { secondRan = true }
	runWithScenarios(t, cfg, scenarios)
	if !secondRan {
		t.Fatal("second scenario must run after first is skipped (regression: outer t.Skipf would Goexit the dispatcher)")
	}
}

// TestValidateConfig_RequiresProvider asserts the precondition that
// Run guards via t.Fatal: an unset Config.Provider must produce
// errProviderRequired. Tested on validateConfig directly because
// asserting t.Fatal-via-Goexit from a sibling *testing.T is not
// cleanly supported (subtest failure propagates). The contract is:
// Run calls validateConfig and t.Fatals on any non-nil error, so
// asserting validateConfig's behavior is equivalent.
func TestValidateConfig_RequiresProvider(t *testing.T) {
	if err := validateConfig(Config{}); !errors.Is(err, errProviderRequired) {
		t.Errorf("expected errProviderRequired for Config{}, got %v", err)
	}
	if err := validateConfig(Config{Provider: newFakeProvider}); err != nil {
		t.Errorf("expected nil error for Config with Provider set, got %v", err)
	}
}

// TestScenario_NeedsReplaceTriggersReplaceAction is the in-tree self-test
// for T7.2: invokes the scenario body directly against a fake provider
// whose Driver.Diff returns NeedsReplace=true, asserting the platform
// classifies as Action="replace". Real provider plugins exercise the
// scenario via conformance.Run with LiveCloud=true; this self-test
// guards the platform-side translation regardless of provider.
func TestScenario_NeedsReplaceTriggersReplaceAction(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{
				Driver: &iactest.NoopDriver{
					DiffResult: &interfaces.DiffResult{NeedsReplace: true},
				},
			}
		},
	}
	scenarioNeedsReplaceTriggersReplaceAction(t, cfg)
}

// TestScenario_DeleteActionInApplyInvokesDriverDelete is the in-tree
// self-test for T7.3: invokes the scenario body directly against a fake
// provider whose Driver bumps DeleteCallCount on every Delete invocation,
// asserting the v2 ApplyPlan dispatch reaches driver.Delete for a delete
// action. This pins the latent-bug-fix from T3.3 — pre-W-3a, DOProvider's
// case-arm-less Apply silently skipped Delete on state-prune actions.
//
// The scenario body itself only asserts portable invariants
// (no per-action error in result.Errors, result.Resources unchanged). The
// driver-dispatch invariant is observed here in the self-test because it
// requires a counter the scenario body cannot portably introspect; for
// real provider plugins the equivalent observation is the cloud
// resource being gone (Read-after-delete returns 404), which the smoke
// gate in T7.13 covers.
func TestScenario_DeleteActionInApplyInvokesDriverDelete(t *testing.T) {
	// Share one driver instance across cfg.Provider() calls so the
	// post-check sees the count incremented by the scenario's
	// ApplyPlan dispatch. iactest.NoopProvider holds the driver by
	// pointer, so wrapping the same *NoopDriver in a fresh
	// NoopProvider per call still routes Diff/Delete/Read into the
	// shared counter.
	driver := &iactest.NoopDriver{}
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{Driver: driver}
		},
	}
	scenarioDeleteActionInApplyInvokesDriverDelete(t, cfg)
	if got := driver.DeleteCallCount.Load(); got != 1 {
		t.Errorf("driver.Delete should be invoked exactly once for a single delete action; got %d (pre-T3.3 dispatch silently skipped delete — this regression-pins the v2 fix)", got)
	}
}

// TestRun_ConsecutiveRunsObserveLiveDriverIndependently is the
// regression-pin for the W-7 follow-up that exported
// platform.SetDiffCacheForTest. Before the fix, conformance scenarios
// bypassed the diff cache via t.Setenv("WFCTL_DIFFCACHE", "disabled"),
// which only worked on a fresh process — sync.Once initialization
// sealed the cache backend on first call to platform.getDiffCache, so
// a CI run that exercised ComputePlan in another test first left a
// primed cache that masked live-driver regressions in subsequent
// conformance runs.
//
// This test seeds the package cache with an in-memory backend
// (simulating prior-test pollution that fired sync.Once with a real
// cache), then runs conformance.Run twice against fresh provider
// instances whose resource shapes hash to identical cache keys. With
// SetDiffCacheForTest installed per-Run by the scenario, each Run
// swaps in a fresh noop cache and the live driver dispatches both
// times. Without the helper, the second Run would hit the seeded
// cache and skip dispatch — driver2.DiffCallCount would stay at 0.
func TestRun_ConsecutiveRunsObserveLiveDriverIndependently(t *testing.T) {
	// Seed cache with an in-memory backend, simulating prior-test
	// pollution. SetDiffCacheForTest's t.Cleanup restores the prior
	// pointer (or a fresh default) when this test returns, so the seed
	// does not leak to sibling tests.
	platform.SetDiffCacheForTest(t, diffcache.NewMemory())

	// runOnce executes conformance.Run with the smoke + live-cloud
	// filter so Scenario_NeedsReplaceTriggersReplaceAction fires (it is
	// the only currently-registered scenario that exercises ComputePlan
	// against the live driver and is therefore the regression surface
	// for cache-pollution masking).
	runOnce := func(t *testing.T) *iactest.NoopDriver {
		t.Helper()
		driver := &iactest.NoopDriver{
			DiffResult: &interfaces.DiffResult{NeedsReplace: true},
		}
		cfg := Config{
			// SmokeOnly=true narrows the run to Smoke=true scenarios so
			// the cache-isolation regression-pin only exercises
			// Scenario_NeedsReplaceTriggersReplaceAction (the sole
			// ComputePlan-using Smoke scenario). Without this filter,
			// later Smoke=false scenarios with their own driver-shape
			// requirements (T7.3 Delete, T7.5 Refresh, …) would also
			// fire here and fail or panic against the bare driver.
			LiveCloud: true,
			SmokeOnly: true,
			Provider: func() interfaces.IaCProvider {
				return &iactest.NoopProvider{Driver: driver}
			},
		}
		Run(t, cfg)
		return driver
	}

	var driver1, driver2 *iactest.NoopDriver
	t.Run("first", func(t *testing.T) { driver1 = runOnce(t) })
	t.Run("second", func(t *testing.T) { driver2 = runOnce(t) })

	if got := driver1.DiffCallCount.Load(); got < 1 {
		t.Errorf("first Run: driver Diff calls = %d, want >= 1 (driver must be dispatched at least once)", got)
	}
	if got := driver2.DiffCallCount.Load(); got < 1 {
		t.Errorf("second Run: driver Diff calls = %d, want >= 1 (cache MUST be reset between Runs to observe live driver — regression in W-7 SetDiffCacheForTest)", got)
	}
}

// TestScenario_DiffSurvivesGRPCRoundTrip is the in-tree self-test for
// T7.4: invokes the scenario body against a fake whose Driver.Diff
// returns a DiffResult with mixed-type FieldChange.Old/New (string,
// number, bool) so the structpb encode/decode path the scenario
// exercises has non-trivial values to round-trip. Asserts driver
// dispatch happened (DiffCallCount >= 1) — proving the wrapper's
// structpb roundtrip on inputs did not short-circuit before reaching
// the delegate.
func TestScenario_DiffSurvivesGRPCRoundTrip(t *testing.T) {
	driver := &iactest.NoopDriver{
		DiffResult: &interfaces.DiffResult{
			NeedsUpdate:  true,
			NeedsReplace: false,
			Changes: []interfaces.FieldChange{
				{Path: "config.region", Old: "nyc1", New: "nyc3"},
				{Path: "config.size", Old: 1, New: 2, ForceNew: true},
				{Path: "config.protected", Old: true, New: false},
			},
		},
	}
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{Driver: driver}
		},
	}
	scenarioDiffSurvivesGRPCRoundTrip(t, cfg)
	if got := driver.DiffCallCount.Load(); got < 1 {
		t.Errorf("driver.Diff must be invoked through the structpb roundtrip wrapper; got DiffCallCount=%d", got)
	}
}

// TestScenario_OutputsRefreshDetectsNewFields is the in-tree self-test
// for T7.5: invokes the scenario body against a fake whose Driver.Read
// returns Outputs with one extra key ("endpoint") beyond what the
// persisted state held ("ip"). Asserts iac/refreshoutputs.Refresh
// reconciles the new key into the returned state. Closes W-3a root-
// cause issue B — state outputs lag after a plugin upgrade.
func TestScenario_OutputsRefreshDetectsNewFields(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{
				Driver: &iactest.NoopDriver{
					ReadResult: &interfaces.ResourceOutput{
						Name:       "vm",
						Type:       "infra.compute",
						ProviderID: "vm-id",
						Outputs: map[string]any{
							"ip":       "1.2.3.4",
							"endpoint": "https://api.example.com",
						},
					},
				},
			}
		},
	}
	scenarioOutputsRefreshDetectsNewFields(t, cfg)
}

// TestScenario_PlanStaleDiagnostic is the in-tree self-test for T7.6:
// invokes the scenario body directly. The scenario is platform-level
// (provider-agnostic) so a no-op fake provider satisfies Run's
// validateConfig precondition — the body never calls cfg.Provider().
func TestScenario_PlanStaleDiagnostic(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{}
		},
	}
	scenarioPlanStaleDiagnostic(t, cfg)
}

// validatingFakeProvider embeds iactest.NoopProvider and adds a
// configurable ValidatePlan implementation so the type satisfies
// interfaces.ProviderValidator. Used by the T7.7 self-test to drive
// the cross-resource constraint scenario against a deterministic
// diagnostic shape.
type validatingFakeProvider struct {
	*iactest.NoopProvider
	diags []interfaces.PlanDiagnostic
}

// Compile-time assertion that the wrapper satisfies both interfaces.
var (
	_ interfaces.IaCProvider       = (*validatingFakeProvider)(nil)
	_ interfaces.ProviderValidator = (*validatingFakeProvider)(nil)
)

// ValidatePlan returns the pre-configured diagnostics regardless of plan
// content; the scenario constructs a plan known to be invalid and the
// fake's job is only to surface the diag to verify the contract path.
func (p *validatingFakeProvider) ValidatePlan(_ *interfaces.IaCPlan) []interfaces.PlanDiagnostic {
	return p.diags
}

// TestScenario_CrossResourceConstraintRejection is the in-tree self-test
// for T7.7: invokes the scenario body against a fake that satisfies
// ProviderValidator and returns one Error-severity diagnostic naming
// the dangling vpc_ref. Asserts the contract surface (interface
// assertion succeeds + non-empty Error diag with non-empty Message).
func TestScenario_CrossResourceConstraintRejection(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &validatingFakeProvider{
				NoopProvider: &iactest.NoopProvider{},
				diags: []interfaces.PlanDiagnostic{
					{
						Severity: interfaces.PlanDiagnosticError,
						Resource: "db",
						Field:    "vpc_ref",
						Message:  "vpc 'missing-vpc' is not present in this plan",
					},
				},
			}
		},
	}
	scenarioCrossResourceConstraintRejection(t, cfg)
}

// TestScenario_CrossResourceConstraintRejection_SkipsWhenNotImplemented
// asserts the negative-path contract: providers that don't implement
// ProviderValidator (the optional interface) cause the scenario to
// skip rather than fail. Pre-W-4 providers and non-validating
// implementations stay green.
func TestScenario_CrossResourceConstraintRejection_SkipsWhenNotImplemented(t *testing.T) {
	cfg := Config{
		// NoopProvider does NOT implement ProviderValidator — the
		// scenario must t.Skipf rather than fail.
		Provider: func() interfaces.IaCProvider { return &iactest.NoopProvider{} },
	}
	// Run the scenario in a subtest and confirm it was skipped (Go's
	// testing.T propagates skip up to the parent only as a "did not
	// pass / did not fail" — we observe via t.Run's return-bool +
	// the inner t.Skipped() flag captured before propagation).
	var innerSkipped bool
	t.Run("inner", func(it *testing.T) {
		defer func() { innerSkipped = it.Skipped() }()
		scenarioCrossResourceConstraintRejection(it, cfg)
	})
	if !innerSkipped {
		t.Errorf("scenario must Skip when provider does not implement ProviderValidator")
	}
}

// TestScenario_InfraOutputCrossModuleResolution is the in-tree self-test
// for T7.8: invokes the scenario body directly. The scenario exercises
// platform-level jitsubst.ResolveSpec, so a no-op fake provider satisfies
// Run's validateConfig precondition — the body never calls cfg.Provider().
func TestScenario_InfraOutputCrossModuleResolution(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider {
			return &iactest.NoopProvider{}
		},
	}
	scenarioInfraOutputCrossModuleResolution(t, cfg)
}

// TestScenario_ProtectedReplaceWithoutOverride is the in-tree self-test
// for T7.9: invokes the scenario body. The body uses
// wfctlhelpers.ValidateAllowReplaceProtected directly, so the fake
// provider exists only to satisfy Run's validateConfig precondition.
func TestScenario_ProtectedReplaceWithoutOverride(t *testing.T) {
	cfg := Config{
		Provider: func() interfaces.IaCProvider { return &iactest.NoopProvider{} },
	}
	scenarioProtectedReplaceWithoutOverride(t, cfg)
}

// TestRegister_AppendsToAllScenarios verifies the registration hook used
// by each scenario_<name>.go init() in T7.2-T7.12. The test save/restores
// the package-level registry so it does not leak state to other tests.
func TestRegister_AppendsToAllScenarios(t *testing.T) {
	saved := registered
	t.Cleanup(func() { registered = saved })
	registered = nil

	register(Scenario{Name: "Scenario_TestOnly_A"})
	register(Scenario{Name: "Scenario_TestOnly_B", Smoke: true})

	got := allScenarios()
	if len(got) != 2 || got[0].Name != "Scenario_TestOnly_A" || got[1].Name != "Scenario_TestOnly_B" {
		t.Fatalf("expected register() to append in order, got %+v", got)
	}
	if !got[1].Smoke {
		t.Errorf("Smoke field not preserved by register()")
	}
}
