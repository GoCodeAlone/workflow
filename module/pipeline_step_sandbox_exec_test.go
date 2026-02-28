package module

import (
	"testing"
	"time"
)

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
		"env":     map[string]any{"FOO": "bar", "NUM": 42},
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
