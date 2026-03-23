package bdd

import (
	"context"
	"errors"

	"github.com/cucumber/godog"
)

// registerStrictHooks adds an AfterStep hook that emits t.Log warnings when a
// step is undefined or pending in lenient mode (the default). In strict mode,
// godog's own Strict flag already causes the suite to fail; no extra hook is needed.
func registerStrictHooks(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	if sc.cfg.strict {
		// Strict mode: godog handles failure natively via Options.Strict = true.
		return
	}
	ctx.StepContext().After(func(_ context.Context, st *godog.Step, _ godog.StepResultStatus, err error) (context.Context, error) {
		switch {
		case errors.Is(err, godog.ErrUndefined):
			sc.t.Logf("WARN: undefined step: %s", st.Text)
		case errors.Is(err, godog.ErrPending):
			sc.t.Logf("WARN: pending step: %s", st.Text)
		}
		return context.Background(), nil
	})
}
