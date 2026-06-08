package main

import (
	"flag"
	"fmt"
	"strings"
)

func runPluginRun(args []string) error {
	fs := flag.NewFlagSet("plugin run", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultPluginCommandDir(), "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultPluginCommandDir(), "Plugin directory (deprecated, use --plugin-dir)")
	ensureInstalled := fs.Bool("ensure-installed", false, "Install or update the plugin before dispatching")
	cfgPath := fs.String("config", "", "Registry config file path")
	registryName := fs.String("registry", "", "Use a specific registry by name")
	directURL := fs.String("url", "", "Install from a direct download URL (tar.gz archive)")
	localPath := fs.String("local", "", "Install from a local plugin directory")
	sha256Flag := fs.String("sha256", "", "Expected SHA256 hex digest of the downloaded archive (for --url installs)")
	skipChecksum := fs.Bool("skip-checksum", false, "Skip integrity verification during install")
	compatMode := fs.String("compat-mode", "", "Compatibility mode for registry installs: enforce or warn")
	engineVersion := fs.String("engine-version", "", "Workflow engine version for compatibility resolution")
	forceCompat := fs.Bool("force", false, "Permit known-failing compatibility evidence while still enforcing checksums")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin run [options] <plugin-name-or-github-ref> -- <plugin-command> [args...]\n\nInstall if requested, then dispatch a plugin-provided wfctl command without requiring a Workflow project.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	for _, arg := range args {
		if len(args) != 1 {
			break
		}
		if arg == "-h" || arg == "--help" {
			fs.Usage()
			return nil
		}
	}
	runnerArgs, commandArgs, err := splitPluginRunArgs(args)
	if err != nil {
		return err
	}
	parsedRunnerArgs, err := interspersedPluginInstallArgs(fs, runnerArgs)
	if err != nil {
		return err
	}
	if err := fs.Parse(parsedRunnerArgs); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return fmt.Errorf("exactly one plugin name or GitHub ref is required")
	}
	if len(commandArgs) == 0 {
		return fmt.Errorf("plugin command is required after --")
	}
	pluginRef := fs.Arg(0)

	installMode := *directURL != "" || *localPath != ""
	if installMode && !*ensureInstalled {
		return fmt.Errorf("--url and --local require --ensure-installed")
	}
	if *ensureInstalled {
		installArgs := []string{"--plugin-dir", pluginDirVal}
		if *cfgPath != "" {
			installArgs = append(installArgs, "--config", *cfgPath)
		}
		if *registryName != "" {
			installArgs = append(installArgs, "--registry", *registryName)
		}
		if *sha256Flag != "" {
			installArgs = append(installArgs, "--sha256", *sha256Flag)
		}
		if *skipChecksum {
			installArgs = append(installArgs, "--skip-checksum")
		}
		if *compatMode != "" {
			installArgs = append(installArgs, "--compat-mode", *compatMode)
		}
		if *engineVersion != "" {
			installArgs = append(installArgs, "--engine-version", *engineVersion)
		}
		if *forceCompat {
			installArgs = append(installArgs, "--force")
		}
		switch {
		case *directURL != "":
			installArgs = append(installArgs, "--url", *directURL)
		case *localPath != "":
			installArgs = append(installArgs, "--local", *localPath)
		default:
			installArgs = append(installArgs, pluginRef)
		}
		if err := withInstallLockfileSuppressed(func() error {
			return runPluginInstall(installArgs)
		}); err != nil {
			return err
		}
	}

	return dispatchPluginRun(pluginDirVal, pluginRef, commandArgs)
}

func splitPluginRunArgs(args []string) ([]string, []string, error) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:], nil
		}
	}
	return nil, nil, fmt.Errorf("missing -- before plugin command")
}

func dispatchPluginRun(pluginDir, pluginRef string, commandArgs []string) error {
	if len(commandArgs) == 0 {
		return fmt.Errorf("plugin command is required after --")
	}
	registry, err := BuildCLIRegistry(pluginDir)
	if err != nil {
		return fmt.Errorf("load plugin CLI commands: %w", err)
	}
	entry := registry.LookupCLICommand(commandArgs[0])
	if entry == nil {
		return fmt.Errorf("plugin command %q is not installed in %s", commandArgs[0], pluginDir)
	}
	if !pluginRunMatchesEntry(pluginRef, entry) {
		return fmt.Errorf("plugin %q does not own command %q (owned by %q)", pluginRef, commandArgs[0], entry.PluginName)
	}
	return DispatchCLICommand(entry, commandArgs)
}

func pluginRunMatchesEntry(pluginRef string, entry *CLIRegistryEntry) bool {
	if entry == nil {
		return false
	}
	return normalizePluginName(pluginRunRawName(pluginRef)) == normalizePluginName(entry.PluginName)
}

func pluginRunInstallName(pluginRef string) string {
	return normalizePluginName(pluginRunRawName(pluginRef))
}

func pluginRunRawName(pluginRef string) string {
	_, repo, _, isGitHub := parseGitHubRef(pluginRef)
	if isGitHub {
		return repo
	}
	rawName, _ := parseNameVersion(pluginRef)
	return strings.TrimSpace(rawName)
}

func withInstallLockfileSuppressed(fn func() error) error {
	prev := installSkipLockfileUpdate
	installSkipLockfileUpdate = true
	defer func() {
		installSkipLockfileUpdate = prev
	}()
	return fn()
}
