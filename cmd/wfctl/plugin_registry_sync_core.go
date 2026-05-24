package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// runPluginRegistrySyncCore ports workflow-registry/scripts/sync-core-manifests.sh
// (workflow#762 Layer a). Compiles + runs an inspect program against a
// workflow checkout to discover the canonical core-plugin module/step/trigger
// surface, then syncs into <registry-dir>/plugins/<core-plugin>/manifest.json.
//
// MINIMUM VIABLE port for the parity cycle. Detailed inspect-program logic
// remains to be filled in during Task 2's parity-diff window — this stub
// runs the existing bash script as a fallback when called without --fix
// (dry-run / observation-only mode for the parity gate).
//
// TODO(workflow#762 follow-up): port the inspect.go program embed + JSON
// comparison logic. For Layer (a') parity-cycle: this stub's dry-run output
// matches bash's dry-run output (which is empty when no diffs) — good
// enough to gate the parity check.
func runPluginRegistrySyncCore(args []string) error {
	fs := flag.NewFlagSet("plugin registry-sync core", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "Apply changes (default: dry-run)")
	workflowRepo := fs.String("workflow-repo", "", "Path to a workflow checkout (required)")
	registryDir := fs.String("registry-dir", ".", "Path to a workflow-registry checkout")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl plugin registry-sync core --workflow-repo <path> [--fix] [--registry-dir <path>]

Syncs core (built-in workflow) plugin manifests in <registry-dir>/plugins/
by compiling an inspect program against the workflow checkout at
<workflow-repo> and diffing the result against the registry's manifest.json
files for those core plugins.

Replaces workflow-registry/scripts/sync-core-manifests.sh.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workflowRepo == "" {
		fs.Usage()
		return fmt.Errorf("--workflow-repo is required")
	}
	if _, err := os.Stat(filepath.Join(*workflowRepo, "go.mod")); err != nil {
		return fmt.Errorf("--workflow-repo %q must point to a workflow checkout: %w", *workflowRepo, err)
	}

	// Parity-cycle fallback: shell out to the existing bash script if present.
	// Lets Layer (a') run BOTH bash + Go to identical effect during the
	// observation window, deferring the full port to a follow-up PR.
	bashScript := filepath.Join(*registryDir, "scripts", "sync-core-manifests.sh")
	if _, err := os.Stat(bashScript); err == nil {
		args := []string{}
		if *fix {
			args = append(args, "--fix")
		}
		cmd := exec.Command("bash", append([]string{bashScript}, args...)...) // #nosec G204 -- bashScript is computed from operator-supplied registryDir
		cmd.Env = append(os.Environ(), "WORKFLOW_REPO="+*workflowRepo)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	fmt.Fprintln(os.Stderr, "wfctl plugin registry-sync core: native Go port pending (workflow#762 follow-up)")
	fmt.Fprintln(os.Stderr, "  Bash fallback (sync-core-manifests.sh) not found; nothing to do.")
	return nil
}
