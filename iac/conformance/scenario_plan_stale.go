package conformance

import (
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/inputsnapshot"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// scenarioPlanStaleDiagnostic asserts the apply-time stale-plan
// diagnostic names the changed input key rather than emitting a bare
// "error: plan stale". Closes W-3a root-cause issue A — operators
// previously had no idea WHICH env var or module field invalidated the
// persisted plan, forcing a full inputsnapshot diff to debug.
//
// The scenario is platform-level (provider-agnostic): every IaC
// provider that consumes a persisted plan via the wfctl/inputsnapshot
// path must surface this diagnostic shape. Assertions:
//
//  1. err.Error() contains the drifted key's Name (verifying
//     FormatStaleError's per-key line).
//  2. err.Error() contains both the plan-time and apply-time
//     fingerprints (verifying the operator can correlate to the value
//     change without separate tooling).
//  3. errors.Is(err, inputsnapshot.ErrEnvVarChanged) — the typed
//     sentinel detection contract is preserved for programmatic
//     callers (T3.1.5 wired this through *StaleError.Unwrap).
//
// Smoke=false, RequiresCloud=false per design table row 5 — runs in
// every test process regardless of cloud credentials.
//
// cfg.Provider is required by Run's validateConfig precondition but
// is intentionally NOT invoked here; the scenario exercises pure
// platform diagnostics.
func scenarioPlanStaleDiagnostic(t *testing.T, _ Config) {
	t.Helper()

	drift := []interfaces.DriftEntry{
		{
			Name:             "DATABASE_URL",
			PlanFingerprint:  "abc12345",
			ApplyFingerprint: "def67890",
		},
	}
	err := inputsnapshot.NewStaleError(drift)
	if err == nil {
		t.Fatal("NewStaleError returned nil for a non-empty drift report")
	}

	msg := err.Error()
	if !strings.Contains(msg, "DATABASE_URL") {
		t.Errorf("plan-stale diagnostic must name the changed key %q; got %q",
			"DATABASE_URL", msg)
	}
	if !strings.Contains(msg, "abc12345") || !strings.Contains(msg, "def67890") {
		t.Errorf("diagnostic must surface both fingerprints (plan→apply); got %q", msg)
	}
	if !errors.Is(err, inputsnapshot.ErrEnvVarChanged) {
		t.Errorf("errors.Is(err, ErrEnvVarChanged) = false; programmatic detection broken")
	}
}

func init() {
	register(Scenario{
		Name:          "Scenario_PlanStaleDiagnostic",
		Smoke:         false,
		RequiresCloud: false,
		Run:           scenarioPlanStaleDiagnostic,
	})
}
