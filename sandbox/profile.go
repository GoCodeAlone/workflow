package sandbox

import "time"

// BuildSandboxConfig maps a named security profile and image to a SandboxConfig.
// This is the single shared profile-→-config mapping used by step.sandbox_exec
// and reused by remote runner implementations (PR7/8) for their profile clamping.
//
// Profiles:
//   - "strict"     — hardened defaults via DefaultSecureSandboxConfig (no network,
//     drop ALL caps, read-only rootfs).
//   - "standard"   — drops a curated set of dangerous capabilities, bridges network.
//   - "permissive" — minimal restrictions, bridges network.
//
// Unknown profiles default to "strict" (same behaviour as the step's original switch).
func BuildSandboxConfig(profile, image string) SandboxConfig {
	switch profile {
	case "permissive":
		return SandboxConfig{
			Profile:     "permissive",
			Image:       image,
			NetworkMode: "bridge",
		}
	case "standard":
		return SandboxConfig{
			Profile:         "standard",
			Image:           image,
			MemoryLimit:     256 * 1024 * 1024,
			CPULimit:        0.5,
			NetworkMode:     "bridge",
			CapDrop:         []string{"NET_ADMIN", "SYS_ADMIN", "SYS_PTRACE", "SETUID", "SETGID"},
			CapAdd:          []string{"NET_BIND_SERVICE"},
			NoNewPrivileges: true,
			PidsLimit:       64,
			Timeout:         5 * time.Minute,
		}
	default: // "strict" and any unknown value
		cfg := DefaultSecureSandboxConfig(image)
		cfg.Profile = "strict"
		return cfg
	}
}
