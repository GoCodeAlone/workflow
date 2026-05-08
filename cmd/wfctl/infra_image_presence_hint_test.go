package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestImageNotInRegistryHint_TypedError verifies the hint fires when the
// driver error is the typed ErrImageNotInRegistry (in-process driver path).
func TestImageNotInRegistryHint_TypedError(t *testing.T) {
	var buf bytes.Buffer
	err := fmt.Errorf("apply: container_service/app: %w",
		fmt.Errorf("image %q not found in DOCR: %w", "ref", interfaces.ErrImageNotInRegistry))
	emitImageNotInRegistryHint(&buf, err)
	if !strings.Contains(buf.String(), "image not found in registry") {
		t.Fatalf("expected actionable hint; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "Re-run") {
		t.Fatalf("expected 're-run' suggestion; got %q", buf.String())
	}
	if !errors.Is(err, interfaces.ErrImageNotInRegistry) {
		t.Fatalf("test setup: errors.Is must match wrapped sentinel")
	}
}

// TestImageNotInRegistryHint_StringMatchFallback verifies the hint fires when
// the driver error has been string-flattened across a gRPC boundary (the
// sentinel identity is gone, only the message string survives).
func TestImageNotInRegistryHint_StringMatchFallback(t *testing.T) {
	var buf bytes.Buffer
	// Simulate a gRPC-wrapped error: the original sentinel identity is lost;
	// only the message string survives.
	err := errors.New("apply: container_service/app: image \"ref\" not found in DOCR repo \"app\": iac: image tag or digest not found in registry")
	if errors.Is(err, interfaces.ErrImageNotInRegistry) {
		t.Fatalf("test setup: errors.Is must NOT match for string-only error (this proves the fallback is needed)")
	}
	emitImageNotInRegistryHint(&buf, err)
	if !strings.Contains(buf.String(), "image not found in registry") {
		t.Fatalf("expected actionable hint via string-match fallback; got %q", buf.String())
	}
}

// TestImageNotInRegistryHint_UnrelatedError_NoHint verifies the hint does NOT
// fire for unrelated errors.
func TestImageNotInRegistryHint_UnrelatedError_NoHint(t *testing.T) {
	var buf bytes.Buffer
	err := errors.New("apply: container_service/app: 500 internal server error")
	emitImageNotInRegistryHint(&buf, err)
	if buf.Len() != 0 {
		t.Fatalf("expected no hint for unrelated error; got %q", buf.String())
	}
}

// TestImageNotInRegistryHint_NilInputs_NoOp verifies safe behavior with nil.
func TestImageNotInRegistryHint_NilInputs_NoOp(t *testing.T) {
	emitImageNotInRegistryHint(nil, nil)             // both nil
	emitImageNotInRegistryHint(&bytes.Buffer{}, nil) // nil err
	var buf bytes.Buffer
	emitImageNotInRegistryHint(nil, interfaces.ErrImageNotInRegistry) // nil writer
	if buf.Len() != 0 {
		t.Fatalf("expected no output; got %q", buf.String())
	}
}
