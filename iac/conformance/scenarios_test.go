package conformance

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/iactest"
	"github.com/GoCodeAlone/workflow/interfaces"
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
