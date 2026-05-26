package wfctlhelpers

import (
	"errors"
	"strings"
	"testing"
)

// TestHasReplaceErrorPrefix_ReplaceColonFamily verifies the "replace:" family
// is recognized (engine-default prefix + backstop wrapper itself).
func TestHasReplaceErrorPrefix_ReplaceColonFamily(t *testing.T) {
	cases := []string{
		"replace: delete: 500",
		"replace: create: 422",
		"replace: canceled after delete: context canceled",
		"replace: driver: kaboom",
		"replace:",
	}
	for _, msg := range cases {
		if !hasReplaceErrorPrefix(errors.New(msg)) {
			t.Errorf("hasReplaceErrorPrefix(%q) = false, want true", msg)
		}
	}
}

// TestHasReplaceErrorPrefix_ResourceTypeReplaceFamily verifies the
// "<resource-type> replace " family is recognized for driver-owned errors.
func TestHasReplaceErrorPrefix_ResourceTypeReplaceFamily(t *testing.T) {
	cases := []string{
		`droplet replace "pg": detach volume "vol-abc": 422`,
		`vpc replace "net": something failed`,
	}
	for _, msg := range cases {
		if !hasReplaceErrorPrefix(errors.New(msg)) {
			t.Errorf("hasReplaceErrorPrefix(%q) = false, want true", msg)
		}
	}
}

// TestHasReplaceErrorPrefix_NonConforming verifies that bare errors without
// a recognized prefix return false (triggering the backstop wrapper).
func TestHasReplaceErrorPrefix_NonConforming(t *testing.T) {
	cases := []string{
		"kaboom",
		"422 Unprocessable Entity",
		"storage already associated with another droplet",
		// Multi-word heads with spaces before " replace " do NOT match:
		"bad prefix: replace something",
		"my resource replace x", // "my resource" has a space → head fails
	}
	for _, msg := range cases {
		if hasReplaceErrorPrefix(errors.New(msg)) {
			t.Errorf("hasReplaceErrorPrefix(%q) = true, want false", msg)
		}
	}
}

// TestWrapDriverReplaceError_BackstopWrapsNonConforming verifies the full
// wrapDriverReplaceError backstop on a non-conforming error.
func TestWrapDriverReplaceError_BackstopWrapsNonConforming(t *testing.T) {
	err := wrapDriverReplaceError(errors.New("kaboom"))
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	if !strings.HasPrefix(err.Error(), "replace: driver: ") {
		t.Errorf("error not wrapped with backstop prefix: %v", err)
	}
}

// TestWrapDriverReplaceError_PassesThroughConformingDriverErrors verifies that
// driver-owned "X replace" errors are NOT double-wrapped.
func TestWrapDriverReplaceError_PassesThroughConformingDriverErrors(t *testing.T) {
	orig := errors.New(`droplet replace "pg": detach volume "vol-abc": 422`)
	err := wrapDriverReplaceError(orig)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.HasPrefix(err.Error(), "replace: driver: ") {
		t.Errorf("error was double-wrapped: %v", err)
	}
	if !strings.HasPrefix(err.Error(), `droplet replace `) {
		t.Errorf("conforming prefix not preserved: %v", err)
	}
}

// TestWrapDriverReplaceError_PassesThroughDefaultReplacePrefix verifies that
// "replace: delete: " errors from DefaultReplace pass through without wrapping.
func TestWrapDriverReplaceError_PassesThroughDefaultReplacePrefix(t *testing.T) {
	orig := errors.New("replace: delete: 500")
	err := wrapDriverReplaceError(orig)
	if !strings.HasPrefix(err.Error(), "replace: delete: ") {
		t.Errorf("replace: family prefix should pass through unchanged: %v", err)
	}
}

// TestWrapDriverReplaceError_NilPassesThrough verifies nil is returned unchanged.
func TestWrapDriverReplaceError_NilPassesThrough(t *testing.T) {
	if wrapDriverReplaceError(nil) != nil {
		t.Error("wrapDriverReplaceError(nil) should return nil")
	}
}
