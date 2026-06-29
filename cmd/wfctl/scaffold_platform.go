package main

import (
	"errors"
	"fmt"
	"os"
)

// runScaffoldCmd is the primary capability-driven scaffolder (the evolved
// `wfctl scaffold`, ADR 0050). It dispatches subcommands and otherwise runs the
// capability-driven assemble (the successor to `wfctl capability assemble`):
//
//	wfctl scaffold --set <caps.json> --out <dir>   # primary: capability-driven
//	wfctl scaffold dockerfile ...                  # legacy hardened Dockerfile
//	wfctl scaffold template [name] ...             # project templates (folds init)
//
// `wfctl capability assemble ...` remains as a backward-compatible alias for the
// primary path.
//
// Note (design D5/P4): `wfctl scaffold` already required a subcommand before
// this change (it dispatched `dockerfile`), so there is no bare-scaffold-
// produces-Dockerfile behavior to deprecate — bare scaffold now points at the
// primary capability path.
func runScaffoldCmd(args []string) error {
	if len(args) == 0 {
		printScaffoldPlatformUsage(os.Stderr)
		return errors.New("scaffold: provide a subcommand (dockerfile|template) or assemble flags (--set/--out)")
	}
	switch args[0] {
	case "dockerfile":
		return runScaffoldDockerfile(args[1:])
	case "template":
		return runScaffoldTemplate(args[1:])
	case "-h", "--help", "help":
		printScaffoldPlatformUsage(os.Stderr)
		return nil
	}
	// Primary: capability-driven assemble (no subcommand consumed). Delegates to
	// the assemble path, which errors helpfully if --set/--out are absent.
	return runCapabilityAssemble(args, os.Stdout)
}

// printScaffoldPlatformUsage prints the evolved scaffold command surface.
func printScaffoldPlatformUsage(out *os.File) {
	fmt.Fprintln(out, `Usage: wfctl scaffold <subcommand|flags> [options]

wfctl scaffold is the capability-driven app scaffolder.

Primary (no subcommand):
  wfctl scaffold --set <caps.json> --out <dir> [--force]
	Assemble a minimal workflow app from a capability set (Assembly Grammar).

Subcommands:
  dockerfile   Generate a hardened Dockerfile.prebuilt (legacy)
  template     Scaffold a project from a template (folds wfctl init)

'wfctl capability assemble ...' is a backward-compatible alias for the primary path.

Run 'wfctl scaffold <subcommand> -h' for subcommand-specific help.`)
}
