package module

import (
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/sandbox"
)

// resolveSandboxRunner returns a SandboxRunner for the requested execution environment.
//
// Supported values for execEnv:
//   - "" or "local-docker" — local Docker daemon (current default behaviour).
//
// Deferred values (will be wired in later PRs):
//   - "remote"    — remote runner (PR7/PR8)
//   - "ephemeral" — ephemeral/cloud runner (PR8/PR9)
//
// Any other value returns a clear error so mis-spelled or not-yet-configured
// exec_env values fail at pipeline construction time rather than silently.
// NOTE: the `app` parameter is intentionally reserved (named `_` today) — PR7/PR8
// (remote runner) + PR9 (Argo) resolve their runner config from the service
// registry via `app`. Do NOT drop it from the signature when wiring those cases.
func resolveSandboxRunner(_ modular.Application, execEnv string, cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
	switch execEnv {
	case "", "local-docker":
		return sandbox.NewLocalDockerRunner(cfg)
	case "remote", "ephemeral":
		// TODO(PR7/PR8/PR9): wire remote and ephemeral runners here.
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not yet configured (deferred to PR7/PR8/PR9)", execEnv)
	default:
		return nil, fmt.Errorf("sandbox_exec: exec_env %q not configured", execEnv)
	}
}
