package interfaces_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestSentinels_ErrorsIs verifies each sentinel is distinct and identifies itself.
func TestSentinels_ErrorsIs(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrResourceNotFound", interfaces.ErrResourceNotFound},
		{"ErrResourceAlreadyExists", interfaces.ErrResourceAlreadyExists},
		{"ErrRateLimited", interfaces.ErrRateLimited},
		{"ErrTransient", interfaces.ErrTransient},
		{"ErrUnauthorized", interfaces.ErrUnauthorized},
		{"ErrForbidden", interfaces.ErrForbidden},
		{"ErrValidation", interfaces.ErrValidation},
		{"ErrImageNotInRegistry", interfaces.ErrImageNotInRegistry},
	}

	for i, s := range sentinels {
		if s.err == nil {
			t.Errorf("%s: sentinel is nil", s.name)
			continue
		}
		// Self-identification.
		if !errors.Is(s.err, s.err) {
			t.Errorf("%s: errors.Is(self) = false", s.name)
		}
		// Distinctness — no sentinel should match another.
		for j, other := range sentinels {
			if i == j {
				continue
			}
			if errors.Is(s.err, other.err) {
				t.Errorf("%s matches %s — sentinels must be distinct", s.name, other.name)
			}
		}
	}
}

// TestResourceReplacerInterfaceShape asserts at compile time that a driver
// implementing ResourceReplacer satisfies the interface shape.
func TestResourceReplacerInterfaceShape(t *testing.T) {
	// Compile-time assertion: a no-op driver that implements
	// ResourceReplacer satisfies the interface.
	var _ interfaces.ResourceReplacer = (*noopReplacer)(nil)
}

type noopReplacer struct{}

func (*noopReplacer) Replace(_ context.Context, _ interfaces.ResourceRef, _ interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

// TestSentinels_Wrappable verifies a wrapped sentinel still matches via errors.Is.
func TestSentinels_Wrappable(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
	}{
		{"ErrResourceNotFound", interfaces.ErrResourceNotFound},
		{"ErrResourceAlreadyExists", interfaces.ErrResourceAlreadyExists},
		{"ErrRateLimited", interfaces.ErrRateLimited},
		{"ErrTransient", interfaces.ErrTransient},
		{"ErrUnauthorized", interfaces.ErrUnauthorized},
		{"ErrForbidden", interfaces.ErrForbidden},
		{"ErrValidation", interfaces.ErrValidation},
		{"ErrImageNotInRegistry", interfaces.ErrImageNotInRegistry},
	}

	for _, c := range cases {
		wrapped := fmt.Errorf("operation failed: %w", c.sentinel)
		if !errors.Is(wrapped, c.sentinel) {
			t.Errorf("%s: wrapped error not matched by errors.Is", c.name)
		}
	}
}

// TestErrImageNotInRegistry_MessageStringStable asserts the exact message
// string. The wfctl render layer matches this string verbatim as the
// gRPC-boundary fallback (structpb does not preserve sentinel identity), so
// changing this string silently breaks the actionable hint for plugin-driven
// drivers. See decisions/0004 in core-dump for the rationale.
func TestErrImageNotInRegistry_MessageStringStable(t *testing.T) {
	want := "iac: image tag or digest not found in registry"
	got := interfaces.ErrImageNotInRegistry.Error()
	if got != want {
		t.Fatalf("ErrImageNotInRegistry.Error() = %q; want %q (load-bearing for gRPC string-match fallback)", got, want)
	}
}

func TestErrResourceNotFound_MessageStringStable(t *testing.T) {
	const expected = "iac: resource not found"
	if got := interfaces.ErrResourceNotFound.Error(); got != expected {
		t.Fatalf("ErrResourceNotFound message changed (was %q, want %q) — load-bearing for cross-process matching", got, expected)
	}
}

func TestIsErrResourceNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"direct", interfaces.ErrResourceNotFound, true},
		{"wrapped with fmt.Errorf %w", fmt.Errorf("driver: %w", interfaces.ErrResourceNotFound), true},
		{"stringified across gRPC (no sentinel)", errors.New("plugin: " + interfaces.ErrResourceNotFound.Error()), true},
		{"unrelated NotFound", errors.New("file: not found"), false},
		{"empty error", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := interfaces.IsErrResourceNotFound(tc.err); got != tc.want {
				t.Errorf("IsErrResourceNotFound(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}
