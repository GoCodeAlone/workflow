package sandbox

import (
	"context"
)

// SandboxRunner is the interface consumed by step.sandbox_exec.
// Only Exec and Close are required — all other DockerSandbox methods are
// lifecycle helpers not used by the step's Execute path.
type SandboxRunner interface {
	// Exec runs cmd inside the sandbox and returns the combined result.
	Exec(ctx context.Context, cmd []string) (*ExecResult, error)
	// Close releases any resources held by the runner (e.g. Docker client).
	Close() error
}

// Compile-time assertion: *DockerSandbox satisfies SandboxRunner.
var _ SandboxRunner = (*DockerSandbox)(nil)

// NewLocalDockerRunner creates a SandboxRunner backed by a local Docker daemon.
// It is the default runner used when exec_env is absent or set to "local-docker".
func NewLocalDockerRunner(cfg SandboxConfig) (SandboxRunner, error) {
	return NewDockerSandbox(cfg)
}
