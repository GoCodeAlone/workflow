package sandbox

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateRecordConfig(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*SandboxConfig)
	}{
		{"host mount", func(c *SandboxConfig) { c.Mounts = []Mount{{Source: "/secret", Target: "/work"}} }},
		{"non-strict", func(c *SandboxConfig) { c.Profile = "standard" }},
		{"root", func(c *SandboxConfig) { c.User = "0:0" }},
		{"writable root", func(c *SandboxConfig) { c.ReadOnlyRootfs = false }},
		{"privileges", func(c *SandboxConfig) { c.NoNewPrivileges = false }},
		{"add cap", func(c *SandboxConfig) { c.CapAdd = []string{"SYS_ADMIN"} }},
		{"retain caps", func(c *SandboxConfig) { c.CapDrop = nil }},
		{"security override", func(c *SandboxConfig) { c.SecurityOpts = []string{"seccomp=unconfined"} }},
		{"zero memory", func(c *SandboxConfig) { c.MemoryLimit = 0 }},
		{"unbounded memory", func(c *SandboxConfig) { c.MemoryLimit = 1<<30 + 1 }},
		{"zero cpu", func(c *SandboxConfig) { c.CPULimit = 0 }},
		{"unbounded cpu", func(c *SandboxConfig) { c.CPULimit = 2.01 }},
		{"NaN cpu", func(c *SandboxConfig) { c.CPULimit = math.NaN() }},
		{"Inf cpu", func(c *SandboxConfig) { c.CPULimit = math.Inf(1) }},
		{"zero pids", func(c *SandboxConfig) { c.PidsLimit = 0 }},
		{"unbounded pids", func(c *SandboxConfig) { c.PidsLimit = 257 }},
		{"zero timeout", func(c *SandboxConfig) { c.Timeout = 0 }},
		{"unbounded timeout", func(c *SandboxConfig) { c.Timeout = 11 * time.Minute }},
		{"host network", func(c *SandboxConfig) { c.NetworkMode = "host" }},
		{"bad workdir", func(c *SandboxConfig) { c.WorkDir = "relative" }},
		{"bad tmpfs", func(c *SandboxConfig) { c.Tmpfs["/tmp"] = "size=64m,exec" }},
		{"missing tmp", func(c *SandboxConfig) { delete(c.Tmpfs, "/tmp") }},
		{"wrong tmp mode", func(c *SandboxConfig) { c.Tmpfs["/tmp"] = "size=64m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev" }},
		{"wrong tmp owner", func(c *SandboxConfig) { c.Tmpfs["/tmp"] = "size=64m,mode=1777,uid=0,gid=0,noexec,nosuid,nodev" }},
		{"empty image", func(c *SandboxConfig) { c.Image = "" }},
		{"image option", func(c *SandboxConfig) { c.Image = "--privileged" }},
		{"reserved env", func(c *SandboxConfig) { c.Env = map[string]string{"GITHUB_TOKEN": "TEST_SECRET_TOKEN"} }},
		{"Docker env", func(c *SandboxConfig) { c.Env = map[string]string{"docker_host": "TEST_SECRET_TOKEN"} }},
		{"invalid env", func(c *SandboxConfig) { c.Env = map[string]string{"A=B": "TEST_SECRET_TOKEN"} }},
		{"NUL env", func(c *SandboxConfig) { c.Env = map[string]string{"A": "TEST_SECRET_TOKEN\x00"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultSecureSandboxConfig("alpine:latest")
			test.edit(&cfg)
			if err := ValidateRecordConfig(cfg); err == nil || strings.Contains(err.Error(), "TEST_SECRET_TOKEN") {
				t.Fatalf("unsafe record config requires redacted denial: %v", err)
			}
		})
	}
	for _, network := range []string{"none", "bridge"} {
		cfg := DefaultSecureSandboxConfig("alpine:latest")
		cfg.NetworkMode, cfg.WorkDir = network, "/work"
		cfg.Tmpfs["/work"] = "size=64m,mode=0700,uid=65532,gid=65532,noexec,nosuid,nodev"
		cfg.Env = map[string]string{"PUBLISH_TOKEN": "explicitly-allowlisted"}
		if err := ValidateRecordConfig(cfg); err != nil {
			t.Fatalf("safe nonroot work tmpfs/explicit env rejected: %v", err)
		}
	}
}
