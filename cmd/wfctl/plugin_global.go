package main

import (
	"flag"
	"os"
	"path/filepath"
)

const globalPluginDirEnv = "WFCTL_GLOBAL_PLUGIN_DIR"

func defaultGlobalPluginDir() string {
	if override := os.Getenv(globalPluginDirEnv); override != "" {
		return override
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "wfctl", "plugins")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".wfctl", "plugins")
	}
	return filepath.Join(home, ".local", "share", "wfctl", "plugins")
}

func resolvePluginDir(pluginDir string, global bool) string {
	if global {
		return defaultGlobalPluginDir()
	}
	return pluginDir
}

func addGlobalPluginFlags(fs *flag.FlagSet, global *bool) {
	fs.BoolVar(global, "global", false, "Use global plugin directory")
	fs.BoolVar(global, "g", false, "Use global plugin directory (shorthand)")
}
