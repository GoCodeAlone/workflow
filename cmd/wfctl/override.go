package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/validation"
)

func runOverride(args []string) error {
	if len(args) < 1 {
		return overrideUsage()
	}
	switch args[0] {
	case "generate":
		return runOverrideGenerate(args[1:])
	case "verify":
		return runOverrideVerify(args[1:])
	default:
		return overrideUsage()
	}
}

func overrideUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl override <action> [options]

Generate and verify challenge-response override tokens.

Actions:
  generate <hash>               Generate a 3-word token for the given rejection hash
  verify   <hash> <token>       Verify a token against a rejection hash

The admin secret is read from the WFCTL_ADMIN_SECRET environment variable.

Examples:
  wfctl override generate deadbeef1234
  wfctl override verify deadbeef1234 able-about-above
`)
	return fmt.Errorf("missing or unknown action")
}

func runOverrideGenerate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: wfctl override generate <hash>")
	}
	hash := args[0]
	secret := os.Getenv("WFCTL_ADMIN_SECRET")
	if secret == "" {
		return fmt.Errorf("WFCTL_ADMIN_SECRET environment variable is required")
	}
	token := validation.GenerateChallenge(secret, hash)
	fmt.Println(token)
	return nil
}

func runOverrideVerify(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: wfctl override verify <hash> <token>")
	}
	hash := args[0]
	token := args[1]
	secret := os.Getenv("WFCTL_ADMIN_SECRET")
	if secret == "" {
		return fmt.Errorf("WFCTL_ADMIN_SECRET environment variable is required")
	}
	if validation.VerifyChallenge(secret, hash, token) {
		fmt.Println("valid")
		return nil
	}
	return fmt.Errorf("invalid token")
}
