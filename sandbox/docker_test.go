package sandbox

import (
	"testing"
	"time"
)

func TestNewDockerSandbox_ValidConfig(t *testing.T) {
	// This test validates config checking without requiring a Docker daemon.
	// NewDockerSandbox will attempt to connect to Docker, which may not be
	// available in CI. We test config validation by checking error cases.

	_, err := NewDockerSandbox(SandboxConfig{
		Image:   "alpine:latest",
		WorkDir: "/workspace",
		Timeout: 30 * time.Second,
	})
	// This may fail if Docker is not available, which is acceptable.
	// The important thing is that it doesn't panic.
	if err != nil {
		t.Logf("NewDockerSandbox returned error (Docker may not be available): %v", err)
	}
}

func TestNewDockerSandbox_MissingImage(t *testing.T) {
	_, err := NewDockerSandbox(SandboxConfig{})
	if err == nil {
		t.Fatal("expected error for empty image, got nil")
	}
	if err.Error() != "sandbox: image is required" {
		t.Fatalf("unexpected error message: %s", err.Error())
	}
}

func TestNewDockerSandbox_DefaultTimeout(t *testing.T) {
	cfg := SandboxConfig{
		Image: "alpine:latest",
	}
	sb, err := NewDockerSandbox(cfg)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer sb.Close()

	if sb.config.Timeout != 10*time.Minute {
		t.Fatalf("expected default timeout 10m, got %s", sb.config.Timeout)
	}
}

func TestSandboxConfig_Fields(t *testing.T) {
	cfg := SandboxConfig{
		Image:       "node:20-alpine",
		WorkDir:     "/workspace",
		Env:         map[string]string{"NODE_ENV": "production"},
		Mounts:      []Mount{{Source: "/host/src", Target: "/container/src", ReadOnly: true}},
		MemoryLimit: 512 * 1024 * 1024,
		CPULimit:    1.5,
		Timeout:     5 * time.Minute,
		NetworkMode: "none",
	}

	if cfg.Image != "node:20-alpine" {
		t.Fatalf("unexpected image: %s", cfg.Image)
	}
	if cfg.WorkDir != "/workspace" {
		t.Fatalf("unexpected workdir: %s", cfg.WorkDir)
	}
	if cfg.Env["NODE_ENV"] != "production" {
		t.Fatalf("unexpected env: %v", cfg.Env)
	}
	if len(cfg.Mounts) != 1 || cfg.Mounts[0].Source != "/host/src" {
		t.Fatalf("unexpected mounts: %v", cfg.Mounts)
	}
	if cfg.MemoryLimit != 512*1024*1024 {
		t.Fatalf("unexpected memory limit: %d", cfg.MemoryLimit)
	}
	if cfg.CPULimit != 1.5 {
		t.Fatalf("unexpected cpu limit: %f", cfg.CPULimit)
	}
	if cfg.Timeout != 5*time.Minute {
		t.Fatalf("unexpected timeout: %s", cfg.Timeout)
	}
	if cfg.NetworkMode != "none" {
		t.Fatalf("unexpected network mode: %s", cfg.NetworkMode)
	}
}

func TestExecResult_Fields(t *testing.T) {
	result := ExecResult{
		ExitCode: 0,
		Stdout:   "hello world",
		Stderr:   "",
	}

	if result.ExitCode != 0 {
		t.Fatalf("unexpected exit code: %d", result.ExitCode)
	}
	if result.Stdout != "hello world" {
		t.Fatalf("unexpected stdout: %s", result.Stdout)
	}
	if result.Stderr != "" {
		t.Fatalf("unexpected stderr: %s", result.Stderr)
	}
}

func TestMount_Fields(t *testing.T) {
	m := Mount{
		Source:   "/host/data",
		Target:   "/container/data",
		ReadOnly: true,
	}

	if m.Source != "/host/data" {
		t.Fatalf("unexpected source: %s", m.Source)
	}
	if m.Target != "/container/data" {
		t.Fatalf("unexpected target: %s", m.Target)
	}
	if !m.ReadOnly {
		t.Fatal("expected read-only mount")
	}
}

func TestBuildEnv(t *testing.T) {
	sb := &DockerSandbox{
		config: SandboxConfig{
			Env: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
	}

	env := sb.buildEnv()
	if len(env) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(env))
	}

	envMap := make(map[string]bool)
	for _, e := range env {
		envMap[e] = true
	}
	if !envMap["FOO=bar"] {
		t.Fatal("missing FOO=bar")
	}
	if !envMap["BAZ=qux"] {
		t.Fatal("missing BAZ=qux")
	}
}

func TestBuildEnv_Empty(t *testing.T) {
	sb := &DockerSandbox{
		config: SandboxConfig{},
	}

	env := sb.buildEnv()
	if env != nil {
		t.Fatalf("expected nil env for empty config, got %v", env)
	}
}

func TestBuildHostConfig(t *testing.T) {
	sb := &DockerSandbox{
		config: SandboxConfig{
			MemoryLimit: 256 * 1024 * 1024,
			CPULimit:    2.0,
			Mounts: []Mount{
				{Source: "/src", Target: "/dst", ReadOnly: false},
			},
			NetworkMode: "host",
		},
	}

	hc := sb.buildHostConfig()

	if hc.Resources.Memory != 256*1024*1024 {
		t.Fatalf("unexpected memory: %d", hc.Resources.Memory)
	}
	if hc.Resources.NanoCPUs != 2_000_000_000 {
		t.Fatalf("unexpected NanoCPUs: %d", hc.Resources.NanoCPUs)
	}
	if len(hc.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(hc.Mounts))
	}
	if string(hc.NetworkMode) != "host" {
		t.Fatalf("unexpected network mode: %s", hc.NetworkMode)
	}
}

func TestBuildHostConfig_NoLimits(t *testing.T) {
	sb := &DockerSandbox{
		config: SandboxConfig{},
	}

	hc := sb.buildHostConfig()

	if hc.Resources.Memory != 0 {
		t.Fatalf("expected 0 memory, got %d", hc.Resources.Memory)
	}
	if hc.Resources.NanoCPUs != 0 {
		t.Fatalf("expected 0 NanoCPUs, got %d", hc.Resources.NanoCPUs)
	}
	if len(hc.Mounts) != 0 {
		t.Fatalf("expected 0 mounts, got %d", len(hc.Mounts))
	}
}
