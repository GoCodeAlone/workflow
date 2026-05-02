package main

import (
	"context"
	"fmt"
	"io"
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

	results, err := provider.DetectDrift(ctx, refs)
	if err != nil {
		// Transient or auth error — propagate; do NOT prune anything.
		return fmt.Errorf("detect drift: %w", err)
	}

	for _, r := range results {
		if r.Class != interfaces.DriftClassGhost {
			// In-sync or config-drift: leave for regular plan/apply phase.
			continue
		}

		isProtected := isRefProtected(states, r.Name)
		if isProtected && !allowProtectedPrune {
			// Hard-block: return an error immediately so the caller sees the
			// problem. No prunes have happened at this point.
			fmt.Fprintf(stderr, "wfctl: BLOCKED: %s is protected; cannot prune without --allow-protected-prune\n", r.Name)
			return fmt.Errorf("refresh: blocked on protected resource %q (use --allow-protected-prune to override)", r.Name)
		}

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
// state Outputs map.
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
