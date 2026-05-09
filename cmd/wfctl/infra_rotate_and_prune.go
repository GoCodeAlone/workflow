package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	var configFile, envName string
	// Declare every flag runInfraRotateAndPrune accepts so flag.Parse
	// doesn't error here. Only --config / --env are captured (used by the
	// provider loader); the rest are re-parsed inside runInfraRotateAndPrune
	// against the same args slice (Go's flag package is idempotent).
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	_ = fs.String("type", "", "Resource type")
	_ = fs.String("name", "", "Canonical credential name")
	_ = fs.String("preserve-names", "", "Regex of names to preserve during prune")
	_ = fs.Bool("confirm", false, "Confirmation flag")
	_ = fs.Bool("non-interactive", false, "Skip y/N prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

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

	for _, p := range providers {
		if _, ok := p.(interfaces.EnumeratorAll); ok {
			adapter := &pruneProviderAdapter{p: p}
			if rc := runInfraRotateAndPrune(args, adapter, rotateAndPruneStdout); rc != 0 {
				return fmt.Errorf("rotate-and-prune exited with code %d", rc)
			}
			return nil
		}
	}
	return fmt.Errorf("rotate-and-prune: no loaded provider implements EnumeratorAll")
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
	data, err := json.MarshalIndent(rec, "", "  ")
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
	var confirm, nonInteractive bool
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

	// Step 1: rotate the canonical credential via bootstrapSecrets's
	// force-rotate path. parseSecretsConfig + resolveSecretsProvider +
	// resolveCredentialRevoker are the existing helpers from
	// cmd/wfctl/infra_secrets.go and infra_bootstrap.go.
	fmt.Fprintf(w, "Step 1: rotating credential %q (type %s)...\n", name, resourceType)

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
	forceRotate := map[string]bool{name: true}
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
	// exclusion target. Step numbering matches the two banners the user
	// sees: Step 1 is the rotate (above), Step 2 is the prune (here).
	// The recovery-write between them is a sub-step of Step 1.
	fmt.Fprintf(w, "\nStep 2: pruning older %s resources...\n", resourceType)
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
