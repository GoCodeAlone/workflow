// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// Command iac-codemod is an AST-based migration tool for IaC plugin providers.
//
// Modes:
//
//	refactor-plan      — rewrite Plan() bodies to delegate to platform.ComputePlan
//	refactor-apply     — rewrite Apply() bodies to delegate to wfctlhelpers.ApplyPlan
//	                     (with informative reports for non-canonical idioms)
//	add-validate-plan  — inject a no-op ValidatePlan stub on providers missing it
//	lint               — static checks (no rewrite); advisory-only mode
//
// All modes default to dry-run. Pass -fix to opt into mutation.
//
// All modes honor the `// wfctl:skip-iac-codemod` marker on functions and
// types; skipped sites are surfaced in each mode's report.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// SkipMarker is the single canonical comment that opts a function or type
// declaration out of every iac-codemod mode (refactor-plan, refactor-apply,
// add-validate-plan, lint). Plan rev2 (line 2400) unifies the four modes
// on this marker specifically to prevent mismatched-marker silent-no-op
// surfaces (e.g. // wfctl:skip-codemod or // wfctl:skip-plan-codemod). All
// downstream parsers (T8.3-T8.5) MUST reference this constant rather than
// the literal string, and each mode surfaces a list of skipped sites in
// its report.
const SkipMarker = "// wfctl:skip-iac-codemod"

// Options carries flags shared by every codemod mode.
//
// Mode implementations MUST treat Fix as the sole authority for mutation.
// DryRun is mirrored as `!Fix` purely for ergonomic reading of report
// preambles and is normalized by run() at the dispatcher boundary so a
// user-supplied -dry-run=false cannot bypass the explicit -fix gate
// (plan §W-8 line 2347: "-dry-run flag default true; -fix opts into
// mutation"). Predicates like `if !opts.DryRun { mutate() }` are safe
// because the dispatcher guarantees DryRun==true whenever Fix==false.
type Options struct {
	// DryRun reports findings without mutating files. Forced true when
	// Fix is false; forced false when Fix is true. The user's
	// -dry-run= value is informational once dispatcher normalization
	// runs.
	DryRun bool
	// Fix opts into mutation. Sole authority for mutation gating.
	Fix bool
}

// modeFunc is the entry point for one of the codemod's subcommand modes.
// args is the residual positional argument list (target paths, etc.) after
// shared flags have been parsed off. Returns a process exit code.
type modeFunc func(args []string, opts *Options, stdout, stderr io.Writer) int

// modes registers every supported subcommand. Tests swap entries in this
// map to capture the parsed Options without spawning a subprocess.
var modes = map[string]modeFunc{
	"refactor-plan":     stubMode("refactor-plan"),
	"refactor-apply":    stubMode("refactor-apply"),
	"add-validate-plan": stubMode("add-validate-plan"),
	"lint":              stubMode("lint"),
}

// stubMode returns a placeholder modeFunc used by the T8.1 skeleton.
// Subsequent tasks (T8.2 lint, T8.3 refactor-plan, T8.4 refactor-apply,
// T8.5 add-validate-plan) replace these entries with real implementations
// in the package's init() inside their own files.
func stubMode(name string) modeFunc {
	return func(_ []string, _ *Options, stdout, _ io.Writer) int {
		fmt.Fprintf(stdout, "iac-codemod %s: not yet implemented (skeleton stub)\n", name)
		return 0
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. Returns the desired process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	}

	mode := args[0]
	rest := args[1:]
	fn, ok := modes[mode]
	if !ok {
		fmt.Fprintf(stderr, "iac-codemod: unknown mode: %s\n\n", mode)
		usage(stderr)
		return 2
	}

	// Round-12 #1: rev1 used a single FlagSet with only `-dry-run`
	// and `-fix` registered, so any mode-specific flag (e.g.
	// `-report-file` for refactor-apply) failed with
	// "flag provided but not defined" BEFORE the mode could parse
	// it. Now we manually extract the two shared flags from the
	// argument list, leaving the rest (including unknown-to-dispatcher
	// flags) intact for the mode's own FlagSet. Manual extraction is
	// preferred over flag.NewFlagSet's `flag.ContinueOnError` because
	// stdlib's parser stops at the first unknown flag and consumes
	// nothing further — manual lets us preserve EVERYTHING the mode
	// needs.
	opts := &Options{}
	residual := []string{}
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		switch arg {
		case "-h", "--help":
			usage(stdout)
			return 0
		case "-dry-run", "--dry-run":
			opts.DryRun = true
		case "-dry-run=true", "--dry-run=true":
			opts.DryRun = true
		case "-dry-run=false", "--dry-run=false":
			opts.DryRun = false
		case "-fix", "--fix":
			opts.Fix = true
		case "-fix=true", "--fix=true":
			opts.Fix = true
		case "-fix=false", "--fix=false":
			opts.Fix = false
		default:
			residual = append(residual, arg)
		}
	}
	// Normalize the mutation gate at the dispatcher boundary: Fix is the
	// sole authority for "may I mutate?". A user-supplied -dry-run=false
	// without -fix must NOT bypass the gate (plan §W-8 line 2347), and
	// -fix must override an explicit -dry-run=true.
	if opts.Fix {
		opts.DryRun = false
	} else {
		opts.DryRun = true
	}
	return fn(residual, opts, stdout, stderr)
}

// shouldSkipDir is the canonical directory-walk filter shared by every
// mode's filepath.WalkDir callback. It excludes:
//
//   - "vendor" — the standard Go vendor tree; mirrors `go build`'s
//     behavior of treating vendor/ as a private dependency island.
//   - "testdata" — by convention not real source.
//   - hidden directories (prefix ".", except the literal "."): .git,
//     .idea, .vscode, etc.
//   - underscore-prefix directories (prefix "_", except the literal
//     "_"): Go tooling itself ignores these (cmd/go skips package paths
//     starting with underscore). The DigitalOcean plugin uses
//     `_worktrees/` for parallel feature branches; without this filter
//     a single lint run reports the same site dozens of times across
//     stale checkouts.
func shouldSkipDir(base string) bool {
	switch base {
	case "vendor", "testdata":
		return true
	}
	if len(base) > 1 && (strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_")) {
		return true
	}
	return false
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage: iac-codemod <mode> [flags] [paths...]

Modes:
  refactor-plan      Rewrite Plan() bodies to delegate to platform.ComputePlan.
  refactor-apply     Rewrite Apply() bodies to delegate to wfctlhelpers.ApplyPlan
                     (with informative reports for non-canonical idioms).
  add-validate-plan  Insert a no-op ValidatePlan stub on providers missing it.
  lint               Run static checks; no rewrite. Advisory-only.

Flags (all modes):
  -dry-run   Report findings without mutating files (default true).
  -fix       Opt into mutation; overrides -dry-run.

Mode-specific flags:
  refactor-apply:
    -report-file <path>  Also write the Markdown report to <path>. Default
                         is stdout-only.

  Flags may appear anywhere on the command line (round-12 #1: the
  dispatcher uses a manual flag scan instead of stdlib flag, so
  positional-then-flag ordering is supported). Mode-specific flags
  (e.g. -report-file) are passed through to the mode's own parser.

Marker:
  Functions and type declarations annotated with the comment
  %s
  are skipped by every mode and surfaced in each mode's report.
`, SkipMarker)
}
