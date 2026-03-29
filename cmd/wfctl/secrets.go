package main

import (
	"flag"
	"fmt"
)

func runSecrets(args []string) error {
	if len(args) < 1 {
		return secretsUsage()
	}
	switch args[0] {
	case "detect":
		return runSecretsDetect(args[1:])
	case "set":
		return runSecretsSet(args[1:])
	case "list":
		return runSecretsList(args[1:])
	case "validate":
		return runSecretsValidate(args[1:])
	case "init":
		return runSecretsInit(args[1:])
	case "rotate":
		return runSecretsRotate(args[1:])
	case "sync":
		return runSecretsSync(args[1:])
	default:
		return secretsUsage()
	}
}

func secretsUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl secrets <action> [options]

Manage application secrets lifecycle.

Actions:
  detect    Scan config for secret-like values and env var references
  set       Set a secret value (use --from-file for certificates/keys)
  list      List all declared secrets and their status
  validate  Validate that all declared secrets are set in the provider
  init      Initialize secrets provider configuration
  rotate    Trigger rotation of a secret
  sync      Copy secret structure between environments

Examples:
  wfctl secrets detect --config app.yaml
  wfctl secrets set DATABASE_URL --value "postgres://..."
  wfctl secrets set TLS_CERT --from-file ./certs/server.crt
  wfctl secrets list --config app.yaml
  wfctl secrets validate --config app.yaml
  wfctl secrets init --provider env --env staging
  wfctl secrets rotate DATABASE_URL --env production
  wfctl secrets sync --from staging --to production
`)
	return fmt.Errorf("missing or unknown action")
}
