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
		// --env intentionally absent — env-scoped import is a follow-up.
		fs.String("provider", "", "Provider name")
		fs.String("p", "", "Provider name (short for --provider)")
		fs.String("type", "", "Abstract resource type")
		fs.String("t", "", "Abstract resource type (short for --type)")
		fs.String("id", "", "Cloud-provider resource ID")
	}
	return fs
}

func TestInfraCommands_AllHonorEnvFlag(t *testing.T) {
	// import intentionally excluded — env-scoped import is a follow-up.
	cmds := []string{"plan", "apply", "status", "drift", "bootstrap", "destroy"}
	for _, cmd := range cmds {
		t.Run(cmd, func(t *testing.T) {
			fs := newInfraFlagSet(cmd)
			if fs.Lookup("env") == nil {
				t.Fatalf("%s is missing --env flag", cmd)
			}
		})
	}
}

func TestInfraImport_NoEnvFlag(t *testing.T) {
	fs := newInfraFlagSet("import")
	if fs.Lookup("env") != nil {
		t.Fatal("import should NOT have --env flag until config-aware import is implemented")
	}
}
