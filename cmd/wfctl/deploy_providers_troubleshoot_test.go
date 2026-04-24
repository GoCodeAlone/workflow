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

type fakeTroubleshootingDriver struct {
	interfaces.ResourceDriver // embed for forward-compatibility; nil is fine for tests
	diags                     []interfaces.Diagnostic
	err                       error
	calls                     int
}

func (f *fakeTroubleshootingDriver) Troubleshoot(_ context.Context, _ interfaces.ResourceRef, _ string) ([]interfaces.Diagnostic, error) {
	f.calls++
	return f.diags, f.err
}

func TestEmitDiagnostics_WritesGroupBlock(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	var buf bytes.Buffer
	diags := []interfaces.Diagnostic{
		{ID: "dep-1", Phase: "pre_deploy", Cause: "exit 1",
			At:     mustTime("2026-04-24T00:00:00Z"),
			Detail: "migration failed"},
	}
	emitDiagnostics(&buf, "bmw-staging", diags, detectCIProvider())
	out := buf.String()
	if !strings.Contains(out, "::group::Troubleshoot: bmw-staging") {
		t.Errorf("missing group marker: %q", out)
	}
	if !strings.Contains(out, "[pre_deploy]") || !strings.Contains(out, "exit 1") {
		t.Errorf("missing diagnostic body: %q", out)
	}
	if !strings.Contains(out, "::endgroup::") {
		t.Errorf("missing endgroup: %q", out)
	}
}

func TestEmitDiagnostics_EmptyIsNoop(t *testing.T) {
	var buf bytes.Buffer
	emitDiagnostics(&buf, "x", nil, plainEmitter{})
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty diags, got %q", buf.String())
	}
}

func TestTroubleshootAfterFailure_Timeout(t *testing.T) {
	f := &fakeTroubleshootingDriver{
		diags: []interfaces.Diagnostic{{ID: "d", Phase: "run", Cause: "ouch", At: mustTime("2026-04-24T00:00:00Z")}},
	}
	var buf bytes.Buffer
	origErr := errors.New("plugin health check \"bmw-staging\": timed out waiting for healthy")
	troubleshootAfterFailure(context.Background(), &buf, f, interfaces.ResourceRef{Name: "bmw-staging"}, origErr, 30*time.Second, plainEmitter{})
	if f.calls != 1 {
		t.Errorf("Troubleshoot not called: calls=%d", f.calls)
	}
	if !strings.Contains(buf.String(), "ouch") {
		t.Errorf("missing Cause in output: %q", buf.String())
	}
}

func TestTroubleshootAfterFailure_NonTroubleshooterSkips(t *testing.T) {
	var buf bytes.Buffer
	type plainDriver struct{ interfaces.ResourceDriver }
	troubleshootAfterFailure(context.Background(), &buf, &plainDriver{}, interfaces.ResourceRef{}, errors.New("x"), 30*time.Second, plainEmitter{})
	if buf.Len() != 0 {
		t.Errorf("non-troubleshooter should not produce output: %q", buf.String())
	}
}
