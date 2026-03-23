package bdd

import (
	"errors"
	"testing"

	"github.com/GoCodeAlone/workflow/wftest"
)

var (
	errHarnessNotInitialized = errors.New("workflow engine not loaded; use 'Given the workflow engine is loaded with config:' first")
	errNoResult              = errors.New("no result available; trigger a pipeline or HTTP request first")
)

// pendingStateSeed holds a state store name and the data to seed into it.
type pendingStateSeed struct {
	store string
	data  map[string]any
}

// ScenarioContext holds state for a single Gherkin scenario.
// A fresh ScenarioContext is created for each scenario.
// Harness creation is deferred until the first "When" action step so that
// all "Given" step options (YAML config, mocks, state seeds) are accumulated
// first and applied together when the harness is built.
type ScenarioContext struct {
	t                 *testing.T
	harness           *wftest.Harness
	result            *wftest.Result
	cfg               *Config
	pendingOpts       []wftest.Option    // accumulated from Given steps
	pendingStateSeeds []pendingStateSeed // applied after harness creation
	hasState          bool               // true once WithState() is queued
}

func newScenarioContext(t *testing.T, cfg *Config) *ScenarioContext {
	t.Helper()
	return &ScenarioContext{t: t, cfg: cfg}
}

// ensureHarness creates the harness if not yet created, applying all
// accumulated pending options (config, mocks, state).
func (sc *ScenarioContext) ensureHarness() error {
	if sc.harness != nil {
		return nil
	}
	if len(sc.pendingOpts) == 0 && len(sc.cfg.globalOpts) == 0 {
		return errHarnessNotInitialized
	}
	opts := make([]wftest.Option, 0, len(sc.cfg.globalOpts)+len(sc.pendingOpts))
	opts = append(opts, sc.cfg.globalOpts...)
	opts = append(opts, sc.pendingOpts...)
	sc.harness = wftest.New(sc.t, opts...)
	sc.pendingOpts = nil
	// Apply any pending state seeds.
	if sc.harness.State() != nil {
		for _, seed := range sc.pendingStateSeeds {
			sc.harness.State().Seed(seed.store, seed.data)
		}
		sc.pendingStateSeeds = nil
	}
	return nil
}

// ensureResult returns an error if no result is available yet.
func (sc *ScenarioContext) ensureResult() error {
	if sc.result == nil {
		return errNoResult
	}
	return nil
}
