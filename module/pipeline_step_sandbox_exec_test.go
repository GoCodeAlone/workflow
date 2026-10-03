package module

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/sandbox"
)

func TestSandboxExecHonorsIntegerCPUsAndRejectsInvalidResources(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("fixture", map[string]any{"cpu_limit": 1, "timeout": "10m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := step.(*SandboxExecStep).buildSandboxConfig()
	if cfg.CPULimit != 1 || cfg.Timeout != 10*time.Minute {
		t.Fatalf("resource config silently changed: CPU=%v timeout=%v", cfg.CPULimit, cfg.Timeout)
	}
	for _, invalid := range []map[string]any{
		{"cpu_limit": 0}, {"cpu_limit": -1}, {"cpu_limit": math.NaN()}, {"cpu_limit": math.Inf(1)}, {"cpu_limit": "1"},
		{"timeout": "0s"}, {"timeout": "-1s"}, {"timeout": ""}, {"timeout": 1},
	} {
		if _, err := factory("fixture", invalid, nil); err == nil {
			t.Fatalf("invalid resource value was ignored: %#v", invalid)
		}
	}
}

type sandboxExecFixtureRunner struct {
	command []string
	exit    int
	closed  bool
}

func (r *sandboxExecFixtureRunner) Exec(_ context.Context, command []string) (*sandbox.ExecResult, error) {
	r.command = command
	return &sandbox.ExecResult{ExitCode: r.exit, Stdout: "private-output"}, nil
}

func (r *sandboxExecFixtureRunner) Close() error { r.closed = true; return nil }

func TestSandboxExecRuntimeTemplatesAndNonzeroError(t *testing.T) {
	step, err := NewSandboxExecStepFactory()("fixture", map[string]any{
		"command": []any{"printf", "{{ .message }}"},
		"env":     map[string]any{"PUBLIC_VALUE": "{{ .message }}"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	runner := &sandboxExecFixtureRunner{exit: 7}
	var gotConfig sandbox.SandboxConfig
	ctx := sandbox.WithLocalRunnerFactory(context.Background(), func(cfg sandbox.SandboxConfig) (sandbox.SandboxRunner, error) {
		gotConfig = cfg
		return runner, nil
	})
	pc := NewPipelineContext(map[string]any{"message": "runtime"}, nil)
	result, err := step.Execute(ctx, pc)
	if err == nil || result != nil {
		t.Fatalf("nonzero execution must return an error, not a successful stop: %#v, %v", result, err)
	}
	if strings.Contains(err.Error(), "private-output") {
		t.Fatal("captured command output leaked into error")
	}
	if len(runner.command) != 2 || runner.command[1] != "runtime" || gotConfig.Env["PUBLIC_VALUE"] != "runtime" || !runner.closed {
		t.Fatalf("runtime command/env/cleanup not applied: %#v %#v closed=%v", runner.command, gotConfig.Env, runner.closed)
	}
	if step.(*SandboxExecStep).command[1] != "{{ .message }}" {
		t.Fatal("execution mutated shared command config")
	}
}

func TestSandboxExecRejectsNonStringAndReservedEnvironment(t *testing.T) {
	for _, env := range []map[string]any{
		{"VALUE": 42}, {"GITHUB_TOKEN": "private"}, {"ACTIONS_RUNTIME_TOKEN": "private"},
		{"DOCKER_HOST": "unix:///host.sock"}, {"VALUE": map[string]any{"nested": "string"}},
	} {
		if _, err := NewSandboxExecStepFactory()("fixture", map[string]any{"command": []any{"true"}, "env": env}, nil); err == nil {
			t.Fatalf("unsafe environment accepted: %v", env)
		}
	}
}

func TestSandboxExecWorkDirectoryAndTmpfs(t *testing.T) {
	step, err := NewSandboxExecStepFactory()("fixture", map[string]any{
		"command": []any{"true"}, "work_dir": "/work",
		"tmpfs": map[string]any{"/work": "size=96m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := step.(*SandboxExecStep).buildSandboxConfig()
	if cfg.WorkDir != "/work" || !strings.Contains(cfg.Tmpfs["/work"], "mode=0700") {
		t.Fatalf("validated private work filesystem missing: %#v", cfg)
	}
	for _, path := range []string{"relative", "/", "/work/../host", "/work:host"} {
		if _, err := NewSandboxExecStepFactory()("fixture", map[string]any{"command": []any{"true"}, "work_dir": path}, nil); err == nil {
			t.Errorf("invalid work_dir accepted: %q", path)
		}
	}
}

func TestNewSandboxExecStepFactory_Defaults(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("test-step", map[string]any{
		"command": []any{"echo", "hello"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.name != "test-step" {
		t.Fatalf("unexpected name: %s", s.name)
	}
	if s.image != defaultSandboxImage {
		t.Fatalf("expected default image, got %s", s.image)
	}
	if s.securityProfile != "strict" {
		t.Fatalf("expected strict profile, got %s", s.securityProfile)
	}
	if !s.failOnError {
		t.Fatal("expected failOnError true by default")
	}
	if len(s.command) != 2 || s.command[0] != "echo" {
		t.Fatalf("unexpected command: %v", s.command)
	}
}

func TestNewSandboxExecStepFactory_CustomImage(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("s", map[string]any{
		"image":   "alpine:3.19",
		"command": []any{"ls"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.image != "alpine:3.19" {
		t.Fatalf("unexpected image: %s", s.image)
	}
}

func TestNewSandboxExecStepFactory_SecurityProfiles(t *testing.T) {
	factory := NewSandboxExecStepFactory()

	for _, profile := range []string{"strict", "standard", "permissive"} {
		step, err := factory("s", map[string]any{
			"security_profile": profile,
			"command":          []any{"ls"},
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error for profile %q: %v", profile, err)
		}
		s := step.(*SandboxExecStep)
		if s.securityProfile != profile {
			t.Fatalf("expected profile %q, got %q", profile, s.securityProfile)
		}
	}
}

func TestNewSandboxExecStepFactory_InvalidProfile(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	_, err := factory("s", map[string]any{
		"security_profile": "unknown",
		"command":          []any{"ls"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid security_profile")
	}
}

func TestNewSandboxExecStepFactory_MemoryLimit(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	tests := []struct {
		input    string
		expected int64
	}{
		{"128m", 128 * 1024 * 1024},
		{"256M", 256 * 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"512k", 512 * 1024},
	}
	for _, tt := range tests {
		step, err := factory("s", map[string]any{
			"command":      []any{"ls"},
			"memory_limit": tt.input,
		}, nil)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tt.input, err)
		}
		s := step.(*SandboxExecStep)
		if s.memoryLimit != tt.expected {
			t.Fatalf("input %q: expected %d, got %d", tt.input, tt.expected, s.memoryLimit)
		}
	}
}

func TestNewSandboxExecStepFactory_Timeout(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("s", map[string]any{
		"command": []any{"ls"},
		"timeout": "30s",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.timeout != 30*time.Second {
		t.Fatalf("expected 30s, got %s", s.timeout)
	}
}

func TestNewSandboxExecStepFactory_InvalidTimeout(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	_, err := factory("s", map[string]any{
		"command": []any{"ls"},
		"timeout": "not-a-duration",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

func TestNewSandboxExecStepFactory_FailOnError(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("s", map[string]any{
		"command":       []any{"ls"},
		"fail_on_error": false,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.failOnError {
		t.Fatal("expected failOnError false")
	}
}

func TestNewSandboxExecStepFactory_EnvAndNetwork(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	step, err := factory("s", map[string]any{
		"command": []any{"env"},
		"env":     map[string]any{"FOO": "bar", "NUM": "42"},
		"network": "bridge",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*SandboxExecStep)
	if s.env["FOO"] != "bar" {
		t.Fatalf("unexpected FOO: %s", s.env["FOO"])
	}
	if s.env["NUM"] != "42" {
		t.Fatalf("unexpected NUM: %s", s.env["NUM"])
	}
	if s.network != "bridge" {
		t.Fatalf("unexpected network: %s", s.network)
	}
}

func TestSandboxExecStep_Name(t *testing.T) {
	s := &SandboxExecStep{name: "my-step"}
	if s.Name() != "my-step" {
		t.Fatalf("unexpected name: %s", s.Name())
	}
}

func TestSandboxExecStep_BuildSandboxConfig_Strict(t *testing.T) {
	s := &SandboxExecStep{
		image:           "alpine:3.19",
		securityProfile: "strict",
	}
	cfg := s.buildSandboxConfig()

	if cfg.NetworkMode != "none" {
		t.Fatalf("strict: expected network none, got %s", cfg.NetworkMode)
	}
	if len(cfg.CapDrop) != 1 || cfg.CapDrop[0] != "ALL" {
		t.Fatalf("strict: expected CapDrop ALL, got %v", cfg.CapDrop)
	}
	if !cfg.NoNewPrivileges {
		t.Fatal("strict: expected NoNewPrivileges true")
	}
	if !cfg.ReadOnlyRootfs {
		t.Fatal("strict: expected ReadOnlyRootfs true")
	}
	if cfg.PidsLimit != 64 {
		t.Fatalf("strict: expected PidsLimit 64, got %d", cfg.PidsLimit)
	}
}

func TestSandboxExecStep_BuildSandboxConfig_Standard(t *testing.T) {
	s := &SandboxExecStep{
		image:           "alpine:3.19",
		securityProfile: "standard",
	}
	cfg := s.buildSandboxConfig()

	if cfg.NetworkMode != "bridge" {
		t.Fatalf("standard: expected network bridge, got %s", cfg.NetworkMode)
	}
	if len(cfg.CapAdd) == 0 {
		t.Fatal("standard: expected NET_BIND_SERVICE in CapAdd")
	}
	if !cfg.NoNewPrivileges {
		t.Fatal("standard: expected NoNewPrivileges true")
	}
}

func TestSandboxExecStep_BuildSandboxConfig_Permissive(t *testing.T) {
	s := &SandboxExecStep{
		image:           "alpine:3.19",
		securityProfile: "permissive",
	}
	cfg := s.buildSandboxConfig()

	if cfg.NetworkMode != "bridge" {
		t.Fatalf("permissive: expected network bridge, got %s", cfg.NetworkMode)
	}
	if len(cfg.CapDrop) > 0 {
		t.Fatalf("permissive: expected no CapDrop, got %v", cfg.CapDrop)
	}
	if cfg.ReadOnlyRootfs {
		t.Fatal("permissive: expected ReadOnlyRootfs false")
	}
}

func TestSandboxExecStep_BuildSandboxConfig_Overrides(t *testing.T) {
	s := &SandboxExecStep{
		image:           "alpine:3.19",
		securityProfile: "strict",
		memoryLimit:     512 * 1024 * 1024,
		cpuLimit:        2.0,
		timeout:         10 * time.Second,
		network:         "bridge",
	}
	cfg := s.buildSandboxConfig()

	if cfg.MemoryLimit != 512*1024*1024 {
		t.Fatalf("unexpected MemoryLimit: %d", cfg.MemoryLimit)
	}
	if cfg.CPULimit != 2.0 {
		t.Fatalf("unexpected CPULimit: %f", cfg.CPULimit)
	}
	if cfg.Timeout != 10*time.Second {
		t.Fatalf("unexpected Timeout: %s", cfg.Timeout)
	}
	if cfg.NetworkMode != "bridge" {
		t.Fatalf("unexpected NetworkMode: %s", cfg.NetworkMode)
	}
}

func TestParseMemoryLimit(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"128m", 128 * 1024 * 1024, false},
		{"256M", 256 * 1024 * 1024, false},
		{"1g", 1024 * 1024 * 1024, false},
		{"2G", 2 * 1024 * 1024 * 1024, false},
		{"512k", 512 * 1024, false},
		{"1024K", 1024 * 1024, false},
		{"1024", 1024, false},
		{"1024b", 1024, false},
		{"", 0, true},
		{"abc", 0, true},
	}

	for _, tt := range tests {
		got, err := parseMemoryLimit(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("input %q: expected error, got nil", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tt.input, err)
		}
		if got != tt.expected {
			t.Fatalf("input %q: expected %d, got %d", tt.input, tt.expected, got)
		}
	}
}

func TestNewSandboxExecStepFactory_InvalidCommandType(t *testing.T) {
	factory := NewSandboxExecStepFactory()
	_, err := factory("s", map[string]any{
		"command": "should-be-a-list",
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-list command")
	}
}
