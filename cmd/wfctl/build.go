package main

import (
	"flag"
	"fmt"
	"os"
)

// runBuild is the top-level `wfctl build` dispatcher.
// It accepts subcommands (go, ui, image, push, custom) or runs all
// target types present in the config when invoked without a subcommand.
func runBuild(args []string) error {
	// Check for explicit subcommand first.
	if len(args) > 0 && !isFlag(args[0]) {
		sub := args[0]
		rest := args[1:]
		switch sub {
		case "go":
			return runBuildGo(rest)
		case "ui":
			return runBuildUIPlugin(rest)
		case "push":
			return runBuildPush(rest)
		case "custom":
			return runBuildCustom(rest)
		default:
			return fmt.Errorf("unknown build subcommand %q — valid: go, ui, image, push, custom", sub)
		}
	}

	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "workflow.yaml", "Path to workflow config file")
	dryRun := fs.Bool("dry-run", false, "Print planned actions without executing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *dryRun {
		os.Setenv("WFCTL_BUILD_DRY_RUN", "1")    //nolint:errcheck
		defer os.Unsetenv("WFCTL_BUILD_DRY_RUN") //nolint:errcheck
	}

	goArgs := []string{"--config", *cfgPath}
	if err := runBuildGo(goArgs); err != nil {
		return err
	}
	return nil
}

func isFlag(s string) bool {
	return len(s) > 0 && s[0] == '-'
}
