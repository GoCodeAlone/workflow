package sandbox

import (
	"testing"
	"time"
)

func TestBuildSandboxConfig_Strict(t *testing.T) {
	cfg := BuildSandboxConfig("strict", "alpine:3.19")

	if cfg.NetworkMode != "none" {
		t.Errorf("strict: expected NetworkMode=none, got %q", cfg.NetworkMode)
	}
	if len(cfg.CapDrop) != 1 || cfg.CapDrop[0] != "ALL" {
		t.Errorf("strict: expected CapDrop=[ALL], got %v", cfg.CapDrop)
	}
	if !cfg.NoNewPrivileges {
		t.Error("strict: expected NoNewPrivileges=true")
	}
	if !cfg.ReadOnlyRootfs {
		t.Error("strict: expected ReadOnlyRootfs=true")
	}
	if cfg.PidsLimit != 64 {
		t.Errorf("strict: expected PidsLimit=64, got %d", cfg.PidsLimit)
	}
	if cfg.MemoryLimit != 256*1024*1024 {
		t.Errorf("strict: expected MemoryLimit=256MiB, got %d", cfg.MemoryLimit)
	}
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("strict: expected Timeout=5m, got %s", cfg.Timeout)
	}
	if cfg.Image != "alpine:3.19" {
		t.Errorf("strict: expected Image=alpine:3.19, got %s", cfg.Image)
	}
}

func TestBuildSandboxConfig_Standard(t *testing.T) {
	cfg := BuildSandboxConfig("standard", "alpine:3.19")

	if cfg.NetworkMode != "bridge" {
		t.Errorf("standard: expected NetworkMode=bridge, got %q", cfg.NetworkMode)
	}
	if len(cfg.CapAdd) == 0 || cfg.CapAdd[0] != "NET_BIND_SERVICE" {
		t.Errorf("standard: expected CapAdd=[NET_BIND_SERVICE], got %v", cfg.CapAdd)
	}
	if !cfg.NoNewPrivileges {
		t.Error("standard: expected NoNewPrivileges=true")
	}
	if cfg.ReadOnlyRootfs {
		t.Error("standard: expected ReadOnlyRootfs=false")
	}
	droppedSet := make(map[string]bool, len(cfg.CapDrop))
	for _, c := range cfg.CapDrop {
		droppedSet[c] = true
	}
	for _, expected := range []string{"NET_ADMIN", "SYS_ADMIN", "SYS_PTRACE", "SETUID", "SETGID"} {
		if !droppedSet[expected] {
			t.Errorf("standard: expected %s in CapDrop, got %v", expected, cfg.CapDrop)
		}
	}
}

func TestBuildSandboxConfig_Permissive(t *testing.T) {
	cfg := BuildSandboxConfig("permissive", "alpine:3.19")

	if cfg.NetworkMode != "bridge" {
		t.Errorf("permissive: expected NetworkMode=bridge, got %q", cfg.NetworkMode)
	}
	if len(cfg.CapDrop) > 0 {
		t.Errorf("permissive: expected no CapDrop, got %v", cfg.CapDrop)
	}
	if cfg.ReadOnlyRootfs {
		t.Error("permissive: expected ReadOnlyRootfs=false")
	}
	if cfg.NoNewPrivileges {
		t.Error("permissive: expected NoNewPrivileges=false")
	}
}

// TestBuildSandboxConfig_UnknownDefaultsToStrict verifies that an unknown profile
// falls back to strict — matching the original step behaviour.
func TestBuildSandboxConfig_UnknownDefaultsToStrict(t *testing.T) {
	cfg := BuildSandboxConfig("unknown-profile", "alpine:3.19")
	strict := BuildSandboxConfig("strict", "alpine:3.19")

	if cfg.NetworkMode != strict.NetworkMode {
		t.Errorf("unknown: expected NetworkMode=%s (strict), got %s", strict.NetworkMode, cfg.NetworkMode)
	}
	if len(cfg.CapDrop) != len(strict.CapDrop) {
		t.Errorf("unknown: expected CapDrop=%v (strict), got %v", strict.CapDrop, cfg.CapDrop)
	}
	if cfg.NoNewPrivileges != strict.NoNewPrivileges {
		t.Errorf("unknown: expected NoNewPrivileges=%v (strict), got %v", strict.NoNewPrivileges, cfg.NoNewPrivileges)
	}
	if cfg.ReadOnlyRootfs != strict.ReadOnlyRootfs {
		t.Errorf("unknown: expected ReadOnlyRootfs=%v (strict), got %v", strict.ReadOnlyRootfs, cfg.ReadOnlyRootfs)
	}
}
