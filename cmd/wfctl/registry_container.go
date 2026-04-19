package main

import (
	"fmt"
	"os"
)

// runRegistry is the new container-registry dispatcher for `wfctl registry`.
// Routes login|push|prune|logout to the appropriate sub-handler.
// The old plugin-catalog registry is now at `wfctl plugin-registry`.
func runRegistry(args []string) error {
	if len(args) < 1 || (len(args) > 0 && args[0] == "--help") {
		return registryContainerUsage()
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "login":
		return runRegistryLogin(rest)
	case "push":
		return runRegistryPush(rest)
	case "prune":
		return runRegistryPrune(rest)
	case "logout":
		return runRegistryLogout(rest)
	default:
		fmt.Fprintf(os.Stderr, "wfctl registry: unknown subcommand %q\n", sub)
		_ = registryContainerUsage()
		return fmt.Errorf("unknown registry subcommand %q — valid: login, push, prune, logout", sub)
	}
}

func registryContainerUsage() error {
	fmt.Fprintf(os.Stderr, `Usage: wfctl registry <subcommand> [options]

Manage container registries declared in ci.registries[].

Subcommands:
  login   Authenticate to a container registry
  push    Push an image to a declared registry
  prune   Garbage-collect and prune old tags
  logout  Remove stored registry credentials

Options:
  --config <file>   Config file (default: workflow.yaml)
  --registry <name> Registry name from ci.registries[] (default: all)

Use 'wfctl plugin-registry' for plugin catalog management.
`)
	return fmt.Errorf("missing or unknown subcommand")
}

// runRegistryPrune stub — full implementation in T25/T26.
func runRegistryPrune(args []string) error {
	fmt.Println("wfctl registry prune: not yet implemented (T25/T26)")
	return nil
}

// runRegistryLogout removes stored credentials for a registry.
func runRegistryLogout(args []string) error {
	fmt.Println("wfctl registry logout: credentials cleared (provider implementation in T22/T23)")
	return nil
}
