package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// troubleshootingRD satisfies interfaces.ResourceDriver via embedding and adds
// a Troubleshoot method. Used as the return value of ResourceDriver() so the
// infra_apply Bug 2 path is actually exercised.
type troubleshootingRD struct {
	interfaces.ResourceDriver // embedded — non-overridden methods panic (not called in tests)
	diags                     []interfaces.Diagnostic
	tsErr                     error
	tsCalls                   *int
}

func (d *troubleshootingRD) Troubleshoot(_ context.Context, _ interfaces.ResourceRef, _ string) ([]interfaces.Diagnostic, error) {
	*d.tsCalls++
	return d.diags, d.tsErr
}

// applyFailProvider is an IaCProvider that always fails Apply and returns a
// Troubleshooter-capable ResourceDriver via ResourceDriver().
type applyFailProvider struct {
	applyCapture
	applyErr error
	tsDriver *troubleshootingRD // nil → ResourceDriver returns (nil, nil)
}

func (p *applyFailProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.applyCalled = true
	p.appliedPlan = plan
	return nil, p.applyErr
}

func (p *applyFailProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	if p.tsDriver != nil {
		return p.tsDriver, nil
	}
	return nil, nil
}

// plainFailProvider fails Apply and returns a ResourceDriver with no Troubleshoot.
type plainFailProvider struct {
	applyCapture
	applyErr error
}

func (p *plainFailProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.applyCalled = true
	p.appliedPlan = plan
	return nil, p.applyErr
}

func TestInfraApply_EmitsDiagnosticsOnFailure(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	diags := []interfaces.Diagnostic{
		{ID: "dep-abc", Phase: "pre_deploy", Cause: "migration failed", At: mustTime("2026-04-24T00:00:00Z")},
	}
	tsCalls := 0
	provider := &applyFailProvider{
		applyErr: errors.New("API error"),
		tsDriver: &troubleshootingRD{diags: diags, tsCalls: &tsCalls},
	}

	infraApplyTroubleshootTimeout = 5 * time.Second
	defer func() { infraApplyTroubleshootTimeout = 30 * time.Second }()

	// spec.Type must be non-empty so ref.Type is set and ResourceDriver is called.
	specs := []interfaces.ResourceSpec{{Name: "bmw-staging", Type: "app_platform"}}
	var diagBuf bytes.Buffer
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil, &diagBuf, "")
	if err == nil {
		t.Fatal("expected error from failing apply")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("original error not preserved: %v", err)
	}
	if tsCalls != 1 {
		t.Errorf("Troubleshoot not called via ResourceDriver: tsCalls=%d", tsCalls)
	}
	out := diagBuf.String()
	if !strings.Contains(out, "::group::") {
		t.Errorf("expected GHA group marker in diagnostic output, got: %q", out)
	}
	if !strings.Contains(out, "migration failed") {
		t.Errorf("expected diagnostic cause in output, got: %q", out)
	}
}

func TestInfraApply_NonTroubleshooterNocrash(t *testing.T) {
	// plainFailProvider.ResourceDriver returns (nil, nil); nil driver → no-op.
	provider := &plainFailProvider{applyErr: errors.New("boom")}
	var diagBuf bytes.Buffer
	specs := []interfaces.ResourceSpec{{Name: "x", Type: "app_platform"}}
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil, &diagBuf, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if diagBuf.Len() != 0 {
		t.Errorf("non-troubleshooter should produce no diagnostic output, got: %q", diagBuf.String())
	}
}

// TestInfraApply_WritesStepSummaryOnFailure verifies that applyWithProviderAndStore
// writes a GHA step summary even when the provider has no Troubleshooter.
// TDD invariant: removing the WriteStepSummary call causes this test to fail.
func TestInfraApply_WritesStepSummaryOnFailure(t *testing.T) {
	tmp := t.TempDir()
	summaryPath := filepath.Join(tmp, "summary.md")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", summaryPath)

	diags := []interfaces.Diagnostic{
		{ID: "dep-abc", Phase: "pre_deploy", Cause: "migration failed", At: mustTime("2026-04-24T00:00:00Z")},
	}
	tsCalls := 0
	provider := &applyFailProvider{
		applyErr: errors.New("API error"),
		tsDriver: &troubleshootingRD{diags: diags, tsCalls: &tsCalls},
	}

	infraApplyTroubleshootTimeout = 5 * time.Second
	defer func() { infraApplyTroubleshootTimeout = 30 * time.Second }()

	specs := []interfaces.ResourceSpec{{Name: "bmw-staging", Type: "app_platform"}}
	var diagBuf bytes.Buffer
	_ = applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil, &diagBuf, "staging")

	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("step summary file not written: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "## wfctl: apply staging — FAILED") {
		t.Errorf("summary missing failure header: %q", got)
	}
	if !strings.Contains(got, "bmw-staging") {
		t.Errorf("summary missing resource name: %q", got)
	}
	if !strings.Contains(got, "migration failed") {
		t.Errorf("summary missing diagnostic cause: %q", got)
	}
}
