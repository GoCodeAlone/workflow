package sandbox

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	RecordMaxMemoryBytes = 1 << 30
	RecordMaxCPUs        = 2
	RecordMaxPIDs        = 256
	RecordMaxTimeout     = 10 * time.Minute
)

// ValidateRecordConfig is the parent-side policy for brokered local execution.
// Errors deliberately never include config values, environment, or credentials.
func ValidateRecordConfig(cfg SandboxConfig) error {
	if cfg.GetProfile() != "strict" || cfg.User != "65532:65532" || len(cfg.Mounts) != 0 ||
		!cfg.ReadOnlyRootfs || !cfg.NoNewPrivileges || len(cfg.CapAdd) != 0 ||
		!slices.Equal(cfg.CapDrop, []string{"ALL"}) || len(cfg.SecurityOpts) != 0 {
		return fmt.Errorf("sandbox: record mode requires strict nonroot mountless execution")
	}
	if cfg.MemoryLimit <= 0 || cfg.MemoryLimit > RecordMaxMemoryBytes ||
		math.IsNaN(cfg.CPULimit) || math.IsInf(cfg.CPULimit, 0) || cfg.CPULimit < 0.01 || cfg.CPULimit > RecordMaxCPUs ||
		cfg.PidsLimit <= 0 || cfg.PidsLimit > RecordMaxPIDs || cfg.Timeout <= 0 || cfg.Timeout > RecordMaxTimeout {
		return fmt.Errorf("sandbox: record mode requires positive bounded resources")
	}
	if cfg.NetworkMode != "none" && cfg.NetworkMode != "bridge" {
		return fmt.Errorf("sandbox: record mode requires none or bridge networking")
	}
	if cfg.Image == "" || strings.HasPrefix(cfg.Image, "-") || strings.ContainsAny(cfg.Image, " \t\r\n\x00") {
		return fmt.Errorf("sandbox: invalid record image")
	}
	if err := ValidateFilesystem(cfg); err != nil {
		return err
	}
	options := make(map[string]string)
	for _, option := range strings.Split(cfg.Tmpfs["/tmp"], ",") {
		key, value, _ := strings.Cut(option, "=")
		options[key] = value
	}
	if options["mode"] != "1777" || options["uid"] != "65532" || options["gid"] != "65532" {
		return fmt.Errorf("sandbox: record mode requires nonroot mode-1777 /tmp tmpfs")
	}
	for key, value := range cfg.Env {
		if key == "" || strings.ContainsAny(key, "=\x00\r\n") || strings.ContainsRune(value, '\x00') || reservedRecordEnvironment(key) {
			return fmt.Errorf("sandbox: invalid or reserved record environment")
		}
	}
	return nil
}

func reservedRecordEnvironment(key string) bool {
	key = strings.ToUpper(key)
	for _, prefix := range []string{"DOCKER_", "WFCTL_", "ACTIONS_", "REGISTRY_PROPOSER_"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	switch key {
	case "GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PAT", "REGISTRY_TOKEN", "SSH_AUTH_SOCK",
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"GOOGLE_APPLICATION_CREDENTIALS", "AZURE_CLIENT_SECRET":
		return true
	}
	return false
}
