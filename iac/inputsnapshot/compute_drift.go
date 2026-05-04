package inputsnapshot

import (
	"sort"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// unsetFingerprintPlaceholder is the in-package constant displayed in the
// ApplyFingerprint field when the var was set at plan time but is missing
// entirely from the applySnap. UNEXPORTED to keep the placeholder a private
// contract between ComputeDrift + FormatStaleError + tests.
const unsetFingerprintPlaceholder = "(unset)"

// ComputeDrift compares plan-time vs apply-time fingerprint snapshots and
// produces a drift report. Iterates over planSnap keys (no phantom
// InputNames field needed; map keys ARE the names). Honors the in-package
// preservedFingerprint sentinel from snapshot.go — keys whose applySnap
// value equals the sentinel are skipped (sub-action cleanup case).
//
// Cross-function contract:
//   - Compute (snapshot.go) passes the sentinel through unhashed.
//   - NewTolerantEnvProvider (snapshot.go, sole sanctioned injector) returns
//     the sentinel for plan-time-set apply-time-unset vars.
//   - ComputeDrift (this function) honors the sentinel by skipping the entry.
func ComputeDrift(planSnap, applySnap map[string]string) []interfaces.DriftEntry {
	var drift []interfaces.DriftEntry
	for name, planFP := range planSnap {
		applyFP, present := applySnap[name]
		if !present {
			drift = append(drift, interfaces.DriftEntry{
				Name:             name,
				PlanFingerprint:  planFP,
				ApplyFingerprint: unsetFingerprintPlaceholder,
			})
			continue
		}
		if applyFP == preservedFingerprint {
			continue // Sentinel — sub-action cleanup unset; not real drift.
		}
		if applyFP != planFP {
			drift = append(drift, interfaces.DriftEntry{
				Name:             name,
				PlanFingerprint:  planFP,
				ApplyFingerprint: applyFP,
			})
		}
	}
	// Sort by Name so the returned slice is deterministic across runs —
	// callers may marshal/log/compare the slice (StaleError.Drift is
	// publicly exposed). FormatStaleError sorts independently for its own
	// output; this sort makes the structured slice match the printed order.
	sort.Slice(drift, func(i, j int) bool { return drift[i].Name < drift[j].Name })
	return drift
}
