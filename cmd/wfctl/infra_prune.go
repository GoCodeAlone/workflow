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
// filter needs. Typed rotations derive the cutoff from the excluded
// credential's fresh provider-inventory record; legacy rotations persist a
// created-before timestamp directly.
type recoveryFile struct {
	Type                        string `json:"type"`
	Name                        string `json:"name"`
	AccessKey                   string `json:"access_key"`
	IdentifierSensitive         bool   `json:"identifier_sensitive,omitempty"`
	CutoffFromExcludedInventory bool   `json:"cutoff_from_excluded_inventory,omitempty"`
	CreatedAt                   string `json:"created_at"`
}

type infraPruneOptions struct {
	IdentifierSensitive         bool
	CutoffFromExcludedInventory bool
	Context                     context.Context
}

func normalizePruneInventory(resourceType string, outputs []*interfaces.ResourceOutput) ([]*interfaces.ResourceOutput, error) {
	normalized := make([]*interfaces.ResourceOutput, 0, len(outputs))
	for index, output := range outputs {
		if output == nil {
			return nil, fmt.Errorf("provider inventory record %d is nil; provider error text suppressed", index)
		}
		if output.Type == "" || output.Type != resourceType {
			return nil, fmt.Errorf("provider inventory record %d has an invalid resource type; provider error text suppressed", index)
		}
		providerID := output.ProviderID
		if providerID == "" {
			providerID, _ = output.Outputs["access_key"].(string)
		}
		name := output.Name
		if name == "" {
			name, _ = output.Outputs["name"].(string)
		}
		if providerID == "" || name == "" {
			return nil, fmt.Errorf("provider inventory record %d has an unusable identity; provider error text suppressed", index)
		}
		copy := *output
		copy.Type = resourceType
		copy.Name = name
		copy.ProviderID = providerID
		normalized = append(normalized, &copy)
	}
	return normalized, nil
}

func cleanupRecoveryFile(w io.Writer) {
	if err := os.Remove(recoveryFilePath()); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(w, "warning: failed to clean up recovery file %s: %v\n", recoveryFilePath(), err)
	}
}

var (
	pruneInventoryCutoffAttempts = 3
	pruneInventoryCutoffDelay    = 250 * time.Millisecond
)

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
	dir := filepath.Dir(p)
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("inspect recovery directory %s: %w", dir, err)
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 || !privateRecoveryStateMode(dirInfo) {
		return nil, fmt.Errorf("recovery directory %s must be a private regular directory", dir)
	}
	pathInfo, err := os.Lstat(p)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", p, err)
	}
	if !pathInfo.Mode().IsRegular() || !privateRecoveryStateMode(pathInfo) {
		return nil, fmt.Errorf("recovery state %s must be a private regular file", p)
	}
	file, err := os.Open(p) //nolint:gosec // validated private state-dir path
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", p, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect opened recovery state %s: %w", p, err)
	}
	if !os.SameFile(pathInfo, openedInfo) {
		return nil, fmt.Errorf("recovery state %s changed while opening; refusing destructive recovery", p)
	}
	data, err := io.ReadAll(file)
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
	return runInfraPruneWithOptions(args, provider, w, infraPruneOptions{})
}

