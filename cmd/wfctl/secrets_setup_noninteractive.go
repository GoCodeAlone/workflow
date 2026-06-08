package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/mattn/go-isatty"
)

// nonInteractiveSetupArgs carries all parsed flags for the non-interactive
// secrets setup path. It is separated from the flag-parsing so it can be
// unit-tested without spawning a real os.Stdin / flag.FlagSet.
type nonInteractiveSetupArgs struct {
	configFile     string
	storeName      string   // --store flag; overrides defaultStore
	fromEnv        bool     // --from-env: read value from $NAME
	secretLiterals []string // --secret NAME=VALUE (process-table leak risk; for local only)
	stdinKV        []string // KEY=VALUE lines read from stdin pipe
	only           []string // --only A,B,C
	skipExisting   bool     // --skip-existing
	autoGenKeys    bool     // --auto-gen-keys: generate random values for _KEY/_SECRET/_TOKEN/_SIGNING
}

// runSecretsSetupNonInteractive is the testable, context-less entry point
// that wraps runSecretsSetupNonInteractiveCtx with context.Background().
func runSecretsSetupNonInteractive(a *nonInteractiveSetupArgs, out io.Writer) error {
	return runSecretsSetupNonInteractiveCtx(context.Background(), a, out)
}

// runSecretsSetupNonInteractiveCtx executes the non-interactive setup path.
// It:
//  1. Loads the workflow config.
//  2. Resolves each secret's store (--store > entry.store > env override >
//     defaultStore > legacy provider > env).
//  3. Builds a selector from --only / --skip-existing / --all.
//  4. Builds a valuer from --from-env > stdin-KV > --secret.
//  5. Writes each selected secret through its resolved provider and prints a summary.
func runSecretsSetupNonInteractiveCtx(ctx context.Context, a *nonInteractiveSetupArgs, out io.Writer) error {
	cfg, err := config.LoadFromFile(a.configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.Secrets == nil || len(cfg.Secrets.Entries) == 0 {
		fmt.Fprintln(out, "No secrets declared in config.")
		return nil
	}

	// Build value lookup maps (priority: from-env > stdin-KV > --secret).
	// --secret literals (process-table leak risk):
	secretMap := make(map[string]string)
	for _, lit := range a.secretLiterals {
		k, v, ok := strings.Cut(lit, "=")
		if !ok {
			return fmt.Errorf("--secret %q: expected NAME=VALUE format", lit)
		}
		secretMap[k] = v
	}
	for _, kv := range a.stdinKV {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		secretMap[k] = v
	}

	decls := buildSetupDecls(cfg, "", a.storeName)
	statuses := querySetupDeclStatuses(ctx, decls, cfg)

	// Selector: --only restricts; --skip-existing filters already-set.
	onlySet := make(map[string]bool, len(a.only))
	for _, n := range a.only {
		onlySet[n] = true
	}

	selector := func(ds []setupDecl, statuses []SecretStatus) ([]setupDecl, error) {
		setMap := make(map[string]bool, len(statuses))
		for _, s := range statuses {
			if s.IsSet {
				setMap[s.Name] = true
			}
		}
		var out []setupDecl
		for _, d := range ds {
			if len(onlySet) > 0 && !onlySet[d.name] {
				continue
			}
			if a.skipExisting && setMap[d.name] {
				continue
			}
			out = append(out, d)
		}
		return out, nil
	}

	// failureReasons records the actionable per-secret error so it can be
	// printed alongside the [failed] summary line (the engine accumulator only
	// keeps the name when stopOnErr=false).
	failureReasons := make(map[string]string)

	// Valuer: --from-env > stdinKV/--secret > --auto-gen-keys > error if none.
	// When --from-env is set, a missing env var is a skip (provided=false)
	// rather than an error — the caller only wants to set what's available.
	// When no value source at all is configured, this is a hard error.
	valuer := func(d setupDecl) (string, bool, error) {
		if a.fromEnv {
			v := os.Getenv(d.name)
			if v != "" {
				return v, true, nil
			}
			// env var not set — check the other sources before skipping.
		}
		if v, ok := secretMap[d.name]; ok {
			return v, true, nil
		}
		// --auto-gen-keys: generate a random value for key/secret/token names.
		if a.autoGenKeys && isAutoGenCandidate(d.name) {
			if gv := generateSecretValue(); gv != "" {
				return gv, true, nil
			}
		}
		if a.fromEnv {
			// from-env was the value source but $NAME was empty → skip, not error.
			return "", false, nil
		}
		// No value source at all — non-interactive hard error naming the secret.
		err := fmt.Errorf("no value for secret %q: set $%s, pass --from-env, or use --secret %s=VALUE", d.name, d.name, d.name)
		failureReasons[d.name] = err.Error()
		return "", false, err
	}

	// Audit: append a JSONL record for each successful Set (never the value).
	auditFn := func(name, store string) {
		_ = writeSecretsAuditRecord(name, store) //nolint:errcheck // best-effort audit
	}

	selectedDecls, err := selector(decls, statuses)
	if err != nil {
		return err
	}
	report, err := runSetupDeclsByStore(ctx, cfg, selectedDecls, valuer, auditFn, false)
	if err != nil {
		return err
	}

	// Print summary (never print values).
	for _, n := range report.Set {
		fmt.Fprintf(out, "  %-24s  [set]\n", n)
	}
	for _, n := range report.Skipped {
		fmt.Fprintf(out, "  %-24s  [skipped]\n", n)
	}
	for _, n := range report.Failed {
		if reason := failureReasons[n]; reason != "" {
			fmt.Fprintf(out, "  %-24s  [failed] %s\n", n, reason)
		} else {
			fmt.Fprintf(out, "  %-24s  [failed]\n", n)
		}
	}
	fmt.Fprintf(out, "\nDone: %d set, %d skipped, %d failed.\n",
		len(report.Set), len(report.Skipped), len(report.Failed))

	if len(report.Failed) > 0 {
		return fmt.Errorf("%d secret(s) failed to set: %s", len(report.Failed), strings.Join(report.Failed, ", "))
	}
	return nil
}

// readStdinKV reads KEY=VALUE pairs from stdin when stdin is a pipe (not a TTY).
// Returns nil if stdin is a terminal (nothing to read without blocking).
func readStdinKV() []string {
	if isatty.IsTerminal(os.Stdin.Fd()) {
		return nil
	}
	var pairs []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			pairs = append(pairs, line)
		}
	}
	return pairs
}

// isSecretSensitive returns true when the name looks like it should be masked
// in interactive prompts (ends in _KEY, _SECRET, _TOKEN, _PASSWORD, etc.).
func isSecretSensitive(name string) bool {
	up := strings.ToUpper(name)
	for _, suffix := range []string{"_KEY", "_SECRET", "_TOKEN", "_PASSWORD", "_PASS", "_SIGNING"} {
		if strings.HasSuffix(up, suffix) {
			return true
		}
	}
	return false
}
