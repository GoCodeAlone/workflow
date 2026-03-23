package bdd

import (
	"testing"

	"github.com/cucumber/godog"
)

// RunFeatures runs all Gherkin feature files at path using the pre-built step definitions.
// Each scenario gets a fresh ScenarioContext and wftest.Harness so scenarios are fully isolated.
func RunFeatures(t *testing.T, path string, opts ...Option) {
	t.Helper()
	cfg := &Config{}
	for _, o := range opts {
		o(cfg)
	}

	suite := godog.TestSuite{
		ScenarioInitializer: func(ctx *godog.ScenarioContext) {
			sc := newScenarioContext(t, cfg)
			registerSteps(ctx, sc)
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{path},
			TestingT: t,
			Strict:   cfg.strict,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

// registerSteps registers all pre-built step definitions into a godog scenario context.
func registerSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	registerEngineSteps(ctx, sc)
	registerMockSteps(ctx, sc)
	registerHTTPSteps(ctx, sc)
	registerTriggerSteps(ctx, sc)
	registerAssertSteps(ctx, sc)
	registerStateSteps(ctx, sc)
}
