package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// runInfraApplyRefreshPhase detects drift against the given provider and prunes
// ghost-in-state entries (where cloud Read returned ErrResourceNotFound). It is
// called by runInfraApply when --refresh is set.
//
// Behavior:
//   - For each DriftClassGhost result: if autoApprove is false, prints a dry-run
//     "would prune" line. If autoApprove is true, calls store.DeleteResource and
//     emits an audit log line to stderr.
//   - Protected resources (protected: true in state Outputs) are blocked unless
//     allowProtectedPrune is also set. Without that flag, an error is returned and
//     no prunes happen.
//   - DriftClassConfig and DriftClassInSync results are left for the regular plan
//   - apply phase; this function does not touch them.
//   - If provider.DetectDrift returns a non-nil error, the error is propagated
//     immediately and no pruning happens (transient API errors must NOT cause
//     state loss).
//
// All parameters must be non-nil except states (nil is valid = no state for
// protected-resource lookup). stdout receives human-readable progress; stderr
// receives audit log lines.
func runInfraApplyRefreshPhase(
	ctx context.Context,
	provider interfaces.IaCProvider,
	refs []interfaces.ResourceRef,
	store infraStateStore,
	autoApprove bool,
	allowProtectedPrune bool,
	states []interfaces.ResourceState,
	stdout io.Writer,
	stderr io.Writer,
) error {
	if len(refs) == 0 {
		fmt.Fprintln(stdout, "Refresh: no state to check.")
		return nil
	}

	// Use DriftConfigDetector when the provider supports it (optional interface).
	// Short-circuits to legacy DetectDrift when specsMap is nil (no "apply"-
	// provenance entries available) to avoid unnecessary RPC round-trips.
	var results []interfaces.DriftResult
	var err error
	if d, ok := provider.(interfaces.DriftConfigDetector); ok {
		specsMap := buildAppliedSpecMap(states, refs)
		if specsMap != nil {
			results, err = d.DetectDriftWithSpecs(ctx, refs, specsMap)
		} else {
			results, err = provider.DetectDrift(ctx, refs)
		}
	} else {
		results, err = provider.DetectDrift(ctx, refs)
	}
	if err != nil {
		// Transient or auth error — propagate; do NOT prune anything.
		return fmt.Errorf("detect drift: %w", err)
	}

	// First pass: pre-scan ALL ghost results for protected resources without the
	// override flag. Collecting all blocked names before any mutation ensures the
	// operator sees the complete list and that no partial state mutation occurs.
	var blocked []string
	for _, r := range results {
		if r.Class != interfaces.DriftClassGhost {
			continue
		}
		if isRefProtected(states, r.Name) && !allowProtectedPrune {
			blocked = append(blocked, r.Name)
		}
	}
	if len(blocked) > 0 {
		for _, name := range blocked {
			fmt.Fprintf(stderr, "wfctl: BLOCKED: %s is protected; cannot prune without --allow-protected-prune\n", name)
		}
		return fmt.Errorf("refresh blocked: %d protected resource(s) require --allow-protected-prune: %s",
			len(blocked), strings.Join(blocked, ", "))
	}

	// Second pass: all pre-validation passed — execute mutations.
	for _, r := range results {
		if r.Class != interfaces.DriftClassGhost {
			// In-sync or config-drift: leave for regular plan/apply phase.
			continue
		}

		isProtected := isRefProtected(states, r.Name)

		if !autoApprove {
			// Dry-run: report what would happen without mutating.
			fmt.Fprintf(stdout, "Refresh: would prune ghost %s (%s) — cloud reports not found.\n", r.Name, r.Type)
			continue
		}

		// Emit audit log before mutation so the log entry is always present,
		// even if DeleteResource fails.
		fmt.Fprintf(stderr, "wfctl: state mutation prune %s (type=%s protected=%v) reason=ghost-in-state at %s\n",
			r.Name, r.Type, isProtected, time.Now().UTC().Format(time.RFC3339))

		if err := store.DeleteResource(ctx, r.Name); err != nil {
			return fmt.Errorf("refresh: prune %s: %w", r.Name, err)
		}
		fmt.Fprintf(stdout, "Refresh: pruned ghost %s (%s)\n", r.Name, r.Type)
	}
	return nil
}

// isRefProtected returns true if the named resource has protected: true in its
// state Outputs map. The type assertion is intentionally strict: if
// Outputs["protected"] is a non-bool (e.g. the string "true"), the assertion
// fails and the function returns false. YAML unmarshals bare `true` as bool,
// so this should not occur in practice, but callers should be aware of the
// silent false-return for unexpected types.
func isRefProtected(states []interfaces.ResourceState, name string) bool {
	for i := range states {
		if states[i].Name == name {
			if p, ok := states[i].Outputs["protected"].(bool); ok && p {
				return true
			}
		}
	}
	return false
}
