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

// rotateAndPruneStdout / rotateAndPruneStderr seam variables mirror the
// auditKeysStdout / pruneStdout pattern so rotate-and-prune dispatcher
// tests don't race on global os.Stdout.
var (
	rotateAndPruneStdout io.Writer = os.Stdout
	rotateAndPruneStderr io.Writer = os.Stderr
)

// rotateAndPruneLoadProviders is the seam tests override to inject fakes.
// Defaults to defaultCleanupLoadProviders so rotate-and-prune inherits the
// same env-resolution + plugin-discovery contract as cleanup / audit-keys
// / prune.
var rotateAndPruneLoadProviders = defaultCleanupLoadProviders

// runInfraRotateAndPruneCmd is the production entry point for `wfctl infra
// rotate-and-prune`. Loads iac.provider modules from infra.yaml, finds the
// first one that implements interfaces.EnumeratorAll, wraps it in
// pruneProviderAdapter, and dispatches to runInfraRotateAndPrune.
//
// runInfraRotateAndPrune already declares --config / --env (it needs them
// for parseSecretsConfig in Step 1 of the rotate flow), so the dispatcher
// forwards args verbatim — no synthesize-clean-inner-args dance required.
// We still pre-parse here to extract --config / --env for the provider
// loader, but we don't reformat the args slice.
func runInfraRotateAndPruneCmd(args []string) error {
	fs := flag.NewFlagSet("infra rotate-and-prune (dispatch)", flag.ContinueOnError)
	fs.SetOutput(rotateAndPruneStderr)
	var configFile, envName, resourceType string
	// Declare every flag runInfraRotateAndPrune accepts so flag.Parse
	// doesn't error here. Only --config / --env / --type are captured here
	// (used by the provider loader and the EnumerateAll pre-flight probe);
	// the rest are re-parsed inside runInfraRotateAndPrune against the same
	// args slice (Go's flag package is idempotent).
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	fs.StringVar(&resourceType, "type", "", "Resource type")
	_ = fs.String("name", "", "Canonical credential name")
	_ = fs.String("preserve-names", "", "Regex of names to preserve during prune")
	_ = fs.Bool("confirm", false, "Confirmation flag")
	_ = fs.Bool("non-interactive", false, "Skip y/N prompt")
	// --prune-first declared here so flag.Parse doesn't error on the dispatcher
	// pre-scan; the inner runInfraRotateAndPrune re-parses against the same
	// args slice (Go's flag package is idempotent) and reads the value there.
	// Default is true (per ADR 0023): the safer behavior IS the new default;
	// the legacy quota-fragile behavior is opt-out via --prune-first=false.
	_ = fs.Bool("prune-first", true, "Prune orphans before rotating (default true; safer at quota — see ADR 0023)")
	var pluginDirFlag string
	fs.StringVar(&pluginDirFlag, "plugin-dir", "", "Plugin directory (overrides WFCTL_PLUGIN_DIR and default data/plugins)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	currentInfraPluginDir = pluginDirFlag
	defer func() { currentInfraPluginDir = "" }()

	ctx := context.Background()
	providers, closers, err := rotateAndPruneLoadProviders(ctx, fs, configFile, envName)
	if err != nil {
		return fmt.Errorf("load providers: %w", err)
	}
	defer func() {
		for _, c := range closers {
			if c == nil {
				continue
			}
			if cerr := c.Close(); cerr != nil {
				fmt.Fprintf(rotateAndPruneStderr, "warning: provider shutdown: %v\n", cerr)
			}
		}
	}()

	// Strict-mode dispatch (v0.27.1, per user mandate "remove the fallback
	// and force strict mode"): every gRPC-loaded provider satisfies
	// interfaces.EnumeratorAll at the type level (proxy bridge in PR #589),
	// but the underlying plugin process may not. Step 1 of
	// runInfraRotateAndPrune is rotation — DESTRUCTIVE and irreversible. If
	// we discover the bridge gap only during Step 2 (prune), the operator is
	// left with a freshly-rotated credential, the old credential still live,
	// and no automated path to clean up.
	//
	// To prevent that, we run a pre-flight probe of EnumerateAll on each
	// candidate provider BEFORE delegating to runInfraRotateAndPrune. The
	// probe is also the multi-provider sieve (PR #589 Copilot Thread 1):
	// because the bridge makes every remote provider satisfy
	// interfaces.EnumeratorAll at the Go-type level, a naive
	// type-assert-then-break would fail the whole run on the first
	// provider whose plugin doesn't support EnumerateAll, never reaching
	// later providers that DO. Treat ErrProviderMethodUnimplemented as
	// "this plugin doesn't support it, try next provider"; any other
	// probe error is loud (auth, network, malformed input) and aborts
	// rotation before any state is mutated. Only fail if NO provider
	// can serve the resource type.
	var lastUnimpl error
	var anyEnumerator bool
	for _, p := range providers {
		if _, ok := p.(interfaces.EnumeratorAll); !ok {
			continue
		}
		anyEnumerator = true
		// Pre-flight: probe EnumerateAll BEFORE Step 1 rotation. The probe
		// uses the resourceType extracted from the dispatcher flag scan
		// above so it exercises the same RPC the prune step will issue.
		// resourceType may be empty if the operator omitted --type;
		// runInfraRotateAndPrune validates that itself and exits 2 before
		// reaching rotation, so we don't duplicate the check here.
		adapter := &pruneProviderAdapter{p: p}
		// `provider` is what we hand to runInfraRotateAndPrune. By default
		// it's the raw adapter; when we successfully probed EnumerateAll
		// on the probe's resourceType, we wrap in cachedPruneProvider so
		// runInfraPrune (invoked by runInfraRotateAndPrune after rotation)
		// serves the cached slice instead of re-issuing the cloud
		// enumeration. This avoids the double-billed EnumerateAll on the
		// successful path. The cache is keyed by resourceType — the
		// freshly-rotated key is excluded by --exclude-access-key in the
		// prune step regardless of whether enumerate sees it, so serving
		// the pre-rotation snapshot is safe.
		var provider pruneProvider = adapter
		if resourceType != "" {
			outs, probeErr := adapter.EnumerateAll(ctx, resourceType)
			if probeErr != nil {
				if errors.Is(probeErr, interfaces.ErrProviderMethodUnimplemented) {
					// This plugin doesn't support EnumerateAll behind
					// the bridge — try the next provider rather than
					// aborting the whole run. No rotation has occurred
					// yet (pre-flight is BEFORE Step 1).
					lastUnimpl = fmt.Errorf("%s: %w", p.Name(), probeErr)
					continue
				}
				return fmt.Errorf("rotate-and-prune pre-flight: provider %q EnumerateAll(%q) failed (rotation aborted; no state mutated): %w",
					p.Name(), resourceType, probeErr)
			}
			provider = &cachedPruneProvider{cached: outs, inner: adapter, resourceType: resourceType}
		}
		rc := runInfraRotateAndPrune(args, provider, rotateAndPruneStdout)
		if rc != 0 {
			return fmt.Errorf("rotate-and-prune exited with code %d", rc)
		}
		return nil
	}
	if anyEnumerator && lastUnimpl != nil {
		return fmt.Errorf("rotate-and-prune pre-flight: no loaded provider implements IaCProvider.EnumerateAll bridge wiring for %q (rotation aborted; no state mutated; last probed: %w)", resourceType, lastUnimpl)
	}
	return fmt.Errorf("rotate-and-prune: no loaded provider implements EnumeratorAll")
}

// buildRotateAndPruneForceRotateSet translates the cloud-side resource name
// passed via `wfctl infra rotate-and-prune --name <NAME>` into the canonical
// secrets.generate[].Key values that bootstrapSecrets keys its forceRotate
// map by.
//
// Per ADR 0023 + docs/runbooks/spaces-key-prune.md, `--name` is the
// cloud-side resource Name (e.g. "coredump-deploy-key"), matching the
// `secrets.generate[].name` field. bootstrapSecrets keys forceRotate by
// `gen.Key` (e.g. "SPACES"), so the CLI must translate or the force-rotate
// path is silently skipped — the false-negative that surfaced as staging
// run 25616807427 ("rotate-and-prune: expected 1 rotation result, got 0"
// AFTER side effects committed).
//
// Match precedence:
//  1. secrets.generate[].name == name → take that gen's Key
//  2. secrets.generate[].key == name → fallback for configs that omit Name
//
// Returns an error when no entry matches (typo guard, mirrors the
// buildForceRotateSet validation in infra_bootstrap.go) so the operator
// gets a fast-fail before bootstrap touches the store.
//
// Strict-contract enforcement (per Copilot review on PR 594):
//   - Exactly ONE matching gen entry. Zero or two-plus matches reject.
//   - Match MUST be type=provider_credential. Other rotatable types
//     (random_hex, random_base64, random_alphanumeric) are intentionally
//     rejected here because rotate-and-prune's downstream invariants
//     (len(rotations)==1, prune step expects an old credential to revoke)
//     are coherent only for provider_credential. Operators rotating
//     non-provider_credential generators must use `wfctl infra bootstrap
//     --force-rotate` directly.
//
// Without these guards a multi-Name config or a non-provider_credential
// --name target would either rotate multiple keys then fail count check
// (side effects committed but errored) OR rotate without appending a
// RotationResult (the false-negative class this PR was opened to fix).
func buildRotateAndPruneForceRotateSet(name string, cfg *SecretsConfig) (map[string]bool, error) {
	if name == "" {
		return nil, fmt.Errorf("--name is required")
	}
	if cfg == nil || len(cfg.Generate) == 0 {
		return nil, fmt.Errorf("config has no secrets.generate entries; nothing to rotate for --name %q", name)
	}
	// Match precedence: secrets.generate[].name first (canonical per ADR 0023),
	// then .key fallback for older configs / unit-test fixtures.
	var matched []SecretGen
	for _, gen := range cfg.Generate {
		if gen.Name == name {
			matched = append(matched, gen)
		}
	}
	if len(matched) == 0 {
		for _, gen := range cfg.Generate {
			if gen.Key == name {
				matched = append(matched, gen)
			}
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("no secrets.generate entry matches --name %q (matched neither .name nor .key)", name)
	}
	if len(matched) > 1 {
		keys := make([]string, len(matched))
		for i, g := range matched {
			keys[i] = g.Key
		}
		return nil, fmt.Errorf("--name %q matches multiple secrets.generate entries (keys: %v); rotate-and-prune supports exactly one provider_credential per dispatch — use `wfctl infra bootstrap --force-rotate` for multi-entry rotation", name, keys)
	}
	if matched[0].Type != "provider_credential" {
		return nil, fmt.Errorf("--name %q matches secrets.generate entry of type %q; rotate-and-prune only operates on provider_credential entries (use `wfctl infra bootstrap --force-rotate` for other rotatable types)", name, matched[0].Type)
	}
	// Defense-in-depth: ensure the matched Key is unique across all
	// secrets.generate[] entries. Without this, a config with two gens
	// sharing the same Key (different Names) would set forceRotate[key]=true
	// → bootstrapSecrets rotates BOTH → fails len(rotations)==1 after side
	// effects committed (the same false-negative class this PR was opened
	// to fix, just via Key collision instead of Name collision).
	keyCount := 0
	var keyCollisionNames []string
	for _, gen := range cfg.Generate {
		if gen.Key == matched[0].Key {
			keyCount++
			if gen.Name != "" {
				keyCollisionNames = append(keyCollisionNames, gen.Name)
			}
		}
	}
	if keyCount > 1 {
		return nil, fmt.Errorf("secrets.generate has %d entries with .key=%q (names: %v); rotate-and-prune requires Key uniqueness so the rotation count invariant holds", keyCount, matched[0].Key, keyCollisionNames)
	}
	return map[string]bool{matched[0].Key: true}, nil
}

// recoveryRecord is persisted to ${WFCTL_STATE_DIR:-$HOME/.wfctl}/last-rotation.json
// after a successful rotate step, BEFORE the prune step. Used by the
// `wfctl infra prune --recovery-from-last-rotation` flow (Task 19) to recover
// from partial-failure scenarios without re-rotating (which would mint another
// key and worsen any leak).
//
// The JSON shape is a superset of `recoveryFile` (defined in infra_prune.go):
// adds Source + RotatedAt for forensics. The prune reader ignores extra fields
// — it only needs Type/Name/AccessKey/CreatedAt.
type recoveryRecord struct {
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	AccessKey string    `json:"access_key"`
	CreatedAt string    `json:"created_at"`
	Source    string    `json:"source,omitempty"`
	RotatedAt time.Time `json:"rotated_at,omitempty"`
}

// writeRecoveryRecord persists recovery JSON to defaultStateDir()/last-rotation.json
// with 0600 file perms (sensitive credential metadata) + 0700 parent dir.
//
// Writes to the same path that recoveryFilePath() in infra_prune.go reads
// from so prune --recovery-from-last-rotation finds it. Uses an atomic-ish
// write (os.WriteFile truncates and rewrites) — no rename-on-temp pattern
// needed since the file is per-host and per-user.
func writeRecoveryRecord(rec recoveryRecord) error {
	dir, err := defaultStateDir()
	if err != nil {
		return fmt.Errorf("rotate-and-prune: resolve state dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("rotate-and-prune: create state dir %s: %w", dir, err)
	}
	// gosec G117 false-positive: AccessKey here is the public DO Spaces
	// access-key identifier (analogous to an AWS access key ID), NOT the
	// credential secret-key, which is stored separately per ADR 0017
	// split-storage. The recovery file persists the access_key intentionally
	// so operators can recover from partial-failure prune via the
	// --recovery-from-last-rotation flag. File perms are 0600 + parent 0700.
	data, err := json.MarshalIndent(rec, "", "  ") //nolint:gosec // G117: access_key is a public identifier; secret_key is stored separately (ADR 0017)
	if err != nil {
		return fmt.Errorf("rotate-and-prune: marshal recovery record: %w", err)
	}
	path := filepath.Join(dir, "last-rotation.json")
	if err := os.WriteFile(path, data, 0600); err != nil { //nolint:gosec // intentional 0600
		return fmt.Errorf("rotate-and-prune: write recovery file %s: %w", path, err)
	}
	return nil
}

// runInfraRotateAndPrune is the all-in-one rotate+prune CLI:
//
//  1. ROTATE the canonical credential by reusing bootstrapSecrets's force-rotate
//     path (per ADR 0020, returns []RotationResult in-process — no subprocess,
//     no stderr parsing).
//  2. PERSIST a recovery record to $WFCTL_STATE_DIR/last-rotation.json with
//     0600 perms BEFORE the prune step. This is the safety net: if prune
//     fails partway, an operator can recover via
//     `wfctl infra prune --recovery-from-last-rotation` without re-rotating
//     (which would worsen any leak by minting yet another live credential).
//  3. DELEGATE the actual prune to runInfraPrune, passing the new key's
//     created_at as --created-before and access_key as --exclude-access-key
//     so only OLDER keys for the same Type are eligible for deletion. On
//     success runInfraPrune deletes the recovery file (it's the consumer);
//     on failure the recovery file is retained.
//
// Same two-key opt-in as runInfraPrune: --confirm flag + WFCTL_CONFIRM_PRUNE=1
// env var. Both required, otherwise the command refuses to run.
//
// `provider` is typed as pruneProvider (the narrow interface defined in
// infra_prune.go) so this CLI shares the unit-test fake surface with prune
// itself — keeps the fake-IaCProvider blast radius small.
//
// Exit codes:
//
//   - 0: rotation + prune both succeeded
//   - 1: opt-in/validation failed, rotate failed, recovery write failed, or
//     prune failed (recovery file retained in the prune-failed case)
//   - 2: argument parse error or missing required --type / --name
func runInfraRotateAndPrune(args []string, provider pruneProvider, w io.Writer) int {
	fs := flag.NewFlagSet("infra rotate-and-prune", flag.ContinueOnError)
	fs.SetOutput(w)
	var resourceType, name, configFile, preserveNames string
	var confirm, nonInteractive, pruneFirst bool
	fs.StringVar(&resourceType, "type", "", "Resource type (required, e.g. infra.spaces_key)")
	fs.StringVar(&name, "name", "", "Canonical credential name to rotate (required)")
	fs.StringVar(&configFile, "config", "infra.yaml", "Config file")
	fs.StringVar(&configFile, "c", "infra.yaml", "Config file (short)")
	// --env is accepted-and-ignored here so the dispatcher (runInfraRotateAndPruneCmd)
	// can forward args verbatim including --env without the inner FlagSet
	// erroring on unknown-flag. The dispatcher already uses --env to scope
	// provider loading; secrets-config resolution happens via --config alone.
	_ = fs.String("env", "", "Environment name (consumed by dispatcher; ignored here)")
	fs.StringVar(&preserveNames, "preserve-names", "", "Regex of resource names to preserve during prune")
	fs.BoolVar(&confirm, "confirm", false, "Required: explicit confirmation flag")
	fs.BoolVar(&nonInteractive, "non-interactive", false, "Skip the prune y/N prompt")
	// --prune-first defaults TRUE (per ADR 0023): the safer behavior is the
	// new default. When the cloud account is at quota (e.g., DO Spaces 200-key
	// limit), Step 1 (rotate = mint new key) fails before Step 2 (prune) gets
	// a chance to free quota — the chicken-and-egg the operator needs the
	// tool for. Pre-pruning orphans first frees quota, then rotation can mint,
	// then a defensive sweep cleans up any old canonical-name remnant. The
	// legacy "rotate-then-prune" order remains available via --prune-first=false
	// for callers that need to preserve the v0.27.1 ordering exactly.
	fs.BoolVar(&pruneFirst, "prune-first", true, "Prune orphans before rotating (default true; safer at quota — see ADR 0023)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if resourceType == "" || name == "" {
		fmt.Fprintln(w, "rotate-and-prune: --type and --name are required")
		return 2
	}
	if !confirm {
		fmt.Fprintln(w, "rotate-and-prune: --confirm flag is required (rotation + prune is destructive)")
		return 1
	}
	if os.Getenv("WFCTL_CONFIRM_PRUNE") != "1" {
		fmt.Fprintln(w, "rotate-and-prune: WFCTL_CONFIRM_PRUNE=1 env var is required (two-key opt-in)")
		return 1
	}

	ctx := context.Background()

	// Pre-prune (Step 0, when --prune-first=true / ADR 0023 default): delete
	// orphans BEFORE rotation so the cloud account has free quota for the
	// `Create` call that mints the new canonical key. Skips the canonical
	// `--name` (it might still be valid; if it's old we replace it in the
	// rotate step below) and any name matching `--preserve-names`. This
	// closes the at-quota chicken-and-egg: when the cloud is at quota,
	// rotation alone fails because Create returns "quota exceeded", and
	// the operator can't get to the post-rotation prune step that would
	// have freed the quota.
	if pruneFirst {
		fmt.Fprintf(w, "Step 0 (--prune-first): pruning orphan %s resources before rotation...\n", resourceType)
		if code := runPreRotationPrune(ctx, provider, resourceType, name, preserveNames, nonInteractive, w); code != 0 {
			fmt.Fprintf(w, "\nrotate-and-prune: pre-rotation prune failed (code=%d). No rotation attempted; no state mutated.\n", code)
			return code
		}
	}

	// Step 1: rotate the canonical credential via bootstrapSecrets's
	// force-rotate path. parseSecretsConfig + resolveSecretsProvider +
	// resolveCredentialRevoker are the existing helpers from
	// cmd/wfctl/infra_secrets.go and infra_bootstrap.go.
	fmt.Fprintf(w, "\nStep 1: rotating credential %q (type %s)...\n", name, resourceType)

	cfg, err := parseSecretsConfig(configFile)
	if err != nil {
		fmt.Fprintf(w, "rotate-and-prune: load config %s: %v\n", configFile, err)
		return 1
	}
	if cfg == nil {
		fmt.Fprintf(w, "rotate-and-prune: config %s has no secrets section\n", configFile)
		return 1
	}
	secretsProvider, err := resolveSecretsProvider(cfg)
	if err != nil {
		fmt.Fprintf(w, "rotate-and-prune: resolve secrets provider: %v\n", err)
		return 1
	}
	// Translate the cloud-side resource name (--name; per ADR 0023 +
	// docs/runbooks/spaces-key-prune.md) into the canonical secrets.generate[].Key
	// values that bootstrapSecrets keys forceRotate by. Without this translation
	// forceRotate[gen.Key] is false for every generator and bootstrapSecrets
	// silently bypasses the force-rotate code path (rotations slice stays empty
	// even when the underlying generator + Set side effects committed) — the
	// staging-dispatch false-negative surfaced 2026-05-09 (run 25616807427).
	forceRotate, err := buildRotateAndPruneForceRotateSet(name, cfg)
	if err != nil {
		fmt.Fprintf(w, "rotate-and-prune: %v\n", err)
		return 1
	}
	revoker, closer := resolveCredentialRevoker(ctx, configFile, cfg, forceRotate)
	if closer != nil {
		defer closer.Close()
	}

	_, rotations, err := bootstrapSecrets(ctx, secretsProvider, cfg, forceRotate, revoker)
	if err != nil {
		fmt.Fprintf(w, "rotate-and-prune: rotate: %v\n", err)
		return 1
	}
	if len(rotations) != 1 {
		fmt.Fprintf(w, "rotate-and-prune: expected 1 rotation result, got %d\n", len(rotations))
		return 1
	}
	rotated := rotations[0]
	fmt.Fprintf(w, "  new access_key=%s, created_at=%s\n", rotated.AccessKey, rotated.CreatedAt)

	// Persist recovery record BEFORE the prune step. Treated as a sub-step
	// of Step 1 (rotate) — operationally invisible from the user's vantage
	// (no Fprintf header), only the resulting path is surfaced for ops use.
	rec := recoveryRecord{
		Type:      resourceType,
		Name:      name,
		AccessKey: rotated.AccessKey,
		CreatedAt: rotated.CreatedAt,
		Source:    rotated.Source,
		RotatedAt: time.Now().UTC(),
	}
	if err := writeRecoveryRecord(rec); err != nil {
		fmt.Fprintf(w, "rotate-and-prune: %v\n", err)
		return 1
	}
	fmt.Fprintf(w, "  recovery file written to %s\n", recoveryFilePath())

	// Step 2 (user-visible): delegate to prune with the new key as the
	// exclusion target. Step numbering matches the banners the user sees:
	// Step 0 (when --prune-first=true) was the pre-rotation orphan sweep;
	// Step 1 is the rotate; Step 2 is the post-rotation prune. When
	// --prune-first=true this is a defensive sweep — should be a no-op if
	// Step 0 was complete, but covers the case where the canonical name's
	// OLD value (now replaced in GH Secrets) is still present in the cloud
	// and should be deleted. When --prune-first=false (legacy ordering),
	// this is the only prune pass and does the full cleanup itself.
	if pruneFirst {
		fmt.Fprintf(w, "\nStep 2: defensive sweep — pruning older %s resources after rotation...\n", resourceType)
	} else {
		fmt.Fprintf(w, "\nStep 2: pruning older %s resources...\n", resourceType)
	}
	pruneArgs := []string{
		"--type", resourceType,
		"--created-before", rotated.CreatedAt,
		"--exclude-access-key", rotated.AccessKey,
		"--confirm",
	}
	if nonInteractive {
		pruneArgs = append(pruneArgs, "--non-interactive")
	}
	if preserveNames != "" {
		pruneArgs = append(pruneArgs, "--preserve-names", preserveNames)
	}
	code := runInfraPrune(pruneArgs, provider, w)
	if code != 0 {
		fmt.Fprintf(w, "\nrotate-and-prune: prune step failed (code=%d). Recovery file retained at %s.\n", code, recoveryFilePath())
		fmt.Fprintf(w, "Re-run prune with: wfctl infra prune --type %s --recovery-from-last-rotation --confirm\n", resourceType)
		return code
	}

	// Success: rotate-and-prune is the recovery file's writer, so it owns
	// cleanup on its own happy path. (runInfraPrune only deletes the file
	// when invoked with --recovery-from-last-rotation, which we don't pass
	// because we already have the rotation result in-process.)
	recPath := recoveryFilePath()
	if err := os.Remove(recPath); err != nil && !os.IsNotExist(err) {
		// Non-fatal: a concurrent operator may have removed it, or perms
		// changed under us. Surface with an explicit cleanup hint so the
		// operator knows the rotation+prune itself succeeded and only the
		// stale state file needs hand-clearing.
		fmt.Fprintf(w, "rotate-and-prune: warning: rotation+prune succeeded but failed to remove stale recovery file at %s: %v\n", recPath, err)
		fmt.Fprintf(w, "rotate-and-prune: hint: this file is no longer needed; remove with `rm %s` once safe.\n", recPath)
	}
	return 0
}

// runPreRotationPrune deletes orphan resources of `resourceType` BEFORE the
// rotate step runs. Used by --prune-first (default true; ADR 0023) to free
// cloud quota so the subsequent rotate's Create call doesn't fail with
// "quota exceeded" — the at-quota chicken-and-egg the rotate-and-prune tool
// is supposed to recover from.
//
// Filter semantics (NAME-based, not time/access-key based — runInfraPrune's
// time + access-key filter requires a successful rotation result we don't
// have yet at this point):
//
//   - Skip the canonical `name` (it might still be the active credential;
//     if it's stale, the rotate step below will replace it via mint-new-
//     then-revoke-old per ADR 0012).
//   - Skip any resource whose `Name` matches the `preserveNames` regex
//     (same operator-supplied allowlist used by post-rotation prune).
//   - Delete everything else of `resourceType`.
//
// Refuses to run without WFCTL_CONFIRM_PRUNE=1 — the caller
// (runInfraRotateAndPrune) already validated this for the post-rotation
// prune; we re-check defensively so this helper is safe if reused.
//
// Returns 0 on success (zero or more deletions); non-zero on enumerate
// failure, regex compile failure, or any individual delete failure.
//
//nolint:cyclop // intentional: deletion-loop + filter logic + interactive prompt are tightly coupled
func runPreRotationPrune(ctx context.Context, provider pruneProvider, resourceType, name, preserveNames string, nonInteractive bool, w io.Writer) int {
	if os.Getenv("WFCTL_CONFIRM_PRUNE") != "1" {
		fmt.Fprintln(w, "rotate-and-prune: pre-rotation prune requires WFCTL_CONFIRM_PRUNE=1 (defensive re-check)")
		return 1
	}

	var preserveRe *regexp.Regexp
	if preserveNames != "" {
		re, reErr := regexp.Compile(preserveNames)
		if reErr != nil {
			fmt.Fprintf(w, "rotate-and-prune: invalid --preserve-names regex: %v\n", reErr)
			return 1
		}
		preserveRe = re
	}

	outs, err := provider.EnumerateAll(ctx, resourceType)
	if err != nil {
		fmt.Fprintf(w, "rotate-and-prune: pre-rotation enumerate: %v\n", err)
		return 1
	}

	var toDelete []*interfaces.ResourceOutput
	for _, out := range outs {
		// Use typed Name (provider contract guarantees ProviderID + Name on
		// EnumerateAll results). Fall back to Outputs["name"] for older
		// providers — same fallback-but-prefer-typed pattern as runInfraPrune.
		resName := out.Name
		if resName == "" {
			resName, _ = out.Outputs["name"].(string)
		}
		if resName == name {
			// Skip the canonical name. The rotate step replaces it in-place
			// via mint-new-then-revoke-old (ADR 0012), so deleting it here
			// would leave the cloud account without the active credential
			// for the brief window between Step 0 and Step 1.
			continue
		}
		if preserveRe != nil && preserveRe.MatchString(resName) {
			continue
		}
		toDelete = append(toDelete, out)
	}

	fmt.Fprintf(w, "Pre-rotation dry-run: %d orphan %s resource(s) to prune (canonical name %q + preserve-names regex are skipped):\n\n", len(toDelete), resourceType, name)
	for _, o := range toDelete {
		resName := o.Name
		if resName == "" {
			resName, _ = o.Outputs["name"].(string)
		}
		ak := o.ProviderID
		if ak == "" {
			ak, _ = o.Outputs["access_key"].(string)
		}
		ca, _ := o.Outputs["created_at"].(string)
		fmt.Fprintf(w, "  - %s (access_key=%s, created=%s)\n", resName, ak, ca)
	}

	if len(toDelete) == 0 {
		return 0
	}

	if !nonInteractive {
		fmt.Fprintf(w, "\nProceed with pre-rotation prune? (y/N): ")
		var ans string
		_, _ = fmt.Scanln(&ans)
		if ans != "y" && ans != "Y" {
			fmt.Fprintln(w, "Aborted (no rotation attempted; no state mutated).")
			return 1
		}
	}

	var failed int
	for _, o := range toDelete {
		ref := interfaces.ResourceRef{Type: o.Type, Name: o.Name, ProviderID: o.ProviderID}
		if delErr := provider.DeleteResource(ctx, ref); delErr != nil {
			fmt.Fprintf(w, "rotate-and-prune: pre-rotation delete %s: %v\n", o.Name, delErr)
			failed++
			continue
		}
		fmt.Fprintf(w, "  ✓ deleted %s\n", o.Name)
	}
	if failed > 0 {
		fmt.Fprintf(w, "\n%d pre-rotation delete(s) failed; aborting before rotation to avoid minting on a partially-cleaned account\n", failed)
		return 1
	}
	fmt.Fprintf(w, "\n%d orphan resource(s) pre-pruned successfully.\n", len(toDelete))
	return 0
}
