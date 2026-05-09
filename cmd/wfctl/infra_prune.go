package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// pruneProvider is the narrow interface runInfraPrune depends on so unit
// tests can use a minimal fake without implementing the full IaCProvider
// surface. Production code wraps an interfaces.IaCProvider in
// pruneProviderAdapter (below) which bridges to the existing
// interfaces.EnumeratorAll + ResourceDriver.Delete primitives.
type pruneProvider interface {
	EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error)
	DeleteResource(ctx context.Context, ref interfaces.ResourceRef) error
}

// recoveryFile is the on-disk state written by `wfctl infra rotate-and-prune`
// (Task 21) and consumed by `wfctl infra prune --recovery-from-last-rotation`
// (Task 19). The shape is intentionally minimal — only the fields the prune
// filter needs (--created-before + --exclude-access-key derived from the new
// rotation's CreatedAt + AccessKey).
type recoveryFile struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	AccessKey string `json:"access_key"`
	CreatedAt string `json:"created_at"`
}

// defaultStateDir returns the canonical wfctl state directory:
// $WFCTL_STATE_DIR if set, else $HOME/.wfctl. Both writers (rotate-and-prune,
// Task 21) and readers (prune --recovery-from-last-rotation) call this so
// paths agree across the lifecycle.
func defaultStateDir() (string, error) {
	if dir := os.Getenv("WFCTL_STATE_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("WFCTL_STATE_DIR unset and $HOME unavailable: %w", err)
	}
	return filepath.Join(home, ".wfctl"), nil
}

// recoveryFilePath returns the canonical path to the rotation recovery file.
// Falls back to a CWD-relative .wfctl/last-rotation.json if defaultStateDir
// errors — readers will fail loudly on the missing file in that case.
func recoveryFilePath() string {
	dir, err := defaultStateDir()
	if err != nil {
		dir = ".wfctl"
	}
	return filepath.Join(dir, "last-rotation.json")
}

// readRecoveryFile loads the recovery file written by rotate-and-prune and
// returns its content. Surface-level error includes the resolved path so
// operators can locate / hand-edit if needed.
func readRecoveryFile() (*recoveryFile, error) {
	p := recoveryFilePath()
	data, err := os.ReadFile(p) //nolint:gosec // intentional state-dir path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	var rec recoveryFile
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	return &rec, nil
}

