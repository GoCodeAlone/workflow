package inputsnapshot

import (
	"errors"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestStaleError_ErrorIsFormatStaleError verifies that *StaleError's Error()
// returns exactly FormatStaleError(drift) — no duplicated sentinel prefix
// from ErrEnvVarChanged appears in the user-facing message.
func TestStaleError_ErrorIsFormatStaleError(t *testing.T) {
	drift := []interfaces.DriftEntry{
		{Name: "FOO", PlanFingerprint: "aaaa", ApplyFingerprint: "bbbb"},
	}
	se := NewStaleError(drift)
	if se == nil {
		t.Fatal("NewStaleError returned nil for non-empty drift")
	}
	got := se.Error()
	want := FormatStaleError(drift)
	if got != want {
		t.Errorf("StaleError.Error() = %q\nwant exactly %q", got, want)
	}
	// Sentinel text must NOT leak into user-facing output.
	if strings.Contains(got, ErrEnvVarChanged.Error()) {
		t.Errorf("StaleError.Error() leaks sentinel text %q: %q", ErrEnvVarChanged.Error(), got)
	}
}

// TestStaleError_UnwrapMatchesSentinel verifies that errors.Is finds
// ErrEnvVarChanged through *StaleError so callers can detect plan-stale
// programmatically without coupling to the message text.
func TestStaleError_UnwrapMatchesSentinel(t *testing.T) {
	se := NewStaleError([]interfaces.DriftEntry{
		{Name: "FOO", PlanFingerprint: "aaaa", ApplyFingerprint: "bbbb"},
	})
	var err error = se
	if !errors.Is(err, ErrEnvVarChanged) {
		t.Errorf("errors.Is(err, ErrEnvVarChanged) = false; want true")
	}
}

// TestNewStaleError_EmptyDriftReturnsNil verifies the constructor returns
// nil for an empty drift report so callers don't accidentally wrap a no-op
// error.
func TestNewStaleError_EmptyDriftReturnsNil(t *testing.T) {
	if got := NewStaleError(nil); got != nil {
		t.Errorf("NewStaleError(nil) = %v; want nil", got)
	}
	if got := NewStaleError([]interfaces.DriftEntry{}); got != nil {
		t.Errorf("NewStaleError(empty) = %v; want nil", got)
	}
}
