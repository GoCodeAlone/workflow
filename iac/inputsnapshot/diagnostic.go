package inputsnapshot

import (
	"fmt"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// FormatStaleError renders a drift report into the canonical human-readable
// message used at every plan-stale call site (cmd/wfctl/infra.go persisted
// `--plan` path; wfctlhelpers.ApplyPlan in-process path in T3.1.5). Output:
//
//	plan stale: %d input(s) changed since plan
//	  KEY1: fingerprint planFP1 (plan) → applyFP1 (apply)
//	  KEY2: fingerprint planFP2 (plan) → applyFP2 (apply)
//	  hint: ensure all env vars referenced by infra.yaml are exported to both Plan and Apply steps
//
// Drift entries are sorted by Name for deterministic output. An empty drift
// report yields the singular line "plan stale: 0 input(s) changed since plan"
// — callers should avoid invoking the formatter when no drift exists.
func FormatStaleError(drift []interfaces.DriftEntry) string {
	sorted := make([]interfaces.DriftEntry, len(drift))
	copy(sorted, drift)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "plan stale: %d input(s) changed since plan\n", len(sorted))
	for _, d := range sorted {
		fmt.Fprintf(&b, "  %s: fingerprint %s (plan) → %s (apply)\n", d.Name, d.PlanFingerprint, d.ApplyFingerprint)
	}
	b.WriteString("  hint: ensure all env vars referenced by infra.yaml are exported to both Plan and Apply steps")
	return b.String()
}
