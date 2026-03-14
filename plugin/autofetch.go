package plugin

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AutoFetchPlugin downloads a plugin from the registry if it's not already installed.
// It shells out to wfctl for the actual download/install logic.
// version is an optional semver constraint (e.g., ">=0.1.0" or "0.2.0").
func AutoFetchPlugin(pluginName, version, pluginDir string) error {
	destDir := filepath.Join(pluginDir, pluginName)
	if _, err := os.Stat(filepath.Join(destDir, "plugin.json")); err == nil {
		return nil // already installed
	}

	fmt.Fprintf(os.Stderr, "[auto-fetch] Plugin %q not found locally, fetching from registry...\n", pluginName)

	// Build install argument with version if specified
	installArg := pluginName
	if version != "" {
		// Strip constraint prefixes for the @version syntax
		v := strings.TrimPrefix(version, ">=")
		v = strings.TrimPrefix(v, "^")
		v = strings.TrimPrefix(v, "~")
		installArg = pluginName + "@" + v
	}

	args := []string{"plugin", "install", "--plugin-dir", pluginDir, installArg}
	cmd := exec.Command("wfctl", args...) //nolint:gosec // G204: args are constructed from config, not user input
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("auto-fetch plugin %q: %w", pluginName, err)
	}
	return nil
}

// AutoFetchDecl is the minimum interface the engine passes per declared external plugin.
type AutoFetchDecl struct {
	Name      string
	Version   string
	AutoFetch bool
}

// AutoFetchDeclaredPlugins iterates the declared external plugins and, for each
// with AutoFetch enabled, calls AutoFetchPlugin. If wfctl is not on PATH, a warning
// is logged and the plugin is skipped rather than failing startup. Other errors are
// logged as warnings but do not abort the remaining plugins.
func AutoFetchDeclaredPlugins(decls []AutoFetchDecl, pluginDir string, logger *slog.Logger) {
	if pluginDir == "" || len(decls) == 0 {
		return
	}

	// Check wfctl availability once.
	if _, err := exec.LookPath("wfctl"); err != nil {
		if logger != nil {
			logger.Warn("wfctl not found on PATH; skipping auto-fetch for declared plugins",
				"plugin_dir", pluginDir)
		}
		return
	}

	for _, d := range decls {
		if !d.AutoFetch {
			continue
		}
		if err := AutoFetchPlugin(d.Name, d.Version, pluginDir); err != nil {
			if logger != nil {
				logger.Warn("auto-fetch failed for plugin", "plugin", d.Name, "error", err)
			}
		}
	}
}