// runInfraPrune destructively prunes cloud resources matching a time +
// access_key discriminator. Three opt-ins are required to run:
//
//   - `--confirm` flag (explicit consent on each invocation)
//   - WFCTL_CONFIRM_PRUNE=1 environment variable (two-key authorization)
//   - interactive y/N prompt (unless --non-interactive is set, e.g. in CI)
//
// And two filter args are mandatory (paranoia rail) — the command refuses
// to run without an explicit exclusion target so a typo can't accidentally
// nuke the active credential:
//
//   - `--created-before <RFC3339>`: only resources older than this are eligible
//   - `--exclude-access-key <AK>`: this access_key is preserved no matter what
//
// `--recovery-from-last-rotation` short-circuits the two filter args by
// reading them from the recovery file written by `infra rotate-and-prune`
// (Task 21). On success the recovery file is deleted; on failure it's
// retained so the operator can re-invoke after diagnosing.
//
// Exit codes:
//
//   - 0: prune succeeded (zero or more deletions; no failures)
//   - 1: opt-in/filter validation failed, enumerate failed, or one or more
//     deletes failed
//   - 2: argument parse error or missing required --type
//
//nolint:cyclop // intentional validation gauntlet — splitting it moves opt-in checks further from --confirm parsing
func runInfraPrune(args []string, provider pruneProvider, w io.Writer) int {
	fs := flag.NewFlagSet("infra prune", flag.ContinueOnError)
	fs.SetOutput(w)
	var resourceType, createdBefore, excludeAK, preserveNames string
	var confirm, nonInteractive, recoveryFromLastRotation bool
	fs.StringVar(&resourceType, "type", "", "Resource type (required, e.g. infra.spaces_key)")
	fs.StringVar(&createdBefore, "created-before", "", "RFC3339 timestamp; only resources older than this are eligible")
	fs.StringVar(&excludeAK, "exclude-access-key", "", "Access key to preserve (required: paranoia rail)")
	// --preserve-names is the unambiguous semantic: names matching this regex
	// are PRESERVED (skipped during delete), not "operated on". Renamed from
	// --allowlist per code-reviewer to remove the operator-error-trap where
	// "allowlist" reads as "list of resources allowed to be deleted".
	fs.StringVar(&preserveNames, "preserve-names", "", "Regex of resource names to preserve (skip during delete; orthogonal to time filter)")
	fs.BoolVar(&confirm, "confirm", false, "Required: explicit confirmation flag")
	fs.BoolVar(&nonInteractive, "non-interactive", false, "Skip the y/N prompt (CI-friendly)")
	fs.BoolVar(&recoveryFromLastRotation, "recovery-from-last-rotation", false, "Read recovery file for filter args (rotate-and-prune writes it)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if resourceType == "" {
		fmt.Fprintln(w, "prune: --type is required")
		return 2
	}
	if !confirm {
		fmt.Fprintln(w, "prune: --confirm flag is required (this command is destructive)")
		return 1
	}
	if os.Getenv("WFCTL_CONFIRM_PRUNE") != "1" {
		fmt.Fprintln(w, "prune: WFCTL_CONFIRM_PRUNE=1 env var is required (two-key opt-in)")
		return 1
	}

	if recoveryFromLastRotation {
		rec, err := readRecoveryFile()
		if err != nil {
			fmt.Fprintf(w, "prune: read recovery file: %v\n", err)
			return 1
		}
		if rec.Type != resourceType {
			fmt.Fprintf(w, "prune: --recovery-from-last-rotation type mismatch — recovery file is %q, --type is %q\n", rec.Type, resourceType)
			return 1
		}
		createdBefore = rec.CreatedAt
		excludeAK = rec.AccessKey
	}

	if createdBefore == "" || excludeAK == "" {
		fmt.Fprintln(w, "prune: --created-before AND --exclude-access-key are both required (paranoia rail)")
		return 1
	}

	cutoff, err := time.Parse(time.RFC3339, createdBefore)
	if err != nil {
		fmt.Fprintf(w, "prune: invalid --created-before timestamp: %v\n", err)
		return 1
	}

	var preserveRe *regexp.Regexp
	if preserveNames != "" {
		re, reErr := regexp.Compile(preserveNames)
		if reErr != nil {
			fmt.Fprintf(w, "prune: invalid --preserve-names regex: %v\n", reErr)
			return 1
		}
		preserveRe = re
	}

	ctx := context.Background()
	outs, err := provider.EnumerateAll(ctx, resourceType)
	if err != nil {
		fmt.Fprintf(w, "prune: enumerate: %v\n", err)
		return 1
	}

	var toDelete []*interfaces.ResourceOutput
	for _, out := range outs {
		// Use typed fields for the load-bearing identifiers — the provider
		// contract guarantees ProviderID + Name on EnumerateAll results.
		// Outputs is for additional metadata (created_at). Falling back to
		// Outputs[*] for name / access_key keeps the older provider behavior
		// working but the typed fields take priority — defensive against any
		// future provider that follows the contract strictly and doesn't
		// duplicate ProviderID into Outputs["access_key"]. Without this
		// fallback-but-prefer-typed pattern, a strict-contract provider would
		// silently delete the active credential because the excludeAK check
		// would compare against an empty string.
		ak := out.ProviderID
		if ak == "" {
			ak, _ = out.Outputs["access_key"].(string)
		}
		name := out.Name
		if name == "" {
			name, _ = out.Outputs["name"].(string)
		}
		ca, _ := out.Outputs["created_at"].(string) // metadata; legitimately Outputs-only
		if ak == excludeAK {
			continue
		}
		if preserveRe != nil && preserveRe.MatchString(name) {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339, ca)
		if parseErr != nil || !t.Before(cutoff) {
			continue
		}
		toDelete = append(toDelete, out)
	}

	fmt.Fprintf(w, "Dry-run: %d resource(s) to prune:\n\n", len(toDelete))
	for _, o := range toDelete {
		name := o.Name
		if name == "" {
			name, _ = o.Outputs["name"].(string)
		}
		ak := o.ProviderID
		if ak == "" {
			ak, _ = o.Outputs["access_key"].(string)
		}
		ca, _ := o.Outputs["created_at"].(string)
		fmt.Fprintf(w, "  - %s (access_key=%s, created=%s)\n", name, ak, ca)
	}

	if len(toDelete) == 0 {
		return 0
	}

	if !nonInteractive {
		fmt.Fprintf(w, "\nProceed? (y/N): ")
		var ans string
		_, _ = fmt.Scanln(&ans)
		if ans != "y" && ans != "Y" {
			fmt.Fprintln(w, "Aborted.")
			return 0
		}
	}

	var failed int
	for _, o := range toDelete {
		ref := interfaces.ResourceRef{Type: o.Type, Name: o.Name, ProviderID: o.ProviderID}
		if delErr := provider.DeleteResource(ctx, ref); delErr != nil {
			fmt.Fprintf(w, "prune: delete %s: %v\n", o.Name, delErr)
			failed++
			continue
		}
		fmt.Fprintf(w, "  ✓ deleted %s\n", o.Name)
	}
	if failed > 0 {
		// Only mention the recovery file when one actually exists — when
		// --recovery-from-last-rotation was set OR rotate-and-prune wrote
		// it before this invocation. Plain prune invocations don't have
		// one; pointing operators at a non-existent path would mislead
		// them into a wild goose chase.
		if recoveryFromLastRotation {
			fmt.Fprintf(w, "\n%d delete(s) failed; recovery file retained at %s\n", failed, recoveryFilePath())
		} else {
			fmt.Fprintf(w, "\n%d delete(s) failed\n", failed)
		}
		return 1
	}
	if recoveryFromLastRotation {
		// Cleanup is best-effort but non-silent: if perms changed or the
		// file is locked, the next --recovery-from-last-rotation invocation
		// would re-read stale data, so warn loud enough that the operator
		// can hand-clean.
		if rmErr := os.Remove(recoveryFilePath()); rmErr != nil && !os.IsNotExist(rmErr) {
			fmt.Fprintf(w, "warning: failed to clean up recovery file %s: %v\n", recoveryFilePath(), rmErr)
		}
	}
	fmt.Fprintf(w, "\n%d resource(s) pruned successfully.\n", len(toDelete))
	return 0
}

