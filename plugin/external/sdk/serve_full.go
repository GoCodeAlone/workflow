package sdk

import (
	"fmt"
	"io"
	"os"
)

// ServePluginFull is the multi-mode entry point for plugin authors that use
// CLI commands and/or build-pipeline hook handlers in addition to the standard
// gRPC plugin server.
//
// Dispatch rules (os.Args inspected at startup):
//  1. --wfctl-cli  → CLIProvider.RunCLI is called; process exits with its return code.
//  2. --wfctl-hook → HookHandler.HandleBuildHook is called with the event name and
//     the JSON payload read from stdin; result is written to stdout; process exits 0.
//  3. Neither flag → falls through to the standard gRPC Serve(p).
//
// Plugins that don't need CLI/hook capabilities keep using Serve(p).
//
// Usage:
//
//	func main() {
//	    sdk.ServePluginFull(&myPlugin{}, &myCLI{}, &myHooks{})
//	}
func ServePluginFull(p PluginProvider, cli CLIProvider, hooks HookHandler) {
	code := DispatchArgs(os.Args, p, cli, hooks)
	if code < 0 {
		// code -1 means "no special flag found — run normal gRPC serve"
		Serve(p)
		return
	}
	os.Exit(code)
}

// DispatchArgs is the testable core of ServePluginFull. It inspects args (which
// should be os.Args in production) and dispatches accordingly.
//
// Returns:
//   - -1 if no wfctl flag is present (caller should fall back to Serve)
//   - 0 on success
//   - >0 on error
func DispatchArgs(args []string, p PluginProvider, cli CLIProvider, hooks HookHandler) int {
	for i, arg := range args {
		switch arg {
		case "--wfctl-cli":
			if cli == nil {
				_, _ = fmt.Fprintln(os.Stderr, "wfctl-cli: plugin does not implement CLIProvider")
				return 1
			}
			// Strip binary name and --wfctl-cli; pass the rest to the provider.
			var cliArgs []string
			if i+1 < len(args) {
				cliArgs = args[i+1:]
			}
			return cli.RunCLI(cliArgs)

		case "--wfctl-hook":
			if hooks == nil {
				_, _ = fmt.Fprintln(os.Stderr, "wfctl-hook: plugin does not implement HookHandler")
				return 1
			}
			event := ""
			if i+1 < len(args) {
				event = args[i+1]
			}
			if event == "" {
				_, _ = fmt.Fprintln(os.Stderr, "wfctl-hook: missing event name after --wfctl-hook")
				return 1
			}
			payload, err := io.ReadAll(os.Stdin)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "wfctl-hook: read stdin: %v\n", err)
				return 1
			}
			result, err := hooks.HandleBuildHook(event, payload)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "wfctl-hook: handler error: %v\n", err)
				return 1
			}
			if len(result) > 0 {
				if _, err := os.Stdout.Write(result); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "wfctl-hook: write result: %v\n", err)
					return 1
				}
			}
			return 0
		}
	}
	// No wfctl flag found — signal caller to run normal gRPC serve.
	return -1
}
