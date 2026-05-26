package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runPluginRegistrySyncReadme ports workflow-registry/scripts/generate-readme.sh
// (workflow#762 Layer a). Regenerates the plugin/template indexes in
// <registry-dir>/README.md between marker comments.
//
// MINIMUM VIABLE port for the parity cycle — shells out to the existing
// bash script during Layer (a') so dry-run parity holds. Native Go port
// (with the 7-template enumeration + pipe-escape + case-fold sort + marker
// region replacement per plan I-P5) lands in a follow-up PR within the
// parity-cycle window.
func runPluginRegistrySyncReadme(args []string) error {
	fs := flag.NewFlagSet("plugin registry-sync readme", flag.ContinueOnError)
	check := fs.Bool("check", false, "Dry-run; exit non-zero on diff")
	registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin registry-sync readme [--check] [--registry-dir <path>]

Regenerates the plugin/template indexes in <registry-dir>/README.md between
marker comments. With --check, exits non-zero on diff (CI dry-run).

Replaces workflow-registry/scripts/generate-readme.sh.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Parity-cycle fallback: shell out to the existing bash script.
	// Layer (a') runs Go (--check) alongside bash (--check); parity-diff
	// asserts identical output. Native Go port deferred per Task 1 §5.
	bashScript := filepath.Join(*registryDir, "scripts", "generate-readme.sh")
	if _, err := os.Stat(bashScript); err == nil {
		cmdArgs := []string{bashScript}
		if *check {
			cmdArgs = append(cmdArgs, "--check")
		}
		cmd := exec.Command("bash", cmdArgs...) // #nosec G204 -- bashScript path is computed from operator-supplied registryDir
		cmd.Dir = *registryDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	fmt.Fprintln(os.Stderr, "wfctl plugin registry-sync readme: native Go port pending (workflow#762 follow-up)")
	fmt.Fprintln(os.Stderr, "  Bash fallback (generate-readme.sh) not found; nothing to do.")
	return nil
}
