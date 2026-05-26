package main

import (
	"context"
	"errors"
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

	renderAuditKeys(outs, resourceType, w)
	return 0
}

// renderAuditKeys writes the audit-keys table. Extracted so the multi-
// provider dispatcher (runInfraAuditKeysCmd) can render after issuing
// EnumerateAll directly — the dispatcher needs to inspect the error to
// continue-on-Unimplemented across providers, which it can't do through
// the int-returning runInfraAuditKeys entry point.
func renderAuditKeys(outs []*interfaces.ResourceOutput, resourceType string, w io.Writer) {
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
}

// runInfraAuditKeysCmd is the production entry point for `wfctl infra
// audit-keys`. It loads iac.provider modules from the config (honoring
// --config / --env), iterates each that implements
// interfaces.EnumeratorAll, and renders the first successful result.
//
// Splitting the dispatcher from runInfraAuditKeys keeps the underlying
// pure function (no config / plugin I/O) available for unit tests while
// still presenting a single CLI surface to operators.
//
// Dispatch shape: this function does NOT delegate to runInfraAuditKeys.
// runInfraAuditKeys is single-provider and would re-issue the
// EnumerateAll call we already made for the multi-provider sieve below,
// so the dispatcher invokes EnumerateAll directly and renders via
// renderAuditKeys to keep the cloud-API call count to one per
// successful provider. The standalone runInfraAuditKeys remains for
// unit tests that exercise the rendering + arg-parsing surface against
// a single fake EnumeratorAll.
//
// Strict-mode policy (v0.27.1, per user mandate "remove the fallback and
// force strict mode"): if every loaded provider's EnumerateAll returns
// interfaces.ErrProviderMethodUnimplemented, that error propagates LOUD
// rather than being swallowed into "Found 0 resources". Plugins that
// declare the bridge but lack the underlying implementation MUST surface
// the gap so operators are not misled into thinking the cloud account is
// empty.
//
// Multi-provider semantics (PR #589 Copilot Thread 1): bridging
// EnumerateAll into *remoteIaCProvider means EVERY gRPC-loaded provider
// satisfies interfaces.EnumeratorAll at the Go-type level, even when the
// plugin process does not actually implement the method. A naive
// type-assert-then-break loop would pick the first remote provider and
// fail the whole run on its Unimplemented response, never trying later
// providers that DO support it. The dispatcher therefore iterates ALL
// candidates: ErrProviderMethodUnimplemented is treated as "this plugin
// doesn't support it, try the next provider", any other error is loud,
// and the run only fails when no provider can serve the resource type.
func runInfraAuditKeysCmd(args []string) error {
	fs := flag.NewFlagSet("infra audit-keys", flag.ContinueOnError)
	fs.SetOutput(auditKeysStderr)
	var configFile, envName, resourceType string
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	fs.StringVar(&resourceType, "type", "", "Resource type to enumerate (e.g. infra.spaces_key)")
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if resourceType == "" {
		return fmt.Errorf("audit-keys: --type is required")
	}
	prevInfraPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = prevInfraPluginDir }()

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

	// Try each loaded provider. Track the last Unimplemented so that if
	// every candidate satisfied the type-assert but none implemented the
	// method, we report a different (more diagnostic) error than the
	// "no provider satisfies the interface at all" case.
	var lastUnimpl error
	var anyEnumerator bool
	for _, p := range providers {
		enum, ok := p.(interfaces.EnumeratorAll)
		if !ok {
			continue
		}
		anyEnumerator = true
		outs, err := enum.EnumerateAll(ctx, resourceType)
		if err != nil {
			if errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
				// Plugin doesn't support the method behind the bridge.
				// Continue probing remaining providers. Recorded so the
				// final error message points at the underlying gap if no
				// provider succeeds.
				lastUnimpl = fmt.Errorf("%s: %w", p.Name(), err)
				continue
			}
			// Any other error is loud — this is real provider failure
			// (auth, network, malformed args), not a bridge gap.
			return fmt.Errorf("audit-keys: enumerate from %s: %w", p.Name(), err)
		}
		// Success — render and stop. This dispatcher mirrors the cleanup
		// pattern (try each, take first that works) but for audit-keys
		// the resource type is single-provider-scoped so the first
		// successful enumeration is authoritative.
		renderAuditKeys(outs, resourceType, auditKeysStdout)
		return nil
	}
	if anyEnumerator && lastUnimpl != nil {
		return fmt.Errorf("audit-keys: no loaded provider implements EnumeratorAll for %q (last probed: %w)", resourceType, lastUnimpl)
	}
	return fmt.Errorf("audit-keys: no loaded provider implements EnumeratorAll")
}
