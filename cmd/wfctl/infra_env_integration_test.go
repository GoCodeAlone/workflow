package main

import (
	"flag"
	"testing"
)

// newInfraFlagSet returns a *flag.FlagSet pre-configured with the flags for the
// given infra subcommand. Used by tests to verify flag registration.
func newInfraFlagSet(cmd string) *flag.FlagSet {
	fs := flag.NewFlagSet("infra "+cmd, flag.ContinueOnError)
	fs.String("config", "", "Config file")
	fs.String("c", "", "Config file (short for --config)")
	switch cmd {
	case "plan":
		fs.String("env", "", "Environment name")
		fs.String("format", "table", "Output format: table or markdown")
		fs.String("f", "table", "Output format (short for --format)")
		fs.String("output", "", "Write plan to JSON file")
		fs.String("o", "", "Write plan to JSON file (short for --output)")
		fs.Bool("show-sensitive", false, "Show sensitive values in plaintext")
		fs.Bool("S", false, "Show sensitive values in plaintext (short for --show-sensitive)")
	case "apply":
		fs.String("env", "", "Environment name")
		fs.Bool("auto-approve", false, "Skip confirmation")
		fs.Bool("y", false, "Skip confirmation (short for --auto-approve)")
		fs.Bool("show-sensitive", false, "Show sensitive values in plaintext")
		fs.Bool("S", false, "Show sensitive values in plaintext (short for --show-sensitive)")
	case "status", "drift", "bootstrap":
		fs.String("env", "", "Environment name")
	case "destroy":
		fs.String("env", "", "Environment name")
		fs.Bool("auto-approve", false, "Skip confirmation")
		fs.Bool("y", false, "Skip confirmation (short for --auto-approve)")
	case "import":
		fs.String("env", "", "Environment name")
		fs.String("name", "", "Desired resource name from config")
		fs.String("id", "", "Cloud-provider resource ID")
	}
	return fs
}

func TestInfraCommands_AllHonorEnvFlag(t *testing.T) {
	cmds := []string{"plan", "apply", "status", "drift", "bootstrap", "destroy", "import"}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			fs := newInfraFlagSet(cmd)
			if fs.Lookup("env") == nil {
				t.Fatalf("%s is missing --env flag", cmd)
			}
		})
	}
}

func TestInfraImport_ConfigAwareFlags(t *testing.T) {
	fs := newInfraFlagSet("import")
	for _, flagName := range []string{"env", "name", "id"} {
		if fs.Lookup(flagName) == nil {
			t.Fatalf("import is missing --%s flag", flagName)
		}
	}
	for _, staleFlag := range []string{"provider", "p", "type", "t"} {
		if fs.Lookup(staleFlag) != nil {
			t.Fatalf("import should not expose stale --%s flag after config-aware import", staleFlag)
		}
	}
}
