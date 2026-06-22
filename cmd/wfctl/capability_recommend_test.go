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
	if !strings.Contains(out.String(), `"capabilities"`) {
		t.Fatalf("expected JSON capabilities, got: %s", out.String())
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