// pruneProviderAdapter bridges interfaces.IaCProvider to the narrow
// pruneProvider interface that runInfraPrune consumes. The adapter does
// the EnumeratorAll type-assertion + ResourceDriver lookup at the
// boundary so runInfraPrune's body stays free of plugin-loading concerns
// (and unit tests don't need to fake the full IaCProvider surface).
type pruneProviderAdapter struct {
	p interfaces.IaCProvider
}

func (a *pruneProviderAdapter) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	enum, ok := a.p.(interfaces.EnumeratorAll)
	if !ok {
		return nil, fmt.Errorf("provider %q does not implement EnumeratorAll", a.p.Name())
	}
	return enum.EnumerateAll(ctx, resourceType)
}

func (a *pruneProviderAdapter) DeleteResource(ctx context.Context, ref interfaces.ResourceRef) error {
	drv, err := a.p.ResourceDriver(ref.Type)
	if err != nil {
		return fmt.Errorf("resolve driver for %s: %w", ref.Type, err)
	}
	return drv.Delete(ctx, ref)
}

// pruneStdout / pruneStderr seam variables mirror cleanupStdout /
// cleanupStderr so prune-related tests don't race on global os.Stdout.
var (
	pruneStdout io.Writer = os.Stdout
	pruneStderr io.Writer = os.Stderr
)

// pruneLoadProviders is the seam tests override to inject fakes.
var pruneLoadProviders = defaultCleanupLoadProviders

