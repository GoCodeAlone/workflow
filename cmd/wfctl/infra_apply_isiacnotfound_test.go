package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestIsIaCNotFound_TypedSentinel confirms native wrapping still works.
func TestIsIaCNotFound_TypedSentinel(t *testing.T) {
	err := fmt.Errorf("database %q: %w", "multisite-pg", interfaces.ErrResourceNotFound)
	if !isIaCNotFound(err) {
		t.Error("expected typed sentinel to be detected")
	}
}

// TestIsIaCNotFound_GRPCStringFallback is the regression test for the
// gocodealone-multisite deploy: the typed gRPC adapter strips sentinel
// identity, leaving only the wrapped error string. Adoption is meant
// to be "look up, fall back to create" — without this fallback the
// remote plugin's not-found returns made apply-prereq fail every run.
func TestIsIaCNotFound_GRPCStringFallback(t *testing.T) {
	grpcErr := errors.New(`rpc error: code = Unknown desc = database "multisite-pg": iac: resource not found`)
	if !isIaCNotFound(grpcErr) {
		t.Error("expected gRPC-flattened not-found to be detected via string match")
	}
}

func TestIsIaCNotFound_NilSafe(t *testing.T) {
	if isIaCNotFound(nil) {
		t.Error("nil err must not be reported as not-found")
	}
}

func TestIsIaCNotFound_OtherErrorsIgnored(t *testing.T) {
	if isIaCNotFound(errors.New("permission denied")) {
		t.Error("non-matching error must not be reported as not-found")
	}
}
