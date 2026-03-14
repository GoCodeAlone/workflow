package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// lockfileEntry is a minimal representation of a plugin lockfile entry.
type lockfileEntry struct {
	SHA256 string `yaml:"sha256"`
}

// lockfileData is the minimal structure of .wfctl.yaml for integrity checks.
type lockfileData struct {
	Plugins map[string]lockfileEntry `yaml:"plugins"`
}

// VerifyPluginIntegrity checks the plugin binary's SHA-256 against the lockfile.
// Returns nil if no lockfile exists, no entry for this plugin, or no checksum pinned.
func VerifyPluginIntegrity(pluginDir, pluginName string) error {
	// Search for lockfile: CWD first, then parent dirs up to 3 levels
	lockfilePath := findLockfile()
	if lockfilePath == "" {
		return nil
	}

	data, err := os.ReadFile(lockfilePath)
	if err != nil {
		return nil //nolint:nilerr // intentional: skip verification when lockfile unreadable
	}

	var lf lockfileData
	if err := yaml.Unmarshal(data, &lf); err != nil {
		return nil //nolint:nilerr // intentional: skip verification when lockfile unparseable
	}

	entry, ok := lf.Plugins[pluginName]
	if !ok || entry.SHA256 == "" {
		return nil // not pinned
	}

	binaryPath := filepath.Join(pluginDir, pluginName, pluginName)
	binaryData, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read plugin binary %s: %w", binaryPath, err)
	}

	h := sha256.Sum256(binaryData)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, entry.SHA256) {
		return fmt.Errorf("plugin %q integrity check failed: binary checksum %s does not match lockfile %s", pluginName, got, entry.SHA256)
	}
	return nil
}

// findLockfile searches for .wfctl.yaml starting from CWD.
func findLockfile() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 4; i++ {
		p := filepath.Join(dir, ".wfctl.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
