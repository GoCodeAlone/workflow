// Copyright (c) 2026 Jon Langevin
// SPDX-License-Identifier: Apache-2.0

// Command iac-codemod is an AST-based migration tool for IaC plugin providers.
//
// Modes:
//
//	refactor-plan      — rewrite Plan() bodies to delegate to wfctlhelpers.Plan
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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

// Options carries flags shared by every codemod mode.
type Options struct {
	// DryRun reports findings without mutating files. Default true.
	DryRun bool
	// Fix opts into mutation; when set, DryRun is forced false by run().
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
	return func(args []string, opts *Options, stdout, stderr io.Writer) int {
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

	fs := flag.NewFlagSet("iac-codemod "+mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	opts := &Options{}
	fs.BoolVar(&opts.DryRun, "dry-run", true, "report findings without mutating files (default)")
	fs.BoolVar(&opts.Fix, "fix", false, "opt into mutation; overrides -dry-run")

	if err := fs.Parse(rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if opts.Fix {
		opts.DryRun = false
	}
	return fn(fs.Args(), opts, stdout, stderr)
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage: iac-codemod <mode> [flags] [paths...]

Modes:
  refactor-plan      Rewrite Plan() bodies to delegate to wfctlhelpers.Plan.
  refactor-apply     Rewrite Apply() bodies to delegate to wfctlhelpers.ApplyPlan
                     (with informative reports for non-canonical idioms).
  add-validate-plan  Insert a no-op ValidatePlan stub on providers missing it.
  lint               Run static checks; no rewrite. Advisory-only.

Flags (all modes):
  -dry-run   Report findings without mutating files (default true).
  -fix       Opt into mutation; overrides -dry-run.

Marker:
  Functions and type declarations annotated with the comment
  // wfctl:skip-iac-codemod
  are skipped by every mode and surfaced in each mode's report.
`)
}
