package sdk

// CLIProvider is implemented by plugins that expose top-level wfctl subcommands.
// When wfctl detects a matching command it invokes the plugin binary with
// --wfctl-cli <command> [args...], and the plugin must exit with the returned code.
type CLIProvider interface {
	// RunCLI handles the command. args contains the command and all subsequent
	// arguments (the plugin binary path and the --wfctl-cli flag are stripped).
	// The return value becomes the process exit code.
	RunCLI(args []string) int
}
