// Package conformance is the public conformance test suite for IaC
// providers. Each provider plugin (DO, AWS, GCP, Azure, …) imports this
// package from its own *_test.go and calls conformance.Run(t, Config) to
// exercise the spec-mandated scenarios that every provider MUST satisfy.
//
// The package is import-only and pure-test: it does not provision real
// resources unless the caller passes Config{LiveCloud: true} and the
// scenario itself is RequiresCloud=true. This split lets non-cloud
// scenarios run on every PR while cloud-touching ones gate on a smoke
// workflow.
//
// Scaffold per W-7 T7.1; individual scenarios are added in T7.2-T7.12.
package conformance

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// errProviderRequired is the error validateConfig returns when
// Config.Provider is nil. Exposed as a package-private sentinel so the
// sibling test can assert via errors.Is rather than string-matching.
var errProviderRequired = errors.New("conformance.Config.Provider is required")

// Scenario is a single conformance test case. Each scenario lives in its
// own file (scenario_<name>.go) and registers itself via register() in an
// init() block.
type Scenario struct {
	// Name is the t.Run subtest name and the SkipScenarios map key.
	// Convention: "Scenario_<CamelCaseDescription>".
	Name string

	// Smoke marks scenarios that the per-PR smoke gate runs against an
	// active provider's real cloud. Non-smoke scenarios run only when a
	// caller opts in (e.g. nightly full-suite).
	Smoke bool

	// RequiresCloud marks scenarios that touch real cloud APIs. The
	// dispatcher skips these unless Config.LiveCloud is true.
	RequiresCloud bool

	// Run is the scenario body. It receives the live *testing.T (a
	// subtest) and the caller's Config so it can obtain a fresh provider
	// via cfg.Provider().
	Run func(t *testing.T, cfg Config)
}

// Config drives a conformance Run.
type Config struct {
	// Provider returns a fresh interfaces.IaCProvider for each scenario.
	// Required.
	Provider func() interfaces.IaCProvider

	// SkipScenarios maps scenario name → reason. The dispatcher emits a
	// t.Skipf-flagged subtest for each entry instead of executing the
	// body, preserving CI visibility into intentional skips.
	SkipScenarios map[string]string

	// SmokeOnly limits the run to scenarios with Smoke=true. Used by the
	// per-PR smoke gate.
	SmokeOnly bool

	// LiveCloud opts in to scenarios that provision real cloud resources
	// (RequiresCloud=true). Default false keeps test-suites hermetic.
	LiveCloud bool
}

// registered is the package-level scenario list populated by register()
// at init() time from each scenario_<name>.go.
var registered []Scenario

// register adds a Scenario to the package-level list. Called from each
// scenario file's init().
func register(s Scenario) {
	registered = append(registered, s)
}

// allScenarios returns the registered scenario list. Exported as a
// package-private getter so tests can inject custom lists via
// runWithScenarios without mutating package state.
func allScenarios() []Scenario {
	return registered
}

// validateConfig returns a non-nil error when cfg is missing required
// fields. Run calls this and t.Fatals on error; the sibling test
// asserts this function directly. Extracted because asserting on
// t.Fatal-via-Goexit from a sibling *testing.T is not cleanly
// supported by the testing framework (subtest failure propagates).
func validateConfig(cfg Config) error {
	if cfg.Provider == nil {
		return errProviderRequired
	}
	return nil
}

// Run is the public entry point provider plugins call from a *_test.go.
// It iterates the registered scenarios, applies the SmokeOnly /
// LiveCloud / SkipScenarios filters, and invokes each via t.Run so
// individual scenarios show up as discrete subtests in CI output.
//
// Plan-spec deviation note: §T7.1 sketched the skip path as
// `t.Skipf(...); continue` on the outer t. That snippet would Goexit
// the dispatcher entirely on the first skip-match. The intended
// behavior — per-scenario skipped subtests — requires wrapping the
// Skipf in t.Run, which is what we do.
//
// Diff-cache caveat: scenarios that exercise platform.ComputePlan
// with cacheable resources (non-empty ProviderID) use t.Setenv to
// force WFCTL_DIFFCACHE=disabled so a stale entry from a prior run
// cannot make the scenario pass against a regressed live driver.
// The platform diff cache is sync.Once-initialized per process, so
// the env-var override only takes effect when conformance.Run is
// invoked BEFORE any other platform.ComputePlan call in the test
// process. Real provider plugins that mix conformance with other
// platform.ComputePlan-based tests should arrange for conformance
// to run first, OR follow the W-7 follow-up that exposes a public
// platform.SetDiffCacheForTest helper.
func Run(t *testing.T, cfg Config) {
	t.Helper()
	if err := validateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	runWithScenarios(t, cfg, allScenarios())
}

// runWithScenarios is the core dispatch loop. Run() is a thin wrapper
// over allScenarios(); tests inject their own list to exercise the
// filter logic without touching the package-level registry.
func runWithScenarios(t *testing.T, cfg Config, scenarios []Scenario) {
	t.Helper()
	for _, s := range scenarios {
		if cfg.SmokeOnly && !s.Smoke {
			continue
		}
		if !cfg.LiveCloud && s.RequiresCloud {
			continue
		}
		t.Run(s.Name, func(t *testing.T) {
			if reason, ok := cfg.SkipScenarios[s.Name]; ok {
				t.Skipf("skipped: %s", reason)
			}
			s.Run(t, cfg)
		})
	}
}
