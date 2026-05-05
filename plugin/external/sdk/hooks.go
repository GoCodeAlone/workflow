package sdk

// HookHandler is implemented by plugins that register build-pipeline hook handlers.
// When wfctl dispatches a hook event it invokes the plugin binary with
// --wfctl-hook <event>, writes the JSON payload to stdin, and reads the
// JSON result from stdout.
type HookHandler interface {
	// HandleBuildHook handles the given hook event.
	// payload is the raw JSON payload from wfctl.
	// result is the raw JSON response written back to wfctl.
	// A non-nil error causes wfctl to apply the plugin's on_hook_failure policy.
	HandleBuildHook(event string, payload []byte) (result []byte, err error)
}
