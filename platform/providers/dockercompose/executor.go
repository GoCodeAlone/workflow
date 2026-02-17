// Package dockercompose implements a platform.Provider that maps abstract
// capability declarations to Docker Compose services, networks, and volumes.
// It uses only the standard library and invokes docker compose via exec.Command.
package dockercompose

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ComposeExecutor defines the interface for running docker compose commands.
// This abstraction allows tests to inject a mock executor.
type ComposeExecutor interface {
	// Up starts services defined in the compose file.
	Up(ctx context.Context, projectDir string, files ...string) (string, error)

	// Down stops and removes services defined in the compose file.
	Down(ctx context.Context, projectDir string, files ...string) (string, error)

	// Ps lists running containers for the compose project.
	Ps(ctx context.Context, projectDir string, files ...string) (string, error)

	// Logs retrieves logs from compose services.
	Logs(ctx context.Context, projectDir string, service string, files ...string) (string, error)

	// Version returns the docker compose version string.
	Version(ctx context.Context) (string, error)

	// IsAvailable checks whether docker compose is installed and reachable.
	IsAvailable(ctx context.Context) error
}

// ShellExecutor executes docker compose commands through the system shell.
type ShellExecutor struct {
	// ComposeCommand is the base command to use. Defaults to "docker" with "compose" subcommand.
	ComposeCommand string
}

// NewShellExecutor creates a ShellExecutor that uses the system docker compose.
func NewShellExecutor() *ShellExecutor {
	return &ShellExecutor{
		ComposeCommand: "docker",
	}
}

// Up starts services with docker compose up -d.
func (e *ShellExecutor) Up(ctx context.Context, projectDir string, files ...string) (string, error) {
	args := e.buildArgs(files, "up", "-d", "--remove-orphans")
	return e.run(ctx, projectDir, args...)
}

// Down stops and removes services with docker compose down.
func (e *ShellExecutor) Down(ctx context.Context, projectDir string, files ...string) (string, error) {
	args := e.buildArgs(files, "down", "--remove-orphans")
	return e.run(ctx, projectDir, args...)
}

// Ps lists containers with docker compose ps.
func (e *ShellExecutor) Ps(ctx context.Context, projectDir string, files ...string) (string, error) {
	args := e.buildArgs(files, "ps", "--format", "json")
	return e.run(ctx, projectDir, args...)
}

// Logs retrieves logs for a service with docker compose logs.
func (e *ShellExecutor) Logs(ctx context.Context, projectDir string, service string, files ...string) (string, error) {
	args := e.buildArgs(files, "logs", "--no-color", service)
	return e.run(ctx, projectDir, args...)
}

// Version returns the docker compose version.
func (e *ShellExecutor) Version(ctx context.Context) (string, error) {
	return e.run(ctx, "", "compose", "version", "--short")
}

// IsAvailable checks whether docker compose is installed and reachable.
func (e *ShellExecutor) IsAvailable(ctx context.Context) error {
	_, err := e.Version(ctx)
	if err != nil {
		return fmt.Errorf("docker compose is not available: %w", err)
	}
	return nil
}

func (e *ShellExecutor) buildArgs(files []string, subArgs ...string) []string {
	args := []string{"compose"}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	args = append(args, subArgs...)
	return args
}

func (e *ShellExecutor) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, e.ComposeCommand, args...) //nolint:gosec // ComposeCommand is set internally, not from user input
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		combinedErr := strings.TrimSpace(stderr.String())
		if combinedErr == "" {
			combinedErr = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("docker compose %s failed: %s: %w",
			strings.Join(args, " "), combinedErr, err)
	}

	return strings.TrimSpace(stdout.String()), nil
}

// MockExecutor is a test double for ComposeExecutor that records calls and
// returns pre-configured responses.
type MockExecutor struct {
	// UpFn is called when Up is invoked. If nil, returns empty string and no error.
	UpFn func(ctx context.Context, projectDir string, files ...string) (string, error)

	// DownFn is called when Down is invoked.
	DownFn func(ctx context.Context, projectDir string, files ...string) (string, error)

	// PsFn is called when Ps is invoked.
	PsFn func(ctx context.Context, projectDir string, files ...string) (string, error)

	// LogsFn is called when Logs is invoked.
	LogsFn func(ctx context.Context, projectDir string, service string, files ...string) (string, error)

	// VersionFn is called when Version is invoked.
	VersionFn func(ctx context.Context) (string, error)

	// IsAvailableFn is called when IsAvailable is invoked.
	IsAvailableFn func(ctx context.Context) error

	// Calls records all method calls for assertion purposes.
	Calls []MockCall
}

// MockCall records a single executor method invocation.
type MockCall struct {
	Method string
	Args   []string
}

// Up implements ComposeExecutor.
func (m *MockExecutor) Up(ctx context.Context, projectDir string, files ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Up", Args: append([]string{projectDir}, files...)})
	if m.UpFn != nil {
		return m.UpFn(ctx, projectDir, files...)
	}
	return "", nil
}

// Down implements ComposeExecutor.
func (m *MockExecutor) Down(ctx context.Context, projectDir string, files ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Down", Args: append([]string{projectDir}, files...)})
	if m.DownFn != nil {
		return m.DownFn(ctx, projectDir, files...)
	}
	return "", nil
}

// Ps implements ComposeExecutor.
func (m *MockExecutor) Ps(ctx context.Context, projectDir string, files ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Ps", Args: append([]string{projectDir}, files...)})
	if m.PsFn != nil {
		return m.PsFn(ctx, projectDir, files...)
	}
	return "", nil
}

// Logs implements ComposeExecutor.
func (m *MockExecutor) Logs(ctx context.Context, projectDir string, service string, files ...string) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Logs", Args: append([]string{projectDir, service}, files...)})
	if m.LogsFn != nil {
		return m.LogsFn(ctx, projectDir, service, files...)
	}
	return "", nil
}

// Version implements ComposeExecutor.
func (m *MockExecutor) Version(ctx context.Context) (string, error) {
	m.Calls = append(m.Calls, MockCall{Method: "Version"})
	if m.VersionFn != nil {
		return m.VersionFn(ctx)
	}
	return "2.24.0", nil
}

// IsAvailable implements ComposeExecutor.
func (m *MockExecutor) IsAvailable(ctx context.Context) error {
	m.Calls = append(m.Calls, MockCall{Method: "IsAvailable"})
	if m.IsAvailableFn != nil {
		return m.IsAvailableFn(ctx)
	}
	return nil
}
