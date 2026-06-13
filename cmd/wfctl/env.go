package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runEnv(args []string) error {
	return runEnvWithOutput(args, os.Stdout)
}

func runEnvWithOutput(args []string, out io.Writer) error {
	if len(args) == 0 {
		printEnvUsage(out)
		return fmt.Errorf("env subcommand is required")
	}
	switch args[0] {
	case "-h", "--help", "help":
		printEnvUsage(out)
		return flag.ErrHelp
	case "setup":
		return runEnvSetup(args[1:], out)
	default:
		printEnvUsage(out)
		return fmt.Errorf("unknown env subcommand %q", args[0])
	}
}

func printEnvUsage(out io.Writer) {
	fmt.Fprintln(out, `Usage: wfctl env <subcommand> [options]

Manage environment input setup across provider secrets, provider variables,
and workflow config references.

Subcommands:
  setup   Configure declared environment inputs for an application

Use "wfctl env <subcommand> -h" for subcommand options.`)
}

func runEnvSetup(args []string, out io.Writer) error {
	if hasHelpArg(args) {
		printEnvSetupUsage(out)
		return flag.ErrHelp
	}
	manifestArgs := args
	if !hasFlag(args, "manifest") {
		manifestArgs = append([]string{"--manifest", "wfctl.yaml"}, args...)
	}
	parsed, err := parseManifestSetupFlags(manifestArgs)
	if err != nil {
		return err
	}
	return runSecretsSetupManifestWithIO(parsed, manifestSetupCommandInput(os.Stdin), out)
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			return true
		}
	}
	return false
}

func printEnvSetupUsage(out io.Writer) {
	fmt.Fprintln(out, `Usage: wfctl env setup [options]

Configure environment inputs discovered from wfctl.yaml, .wfctl-lock.yaml,
installed plugin required_secrets[], installed plugin required_config[], and
workflow config ${ENV_VAR} references.

Sensitive inputs are stored as provider secrets. Non-secret inputs are stored as
provider variables when the selected provider supports variables.

Options:
  --manifest PATH       wfctl.yaml plugin manifest (default "wfctl.yaml")
  --lock-file PATH      wfctl plugin lockfile (default ".wfctl-lock.yaml")
  --plugin-dir PATH     installed plugin directory
  --config PATTERNS     workflow config file or comma-separated glob list
  --kind KIND           input kind to configure: all | secret | var (default all)
  --scope SCOPE         GitHub scope: repo | env | org (default repo)
  --env NAME            environment name for --scope env
  --org NAME            organization slug for --scope org
  --visibility VALUE    org visibility: all | selected | private (default private)
  --token-env NAME      environment variable containing the provider token
  --from-env            read values from environment variables
  --non-interactive     never prompt
  --secret NAME=VALUE   inline value; avoid in CI because process args can leak
  --only A,B            configure only listed logical input names
  --all                 include inputs that are already set
  --skip-existing       skip inputs already present in the target
  --name-map A=B        store logical input A under provider key B
  --write-config        rewrite matching ${LOGICAL} refs after setup succeeds
  --verbose             show source and full target details

Compatibility:
  wfctl secrets setup --manifest ... remains supported for secrets setup.
  wfctl vars setup ... remains supported for non-secret variable setup.`)
}

func parseEnvSetupKind(raw string) (envSetupInputKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "all":
		return "", nil
	case "secret", "secrets":
		return envSetupInputSecret, nil
	case "var", "vars", "variable", "variables":
		return envSetupInputVar, nil
	default:
		return "", fmt.Errorf("--kind must be one of secret|var|all, got %q", raw)
	}
}
