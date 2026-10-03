package sandbox

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// ValidateFilesystem validates the step's literal container-only writable paths.
func ValidateFilesystem(cfg SandboxConfig) error {
	validPath := func(value string) bool {
		return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value &&
			!strings.ContainsAny(value, ":,{}\x00\r\n")
	}
	if cfg.WorkDir != "" && !validPath(cfg.WorkDir) {
		return fmt.Errorf("sandbox: work_dir must be a canonical absolute container path")
	}
	for target, spec := range cfg.Tmpfs {
		if !validPath(target) {
			return fmt.Errorf("sandbox: tmpfs target must be a canonical absolute container path")
		}
		options := make(map[string]string)
		for _, option := range strings.Split(spec, ",") {
			key, value, _ := strings.Cut(option, "=")
			if _, exists := options[key]; exists {
				return fmt.Errorf("sandbox: duplicate tmpfs option")
			}
			options[key] = value
			switch key {
			case "noexec", "nosuid", "nodev":
				if value != "" {
					return fmt.Errorf("sandbox: invalid tmpfs flag")
				}
			case "uid", "gid":
				if _, err := strconv.ParseUint(value, 10, 32); err != nil {
					return fmt.Errorf("sandbox: invalid tmpfs owner")
				}
			case "mode":
				mode, err := strconv.ParseUint(value, 8, 16)
				if err != nil || mode > 01777 || mode == 0 {
					return fmt.Errorf("sandbox: invalid tmpfs mode")
				}
			case "size":
				number := value
				if len(value) != 0 && strings.ContainsRune("kKmMgG", rune(value[len(value)-1])) {
					number = value[:len(value)-1]
				}
				size, err := strconv.ParseUint(number, 10, 63)
				if err != nil || size == 0 {
					return fmt.Errorf("sandbox: invalid tmpfs size")
				}
			default:
				return fmt.Errorf("sandbox: unsupported tmpfs option")
			}
		}
		for _, key := range []string{"size", "mode", "uid", "gid", "noexec", "nosuid", "nodev"} {
			if _, exists := options[key]; !exists {
				return fmt.Errorf("sandbox: tmpfs requires size/mode/uid/gid/noexec/nosuid/nodev")
			}
		}
	}
	return nil
}
