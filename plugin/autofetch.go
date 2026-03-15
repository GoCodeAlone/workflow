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
	// Check both pluginName and workflow-plugin-<pluginName> (or the short form
	// if pluginName already has the "workflow-plugin-" prefix).
	if isPluginInstalled(pluginName, pluginDir) {
		return nil
	}

	fmt.Fprintf(os.Stderr, "[auto-fetch] Plugin %q not found locally, fetching from registry...\n", pluginName)

	// Build install argument with version if specified.
	installArg := pluginName
	if version != "" {
		stripped, ok := stripVersionConstraint(version)
		if !ok {
			// Complex constraint (e.g. ">=0.1.0,<0.2.0") — install latest instead.
			fmt.Fprintf(os.Stderr, "[auto-fetch] Version constraint %q is complex; installing latest version of %q\n", version, pluginName)
			stripped = ""
		}
		if stripped != "" {
			installArg = pluginName + "@" + stripped
		}
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

// isPluginInstalled returns true if the plugin is already present under pluginDir.
// It checks both pluginName and the "workflow-plugin-<short>" alternate form.
func isPluginInstalled(pluginName, pluginDir string) bool {
	if _, err := os.Stat(filepath.Join(pluginDir, pluginName, "plugin.json")); err == nil {
		return true
	}

	// Also check the alternate naming convention.
	const prefix = "workflow-plugin-"
	var alt string
	if strings.HasPrefix(pluginName, prefix) {
		// e.g. "workflow-plugin-foo" → check "foo"
		alt = pluginName[len(prefix):]
	} else {
		// e.g. "foo" → check "workflow-plugin-foo"
		alt = prefix + pluginName
	}
	if _, err := os.Stat(filepath.Join(pluginDir, alt, "plugin.json")); err == nil {
		return true
	}

	return false
}

// stripVersionConstraint strips a simple semver constraint prefix (>=, ^, ~) from
// version and returns the bare version string. The second return value is false when
// the constraint is compound (contains commas or spaces between tokens) and cannot
// be reduced to a single version — callers should fall back to installing the latest.
func stripVersionConstraint(version string) (string, bool) {
	if version == "" {
		return "", true
	}

	// Detect compound constraints such as ">=0.1.0,<0.2.0" or ">=0.1.0 <0.2.0".
	if strings.Contains(version, ",") || strings.Count(version, " ") > 1 {
		return "", false
	}

	v := version
	for _, p := range []string{">=", "<=", "!=", "^", "~", ">", "<"} {
		if strings.HasPrefix(v, p) {
			v = v[len(p):]
			break
		}
	}

	// After stripping, if the result still contains operators it's complex.
	if strings.ContainsAny(v, "<>=!,") {
		return "", false
	}

	return v, true
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
//
// Callers should invoke this before plugin discovery/loading so that newly
// fetched plugins are available in the current startup.
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

	anyFetched := false
	for _, d := range decls {
		if !d.AutoFetch {
			continue
		}
		// Record whether the plugin was already present before fetching.
		alreadyPresent := isPluginInstalled(d.Name, pluginDir)
		if err := AutoFetchPlugin(d.Name, d.Version, pluginDir); err != nil {
			if logger != nil {
				logger.Warn("auto-fetch failed for plugin", "plugin", d.Name, "error", err)
			}
			continue
		}
		if !alreadyPresent && isPluginInstalled(d.Name, pluginDir) {
			anyFetched = true
		}
	}

	if anyFetched && logger != nil {
		logger.Info("auto-fetch downloaded new plugins; they will be discovered during startup",
			"plugin_dir", pluginDir)
	}
}
