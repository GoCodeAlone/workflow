package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// applyFailProvider is an IaCProvider that always fails Apply and implements
// Troubleshooter so we can verify diagnostics are surfaced on failure.
type applyFailProvider struct {
	applyCapture
	applyErr error
	diags    []interfaces.Diagnostic
	tsErr    error
	tsCalls  int
}

func (p *applyFailProvider) Apply(_ context.Context, plan *interfaces.IaCPlan) (*interfaces.ApplyResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.applyCalled = true
	p.appliedPlan = plan
	return nil, p.applyErr
}

func (p *applyFailProvider) Troubleshoot(_ context.Context, _ interfaces.ResourceRef, _ string) ([]interfaces.Diagnostic, error) {
	p.tsCalls++
	return p.diags, p.tsErr
}

// plainFailProvider is an IaCProvider that always fails Apply but does NOT
// implement Troubleshooter. Used to verify the non-troubleshooter path is a no-op.
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
	provider := &applyFailProvider{
		applyErr: errors.New("API error"),
		diags:    diags,
	}

	infraApplyTroubleshootTimeout = 5 * time.Second
	defer func() { infraApplyTroubleshootTimeout = 30 * time.Second }()

	var diagBuf bytes.Buffer
	specs := []interfaces.ResourceSpec{{Name: "bmw-staging", Type: "app_platform"}}
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil, &diagBuf)
	if err == nil {
		t.Fatal("expected error from failing apply")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("original error not preserved: %v", err)
	}
	if provider.tsCalls != 1 {
		t.Errorf("Troubleshoot not called: tsCalls=%d", provider.tsCalls)
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
	// plainFailProvider does not implement Troubleshooter — should be a silent no-op.
	provider := &plainFailProvider{
		applyErr: errors.New("boom"),
	}
	var diagBuf bytes.Buffer
	specs := []interfaces.ResourceSpec{{Name: "x", Type: "app_platform"}}
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil, &diagBuf)
	if err == nil {
		t.Fatal("expected error")
	}
	if diagBuf.Len() != 0 {
		t.Errorf("non-troubleshooter should produce no diagnostic output, got: %q", diagBuf.String())
	}
}