// runInfraPruneCmd is the production entry point for `wfctl infra prune`.
// Loads iac.provider modules from infra.yaml, finds the first one that
// implements interfaces.EnumeratorAll, wraps it in pruneProviderAdapter,
// and dispatches to runInfraPrune.
//
// Args-passing contract: this dispatcher captures EVERY flag it parses
// (including all of runInfraPrune's flags) and synthesizes a clean inner-
// args slice with only the flags runInfraPrune understands. Forwarding
// the raw args slice would error inside runInfraPrune with "flag provided
// but not defined: -config" because its inner FlagSet doesn't declare
// --config / --env (those belong to the dispatcher's provider-loading
// concern).
func runInfraPruneCmd(args []string) error {
	fs := flag.NewFlagSet("infra prune", flag.ContinueOnError)
	fs.SetOutput(pruneStderr)
	var configFile, envName string
	var resourceType, createdBefore, excludeAK, preserveNames string
	var confirm, nonInteractive, recoveryFromLastRotation bool
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	fs.StringVar(&resourceType, "type", "", "Resource type")
	fs.StringVar(&createdBefore, "created-before", "", "RFC3339 timestamp")
	fs.StringVar(&excludeAK, "exclude-access-key", "", "Access key to preserve")
	fs.StringVar(&preserveNames, "preserve-names", "", "Regex of resource names to preserve (skip during delete)")
	fs.BoolVar(&confirm, "confirm", false, "Confirmation flag")
	fs.BoolVar(&nonInteractive, "non-interactive", false, "Skip y/N prompt")
	fs.BoolVar(&recoveryFromLastRotation, "recovery-from-last-rotation", false, "Read recovery file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	providers, closers, err := pruneLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintf(pruneStderr, "warning: provider shutdown: %v\n", cerr)
			}
		}
	}()

	// Synthesize a clean inner-args slice with only flags runInfraPrune
	// declares. Empty strings / false bools are omitted so they don't
	// override runInfraPrune's defaults (and so the inner validation
	// surfaces the same "X is required" messages the user expects).
	inner := []string{"--type", resourceType}
	if createdBefore != "" {
		inner = append(inner, "--created-before", createdBefore)
	}
	if excludeAK != "" {
		inner = append(inner, "--exclude-access-key", excludeAK)
	}
	if preserveNames != "" {
		inner = append(inner, "--preserve-names", preserveNames)
	}
	if confirm {
		inner = append(inner, "--confirm")
	}
	if nonInteractive {
		inner = append(inner, "--non-interactive")
	}
	if recoveryFromLastRotation {
		inner = append(inner, "--recovery-from-last-rotation")
	}

	// v0.27.1 dispatch policy: every gRPC-loaded provider satisfies
	// interfaces.EnumeratorAll at the type level after the proxy bridge
	// (PR #589). To preserve the pre-v0.27.1 iterate-and-skip semantics for
	// plugins whose process does NOT implement EnumerateAll, we wrap each
	// candidate adapter in a probedPruneProvider that translates
	// interfaces.ErrProviderMethodUnimplemented into a "skip and try next"
	// signal. The probe is single-call: the wrapper records whether the
	// underlying EnumerateAll returned Unimplemented and the dispatcher
	// uses the recorded flag to emit the structured "skipped" message
	// without issuing a second RPC.
	for _, p := range providers {
		if _, ok := p.(interfaces.EnumeratorAll); !ok {
			continue
		}
		adapter := &pruneProviderAdapter{p: p}
		probed := &probedPruneProvider{inner: adapter}
		rc := runInfraPrune(inner, probed, pruneStdout)
		if probed.unimplemented {
			fmt.Fprintf(pruneStdout, "skipped %s: provider does not implement EnumeratorAll\n", p.Name())
			continue
		}
		if rc != 0 {
			return fmt.Errorf("prune exited with code %d", rc)
		}
		return nil
	}
	return fmt.Errorf("prune: no loaded provider implements EnumeratorAll")
}

// probedPruneProvider wraps a pruneProvider and traps
// interfaces.ErrProviderMethodUnimplemented from EnumerateAll, returning
// (nil, nil) so the runner renders an empty result and the dispatcher can
// emit the structured "skipped" message and continue to the next provider.
// DeleteResource is forwarded unchanged — if EnumerateAll returned
// Unimplemented there are no refs to delete anyway.
type probedPruneProvider struct {
	inner         pruneProvider
	unimplemented bool
}

func (p *probedPruneProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	outs, err := p.inner.EnumerateAll(ctx, resourceType)
	if err != nil && errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
		p.unimplemented = true
		return nil, nil
	}
	return outs, err
}

func (p *probedPruneProvider) DeleteResource(ctx context.Context, ref interfaces.ResourceRef) error {
	return p.inner.DeleteResource(ctx, ref)
}
