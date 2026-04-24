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

func TestE2EDeployFailure_EmitsFullTroubleshootBlock(t *testing.T) {
	t.Setenv("GITHUB_ACTIONS", "true")
	summary := t.TempDir() + "/summary.md"
	t.Setenv("GITHUB_STEP_SUMMARY", summary)

	diags := []interfaces.Diagnostic{
		{ID: "dep-1", Phase: "pre_deploy", Cause: "migration failed",
			At:     mustTime("2026-04-24T17:42:45Z"),
			Detail: "exit status 1"},
		{ID: "dep-1", Phase: "build", Cause: "",
			At: mustTime("2026-04-24T17:40:00Z"), Detail: ""},
	}
	f := &fakeTroubleshootingDriver{diags: diags}
	var stderr bytes.Buffer
	em := detectCIProvider()
	troubleshootAfterFailure(context.Background(), &stderr, f, interfaces.ResourceRef{Name: "bmw-staging"},
		errors.New("timed out"), 30*time.Second, em)
	if f.calls != 1 {
		t.Fatal("Troubleshoot not called")
	}
	out := stderr.String()
	if !strings.Contains(out, "::group::Troubleshoot: bmw-staging") {
		t.Error("missing group marker")
	}
	if !strings.Contains(out, "[pre_deploy]") {
		t.Error("missing phase marker")
	}
	if !strings.Contains(out, "migration failed") {
		t.Error("missing cause")
	}
	if !strings.Contains(out, "exit status 1") {
		t.Error("missing detail")
	}
}

func TestE2ENoTroubleshooter_NoCrash(t *testing.T) {
	var stderr bytes.Buffer
	type plainDriver struct{ interfaces.ResourceDriver }
	troubleshootAfterFailure(context.Background(), &stderr, &plainDriver{}, interfaces.ResourceRef{},
		errors.New("x"), 30*time.Second, plainEmitter{})
	if stderr.Len() != 0 {
		t.Errorf("unexpected output: %q", stderr.String())
	}
}
