package main

import (
	"context"
	"encoding/json"
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
// move opt-in checks further from --confirm parsing.
//
//nolint:cyclop // the validation gauntlet is intentional; splitting it would
func runInfraPrune(args []string, provider pruneProvider, w io.Writer) int {
	fs := flag.NewFlagSet("infra prune", flag.ContinueOnError)
	fs.SetOutput(w)
	var resourceType, createdBefore, excludeAK, allowlist string
	var confirm, nonInteractive, recoveryFromLastRotation bool
	fs.StringVar(&resourceType, "type", "", "Resource type (required, e.g. infra.spaces_key)")
	fs.StringVar(&createdBefore, "created-before", "", "RFC3339 timestamp; only resources older than this are eligible")
	fs.StringVar(&excludeAK, "exclude-access-key", "", "Access key to preserve (required: paranoia rail)")
	fs.StringVar(&allowlist, "allowlist", "", "Regex matching names to skip (orthogonal to time filter)")
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

	var allowlistRe *regexp.Regexp
	if allowlist != "" {
		re, reErr := regexp.Compile(allowlist)
		if reErr != nil {
			fmt.Fprintf(w, "prune: invalid --allowlist regex: %v\n", reErr)
			return 1
		}
		allowlistRe = re
	}

	ctx := context.Background()
	outs, err := provider.EnumerateAll(ctx, resourceType)
	if err != nil {
		fmt.Fprintf(w, "prune: enumerate: %v\n", err)
		return 1
	}

	var toDelete []*interfaces.ResourceOutput
	for _, out := range outs {
		ak, _ := out.Outputs["access_key"].(string)
		ca, _ := out.Outputs["created_at"].(string)
		name, _ := out.Outputs["name"].(string)
		if ak == excludeAK {
			continue
		}
		if allowlistRe != nil && allowlistRe.MatchString(name) {
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
		name, _ := o.Outputs["name"].(string)
		ak, _ := o.Outputs["access_key"].(string)
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
		fmt.Fprintf(w, "\n%d delete(s) failed; recovery file retained at %s\n", failed, recoveryFilePath())
		return 1
	}
	if recoveryFromLastRotation {
		_ = os.Remove(recoveryFilePath())
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
func runInfraPruneCmd(args []string) error {
	fs := flag.NewFlagSet("infra prune", flag.ContinueOnError)
	fs.SetOutput(pruneStderr)
	var configFile, envName string
	fs.StringVar(&configFile, "config", "", "Config file (default: infra.yaml or config/infra.yaml)")
	fs.StringVar(&configFile, "c", "", "Config file (short for --config)")
	fs.StringVar(&envName, "env", "", "Environment name for config resolution")
	// Declared so flag.Parse doesn't error; runInfraPrune reparses against the same args slice.
	_ = fs.String("type", "", "")
	_ = fs.String("created-before", "", "")
	_ = fs.String("exclude-access-key", "", "")
	_ = fs.String("allowlist", "", "")
	_ = fs.Bool("confirm", false, "")
	_ = fs.Bool("non-interactive", false, "")
	_ = fs.Bool("recovery-from-last-rotation", false, "")
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

	for _, p := range providers {
		if _, ok := p.(interfaces.EnumeratorAll); ok {
			adapter := &pruneProviderAdapter{p: p}
			if rc := runInfraPrune(args, adapter, pruneStdout); rc != 0 {
				return fmt.Errorf("prune exited with code %d", rc)
			}
			return nil
		}
	}
	return fmt.Errorf("prune: no loaded provider implements EnumeratorAll")
}
