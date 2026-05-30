package main

import (
	"context"
	"fmt"
)

// setupReport summarises the result of a runSetupEngine call.
type setupReport struct {
	// Set lists secret names that were successfully written to the provider.
	Set []string
	// Skipped lists secrets that were not written — either because the
	// selector excluded them or because the valuer returned provided=false.
	Skipped []string
	// Failed lists secrets where provider.Set returned an error.
	Failed []string
}

// runSetupEngine is a pure, front-end-agnostic secrets-setup engine.
// It is generic over the declared-secret type D so that callers can
// carry whatever extra fields (sensitive, description, …) they need
// without this engine knowing about them.
//
// Parameters:
//
//   - ctx       — context for provider calls.
//   - decls     — all secrets declared in the config / plugin manifest.
//   - nameOf    — extracts the secret name from D.
//   - provider  — the SecretsProvider to query and write.
//   - selector  — given the full declared list and the statuses queried
//     from the provider, returns the subset that should be set.
//     Return (nil, err) for a fatal selector error.
//   - valuer    — given one declared secret, returns (value, provided, err).
//     When provided=false the secret is skipped (not an error).
//     Return ("", _, err) to signal a per-secret error.
//   - audit     — called after each successful Set(name, value); receives
//     name and storeName. NEVER receives the value.
//   - stopOnErr — when true the engine returns after the first Set error;
//     when false all secrets are attempted and failures accumulate.
//
// Non-fatal errors (per-secret Set failures) are collected in
// setupReport.Failed. A non-nil overall error is returned only when the
// provider is fundamentally unusable (e.g. List fails fatally) or when
// stopOnErr=true and a Set error occurs.
func runSetupEngine[D any](
	ctx context.Context,
	decls []D,
	nameOf func(D) string,
	provider SecretsProvider,
	selector func([]D, []SecretStatus) ([]D, error),
	valuer func(D) (string, bool, error),
	audit func(name, store string),
	stopOnErr bool,
) (setupReport, error) {
	var report setupReport

	// Query current statuses from the provider for all declared secrets.
	// We do a per-name Check rather than a List so that write-only stores
	// (GitHub) and stores that cannot enumerate still work.
	statuses := make([]SecretStatus, 0, len(decls))
	for _, d := range decls {
		name := nameOf(d)
		state, _ := provider.Check(ctx, name)
		statuses = append(statuses, SecretStatus{
			Name:  name,
			State: state,
			IsSet: state == SecretSet,
		})
	}

	// Let the selector pick which secrets to process.
	selected, err := selector(decls, statuses)
	if err != nil {
		return report, fmt.Errorf("setup engine selector: %w", err)
	}

	// Build a quick set-name lookup so we know which decls were skipped.
	selectedSet := make(map[string]bool, len(selected))
	for _, d := range selected {
		selectedSet[nameOf(d)] = true
	}
	for _, d := range decls {
		if !selectedSet[nameOf(d)] {
			report.Skipped = append(report.Skipped, nameOf(d))
		}
	}

	// Process each selected secret.
	for _, d := range selected {
		name := nameOf(d)

		value, provided, vErr := valuer(d)
		if vErr != nil {
			report.Failed = append(report.Failed, name)
			if stopOnErr {
				return report, fmt.Errorf("setup engine valuer for %q: %w", name, vErr)
			}
			continue
		}
		if !provided {
			report.Skipped = append(report.Skipped, name)
			continue
		}

		if setErr := provider.Set(ctx, name, value); setErr != nil {
			report.Failed = append(report.Failed, name)
			if stopOnErr {
				return report, fmt.Errorf("setup engine set %q: %w", name, setErr)
			}
			continue
		}

		report.Set = append(report.Set, name)
		if audit != nil {
			audit(name, "") // storeName is filled in by the caller if needed
		}
	}

	return report, nil
}
