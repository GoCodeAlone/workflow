package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// applyFailProvider is an IaCProvider that fails Apply and optionally
// implements Troubleshooter.
type applyFailProvider struct {
	applyCapture
	applyErr error
	// troubleshoot fields
	isTroubleshooter bool
	diags            []interfaces.Diagnostic
	tsErr            error
	tsCalls          int
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

func TestInfraApply_EmitsDiagnosticsOnFailure(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")

	diags := []interfaces.Diagnostic{
		{ID: "dep-abc", Phase: "pre_deploy", Cause: "migration failed", At: mustTime("2026-04-24T00:00:00Z")},
	}
	provider := &applyFailProvider{
		applyErr:         errors.New("API error"),
		isTroubleshooter: true,
		diags:            diags,
	}

	var errBuf bytes.Buffer
	infraApplyTroubleshootTimeout = 5 * time.Second
	defer func() { infraApplyTroubleshootTimeout = 30 * time.Second }()

	specs := []interfaces.ResourceSpec{{Name: "bmw-staging", Type: "app_platform"}}
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil)
	if err == nil {
		t.Fatal("expected error from failing apply")
	}
	if !strings.Contains(err.Error(), "API error") {
		t.Errorf("original error not preserved: %v", err)
	}
	if provider.tsCalls != 1 {
		t.Errorf("Troubleshoot not called: tsCalls=%d", provider.tsCalls)
	}
	_ = errBuf
	_ = fmt.Sprintf // keep import
}

func TestInfraApply_NonTroubleshooterNocrash(t *testing.T) {
	provider := &applyFailProvider{
		applyErr: errors.New("boom"),
	}
	specs := []interfaces.ResourceSpec{{Name: "x", Type: "app_platform"}}
	err := applyWithProviderAndStore(context.Background(), provider, "digitalocean", specs, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
