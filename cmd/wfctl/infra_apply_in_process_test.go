package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// fingerprintForTest is provided by infra_apply_plan_test.go in this
// package — it delegates to inputsnapshot.Compute so the production
// fingerprint algorithm is always used. Reused here to keep the
// in-process test consistent with the persisted-plan test.

// inProcessFakeProvider satisfies interfaces.IaCProvider with no-op
// methods — TestApply_InProcess_PlanStaleDiagnostic_NamesChangedKeys
// exercises wfctlhelpers.ApplyPlan's drift postcondition independently
// of any per-action driver behavior.
type inProcessFakeProvider struct{}

func (inProcessFakeProvider) Name() string                                         { return "in-process-fake" }
func (inProcessFakeProvider) Version() string                                      { return "0.0.0" }
func (inProcessFakeProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (inProcessFakeProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return nil }
func (inProcessFakeProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return &interfaces.IaCPlan{}, nil
}
func (inProcessFakeProvider) Apply(_ context.Context, _ *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	return &interfaces.ApplyResult{}, nil
}
func (inProcessFakeProvider) Destroy(_ context.Context, _ []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	return nil, nil
}
func (inProcessFakeProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}
func (inProcessFakeProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return nil, nil
}
func (inProcessFakeProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (inProcessFakeProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (inProcessFakeProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (inProcessFakeProvider) SupportedCanonicalKeys() []string { return nil }
func (inProcessFakeProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (inProcessFakeProvider) Close() error { return nil }

// TestApply_InProcess_PlanStaleDiagnostic_NamesChangedKeys validates the
// full T3.1.5 chain: plan-time env value → wfctlhelpers.ApplyPlan
// captures InitialInputSnapshot at entry, then the deferred postcondition
// computes drift against the apply-time snapshot, populating
// result.InputDriftReport. cmd/wfctl/printDriftReportIfAny then formats
// the report onto stderr in the canonical FormatStaleError shape.
//
// This is the in-process counterpart to the persisted-`--plan` path
// covered by cmd/wfctl/infra.go (W-1/T1.5). Once W-3b/T3.7 wires
// wfctlhelpers.ApplyPlan into the in-process apply call site, this test
// exercises exactly the path operators run during `wfctl infra apply`
// against a v2-conformant IaCProvider plugin.
func TestApply_InProcess_PlanStaleDiagnostic_NamesChangedKeys(t *testing.T) {
	const varName = "WFCTL_T315_INPROC_PASSWORD"
	planFP := fingerprintForTest("old-value")
	t.Setenv(varName, "new-value") // post-plan env value differs from plan-time fingerprint
	plan := &interfaces.IaCPlan{
		InputSnapshot: map[string]string{varName: planFP},
	}
	result, err := wfctlhelpers.ApplyPlan(context.Background(), inProcessFakeProvider{}, plan)
	if err != nil {
		t.Fatalf("ApplyPlan returned top-level error: %v", err)
	}
	// Postcondition assertion: exactly one entry naming the changed key.
	if len(result.InputDriftReport) != 1 {
		t.Fatalf("expected 1 drift entry; got %d (%+v)",
			len(result.InputDriftReport), result.InputDriftReport)
	}
	if got := result.InputDriftReport[0].Name; got != varName {
		t.Errorf("expected drift on %q; got %q", varName, got)
	}

	// cmd/wfctl wiring assertion: printDriftReportIfAny formats the
	// report onto the supplied writer in the canonical shape.
	var buf bytes.Buffer
	printDriftReportIfAny(&buf, result)
	out := buf.String()
	if !strings.Contains(out, "plan stale:") {
		t.Errorf("expected canonical stale-error header in output; got %q", out)
	}
	if !strings.Contains(out, varName) {
		t.Errorf("expected drift output to name %q; got %q", varName, out)
	}
	if !strings.Contains(out, "hint: ensure all env vars referenced") {
		t.Errorf("expected canonical hint line in output; got %q", out)
	}
}

// TestPrintDriftReportIfAny_NoOpOnEmpty locks the no-op contract so
// callers don't need to guard the call: nil result, nil report, and
// empty-but-non-nil report all produce zero output.
func TestPrintDriftReportIfAny_NoOpOnEmpty(t *testing.T) {
	cases := []struct {
		name   string
		result *interfaces.ApplyResult
	}{
		{"nil-result", nil},
		{"nil-report", &interfaces.ApplyResult{}},
		{"empty-report", &interfaces.ApplyResult{InputDriftReport: []interfaces.DriftEntry{}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			printDriftReportIfAny(&buf, c.result)
			if buf.Len() != 0 {
				t.Errorf("expected no output for %s; got %q", c.name, buf.String())
			}
		})
	}
}
