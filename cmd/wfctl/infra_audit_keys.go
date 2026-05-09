package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// auditKeysStdout / auditKeysStderr are seam variables tests override to
// capture the subcommand's structured output without redirecting os.Stdout
// globally. Mirrors the cleanupStdout / cleanupStderr pattern.
var (
	auditKeysStdout io.Writer = os.Stdout
	auditKeysStderr io.Writer = os.Stderr
)

// auditKeysLoadProviders is the seam used by runInfraAuditKeysCmd to obtain
// the IaCProvider instances declared in the config's iac.provider modules.
// The default implementation defers to defaultCleanupLoadProviders so
// audit-keys inherits the same env-resolution + plugin-discovery contract
// established by `wfctl infra cleanup` (and R-A10). Tests override this var
// to inject fake providers without spinning up real plugin subprocesses.
var auditKeysLoadProviders = defaultCleanupLoadProviders

// runInfraAuditKeys lists every cloud-side resource of `--type <T>` via the
// provider's interfaces.EnumeratorAll. This is the read-only surface for
// drift correction before the destructive `wfctl infra prune` (Task 19).
//
// Signature note: this function takes interfaces.EnumeratorAll directly
// (not the broader IaCProvider) so unit tests can pass a minimal fake
// without implementing every IaCProvider method. The dispatcher in
// runInfraAuditKeysCmd performs the IaCProvider → EnumeratorAll
// type-assertion at the boundary; providers that don't implement the
// optional interface are surfaced as a structured error there, not here.
//
// Exit codes:
//
//   - 0: enumeration succeeded (zero or more resources rendered)
//   - 1: enumeration failed (provider error)
//   - 2: argument parse error or missing required --type
//
// Output goes to w as a fixed-width table — `audit-keys` is intended for
// human + CI consumption (the prune subcommand consumes the same shape).
func runInfraAuditKeys(args []string, enumerator interfaces.EnumeratorAll, w io.Writer) int {
	fs := flag.NewFlagSet("infra audit-keys", flag.ContinueOnError)
	fs.SetOutput(w)
	var resourceType string
	fs.StringVar(&resourceType, "type", "", "Resource type to enumerate (e.g. infra.spaces_key)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if resourceType == "" {
		fmt.Fprintln(w, "audit-keys: --type is required")
		return 2
	}

	// EnumerateAll returns []*ResourceOutput (full metadata) per the
	// workflow contract, so audit-keys can render without a second Read.
	outs, err := enumerator.EnumerateAll(context.Background(), resourceType)
	if err != nil {
		fmt.Fprintf(w, "audit-keys: %v\n", err)
		return 1
	}

	fmt.Fprintf(w, "Found %d %s resource(s):\n\n", len(outs), resourceType)
	fmt.Fprintf(w, "%-30s %-30s %s\n", "NAME", "ACCESS_KEY", "CREATED_AT")
	for _, o := range outs {
		// Prefer typed fields — the EnumeratorAll contract guarantees
		// ProviderID + Name. Outputs is for additional metadata
		// (created_at). Fall back to Outputs[*] for backward compat with
		// providers that populate both, but typed fields take priority so
		// audit-keys renders correctly even for providers that follow the
		// strict contract without redundant Outputs writes.
		name := o.Name
		if name == "" {
			name, _ = o.Outputs["name"].(string)
		}
		ak := o.ProviderID
		if ak == "" {
			ak, _ = o.Outputs["access_key"].(string)
		}
		ca, _ := o.Outputs["created_at"].(string) // metadata; legitimately Outputs-only
		fmt.Fprintf(w, "%-30s %-30s %s\n", name, ak, ca)
	}
	return 0
}

// runInfraAuditKeysCmd is the production entry point for `wfctl infra
// audit-keys`. It loads iac.provider modules from the config (honoring
// --config / --env), finds the first one that implements
// interfaces.EnumeratorAll for the requested --type, and dispatches to
// runInfraAuditKeys.
//
// Splitting the dispatcher from runInfraAuditKeys keeps the testable
// function pure (no config / plugin I/O) while still presenting a single
// CLI surface to operators.
//
// Args-passing contract: this dispatcher captures EVERY flag it parses
// (including --type) and synthesizes a clean inner-args slice with only
// the flags runInfraAuditKeys understands. Forwarding the raw args slice
// would error inside runInfraAuditKeys with "flag provided but not
// defined: -config" because its inner FlagSet only declares --type.
//
// Strict-mode policy (v0.27.1, per user mandate "remove the fallback and
// force strict mode"): if an enumeration call returns
// interfaces.ErrProviderMethodUnimplemented, that error propagates LOUD
// rather than being swallowed into "Found 0 resources". Plugins that
// declare the bridge but lack the underlying implementation MUST surface
// the gap so operators are not misled into thinking the cloud account is
// empty.
func runInfraAuditKeysCmd(args []string) error {
	fs := flag.NewFlagSet("infra audit-keys", flag.ContinueOnError)
	fs.SetOutput(auditKeysStderr)
	var configFile, envName, resourceType string
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	fs.StringVar(&resourceType, "type", "", "Resource type to enumerate (e.g. infra.spaces_key)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	providers, closers, err := auditKeysLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintf(auditKeysStderr, "warning: provider shutdown: %v\n", cerr)
			}
		}
	}()

	// Synthesize a clean inner-args slice — only flags runInfraAuditKeys
	// declares. resourceType may be empty; runInfraAuditKeys handles the
	// "--type required" error itself with its own structured message.
	inner := []string{"--type", resourceType}
	for _, p := range providers {
		enum, ok := p.(interfaces.EnumeratorAll)
		if !ok {
			continue
		}
		// Strict-mode dispatch: hand the enumerator directly to the
		// renderer. If the underlying RPC returns
		// ErrProviderMethodUnimplemented, runInfraAuditKeys prints the
		// error to stdout and returns exit code 1 — exactly the loud
		// failure mode the user mandate requires. No silent swallow.
		rc := runInfraAuditKeys(inner, enum, auditKeysStdout)
		if rc != 0 {
			return fmt.Errorf("audit-keys exited with code %d", rc)
		}
		return nil
	}
	return fmt.Errorf("audit-keys: no loaded provider implements EnumeratorAll")
}
