package main

// Note: as of v0.27.0 (engine-side sensitive-output routing), no in-tree
// call site dispatches driver.Diff against state.Outputs that may contain
// sensitive.PlaceholderPrefix entries. Per-provider Diff implementations
// receive desired/current via gRPC and are out of scope for engine-side
// masking. iac/sensitive.MaskSensitiveForDiff is exported as a helper
// for future in-tree consumers (e.g., a future engine-side drift command
// that compares state-Outputs to live ResourceOutput before calling
// Diff). When such a call site lands, wire MaskSensitiveForDiff(
// driver.SensitiveKeys(), desired.Config, current.Outputs) before the
// Diff dispatch to prevent placeholder-vs-plaintext false-positives.
//
// Verified at v0.27.0 by `grep -rn "driver\.Diff(\|d\.Diff(" cmd/wfctl/
// iac/`: only iac/conformance/scenario_grpc_roundtrip.go matches, and
// it's a conformance-test tool that synthesizes its own desired/current.

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

	// Per Task 17 of the strict-contracts force-cutover: replace the
	// legacy `provider.(interfaces.DriftConfigDetector)` type-assert
	// with a typed pb.IaCProviderDriftConfigDetectorClient lookup via
	// the typed adapter's capability accessor. When the plugin's
	// ContractRegistry didn't advertise IaCProviderDriftConfigDetector,
	// the accessor returns nil and we short-circuit to the required
	// IaCProvider.DetectDrift path — preserving the v0.27.1 behavior
	// without the wasted RPC + sentinel-error round-trip.
	var results []interfaces.DriftResult
	var err error
	if adapter, ok := provider.(*typedIaCAdapter); ok {
		if cli := adapter.DriftConfigDetector(); cli != nil {
			specsMap := buildAppliedSpecMap(states, refs)
			if specsMap != nil {
				results, err = detectDriftConfigTyped(ctx, cli, refs, specsMap)
			} else {
				results, err = provider.DetectDrift(ctx, refs)
			}
		} else {
			results, err = provider.DetectDrift(ctx, refs)
		}
	} else {
		// Provider isn't a typedIaCAdapter (e.g., test fake). Fall back
		// to the Go-interface path the test provides directly.
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
