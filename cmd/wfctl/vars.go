package main

import (
	"flag"
	"fmt"
)

func runVars(args []string) error {
	if len(args) < 1 {
		return varsUsage()
	}
	switch args[0] {
	case "-h", "--help", "help":
		printVarsUsage()
		return flag.ErrHelp
	case "setup":
		return runVarsSetupPlugin(args[1:])
	default:
		return varsUsage()
	}
}

func varsUsage() error {
	printVarsUsage()
	return fmt.Errorf("missing or unknown action")
}

func printVarsUsage() {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl vars <action> [options]

Manage non-secret provider variables and configuration values.

Actions:
  setup   Configure non-secret variables declared by plugin required_config[]

Examples:
  wfctl vars setup --plugin workflow-plugin-cloudflare --from-env
  wfctl vars setup --plugin workflow-plugin-namecheap --scope env --env production
`)
}
