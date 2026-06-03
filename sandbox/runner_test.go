package sandbox

import (
	"testing"
)

// TestSandboxRunnerInterfaceAssertion verifies at compile time that *DockerSandbox
// satisfies SandboxRunner.  The var _ declaration in runner.go is the real gate;
// this test is a human-readable reminder that the assertion exists.
func TestSandboxRunnerInterfaceAssertion(t *testing.T) {
	// If this file compiles, the compile-time assertion in runner.go passed.
	var _ SandboxRunner = (*DockerSandbox)(nil)
}

// TestNewLocalDockerRunner_ReturnsError verifies that NewLocalDockerRunner fails
// gracefully when the SandboxConfig has no image (the only validation
// NewDockerSandbox enforces without a real Docker socket).
func TestNewLocalDockerRunner_ReturnsError(t *testing.T) {
	_, err := NewLocalDockerRunner(SandboxConfig{Image: ""})
	if err == nil {
		t.Fatal("expected error for empty image, got nil")
	}
}

// TestNewLocalDockerRunner_ReturnsRunnerWithValidConfig verifies that
// NewLocalDockerRunner returns a non-nil SandboxRunner when given a valid config.
// Note: this does NOT connect to Docker; it only initialises the Docker client
// via environment variables (which succeeds even without a running daemon).
func TestNewLocalDockerRunner_ReturnsRunnerWithValidConfig(t *testing.T) {
	runner, err := NewLocalDockerRunner(SandboxConfig{Image: "alpine:3.19"})
	if err != nil {
		// Depends on the Docker client env (DOCKER_HOST/TLS); a failure is a
		// Docker-availability issue, not a regression. Skip (matches docker_test.go).
		t.Skipf("docker client unavailable: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
	// Close the underlying Docker client to avoid fd leaks in tests.
	_ = runner.Close()
}
