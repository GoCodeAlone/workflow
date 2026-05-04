package wfctlhelpers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// fingerprint returns the canonical 16-hex-char sha256 prefix used by
// inputsnapshot.Compute. Local helper so tests can build plan
// InputSnapshot fixtures with realistic-looking values without coupling
// to the inputsnapshot package's unexported constants.
func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}

// TestApplyPlan_InitialInputSnapshot_CapturedAtEntry verifies that
// ApplyPlan populates ApplyResult.InitialInputSnapshot at apply entry by
// fingerprinting every name listed in plan.InputSnapshot through the
// real OS env. T3.1.5 lands this capture; T3.1's skeleton did not.
func TestApplyPlan_InitialInputSnapshot_CapturedAtEntry(t *testing.T) {
	t.Setenv("WFCTL_T315_FOO", "value-foo")
	t.Setenv("WFCTL_T315_BAR", "value-bar")
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{
			"WFCTL_T315_FOO": fingerprint("value-foo"),
			"WFCTL_T315_BAR": fingerprint("value-bar"),
		},
	}
	fp := newFakeProvider()
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.InitialInputSnapshot["WFCTL_T315_FOO"], fingerprint("value-foo"); got != want {
		t.Errorf("InitialInputSnapshot[WFCTL_T315_FOO]: got %q want %q", got, want)
	}
	if got, want := result.InitialInputSnapshot["WFCTL_T315_BAR"], fingerprint("value-bar"); got != want {
		t.Errorf("InitialInputSnapshot[WFCTL_T315_BAR]: got %q want %q", got, want)
	}
}

// TestApply_Postcondition_PanicDoesNotCorruptResult verifies that a
// panic inside the deferred postcondition (e.g., a buggy env-provider
// closure) does not crash apply or surface as the top-level error.
// Recovers and clears result.InputDriftReport so an operator sees an
// empty drift report rather than a partially-populated one. Uses the
// internal seam applyPlanWithEnvProvider to inject the panicky provider.
func TestApply_Postcondition_PanicDoesNotCorruptResult(t *testing.T) {
	panickyEnv := func(_ string) (string, bool) { panic("env-provider closure freed") }
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{"FOO": fingerprint("v")},
	}
	fp := newFakeProvider()
	result, err := applyPlanWithEnvProvider(context.Background(), fp, plan, panickyEnv)
	if err != nil {
		t.Fatalf("apply should not surface postcondition panic: %v", err)
	}
	if result.InputDriftReport != nil {
		t.Errorf("on postcondition panic, drift report should be nil; got %+v", result.InputDriftReport)
	}
}

// TestApply_Postcondition_FingerprintAfterEnvUnset_NoFalsePositive
// covers the cycle-3 sub-action cleanup case: an apply action unsets a
// credential env var (e.g., the post-apply security hardening pattern).
// The postcondition must NOT flag this as drift — the operator's mental
// model says the value at plan time is what matters; an unset post-apply
// is not "the env changed mid-flight" in the user-facing sense. The
// production NewTolerantEnvProvider preserves fingerprints for
// plan-time-set apply-time-unset vars via the in-package sentinel; this
// test exercises that path end-to-end through ApplyPlan.
func TestApply_Postcondition_FingerprintAfterEnvUnset_NoFalsePositive(t *testing.T) {
	const varName = "WFCTL_T315_PG_PASSWORD"
	t.Setenv(varName, "value")
	planFP := fingerprint("value")
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{varName: planFP},
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.vpc")},
		},
	}
	// envUnsetingFakeProvider unsets the env var inside Create, simulating
	// a sub-action cleanup that happens between snapshot capture and
	// postcondition execution.
	fp := &envUnsetingFakeProvider{
		fakeProvider: newFakeProvider(),
		varToUnset:   varName,
	}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InputDriftReport) != 0 {
		t.Errorf("post-apply env-unset must not trigger drift false-positive; got: %+v", result.InputDriftReport)
	}
	// Sanity: the sub-action did unset the var.
	if _, set := os.LookupEnv(varName); set {
		t.Errorf("test setup: sub-action did not unset %s", varName)
	}
}

// envUnsetingFakeProvider returns a fake driver whose Create method
// unsets a configured env var as a side-effect, simulating the
// sub-action credential-cleanup pattern that the tolerant env provider
// must accommodate without false-positive drift.
type envUnsetingFakeProvider struct {
	*fakeProvider
	varToUnset string
}

