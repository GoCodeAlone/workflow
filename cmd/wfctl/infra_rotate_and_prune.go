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
)

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
	var resourceType, name, configFile, envName, allowlist string
	var confirm, nonInteractive bool
	fs.StringVar(&resourceType, "type", "", "Resource type (required, e.g. infra.spaces_key)")
	fs.StringVar(&name, "name", "", "Canonical credential name to rotate (required)")
	fs.StringVar(&configFile, "config", "infra.yaml", "Config file")
	fs.StringVar(&configFile, "c", "infra.yaml", "Config file (short)")
	fs.StringVar(&envName, "env", "", "Environment name")
	_ = envName // currently unused; reserved for future per-env config selection
	fs.StringVar(&allowlist, "allowlist", "", "Regex matching names to skip during prune")
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

	// Step 2: persist recovery record BEFORE the prune step.
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

	// Step 3: delegate to prune with the new key as the exclusion target.
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
	if allowlist != "" {
		pruneArgs = append(pruneArgs, "--allowlist", allowlist)
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
	if err := os.Remove(recoveryFilePath()); err != nil && !os.IsNotExist(err) {
		// Non-fatal: recovery file already lacked-perms-removed or
		// surprise concurrent operator. Surface so the operator can
		// hand-clean if needed.
		fmt.Fprintf(w, "rotate-and-prune: warning: failed to remove recovery file %s: %v\n", recoveryFilePath(), err)
	}
	return 0
}