func runInfraPruneWithOptions(args []string, provider pruneProvider, w io.Writer, options infraPruneOptions) int {
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

	var releaseRecoveryLock func()
	if recoveryFromLastRotation {
		lockDir, err := credentialOperationLockDir()
		if err != nil {
			fmt.Fprintf(w, "prune: resolve recovery-state lock directory: %v\n", err)
			return 1
		}
		releaseRecoveryLock, err = acquireCredentialOperationLock(lockDir, "wfctl.rotate-and-prune-recovery", "global")
		if err != nil {
			fmt.Fprintf(w, "prune: acquire global recovery-state lock: %v\n", err)
			return 1
		}
		defer releaseRecoveryLock()
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
		options.IdentifierSensitive = options.IdentifierSensitive || rec.IdentifierSensitive
		options.CutoffFromExcludedInventory = options.CutoffFromExcludedInventory || rec.CutoffFromExcludedInventory
	}

	if excludeAK == "" || (createdBefore == "" && !options.CutoffFromExcludedInventory) {
		fmt.Fprintln(w, "prune: --exclude-access-key and either --created-before or a typed recovery inventory cutoff are required (paranoia rail)")
		return 1
	}

	var (
		cutoff time.Time
		err    error
	)
	if createdBefore != "" && !options.CutoffFromExcludedInventory {
		cutoff, err = time.Parse(time.RFC3339, createdBefore)
		if err != nil {
			fmt.Fprintf(w, "prune: invalid --created-before timestamp: %v\n", err)
			return 1
		}
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

	ctx := options.Context
	if ctx == nil {
		var stop context.CancelFunc
		ctx, stop = boundedProviderCommandContext(providerCommandOperationTimeout)
		defer stop()
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(w, "prune: command cancelled before provider inventory")
		return 1
	}
	var outs []*interfaces.ResourceOutput
	if options.CutoffFromExcludedInventory {
		outs, cutoff, err = derivePruneCutoffFromExcludedInventory(ctx, provider, resourceType, excludeAK)
		if err != nil {
			fmt.Fprintf(w, "prune: resolve excluded credential inventory cutoff: %v\n", err)
			return 1
		}
	} else {
		outs, err = provider.EnumerateAll(ctx, resourceType)
		if err != nil {
			fmt.Fprintln(w, "prune: enumerate failed; provider error text suppressed")
			return 1
		}
	}
	outs, err = normalizePruneInventory(resourceType, outs)
	if err != nil {
		fmt.Fprintf(w, "prune: %v\n", err)
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
		fmt.Fprintf(w, "  - %s (access_key=%s, created=%s)\n", name, credentialIdentifierForLog(ak, options.IdentifierSensitive), ca)
	}

	if len(toDelete) == 0 {
		if recoveryFromLastRotation {
			if err := ctx.Err(); err != nil {
				fmt.Fprintln(w, "prune: command cancelled; recovery file retained")
				return 1
			}
			cleanupRecoveryFile(w)
		}
		return 0
	}

	if !nonInteractive {
		ok, err := confirmAction("Proceed?", false, w, nil)
		if err != nil {
			fmt.Fprintf(w, "prune: confirm: %v\n", err)
			return 1
		}
		if !ok {
			return 0
		}
	}

	var failed int
	for _, o := range toDelete {
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(w, "prune: command cancelled before remaining deletes")
			return 1
		}
		ref := interfaces.ResourceRef{Type: o.Type, Name: o.Name, ProviderID: o.ProviderID}
		if delErr := provider.DeleteResource(ctx, ref); delErr != nil {
			fmt.Fprintf(w, "prune: delete %s failed; provider error text suppressed\n", o.Name)
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
		if err := ctx.Err(); err != nil {
			fmt.Fprintln(w, "prune: command cancelled after deletes; recovery file retained")
			return 1
		}
		cleanupRecoveryFile(w)
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
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prevInfraPluginDir := currentInfraPluginDir
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = prevInfraPluginDir }()

	ctx, stopProviderCommand := boundedProviderCommandContext(providerCommandOperationTimeout)
	defer stopProviderCommand()
	ctx = withProviderCapabilityDiagnosticsSuppressed(ctx)
	providers, closers, err := pruneLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers failed; provider error text suppressed")
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintln(pruneStderr, "warning: provider shutdown failed; provider error text suppressed")
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

	// Strict-mode dispatch (v0.27.1, per user mandate "remove the fallback
	// and force strict mode"): if every loaded provider's EnumerateAll
	// returns ErrProviderMethodUnimplemented, that is a loud failure — NOT
	// swallowed into "Dry-run: 0 resource(s) to prune". Plugins that
	// declare the bridge but lack the underlying implementation MUST
	// surface the gap so operators are not misled into thinking there is
	// nothing to prune.
	//
	// Multi-provider semantics (PR #589 Copilot Thread 1): bridging
	// EnumerateAll into *remoteIaCProvider means EVERY gRPC-loaded
	// provider satisfies interfaces.EnumeratorAll at the Go-type level,
	// even when the plugin process does not implement the method. A naive
	// type-assert-then-break loop would pick the first remote provider
	// and fail the whole run on its Unimplemented response, never trying
	// later providers that DO support it. The dispatcher therefore probes
	// EnumerateAll once on each candidate and only delegates to
	// runInfraPrune for the first provider that actually returns data;
	// the cached-enumerator wrapper avoids a second cloud call inside
	// runInfraPrune.
	var lastUnimplProvider string
	var anyEnumerator bool
	for _, p := range providers {
		if _, ok := p.(interfaces.EnumeratorAll); !ok {
			continue
		}
		anyEnumerator = true
		adapter := &pruneProviderAdapter{p: p}
		// Probe EnumerateAll. Note: resourceType may be empty if the
		// operator omitted --type — runInfraPrune validates that and
		// exits 2 itself, so skip the probe in that case and let the
		// inner validator surface the standard "--type is required"
		// message rather than the dispatcher emitting a less helpful
		// "all providers Unimplemented" wrapper.
		if resourceType == "" {
			rc := runInfraPruneWithOptions(inner, adapter, pruneStdout, infraPruneOptions{Context: ctx})
			if rc != 0 {
				return fmt.Errorf("prune exited with code %d", rc)
			}
			return nil
		}
		outs, probeErr := adapter.EnumerateAll(ctx, resourceType)
		if probeErr != nil {
			if errors.Is(probeErr, interfaces.ErrProviderMethodUnimplemented) {
				// Plugin doesn't support EnumerateAll behind the bridge.
				// Continue probing remaining providers.
				lastUnimplProvider = p.Name()
				continue
			}
			// Any other error is loud — this is real provider failure.
			return fmt.Errorf("prune: enumerate from %s failed; provider error text suppressed", p.Name())
		}
		// Success — wrap the probed outs in a cached adapter so
		// runInfraPrune's internal EnumerateAll call serves from cache
		// and we don't double-bill the cloud API.
		cached := &cachedPruneProvider{cached: outs, inner: adapter, resourceType: resourceType}
		rc := runInfraPruneWithOptions(inner, cached, pruneStdout, infraPruneOptions{Context: ctx})
		if rc != 0 {
			return fmt.Errorf("prune exited with code %d", rc)
		}
		return nil
	}
	if anyEnumerator && lastUnimplProvider != "" {
		return fmt.Errorf("prune: no loaded provider implements EnumeratorAll for %q (last probed: %s; provider error text suppressed)", resourceType, lastUnimplProvider)
	}
	return fmt.Errorf("prune: no loaded provider implements EnumeratorAll")
}

// cachedPruneProvider serves a previously-probed EnumerateAll result for a
// single resourceType. Typed inventory-cutoff recovery calls EnumerateAllFresh
// to bypass it; other paths avoid repeating the dispatcher's capability probe.
//
// Falls through to the underlying provider for any non-cached resourceType
// (defensive — runInfraPrune's --type matches the dispatcher's --type so
// this branch is never hit in production, but keeping it correct avoids
// surprising future call sites).
type cachedPruneProvider struct {
	cached       []*interfaces.ResourceOutput
	resourceType string
	inner        pruneProvider
}

func (c *cachedPruneProvider) EnumerateAll(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	if resourceType == c.resourceType {
		return c.cached, nil
	}
	return c.inner.EnumerateAll(ctx, resourceType)
}

func (c *cachedPruneProvider) EnumerateAllFresh(ctx context.Context, resourceType string) ([]*interfaces.ResourceOutput, error) {
	return c.inner.EnumerateAll(ctx, resourceType)
}

type freshPruneInventoryProvider interface {
	EnumerateAllFresh(context.Context, string) ([]*interfaces.ResourceOutput, error)
}

func derivePruneCutoffFromExcludedInventory(ctx context.Context, provider pruneProvider, resourceType, excludeIdentifier string) ([]*interfaces.ResourceOutput, time.Time, error) {
	var lastProblem string
	for attempt := 0; attempt < pruneInventoryCutoffAttempts; attempt++ {
		var (
			outputs []*interfaces.ResourceOutput
			err     error
		)
		if fresh, ok := provider.(freshPruneInventoryProvider); ok {
			outputs, err = fresh.EnumerateAllFresh(ctx, resourceType)
		} else {
			outputs, err = provider.EnumerateAll(ctx, resourceType)
		}
		if err != nil {
			return nil, time.Time{}, fmt.Errorf("fresh inventory failed; provider error text suppressed")
		}
		outputs, err = normalizePruneInventory(resourceType, outputs)
		if err != nil {
			return nil, time.Time{}, err
		}
		var matches []*interfaces.ResourceOutput
		for _, output := range outputs {
			if output.ProviderID == excludeIdentifier {
				matches = append(matches, output)
			}
		}
		if len(matches) > 1 {
			return nil, time.Time{}, fmt.Errorf("fresh inventory returned multiple exact excluded-credential matches")
		}
		if len(matches) == 1 {
			createdAt, _ := matches[0].Outputs["created_at"].(string)
			cutoff, parseErr := time.Parse(time.RFC3339, createdAt)
			if parseErr == nil {
				return outputs, cutoff, nil
			}
			lastProblem = "exact excluded-credential match has no valid RFC3339 created_at"
		} else {
			lastProblem = "exact excluded-credential match not found"
		}
		if attempt+1 < pruneInventoryCutoffAttempts {
			timer := time.NewTimer(pruneInventoryCutoffDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, time.Time{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, time.Time{}, fmt.Errorf("%s after %d fresh inventory attempts", lastProblem, pruneInventoryCutoffAttempts)
}

func (c *cachedPruneProvider) DeleteResource(ctx context.Context, ref interfaces.ResourceRef) error {
	return c.inner.DeleteResource(ctx, ref)
}
