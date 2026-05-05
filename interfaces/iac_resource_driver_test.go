package interfaces_test

import (
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
	}

	for _, c := range cases {
		wrapped := fmt.Errorf("operation failed: %w", c.sentinel)
		if !errors.Is(wrapped, c.sentinel) {
			t.Errorf("%s: wrapped error not matched by errors.Is", c.name)
		}
	}
}