func (p *envUnsetingFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &envUnsetingFakeDriver{fakeDriver: p.driver, varToUnset: p.varToUnset}, nil
}

type envUnsetingFakeDriver struct {
	*fakeDriver
	varToUnset string
}

func (d *envUnsetingFakeDriver) Create(ctx context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	out, err := d.fakeDriver.Create(ctx, spec)
	if err != nil {
		return out, err
	}
	_ = os.Unsetenv(d.varToUnset)
	return out, nil
}

// TestApplyPlan_PlanStaleDiagnostic_NamesChangedKeys is the canonical
// drift-detection test: an env var captured at plan time has a
// different value at apply time. The postcondition must record exactly
// one drift entry naming the changed key, with both fingerprints
// distinct.
func TestApplyPlan_PlanStaleDiagnostic_NamesChangedKeys(t *testing.T) {
	const varName = "WFCTL_T315_STAGING_PG_PASSWORD"
	planFP := fingerprint("old-value")
	t.Setenv(varName, "new-value") // post-plan env value differs from plan-time fingerprint
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{varName: planFP},
	}
	fp := newFakeProvider()
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InputDriftReport) != 1 {
		t.Fatalf("expected 1 drift entry, got %d (%+v)", len(result.InputDriftReport), result.InputDriftReport)
	}
	got := result.InputDriftReport[0]
	if got.Name != varName {
		t.Errorf("Name: got %q want %q", got.Name, varName)
	}
	if got.PlanFingerprint != planFP {
		t.Errorf("PlanFingerprint: got %q want %q", got.PlanFingerprint, planFP)
	}
	if got.ApplyFingerprint == planFP || got.ApplyFingerprint == "" {
		t.Errorf("ApplyFingerprint should be a distinct, non-empty value; got %q", got.ApplyFingerprint)
	}
}

// TestApplyPlan_NoDriftWhenInputSnapshotEmpty verifies the no-op case:
// a plan with no InputSnapshot must produce no drift report. Avoids
// the postcondition incorrectly synthesizing entries for vars that
// were never tracked.
func TestApplyPlan_NoDriftWhenInputSnapshotEmpty(t *testing.T) {
	plan := &interfaces.IaCPlan{}
	fp := newFakeProvider()
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.InputDriftReport) != 0 {
		t.Errorf("empty InputSnapshot must yield empty drift; got %+v", result.InputDriftReport)
	}
}

// errFromDispatch is a sentinel for the "apply errored AND drift
// detected" interleave test. Confirms the deferred postcondition runs
// regardless of dispatch outcome.
var errFromDispatch = errors.New("dispatch failure (test)")

// TestApplyPlan_PostconditionRunsEvenIfDispatchErrored verifies the
// "regardless of apply success/error path" contract on the
// InputDriftReport godoc. A driver that returns an error must still
// allow drift detection to populate the report — the postcondition is
// strictly best-effort but unconditional.
func TestApplyPlan_PostconditionRunsEvenIfDispatchErrored(t *testing.T) {
	const varName = "WFCTL_T315_DRIFT_DURING_FAIL"
	planFP := fingerprint("plan-time")
	t.Setenv(varName, "apply-time")
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{varName: planFP},
		Actions: []interfaces.PlanAction{
			{Action: "create", Resource: spec("a", "infra.vpc")},
		},
	}
	fp := &erroringFakeProvider{fakeProvider: newFakeProvider()}
	result, err := ApplyPlan(context.Background(), fp, plan)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected per-action error from erroring driver; got %d (%v)", len(result.Errors), result.Errors)
	}
	if len(result.InputDriftReport) != 1 {
		t.Errorf("postcondition must populate drift report even when dispatch errors; got %+v", result.InputDriftReport)
	}
}

// erroringFakeProvider returns a driver whose Create returns
// errFromDispatch so apply produces a per-action error.
type erroringFakeProvider struct {
	*fakeProvider
}

func (p *erroringFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &erroringFakeDriver{fakeDriver: p.driver}, nil
}

type erroringFakeDriver struct {
	*fakeDriver
}

func (d *erroringFakeDriver) Create(_ context.Context, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	d.fakeDriver.createCount++
	return nil, errFromDispatch
}
