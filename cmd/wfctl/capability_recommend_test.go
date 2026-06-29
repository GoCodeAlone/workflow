package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCapabilityRecommend_JSON(t *testing.T) {
	var out bytes.Buffer
	args := []string{"recommend", "-capability", "auth.authz",
		"-registry", "testdata/capability/registry",
		"-repo-root", "testdata/capability/repos",
		"-taxonomy", "../../capability/inventory/testdata/taxonomy.yaml"}
	if err := runCapabilityWithOutput(args, &out); err != nil {
		t.Fatalf("runCapability recommend: %v", err)
	}
	// recommend --format json emits the agent-consumable wizard state (Task 8),
	// whose top-level key is "chosen" (the selected providers + facts).
	if !strings.Contains(out.String(), `"chosen"`) {
		t.Fatalf("expected wizard-state JSON with \"chosen\", got: %s", out.String())
	}
	if !strings.Contains(out.String(), "auth") {
		t.Fatalf("expected auth provider in output: %s", out.String())
	}
}

func TestCapabilityRecommend_Help(t *testing.T) {
	var out bytes.Buffer
	if err := runCapabilityWithOutput([]string{"recommend", "-h"}, &out); err == nil {
		t.Fatal("expected non-nil error from -h")
	}
	if !strings.Contains(out.String(), "recommend") {
		t.Fatalf("help missing recommend: %s", out.String())
	}
}
