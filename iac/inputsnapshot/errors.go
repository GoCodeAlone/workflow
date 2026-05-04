package inputsnapshot

import (
	"errors"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ErrEnvVarChanged is the typed sentinel returned by the apply paths
// (cmd/wfctl/infra.go persisted-`--plan` path in W-1; wfctlhelpers.ApplyPlan
// in-process path in W-3a/T3.1.5) when an env var referenced at plan time
// has a different fingerprint at apply time. Callers match with
// errors.Is(err, ErrEnvVarChanged) to detect the plan-stale case
// programmatically. To avoid the sentinel's text appearing in user-facing
// output, callers SHOULD construct the user-visible error via
// NewStaleError(drift) — that returns a *StaleError whose Error() is
// exactly FormatStaleError(drift) and whose Unwrap() chain yields this
// sentinel for errors.Is.
var ErrEnvVarChanged = errors.New("env-var changed since plan")

// StaleError is the user-facing error returned by apply paths when env-var
// drift is detected. Its Error() is exactly FormatStaleError(drift) so the
// printed message is the canonical human diagnostic (no duplicated sentinel
// prefix); Unwrap() returns ErrEnvVarChanged so errors.Is works for
// programmatic detection.
type StaleError struct {
	Drift []interfaces.DriftEntry
}

// Error returns FormatStaleError(s.Drift) — the canonical human-readable
// per-key diagnostic with sorted entries and trailing hint.
func (s *StaleError) Error() string { return FormatStaleError(s.Drift) }

// Unwrap returns ErrEnvVarChanged so callers can use
// errors.Is(err, inputsnapshot.ErrEnvVarChanged) to detect the plan-stale
// case without coupling to the Error() text.
func (s *StaleError) Unwrap() error { return ErrEnvVarChanged }

// NewStaleError constructs the canonical *StaleError for a non-empty drift
// report. Returns nil when drift is empty (caller should treat that as
// "no plan-stale condition" rather than wrapping a no-op error).
func NewStaleError(drift []interfaces.DriftEntry) *StaleError {
	if len(drift) == 0 {
		return nil
	}
	return &StaleError{Drift: drift}
}
