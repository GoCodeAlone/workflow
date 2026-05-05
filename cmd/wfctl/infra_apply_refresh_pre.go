package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
)

// applyPreStepRefreshOutputsEnvVar is the environment variable that opts in
// to running iac/refreshoutputs.Refresh as a pre-step before apply computes
// its plan. The default is OFF so apply behaves identically to pre-W-2 for
// every operator who doesn't set it.
//
// The value is parsed with strconv.ParseBool: "1", "t", "T", "TRUE",
// "true", "True" enable; "0", "f", "F", "FALSE", "false", "False" disable;
// unset, empty, or unrecognised values disable. Operators who use the
// "0" / "false" convention to turn features off therefore get the
// expected behaviour rather than the off-by-one foot-gun a presence-only
// toggle would produce.
const applyPreStepRefreshOutputsEnvVar = "WFCTL_REFRESH_OUTPUTS"

// applyPreStepRefreshOutputs runs the read-only output refresh against
// every iac.provider declared in cfgFile and persists any state entries
// whose Outputs changed. It is the apply-time counterpart to wfctl infra
// refresh-outputs (T2.2): same helpers, same error semantics. On any Read
// or driver-resolution failure, it returns the wrapped error so apply
// aborts before computing a plan against stale outputs.
//
// This function exists as its own file so reverting commit
// "feat(iac): apply-time refresh-outputs pre-step ..." removes the entire
// code path in one operation. The caller in runInfraApply gates this on
// three conditions, all of which must be true for the pre-step to fire:
//
//   - applyPreStepRefreshEnabled returns true (WFCTL_REFRESH_OUTPUTS
//     parses to true and --skip-refresh was not passed).
//   - hasInfraModules(cfgFile) is true (legacy platform.* configs are
//     skipped — they don't flow through iac/refreshoutputs).
//
// The helper itself is purely additive on top of those gates: it never
// short-circuits on values it would otherwise accept, so a caller that
// has already decided to refresh can rely on the helper to do exactly
// that.
func applyPreStepRefreshOutputs(ctx context.Context, cfgFile, envName string, stdout io.Writer) error {
	providerDefs, err := discoverIaCProvidersForRefresh(cfgFile, envName)
	if err != nil {
		return err
	}
	if len(providerDefs) == 0 {
		// No provider for this env — log and return nil rather than
		// aborting apply: the downstream applyInfraModules call will
		// produce a more actionable error if a provider was actually
		// expected. The log line makes the no-op visible to operators
		// who explicitly opted in.
		fmt.Fprintln(stdout, "Refresh pre-step: no iac.provider modules in config; skipping.")
		return nil
	}

	states, err := loadCurrentState(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("load current state: %w", err)
	}
	if len(states) == 0 {
		return nil
	}

	store, err := resolveStateStore(cfgFile, envName)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}

	fmt.Fprintln(stdout, "Refreshing outputs from cloud (read-only)...")
	return refreshOutputsAcrossProviders(ctx, providerDefs, states, store, 0 /* default concurrency */, stdout)
}

// applyPreStepRefreshEnabled reports whether the opt-in env var parses
// to true AND --skip-refresh was not passed. The flag always wins so
// operators can disable the pre-step in environments where the env var
// is forced on globally (e.g. CI that exports it for every job).
//
// Empty/unset and unrecognised values both disable: the env var is
// strictly opt-in, never opt-out. "0" / "false" therefore disable
// rather than (mis-)enabling, which is the convention every operator
// expects.
func applyPreStepRefreshEnabled(skipRefreshFlag bool) bool {
	if skipRefreshFlag {
		return false
	}
	v := os.Getenv(applyPreStepRefreshOutputsEnvVar)
	if v == "" {
		return false
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return enabled
}
