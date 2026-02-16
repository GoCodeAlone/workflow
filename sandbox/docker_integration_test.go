//go:build integration

package sandbox

import (
	"context"
	"testing"
	"time"
)

func TestExec_EchoHello(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "hello" {
		t.Fatalf("expected stdout 'hello', got %q", result.Stdout)
	}
}

func TestExec_NonZeroExit(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"sh", "-c", "exit 42"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", result.ExitCode)
	}
}

func TestExec_Stderr(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"sh", "-c", "echo error >&2"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.Stderr != "error" {
		t.Fatalf("expected stderr 'error', got %q", result.Stderr)
	}
}

func TestExec_WithEnvVars(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Env:     map[string]string{"MY_VAR": "hello_world"},
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"sh", "-c", "echo $MY_VAR"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.Stdout != "hello_world" {
		t.Fatalf("expected stdout 'hello_world', got %q", result.Stdout)
	}
}

func TestExec_WithWorkDir(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		WorkDir: "/tmp",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"pwd"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.Stdout != "/tmp" {
		t.Fatalf("expected stdout '/tmp', got %q", result.Stdout)
	}
}

func TestExec_Timeout(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	_, err = sb.Exec(context.Background(), []string{"sleep", "30"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestExec_WithResourceLimits(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:       "alpine:latest",
		MemoryLimit: 64 * 1024 * 1024, // 64MB
		CPULimit:    0.5,
		Timeout:     60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	result, err := sb.Exec(context.Background(), []string{"echo", "constrained"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.Stdout != "constrained" {
		t.Fatalf("expected stdout 'constrained', got %q", result.Stdout)
	}
}

func TestExec_NetworkNone(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:       "alpine:latest",
		NetworkMode: "none",
		Timeout:     60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	// Should succeed even with no network
	result, err := sb.Exec(context.Background(), []string{"echo", "isolated"})
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}

	if result.Stdout != "isolated" {
		t.Fatalf("expected stdout 'isolated', got %q", result.Stdout)
	}
}

func TestExec_EmptyCommand(t *testing.T) {
	sb, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	defer sb.Close()

	_, err = sb.Exec(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}
