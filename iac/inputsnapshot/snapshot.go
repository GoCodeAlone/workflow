// Package inputsnapshot computes plan-time env-var fingerprints for the
// plan-stale diagnostic. Fingerprints are 16 hex chars (64 bits of preimage
// resistance); plan.json is treated as semi-sensitive and gitignored.
package inputsnapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// Compute returns a map of env-var name → 16-hex-char sha256 prefix of the value.
// Variables that aren't set (lookup returns ok=false) are omitted from the snapshot.
func Compute(varNames []string, lookup func(string) (string, bool)) map[string]string {
	out := make(map[string]string)
	for _, name := range varNames {
		val, ok := lookup(name)
		if !ok {
			continue
		}
		if val == preservedFingerprint {
			// Sentinel from NewTolerantEnvProvider — pass through unhashed
			// so ComputeDrift recognizes the preservation signal. (rev6 —
			// unexported per cycle-5; in-package access only.)
			out[name] = preservedFingerprint
			continue
		}
		sum := sha256.Sum256([]byte(val))
		out[name] = hex.EncodeToString(sum[:])[:16]
	}
	return out
}

// Snapshot is an alias for Compute that reads slightly more naturally at
// the in-process apply postcondition call site (T3.1.5).
func Snapshot(names []string, provider func(string) (string, bool)) map[string]string {
	return Compute(names, provider)
}

// OSEnvProvider is the canonical env-provider closure that reads from
// process env via os.LookupEnv. Used by start-of-apply InputSnapshot capture.
func OSEnvProvider(name string) (string, bool) { return os.LookupEnv(name) }

// preservedFingerprint is a sentinel value indicating an env-var was set at
// plan time but is unset at apply time (sub-action cleanup is the canonical
// case). ComputeDrift (T1.5) skips drift detection for keys whose applySnap
// value is this sentinel. UNEXPORTED (rev6 — addresses cycle-5 Important on
// external-bypass channel): NewTolerantEnvProvider is the only sanctioned
// way to inject the sentinel; external callers cannot defeat drift detection.
//
// Cross-function contract:
//   - Compute (this file, in-package) passes the sentinel through unhashed.
//   - NewTolerantEnvProvider (this file) returns the sentinel for plan-time-set
//     but apply-time-unset vars (in-package access to the constant).
//   - ComputeDrift (compute_drift.go, T1.5, same package) honors the sentinel
//     by skipping drift detection for that key.
const preservedFingerprint = "__plan_time_preserved__"

// NewTolerantEnvProvider returns an EnvProvider closure used by the
// in-process apply postcondition (T3.1.5). When a var was set at plan time
// (present in planSnapshot) but is now unset (sub-action cleanup), the
// closure returns the in-package preservedFingerprint sentinel so
// ComputeDrift suppresses the (false-positive) drift entry. For vars
// genuinely unset at both times, returns ("", false) → Compute drops the
// key from the resulting map.
//
// This is the ONLY sanctioned way to inject the preservation sentinel.
// Direct callers of Compute with a custom env-provider cannot construct
// the sentinel value because it is unexported.
func NewTolerantEnvProvider(planSnapshot map[string]string) func(name string) (string, bool) {
	return func(name string) (string, bool) {
		if val, ok := os.LookupEnv(name); ok {
			return val, true
		}
		if _, wasInPlan := planSnapshot[name]; wasInPlan {
			return preservedFingerprint, true
		}
		return "", false
	}
}
