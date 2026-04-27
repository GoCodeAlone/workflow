package main

import (
	"flag"
	"fmt"
)

// runConfig is the wfctl config command dispatcher.
// It groups engine-config-domain subcommands under a single namespace,
// starting with `wfctl config migrate` (formerly `wfctl migrate`).
func runConfig(args []string) error {
	if len(args) < 1 {
		return configUsage()
	}
	switch args[0] {
	case "migrate":
		return runConfigMigrate(args[1:])
	default:
		return fmt.Errorf("unknown wfctl config subcommand %q (available: migrate)", args[0])
	}
}

func configUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl config <subcommand> [options]

Manage engine configuration.

Subcommands:
  migrate   Manage engine config database schema migrations
            (replaces the deprecated wfctl migrate command)

`)
	return fmt.Errorf("missing or unknown subcommand")
}
