package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	engineplugin "github.com/GoCodeAlone/workflow/plugin"
)

// defaultDataDir is the default location for installed plugin binaries.
const defaultDataDir = "data/plugins"

// installSkipLockfileUpdate suppresses ALL lockfile writes when set. Outer
// installers (installFromLockfile / installFromWfctlLockfile) hold the
// lockfile in memory and re-save it themselves; without this guard, inner
// install paths' lockfile writes would be silently overwritten by the
// outer re-save (workflow#771 cycle-5 chokepoint pattern).
//
// NOTE: package-level state. Tests touching this MUST NOT call t.Parallel() —
// cross-test flag leakage would silently break lockfile invariants. See
// design doc §"Top 3 doubts #2" for rationale on rejecting context.Context
// threading.
var installSkipLockfileUpdate bool

func runPluginSearch(args []string) error {
	fs := flag.NewFlagSet("plugin search", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "Registry config file path")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin search [options] [<query>]\n\nSearch the plugin registry by name, description, or keyword.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := ""
	if fs.NArg() > 0 {
		query = strings.Join(fs.Args(), " ")
	}

	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return fmt.Errorf("load registry config: %w", err)
	}
	mr := NewMultiRegistry(cfg)

	fmt.Fprintf(os.Stderr, "Searching registry...\n")
	plugins, err := mr.SearchPlugins(query)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}
	if len(plugins) == 0 {
		fmt.Println("No plugins found.")
		return nil
	}
	fmt.Print(formatPluginSearchResults(plugins))
	return nil
}

// formatPluginSearchResults renders the wfctl plugin search table as a string.
// Extracted from runPluginSearch so unit tests can exercise the formatter
// without capturing stdout.
func formatPluginSearchResults(plugins []PluginSearchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-20s %-10s %-12s %-14s %-12s %s\n", "NAME", "VERSION", "TIER", "STATUS", "SOURCE", "DESCRIPTION")
	fmt.Fprintf(&b, "%-20s %-10s %-12s %-14s %-12s %s\n", "----", "-------", "----", "------", "------", "-----------")
	for _, p := range plugins {
		desc := p.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		status := p.Status
		if status == "" {
			status = "-"
		}
		fmt.Fprintf(&b, "%-20s %-10s %-12s %-14s %-12s %s\n", p.Name, p.Version, p.Tier, status, p.Source, desc)
	}
	return b.String()
}

func runPluginInstall(args []string) error {
	fs := flag.NewFlagSet("plugin install", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	cfgPath := fs.String("config", "", "Registry config file path")
	registryName := fs.String("registry", "", "Use a specific registry by name")
	directURL := fs.String("url", "", "Install from a direct download URL (tar.gz archive)")
	localPath := fs.String("local", "", "Install from a local plugin directory")
	fromConfig := fs.String("from-config", "", "Install all requires.plugins[] from a workflow config file")
	locked := fs.Bool("locked", false, "Install from lockfile without modifying wfctl.yaml or .wfctl-lock.yaml")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to .wfctl-lock.yaml")
	sha256Flag := fs.String("sha256", "", "Expected SHA256 hex digest of the downloaded archive (for --url installs)")
	skipChecksum := fs.Bool("skip-checksum", false, "Skip integrity verification (WARNING: disables supply-chain protection)")
	compatMode := fs.String("compat-mode", "", "Compatibility mode for registry installs: enforce or warn")
	engineVersion := fs.String("engine-version", "", "Workflow engine version for compatibility resolution")
	forceCompat := fs.Bool("force", false, "Permit known-failing compatibility evidence while still enforcing checksums")
	quiet := fs.Bool("quiet", false, "Suppress per-download progress output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin install [options] [<name>[@<version>]]\n\nInstall a plugin from the registry, a URL, a local directory, or from the lockfile.\n\n  wfctl plugin install <name>              Install latest from registry\n  wfctl plugin install <name>@v1.0.0       Install specific version\n  wfctl plugin install --url <url>          Install from a direct download URL\n  wfctl plugin install --local <dir>        Install from a local plugin directory\n  wfctl plugin install --from-config <f>    Install all requires.plugins[] from workflow config\n  wfctl plugin install                      Sync wfctl.yaml when needed, then install all plugins from .wfctl-lock.yaml\n  wfctl plugin install --locked             Install all plugins from .wfctl-lock.yaml without writing\n\nOptions:\n")
		fs.PrintDefaults()
	}
	parsedArgs, err := interspersedPluginInstallArgs(fs, args)
	if err != nil {
		return err
	}
	if err := fs.Parse(parsedArgs); err != nil {
		return err
	}
	restoreDownloadProgress := setDownloadProgressQuiet(*quiet)
	defer restoreDownloadProgress()
	// Validate flag combinations before doing anything else.
	if *sha256Flag != "" && *directURL == "" {
		return fmt.Errorf("--sha256 requires --url (no download URL specified)")
	}
	if *skipChecksum && *sha256Flag != "" {
		return fmt.Errorf("--skip-checksum and --sha256 are contradictory: cannot skip verification while supplying an expected hash")
	}
	if *skipChecksum {
		// Full bypass: ALL checksum verification is skipped, including manifest SHA256
		// and GitHub checksums.txt auto-fetch. Use only for trusted internal URLs.
		fmt.Fprintf(os.Stderr, "WARNING: --skip-checksum is set; ALL integrity verification is disabled.\n")
	}
	if *locked && (*fromConfig != "" || *directURL != "" || *localPath != "" || fs.NArg() > 0) {
		return fmt.Errorf("--locked is only supported for lockfile installs; run without plugin arguments, --from-config, --url, or --local")
	}

	// --from-config: batch install from workflow requires.plugins[].
	if *fromConfig != "" {
		return installFromWorkflowConfig(*fromConfig, pluginDirVal, *cfgPath)
	}

	// Validate mutual exclusivity of install modes.
	modes := 0
	if *directURL != "" {
		modes++
	}
	if *localPath != "" {
		modes++
	}
	if fs.NArg() > 0 {
		modes++
	}
	if modes > 1 {
		return fmt.Errorf("specify only one of: <name>, --url, or --local")
	}

	if *directURL != "" {
		return installFromURL(*directURL, pluginDirVal, *sha256Flag, *skipChecksum)
	}

	if *localPath != "" {
		return installFromLocal(*localPath, pluginDirVal)
	}

	// No args: install all plugins from .wfctl-lock.yaml lockfile.
	if fs.NArg() < 1 {
		if err := prepareProjectLockfileForInstall(*manifestPath, *lockPath, *locked, pluginLockCompatibilityOptions{
			CompatMode:    *compatMode,
			EngineVersion: *engineVersion,
			Force:         *forceCompat,
		}); err != nil {
			return err
		}
		return installFromLockfileWithOptions(pluginDirVal, *cfgPath, *lockPath, *locked)
	}

	nameArg := fs.Arg(0)
	rawName, requestedVersion := parseNameVersion(nameArg)
	pluginName := normalizePluginName(rawName)

	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return fmt.Errorf("load registry config: %w", err)
	}

	var mr *MultiRegistry
	if *registryName != "" {
		// Filter config to only the requested registry
		filtered := &RegistryConfig{}
		for i := range cfg.Registries {
			if cfg.Registries[i].Name == *registryName {
				filtered.Registries = append(filtered.Registries, cfg.Registries[i])
				break
			}
		}
		if len(filtered.Registries) == 0 {
			return fmt.Errorf("registry %q not found in config", *registryName)
		}
		mr = NewMultiRegistry(filtered)
	} else {
		mr = NewMultiRegistry(cfg)
	}

	// Pass rawName (the original, un-normalized name) to FetchManifest so that
	// "workflow-plugin-auth" is tried first in the registry before falling back
	// to the normalized short name "auth". pluginName (normalized) is used only
	// for the on-disk install directory path.
	fmt.Fprintf(os.Stderr, "Fetching manifest for %q...\n", rawName)
	manifest, index, sourceName, registryErr := mr.FetchManifestAndVersionIndex(rawName)

	if registryErr != nil {
		// Registry lookup failed. Try GitHub direct install if input looks like owner/repo[@version].
		ghOwner, ghRepo, ghVersion, isGH := parseGitHubRef(nameArg)
		if !isGH {
			return registryErr
		}
		pluginName = normalizePluginName(ghRepo)
		destDir := filepath.Join(pluginDirVal, pluginName)
		if err := installFromGitHub(ghOwner, ghRepo, ghVersion, destDir, *skipChecksum); err != nil {
			return fmt.Errorf("registry: %w; github: %w", registryErr, err)
		}
		if err := ensurePluginBinary(destDir, pluginName); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not normalize binary name: %v\n", err)
		}
		fmt.Printf("Installed %s to %s\n", nameArg, destDir)
		return nil
	}

	fmt.Fprintf(os.Stderr, "Found in registry %q.\n", sourceName)
	resolvedCompatMode, err := resolvePluginCompatMode(*compatMode, cfg)
	if err != nil {
		return err
	}
	decision, err := ResolvePluginCompatibility(index, manifest, PluginCompatResolverOptions{
		RequestedVersion: requestedVersion,
		EngineVersion:    *engineVersion,
		CompatMode:       resolvedCompatMode,
		Force:            *forceCompat,
		ForceReason:      PluginCompatForceInstall,
		Trust:            registryTrustMode(cfg, sourceName),
	})
	if err != nil {
		return err
	}
	if decision.Warning != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", decision.Warning)
	}
	if decision.Forced {
		fmt.Fprintf(os.Stderr, "warning: forcing compatibility decision (%s)\n", decision.Reason)
	}

	// Pin the manifest to the requested version when it differs from what the registry has.
	// The registry manifest may be stale (e.g. v0.1.0) while the user requests v0.2.1.
	// pinManifestToVersion rewrites download URLs in-place so the right release is fetched.
	registryVersion := manifest.Version
	if decision.Version != "" && decision.Version != manifest.Version {
		manifest = manifestForCompatibilityVersion(manifest, index, decision.Version)
	}

	// Resolve and install dependencies before installing the plugin itself.
	if len(manifest.Dependencies) > 0 {
		resolved := make(map[string]string)
		if err := resolveDependencies(pluginName, manifest, pluginDirVal, *cfgPath, []string{}, resolved); err != nil {
			return fmt.Errorf("resolve dependencies for %q: %w", pluginName, err)
		}
	}

	if err := installPluginFromManifest(pluginDirVal, pluginName, manifest, nil, *skipChecksum); err != nil {
		if requestedVersion != "" && requestedVersion != registryVersion {
			return fmt.Errorf("requested version %s not available for %q (registry manifest is at %s): %w",
				requestedVersion, pluginName, registryVersion, err)
		}
		return err
	}

	// Update .wfctl-lock.yaml lockfile (workflow#771: always-track, gate removed).
	// The chokepoint guard inside updateLockfileWithChecksum (Task 1) is responsible
	// for suppressing writes during outer-frame installers.
	pluginName = normalizePluginName(pluginName)
	binaryChecksum := ""
	binaryPath := filepath.Join(pluginDirVal, pluginName, pluginName)
	if cs, hashErr := hashFileSHA256(binaryPath); hashErr == nil {
		binaryChecksum = cs
	} else {
		fmt.Fprintf(os.Stderr, "warning: could not hash binary %s: %v (lockfile will have no checksum)\n", binaryPath, hashErr)
	}
	updateLockfileWithChecksum(pluginName, manifest.Version, manifest.Repository, sourceName, binaryChecksum)

	return nil
}

func runPluginCI(args []string) error {
	fs := flag.NewFlagSet("plugin ci", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	cfgPath := fs.String("config", "", "Registry config file path")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to .wfctl-lock.yaml")
	quiet := fs.Bool("quiet", false, "Suppress per-download progress output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin ci [options]\n\nInstall plugins from .wfctl-lock.yaml without modifying wfctl.yaml or .wfctl-lock.yaml.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return fmt.Errorf("plugin ci does not accept plugin arguments")
	}
	restoreDownloadProgress := setDownloadProgressQuiet(*quiet)
	defer restoreDownloadProgress()
	if err := prepareProjectLockfileForInstall(*manifestPath, *lockPath, true, pluginLockCompatibilityOptions{}); err != nil {
		return err
	}
	return installFromLockfileWithOptions(pluginDirVal, *cfgPath, *lockPath, true)
}

func prepareProjectLockfileForInstall(manifestPath, lockPath string, locked bool, compatOpts pluginLockCompatibilityOptions) error {
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			if locked {
				return fmt.Errorf("manifest %s not found; locked plugin install requires an existing manifest", manifestPath)
			}
			return nil
		}
		return fmt.Errorf("stat manifest %s: %w", manifestPath, err)
	}
	if err := validateLockfileProvenanceForManifest(manifestPath, lockPath); err == nil {
		return nil
	} else if locked {
		return err
	} else {
		fmt.Fprintf(os.Stderr, "warning: %v; regenerating %s from %s\n", err, lockPath, manifestPath)
	}
	return runPluginLockFromManifestWithOptions(manifestPath, lockPath, compatOpts)
}

func validateLockfileProvenanceForManifest(manifestPath, lockPath string) error {
	manifest, err := config.LoadWfctlManifest(manifestPath)
	if err != nil {
		return err
	}
	lockfile, err := config.LoadWfctlLockfile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("lockfile %s not found; run 'wfctl plugin install' or 'wfctl plugin lock'", lockPath)
		}
		return err
	}
	return config.ValidateWfctlLockfileProvenance(manifest, lockfile)
}

type boolFlag interface {
	IsBoolFlag() bool
}

func interspersedPluginInstallArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	if len(args) == 0 {
		return args, nil
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			name = name[:idx]
		}
		f := fs.Lookup(name)
		if f == nil || strings.Contains(arg, "=") {
			continue
		}
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			continue
		}
		remaining := args[i+1:]
		if len(remaining) == 0 {
			return nil, fmt.Errorf("flag needs an argument: -%s", name)
		}
		value := remaining[0]
		i++
		flags = append(flags, value)
	}
	return append(flags, positionals...), nil
}

// installPluginFromManifest downloads, extracts, and installs a plugin using the
// provided registry manifest. It is shared by runPluginInstall and runPluginUpdate.
// The plugin.json is always written/updated from the manifest to keep version tracking correct.
//
// When verify is non-nil, the install_verify hook is emitted after tarball download
// and before extraction. If the hook dispatcher returns a non-zero error the install
// is aborted and the error is returned.
//
// skipChecksum bypasses integrity verification. When false (the default), installation
// fails unless the checksum can be verified via the manifest SHA256 or auto-fetched
// checksums.txt (for GitHub release URLs).
func installPluginFromManifest(dataDir, pluginName string, manifest *RegistryManifest, verify *config.PluginVerifyConfig, skipChecksum bool) error {
	dl, err := manifest.FindDownload(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	destDir := filepath.Join(dataDir, pluginName)

	// Prepare a staging directory alongside the final destination so that any
	// existing installation is never mutated in place. This prevents stale
	// binaries from surviving an upgrade when the tarball uses a differently-
	// named executable (e.g. GoReleaser platform-suffix names).
	stagingDir, cleanupStaging, err := preparePluginStagingDir(destDir)
	if err != nil {
		return err
	}
	defer cleanupStaging() // no-op if commitPluginStagingDir succeeds (staging renamed away)

	fmt.Fprintf(os.Stderr, "Downloading %s...\n", dl.URL)
	data, err := downloadURL(dl.URL)
	if err != nil {
		return fmt.Errorf("download plugin: %w", err)
	}

	// Integrity check: fail closed unless the checksum can be verified.
	// skipChecksum bypasses ALL verification — honour it first so that callers
	// who set the flag never get a surprise verification failure.
	if !skipChecksum {
		if dl.SHA256 != "" {
			// Manifest provides SHA256 directly — verify it.
			if err := verifyChecksum(data, dl.SHA256); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Checksum verified.\n")
		} else if _, _, _, _, isGH := parseGitHubReleaseDownloadURL(dl.URL); isGH {
			// GitHub release URL without a manifest SHA — auto-fetch checksums.txt.
			expectedSHA, lookupErr := lookupChecksumForURL(dl.URL)
			if lookupErr != nil {
				return fmt.Errorf("auto-fetch checksum for %q: %w", dl.URL, lookupErr)
			}
			if err := verifyChecksum(data, expectedSHA); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Checksum verified (auto-fetched from checksums.txt).\n")
		} else {
			return fmt.Errorf("cannot verify integrity of %q: no SHA256 in manifest and URL is not a GitHub release download (use --skip-checksum to bypass)", dl.URL)
		}
	}

	// Emit install_verify hook after download and before extraction (opt-in via req.Verify).
	// Write tarball to disk so hook handlers can inspect it (e.g. sigstore cosign verify).
	if verify != nil {
		tarballPath := filepath.Join(stagingDir, pluginName+".tar.gz")
		if writeErr := os.WriteFile(tarballPath, data, 0600); writeErr != nil {
			return fmt.Errorf("write tarball for verify hook: %w", writeErr)
		}
		defer os.Remove(tarballPath) //nolint:errcheck
		if hookErr := emitInstallVerifyHook(context.Background(), tarballPath, verify, defaultInstallVerifyHookFn(dataDir)); hookErr != nil {
			return fmt.Errorf("install_verify hook aborted install of %q: %w", pluginName, hookErr)
		}
	}

	fmt.Fprintf(os.Stderr, "Extracting to %s...\n", destDir)
	if err := extractTarGz(data, stagingDir); err != nil {
		return fmt.Errorf("extract plugin: %w", err)
	}

	// Ensure the plugin binary is named to match the plugin name so that
	// ExternalPluginManager.DiscoverPlugins() can find it (expects <dir>/<name>/<name>).
	if err := ensurePluginBinary(stagingDir, pluginName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not normalize binary name: %v\n", err)
	}

	// Write plugin.json from the registry manifest. This keeps the installed
	// version metadata in sync with the manifest. If the tarball already
	// extracted a plugin.json, this overwrites it with the registry version.
	// Failure is a hard error: continuing with an archive-supplied plugin.json
	// could silently drop registry-only metadata (capabilities, CLI commands,
	// build hooks) and would let the archive version bypass verifyInstalledVersion.
	pluginJSONPath := filepath.Join(stagingDir, "plugin.json")
	if writeErr := writeInstalledManifest(pluginJSONPath, manifest); writeErr != nil {
		return fmt.Errorf("write plugin.json: %w", writeErr)
	}

	// Verify the staged plugin.json is valid for ExternalPluginManager.
	fmt.Fprintf(os.Stderr, "Verifying plugin manifest...\n")
	if verifyErr := verifyInstalledPlugin(stagingDir, pluginName); verifyErr != nil {
		return fmt.Errorf("post-install verification failed: %w", verifyErr)
	}

	// Verify the installed version matches what the manifest declares. This
	// catches cases where plugin.json could not be written above.
	if verifyErr := verifyInstalledVersion(stagingDir, manifest.Version); verifyErr != nil {
		return fmt.Errorf("post-install version check failed: %w", verifyErr)
	}

	// Atomically replace any previous installation with the validated staging
	// directory. If this step fails the old installation is left intact.
	if commitErr := commitPluginStagingDir(stagingDir, destDir); commitErr != nil {
		return commitErr
	}

	// Strip any existing "v" prefix from the version before printing so that
	// manifests that store "v0.6.1" don't produce "Installed X vv0.6.1".
	fmt.Printf("Installed %s v%s to %s\n", manifest.Name, strings.TrimPrefix(manifest.Version, "v"), destDir)
	return nil
}

func runPluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin list [options]\n\nList installed plugins.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := os.ReadDir(pluginDirVal)
	if os.IsNotExist(err) {
		fmt.Println("No plugins installed.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read data dir %s: %w", pluginDirVal, err)
	}

	type installed struct {
		name        string
		version     string
		pluginType  string
		description string
	}
	var plugins []installed
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver, pType, desc := readInstalledInfo(filepath.Join(pluginDirVal, e.Name()))
		plugins = append(plugins, installed{name: e.Name(), version: ver, pluginType: pType, description: desc})
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	fmt.Printf("%-20s %-10s %-10s %-14s %s\n", "NAME", "VERSION", "TYPE", "STATUS", "DESCRIPTION")
	fmt.Printf("%-20s %-10s %-10s %-14s %s\n", "----", "-------", "----", "------", "-----------")
	for _, p := range plugins {
		desc := p.description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		// Status is not persisted to disk on install; render "-" for installed plugins.
		fmt.Printf("%-20s %-10s %-10s %-14s %s\n", p.name, p.version, p.pluginType, "-", desc)
	}
	return nil
}

func runPluginUpdate(args []string) error {
	fs := flag.NewFlagSet("plugin update", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	cfgPath := fs.String("config", "", "Registry config file path")
	pinVersion := fs.String("version", "", "Pin to this specific version in wfctl.yaml (skips registry lookup)")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to lockfile")
	compatMode := fs.String("compat-mode", "", "Compatibility mode for registry updates: enforce or warn")
	engineVersion := fs.String("engine-version", "", "Workflow engine version for compatibility resolution")
	forceCompat := fs.Bool("force", false, "Permit known-failing compatibility evidence while still enforcing checksums")
	skipChecksum := fs.Bool("skip-checksum", false, "Skip integrity verification (WARNING: disables supply-chain protection)")
	quiet := fs.Bool("quiet", false, "Suppress per-download progress output")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin update [options] <name>\n\nUpdate an installed plugin to its latest version.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	restoreDownloadProgress := setDownloadProgressQuiet(*quiet)
	defer restoreDownloadProgress()
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	pluginName := fs.Arg(0)

	// --version: pin a specific version in the manifest without hitting registry.
	if *pinVersion != "" {
		if err := updateManifestVersion(pluginName, *pinVersion, *manifestPath, *lockPath); err != nil {
			return err
		}
		fmt.Printf("Pinned %s@%s in wfctl.yaml\n", pluginName, *pinVersion)
		return nil
	}

	pluginDir := filepath.Join(pluginDirVal, pluginName)
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}

	// Read the local plugin.json for fallback: if the central registry doesn't
	// list this plugin, we can try fetching the manifest directly from the
	// plugin's own repository (the "repository" field in plugin.json).
	var localRepoURL string
	if data, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json")); err == nil {
		var pj installedPluginJSON
		if json.Unmarshal(data, &pj) == nil {
			localRepoURL = pj.Repository
		}
	}

	// Check the registry for the latest version before downloading.
	cfg, err := LoadRegistryConfig(*cfgPath)
	if err != nil {
		return fmt.Errorf("load registry config: %w", err)
	}
	mr := NewMultiRegistry(cfg)

	fmt.Fprintf(os.Stderr, "Fetching manifest for %q...\n", pluginName)
	manifest, index, sourceName, registryErr := mr.FetchManifestAndVersionIndex(pluginName)
	if registryErr == nil {
		fmt.Fprintf(os.Stderr, "Found in registry %q.\n", sourceName)
		resolvedCompatMode, err := resolvePluginCompatMode(*compatMode, cfg)
		if err != nil {
			return err
		}
		decision, err := ResolvePluginCompatibility(index, manifest, PluginCompatResolverOptions{
			EngineVersion: *engineVersion,
			CompatMode:    resolvedCompatMode,
			Force:         *forceCompat,
			ForceReason:   PluginCompatForceUpdate,
			Trust:         registryTrustMode(cfg, sourceName),
		})
		if err != nil {
			return err
		}
		if decision.Warning != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", decision.Warning)
		}
		if decision.Forced {
			fmt.Fprintf(os.Stderr, "warning: forcing compatibility decision (%s)\n", decision.Reason)
		}
		if decision.Version != "" && decision.Version != manifest.Version {
			manifest = manifestForCompatibilityVersion(manifest, index, decision.Version)
		}
		installedVer := readInstalledVersion(pluginDir)
		if installedVer == manifest.Version {
			fmt.Printf("already at latest version (%s)\n", manifest.Version)
			return nil
		}
		fmt.Fprintf(os.Stderr, "Updating from %s to %s...\n", installedVer, manifest.Version)
		return installPluginFromManifest(pluginDirVal, pluginName, manifest, nil, *skipChecksum)
	}

	// Registry lookup failed. If the plugin's manifest declares a repository
	// URL, try fetching the manifest directly from there as a fallback.
	if localRepoURL != "" {
		fmt.Fprintf(os.Stderr, "Not found in registry. Trying repository URL %q...\n", localRepoURL)
		manifest, err = fetchManifestFromRepoURL(localRepoURL)
		if err != nil {
			return fmt.Errorf("registry lookup failed (%v); repository fallback also failed: %w", registryErr, err)
		}
		// Validate that the fetched manifest is for the plugin we're updating.
		if manifest.Name != pluginName {
			return fmt.Errorf("manifest name %q does not match plugin %q; refusing to update to prevent installing the wrong plugin", manifest.Name, pluginName)
		}
		installedVer := readInstalledVersion(pluginDir)
		if installedVer == manifest.Version {
			fmt.Printf("already at latest version (%s)\n", manifest.Version)
			return nil
		}
		fmt.Fprintf(os.Stderr, "Updating from %s to %s...\n", installedVer, manifest.Version)
		return installPluginFromManifest(pluginDirVal, pluginName, manifest, nil, *skipChecksum)
	}

	return registryErr
}

func runPluginRemove(args []string) error {
	fs := flag.NewFlagSet("plugin remove", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	manifestPath := fs.String("manifest", wfctlManifestPath, "Path to wfctl.yaml manifest")
	lockPath := fs.String("lock-file", wfctlLockPath, "Path to lockfile")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin remove [options] <name>\n\nUninstall a plugin.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	pluginName := fs.Arg(0)
	// Normalize name for filesystem paths: installs use short names (e.g. "digitalocean"),
	// but the CLI accepts full names too (e.g. "workflow-plugin-digitalocean").
	fsName := normalizePluginName(pluginName)
	pluginDir := filepath.Join(pluginDirVal, fsName)
	binaryInstalled := true
	if _, err := os.Stat(pluginDir); os.IsNotExist(err) {
		binaryInstalled = false
	}

	// Check if the plugin is in the manifest.
	inManifest, manifestErr := pluginExistsInManifest(pluginName, *manifestPath)
	if manifestErr != nil {
		return fmt.Errorf("check manifest: %w", manifestErr)
	}

	// Check lockfile as well: covers the legacy case where no manifest exists
	// but the plugin was recorded in .wfctl-lock.yaml.
	inLockfile, lockfileErr := pluginExistsInLockfile(pluginName, *lockPath)
	if lockfileErr != nil {
		return fmt.Errorf("check lockfile: %w", lockfileErr)
	}

	if !binaryInstalled && !inManifest && !inLockfile {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}

	// Remove from manifest + lockfile when those files exist.
	if err := removeFromManifestAndLockfile(pluginName, *manifestPath, *lockPath); err != nil {
		return err
	}

	if !binaryInstalled {
		fmt.Printf("Removed plugin %q from manifest\n", pluginName)
		return nil
	}
	if err := os.RemoveAll(pluginDir); err != nil {
		return fmt.Errorf("remove plugin %q: %w", pluginName, err)
	}
	fmt.Printf("Removed plugin %q\n", pluginName)
	return nil
}

func runPluginInfo(args []string) error {
	fs := flag.NewFlagSet("plugin info", flag.ContinueOnError)
	var pluginDirVal string
	fs.StringVar(&pluginDirVal, "plugin-dir", defaultDataDir, "Plugin directory")
	fs.StringVar(&pluginDirVal, "data-dir", defaultDataDir, "Plugin directory (deprecated, use -plugin-dir)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: wfctl plugin info [options] <name>\n\nShow details about an installed plugin.\n\nOptions:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("plugin name is required")
	}

	pluginName := fs.Arg(0)
	pluginDir := filepath.Join(pluginDirVal, pluginName)
	absDir, _ := filepath.Abs(pluginDir)
	manifestPath := filepath.Join(absDir, "plugin.json")

	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var m installedPluginJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	fmt.Printf("Name:         %s\n", m.Name)
	fmt.Printf("Version:      %s\n", m.Version)
	fmt.Printf("Author:       %s\n", m.Author)
	fmt.Printf("Description:  %s\n", m.Description)
	if m.License != "" {
		fmt.Printf("License:      %s\n", m.License)
	}
	if m.Type != "" {
		fmt.Printf("Type:         %s\n", m.Type)
	}
	if m.Tier != "" {
		fmt.Printf("Tier:         %s\n", m.Tier)
	}
	if m.Repository != "" {
		fmt.Printf("Repository:   %s\n", m.Repository)
	}
	if m.Capabilities != nil {
		if len(m.Capabilities.ModuleTypes) > 0 {
			fmt.Printf("Module Types: %s\n", strings.Join(m.Capabilities.ModuleTypes, ", "))
		}
		if len(m.Capabilities.StepTypes) > 0 {
			fmt.Printf("Step Types:   %s\n", strings.Join(m.Capabilities.StepTypes, ", "))
		}
		if len(m.Capabilities.TriggerTypes) > 0 {
			fmt.Printf("Trigger Types: %s\n", strings.Join(m.Capabilities.TriggerTypes, ", "))
		}
		if m.Capabilities.IaCProvider != nil {
			fmt.Printf("IaC Provider: %s\n", m.Capabilities.IaCProvider.Name)
		}
	}
	if len(m.Tags) > 0 {
		fmt.Printf("Tags:         %s\n", strings.Join(m.Tags, ", "))
	}

	// Check binary status.
	binaryPath := filepath.Join(absDir, pluginName)
	if info, statErr := os.Stat(binaryPath); statErr == nil {
		fmt.Printf("Binary:       %s (%d bytes)\n", binaryPath, info.Size())
		if info.Mode()&0111 != 0 {
			fmt.Printf("Executable:   yes\n")
		} else {
			fmt.Printf("Executable:   no (WARNING: not executable)\n")
		}
	} else {
		fmt.Printf("Binary:       NOT FOUND (WARNING)\n")
	}

	return nil
}

// installFromURL downloads a plugin tarball from a direct URL and installs it.
//
// expectedSHA256, when non-empty, is verified against the downloaded archive.
// skipChecksum bypasses integrity enforcement; when false and expectedSHA256 is
// empty, the URL must be a GitHub release download so checksums.txt can be
// auto-fetched. Non-GitHub URLs with no SHA and skipChecksum=false are rejected.
func installFromURL(rawURL, pluginDir, expectedSHA256 string, skipChecksum bool) error {
	url := rawURL
	fmt.Fprintf(os.Stderr, "Downloading %s...\n", url)
	data, err := downloadURL(url)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Integrity check: fail closed unless the checksum can be verified.
	// skipChecksum bypasses ALL verification — honour it first.
	if !skipChecksum {
		if expectedSHA256 != "" {
			if err := verifyChecksum(data, expectedSHA256); err != nil {
				return err
			}
		} else if _, _, _, _, isGH := parseGitHubReleaseDownloadURL(rawURL); isGH {
			// GitHub release URL — auto-fetch checksums.txt.
			expectedSHA, lookupErr := lookupChecksumForURL(rawURL)
			if lookupErr != nil {
				return fmt.Errorf("auto-fetch checksum for %q: %w", rawURL, lookupErr)
			}
			if err := verifyChecksum(data, expectedSHA); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("cannot verify integrity of %q: no --sha256 provided and URL is not a GitHub release download (use --skip-checksum to bypass)", rawURL)
		}
	}

	tmpDir, err := os.MkdirTemp("", "wfctl-plugin-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarGz(data, tmpDir); err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	pjData, err := os.ReadFile(filepath.Join(tmpDir, "plugin.json"))
	if err != nil {
		return fmt.Errorf("no plugin.json found in archive: %w", err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(pjData, &pj); err != nil {
		return fmt.Errorf("parse plugin.json: %w", err)
	}
	if pj.Name == "" {
		return fmt.Errorf("plugin.json missing name field")
	}

	pluginName := normalizePluginName(pj.Name)
	destDir := filepath.Join(pluginDir, pluginName)

	// Prepare a staging directory alongside the final destination so that any
	// existing installation is never mutated in place.
	stagingDir, cleanupStaging, err := preparePluginStagingDir(destDir)
	if err != nil {
		return err
	}
	defer cleanupStaging()

	if err := extractTarGz(data, stagingDir); err != nil {
		return fmt.Errorf("extract to dest: %w", err)
	}

	if err := ensurePluginBinary(stagingDir, pluginName); err != nil {
		return fmt.Errorf("could not normalize binary name: %w", err)
	}

	// Validate the staged plugin (same checks as registry installs).
	if verifyErr := verifyInstalledPlugin(stagingDir, pluginName); verifyErr != nil {
		return fmt.Errorf("post-install verification failed: %w", verifyErr)
	}

	// Atomically replace any previous installation.
	if commitErr := commitPluginStagingDir(stagingDir, destDir); commitErr != nil {
		return commitErr
	}

	// Hash the installed binary (not the archive) so that verifyInstalledChecksum matches.
	binaryPath := filepath.Join(destDir, pluginName)
	checksum, hashErr := hashFileSHA256(binaryPath)
	if hashErr != nil {
		return fmt.Errorf("hash installed binary for lockfile: %w", hashErr)
	}
	updateLockfileWithChecksum(pluginName, pj.Version, pj.Repository, "", checksum)

	fmt.Printf("Installed %s v%s to %s\n", pluginName, strings.TrimPrefix(pj.Version, "v"), destDir)
	return nil
}

// hashFileSHA256 computes the SHA-256 hex digest of the file at path using streaming I/O.
func hashFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash file %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyInstalledChecksum reads the plugin binary and verifies its SHA-256 checksum.
func verifyInstalledChecksum(pluginDir, pluginName, expectedSHA256 string) error {
	binaryPath := filepath.Join(pluginDir, pluginName)
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read binary %s: %w", binaryPath, err)
	}
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expectedSHA256) {
		return fmt.Errorf("binary checksum mismatch: got %s, want %s", got, expectedSHA256)
	}
	return nil
}

// installFromLocal installs a plugin from a local directory.
func installFromLocal(srcDir, pluginDir string) error {
	pjPath := filepath.Join(srcDir, "plugin.json")
	pjData, err := os.ReadFile(pjPath)
	if err != nil {
		return fmt.Errorf("read plugin.json in %s: %w", srcDir, err)
	}
	var pj installedPluginJSON
	if err := json.Unmarshal(pjData, &pj); err != nil {
		return fmt.Errorf("parse plugin.json: %w", err)
	}
	if pj.Name == "" {
		return fmt.Errorf("plugin.json missing name field")
	}

	pluginName := normalizePluginName(pj.Name)
	destDir := filepath.Join(pluginDir, pluginName)

	// Prepare a staging directory alongside the final destination so that any
	// existing installation is never mutated in place.
	stagingDir, cleanupStaging, err := preparePluginStagingDir(destDir)
	if err != nil {
		return err
	}
	defer cleanupStaging()

	// Copy plugin.json
	if err := copyFile(pjPath, filepath.Join(stagingDir, "plugin.json"), 0640); err != nil {
		return err
	}

	// Find and copy the binary
	srcBinary := filepath.Join(srcDir, pluginName)
	if _, err := os.Stat(srcBinary); os.IsNotExist(err) {
		fullName := "workflow-plugin-" + pluginName
		srcBinary = filepath.Join(srcDir, fullName)
		if _, err := os.Stat(srcBinary); os.IsNotExist(err) {
			return fmt.Errorf("no plugin binary found in %s (tried %s and %s)", srcDir, pluginName, fullName)
		}
	}
	if err := copyFile(srcBinary, filepath.Join(stagingDir, pluginName), 0750); err != nil {
		return err
	}

	// Atomically replace any previous installation.
	if commitErr := commitPluginStagingDir(stagingDir, destDir); commitErr != nil {
		return commitErr
	}

	binaryChecksum, hashErr := hashFileSHA256(filepath.Join(destDir, pluginName))
	if hashErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not compute binary checksum: %v\n", hashErr)
	}
	updateLockfileWithChecksum(pluginName, pj.Version, "", "", binaryChecksum)

	fmt.Printf("Installed %s v%s from %s to %s\n", pluginName, strings.TrimPrefix(pj.Version, "v"), srcDir, destDir)
	return nil
}

// pinManifestToVersion rewrites the manifest's version and all download URLs to
// use requestedVersion. The registry manifest may lag behind the actual release
// (e.g. manifest says v0.1.0 but the user requests v0.2.1). GitHub release URLs
// follow a predictable pattern: replace /releases/download/<old>/<filename> with
// /releases/download/<new>/<filename>. SHA256 checksums are cleared since they are
// only valid for the original version's assets.
//
// Version comparison is v-prefix-tolerant: "v0.6.1" and "0.6.1" are treated as
// the same version, so passing @v0.6.1 when the registry manifest has "0.6.1" is
// a no-op rather than a rewrite that would corrupt download URLs.
//
// If requestedVersion matches manifest.Version (after v-normalization), this is a no-op.
func pinManifestToVersion(manifest *RegistryManifest, requestedVersion string) {
	// Normalize both versions by stripping the leading "v" for comparison.
	// This prevents treating "0.6.1" and "v0.6.1" as different versions, which
	// would corrupt download URLs by producing "vv0.6.1" via the fallback replacement.
	normalizedOld := strings.TrimPrefix(manifest.Version, "v")
	normalizedNew := strings.TrimPrefix(requestedVersion, "v")
	if normalizedOld == normalizedNew {
		return // same version, v-prefix convention mismatch only
	}
	oldVersion := manifest.Version
	manifest.Version = requestedVersion
	for i := range manifest.Downloads {
		url := manifest.Downloads[i].URL
		// 1. Try replacing the exact manifest version string in the GitHub releases path.
		rewritten := strings.ReplaceAll(url,
			"/releases/download/"+oldVersion+"/",
			"/releases/download/"+requestedVersion+"/")
		// 2. If no match, try v-normalized replacement. This handles the common case
		//    where the manifest stores "0.6.1" but the GitHub release tag is "v0.6.1".
		if rewritten == url {
			rewritten = strings.ReplaceAll(url,
				"/releases/download/v"+normalizedOld+"/",
				"/releases/download/v"+normalizedNew+"/")
		}
		// 3. Fallback: replace the bare version number anywhere in the URL, using
		//    normalized (no-v) forms so we don't double-up the "v" prefix.
		if rewritten == url && normalizedOld != "" {
			rewritten = strings.ReplaceAll(url, normalizedOld, normalizedNew)
		}
		if normalizedOld != "" {
			rewritten = rewriteArchiveFilenameVersion(rewritten, normalizedOld, normalizedNew)
		}
		manifest.Downloads[i].URL = rewritten
		manifest.Downloads[i].SHA256 = "" // checksums are for the old version's assets
	}
}

func manifestForCompatibilityVersion(manifest *RegistryManifest, index *PluginVersionIndex, version string) *RegistryManifest {
	if manifest == nil {
		return nil
	}
	out := *manifest
	out.Downloads = append([]PluginDownload(nil), manifest.Downloads...)
	out.Dependencies = append([]PluginDependency(nil), manifest.Dependencies...)
	out.Keywords = append([]string(nil), manifest.Keywords...)
	out.Contracts = append([]pluginContractDescriptor(nil), manifest.Contracts...)
	if index != nil {
		for _, rec := range index.Versions {
			if rec.Version == version {
				out.Version = version
				if len(rec.Downloads) > 0 {
					out.Downloads = append([]PluginDownload(nil), rec.Downloads...)
				}
				if rec.MinEngineVersion != "" {
					out.MinEngineVersion = rec.MinEngineVersion
				}
				return &out
			}
		}
	}
	pinManifestToVersion(&out, version)
	return &out
}

func rewriteArchiveFilenameVersion(rawURL, oldVersion, newVersion string) string {
	if oldVersion == "" || oldVersion == newVersion {
		return rawURL
	}
	idx := strings.LastIndex(rawURL, "/")
	if idx < 0 {
		return strings.ReplaceAll(rawURL, oldVersion, newVersion)
	}
	prefix, filename := rawURL[:idx+1], rawURL[idx+1:]
	return prefix + strings.ReplaceAll(filename, oldVersion, newVersion)
}

// parseNameVersion splits "name@version" into (name, version). Version is empty if absent.
func parseNameVersion(arg string) (name, ver string) {
	if idx := strings.Index(arg, "@"); idx >= 0 {
		return arg[:idx], arg[idx+1:]
	}
	return arg, ""
}

// gitHubAPIBaseURL is the GitHub API base URL. It is a package-level variable
// so tests can override it to point at a local mock server.
var gitHubAPIBaseURL = "https://api.github.com"

// gitHubAPIClient is used for GitHub API metadata calls (releases/tags,
// releases/assets). Separate from http.DefaultClient so tests can override it
// independently. A generous timeout covers large binary asset downloads.
var gitHubAPIClient = &http.Client{Timeout: 10 * time.Minute}

const gitHubReleaseMetadataTimeout = 30 * time.Second

// gitHubToken returns the first non-empty GitHub token from the environment,
// checking RELEASES_TOKEN, GH_TOKEN, and GITHUB_TOKEN in order.
func gitHubToken() string {
	for _, k := range []string{"RELEASES_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if tok := os.Getenv(k); tok != "" {
			return tok
		}
	}
	return ""
}

// isGitHubHost returns true only for github.com and its subdomains (e.g.
// api.github.com). Requires the dot separator to avoid matching evilgithub.com.
func isGitHubHost(host string) bool {
	h := strings.ToLower(host)
	return h == "github.com" || strings.HasSuffix(h, ".github.com")
}

// parseGitHubReleaseDownloadURL parses a GitHub release download URL of the form
// https://github.com/OWNER/REPO/releases/download/TAG/FILENAME and returns the
// components. Returns ok=false for any URL that doesn't match this exact pattern,
// including non-HTTPS schemes and non-GitHub hosts.
func parseGitHubReleaseDownloadURL(rawURL string) (owner, repo, tag, filename string, ok bool) {
	u, err := neturl.Parse(rawURL)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !isGitHubHost(u.Hostname()) {
		return
	}
	// Reject URLs with userinfo (user:pass@host) — prevents credential injection attacks.
	if u.User != nil {
		return
	}
	// Reject URLs with any explicit port. u.Hostname() strips the port before
	// isGitHubHost sees it, so https://github.com:8080/... would pass without this check.
	// Even the HTTPS default port (:443) is rejected — an explicit port signals a proxy
	// or redirect and should not be trusted as a canonical GitHub release URL.
	if u.Port() != "" {
		return
	}
	// Path must be exactly: /owner/repo/releases/download/tag/filename (6 segments).
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) != 6 || parts[2] != "releases" || parts[3] != "download" ||
		parts[0] == "" || parts[1] == "" || parts[4] == "" || parts[5] == "" {
		return
	}
	return parts[0], parts[1], parts[4], parts[5], true
}

// downloadGitHubReleaseAsset downloads a private GitHub release asset using the
// two-step REST API flow:
//  1. GET api.github.com/repos/OWNER/REPO/releases/tags/TAG — find the asset ID
//     matching filename in the release's assets array.
//  2. GET api.github.com/repos/OWNER/REPO/releases/assets/:id with
//     Accept: application/octet-stream — streams the binary content.
//
// This is the correct approach for private repos; the plain download URL
// (github.com/.../releases/download/.../file) redirects to a signed S3 URL and
// does not propagate the Authorization header correctly.
func downloadGitHubReleaseAsset(owner, repo, tag, filename, token string) ([]byte, error) {
	return downloadGitHubReleaseAssetWithTimeout(owner, repo, tag, filename, token, downloadTimeout)
}

func downloadGitHubReleaseAssetWithTimeout(owner, repo, tag, filename, token string, assetTimeout time.Duration) ([]byte, error) {
	// Step 1: resolve the asset ID from the release metadata.
	releaseURL := fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", //nolint:gosec // G107
		gitHubAPIBaseURL,
		neturl.PathEscape(owner),
		neturl.PathEscape(repo),
		neturl.PathEscape(tag),
	)
	metadataCtx, metadataCancel := context.WithTimeout(context.Background(), gitHubReleaseMetadataTimeout)
	req, err := http.NewRequestWithContext(metadataCtx, http.MethodGet, releaseURL, nil)
	if err != nil {
		metadataCancel()
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "wfctl/"+version)

	resp, err := gitHubAPIClient.Do(req)
	if err != nil {
		closeResponseBody(resp)
		metadataCancel()
		return nil, fmt.Errorf("GitHub releases API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		metadataCancel()
		return nil, fmt.Errorf("GitHub releases API: HTTP %d for %s/%s@%s", resp.StatusCode, owner, repo, tag)
	}

	var release struct {
		Assets []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		metadataCancel()
		return nil, fmt.Errorf("decode GitHub release response: %w", err)
	}
	metadataCancel()

	var assetID int64
	for _, a := range release.Assets {
		if a.Name == filename {
			assetID = a.ID
			break
		}
	}
	if assetID == 0 {
		return nil, fmt.Errorf("asset %q not found in release %s/%s@%s", filename, owner, repo, tag)
	}

	// Step 2: download the asset binary.
	assetURL := fmt.Sprintf("%s/repos/%s/%s/releases/assets/%d", //nolint:gosec // G107
		gitHubAPIBaseURL,
		neturl.PathEscape(owner),
		neturl.PathEscape(repo),
		assetID,
	)
	assetCtx, assetCancel := context.WithTimeout(context.Background(), assetTimeout)
	defer assetCancel()
	req2, err := http.NewRequestWithContext(assetCtx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req2.Header.Set("Authorization", "Bearer "+token)
	req2.Header.Set("Accept", "application/octet-stream")
	req2.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req2.Header.Set("User-Agent", "wfctl/"+version)

	resp2, err := gitHubAPIClient.Do(req2)
	if err != nil {
		closeResponseBody(resp2)
		return nil, fmt.Errorf("GitHub asset download API: %w", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub asset download API: HTTP %d for asset %d", resp2.StatusCode, assetID)
	}
	return readDownloadBodyWithProgress(resp2.Body, resp2.ContentLength)
}

// downloadURL fetches a URL and returns the body bytes.
//
// For GitHub release download URLs (github.com/OWNER/REPO/releases/download/...),
// when a token is available it uses the two-step GitHub REST API flow
// (releases/tags + releases/assets) which correctly handles private repos.
// Without a token it falls back to a direct GET (works for public repos).
//
// For all other github.com URLs a Bearer header is injected when a token is
// available. Non-GitHub URLs are fetched unauthenticated.
func downloadURL(rawURL string) ([]byte, error) {
	// Private GitHub release asset path: use the API two-step flow.
	if owner, repo, tag, filename, ok := parseGitHubReleaseDownloadURL(rawURL); ok {
		if tok := gitHubToken(); tok != "" {
			return downloadGitHubReleaseAsset(owner, repo, tag, filename, tok)
		}
	}

	// Public repos and non-release GitHub URLs: direct GET with optional Bearer.
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil) //nolint:gosec // G107: URL comes from registry manifest
	if err != nil {
		return nil, err
	}
	if parsed, err2 := neturl.Parse(rawURL); err2 == nil && isGitHubHost(parsed.Hostname()) {
		if tok := gitHubToken(); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		closeResponseBody(resp)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	return readDownloadBodyWithProgress(resp.Body, resp.ContentLength)
}

func closeResponseBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
}

// verifyChecksum checks that data matches the expected SHA256 hex string.
func verifyChecksum(data []byte, expected string) error {
	h := sha256.Sum256(data)
	got := hex.EncodeToString(h[:])
	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch: got: %s, want: %s -- may indicate a corrupted download or a supply-chain attack", got, expected)
	}
	return nil
}

// rawGitHubContentBaseURL is the base URL for raw GitHub content. It is a
// package-level variable so tests can override it to point at a local server.
var rawGitHubContentBaseURL = "https://raw.githubusercontent.com"

// fetchManifestFromRepoURL fetches a plugin's manifest.json directly from its
// GitHub repository. It expects the repository URL in the form
// https://github.com/{owner}/{repo} and looks for a manifest.json at the root
// of the default branch.
func fetchManifestFromRepoURL(repoURL string) (*RegistryManifest, error) {
	owner, repo, err := parseGitHubRepoURL(repoURL)
	if err != nil {
		return nil, fmt.Errorf("parse repository URL %q: %w", repoURL, err)
	}
	url := fmt.Sprintf("%s/%s/%s/main/manifest.json", rawGitHubContentBaseURL, owner, repo)
	resp, err := http.Get(url) //nolint:gosec // G107: URL constructed from plugin's own repository field
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from %q: %w", repoURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no manifest.json found in repository %q (tried %s)", repoURL, url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("repository %q returned HTTP %d", repoURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read manifest from %q: %w", repoURL, err)
	}
	var m RegistryManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest from %q: %w", repoURL, err)
	}
	return &m, nil
}

// parseGitHubRepoURL parses a GitHub repository URL and returns the owner and
// repository name. It accepts URLs in the form https://github.com/{owner}/{repo}
// (with or without trailing slash or .git suffix) and rejects URLs with extra
// path segments (e.g. https://github.com/owner/repo/tree/main).
func parseGitHubRepoURL(repoURL string) (owner, repo string, err error) {
	u := strings.TrimPrefix(repoURL, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimSuffix(u, "/")
	// Split into at most 4 parts to detect extra path segments.
	parts := strings.SplitN(u, "/", 4)
	if len(parts) < 3 || parts[0] != "github.com" || parts[1] == "" || parts[2] == "" {
		return "", "", fmt.Errorf("not a GitHub repository URL: %q (expected https://github.com/owner/repo)", repoURL)
	}
	if len(parts) == 4 {
		// Extra path segments present (e.g. /tree/main, /blob/main/file.go).
		return "", "", fmt.Errorf("not a GitHub repository URL: %q (unexpected extra path; expected https://github.com/owner/repo)", repoURL)
	}
	repoName := strings.TrimSuffix(parts[2], ".git")
	if repoName == "" {
		return "", "", fmt.Errorf("not a GitHub repository URL: %q (expected https://github.com/owner/repo)", repoURL)
	}
	return parts[1], repoName, nil
}

// extractTarGz decompresses and extracts a .tar.gz archive into destDir.
// It guards against path traversal (zip-slip) attacks.
func extractTarGz(data []byte, destDir string) error {
	return extractTarGzReader(bytes.NewReader(data), destDir)
}

func extractTarGzReader(r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Strip leading path component (common in tarballs: plugin-name-os-arch/binary).
		name := stripTopDir(hdr.Name)
		if name == "" || name == "." {
			continue
		}

		// Guard against path traversal.
		destPath, err := safeJoin(destDir, name)
		if err != nil {
			return err
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0750); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0750); err != nil {
				return fmt.Errorf("mkdir parent %s: %w", filepath.Dir(destPath), err)
			}
			mode := hdr.FileInfo().Mode()
			f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode) //nolint:gosec // G304: path validated by safeJoin
			if err != nil {
				return fmt.Errorf("create file %s: %w", destPath, err)
			}
			if _, copyErr := io.Copy(f, tr); copyErr != nil { //nolint:gosec // G110: tar size is bounded by download
				f.Close()
				return fmt.Errorf("write file %s: %w", destPath, copyErr)
			}
			f.Close()
		}
	}
	return nil
}

// stripTopDir removes the first path component from a tar entry name.
// e.g. "workflow-plugin-admin-darwin-amd64/admin.plugin" -> "admin.plugin"
func stripTopDir(name string) string {
	name = filepath.ToSlash(name)
	if idx := strings.Index(name, "/"); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// safeJoin joins base and name, returning an error if the result escapes base.
func safeJoin(base, name string) (string, error) {
	dest := filepath.Join(base, filepath.FromSlash(name))
	rel, err := filepath.Rel(base, dest)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path traversal detected: %q", name)
	}
	return dest, nil
}

// installedPluginJSON is the JSON format for plugin.json written after install.
// This must be compatible with plugin.PluginManifest so that
// ExternalPluginManager.LoadPlugin() can validate it AND compatible with
// wfctl's deploy_providers.iacPluginManifest so findIaCPluginDir can read
// capabilities.iacProvider.name.
type installedPluginJSON struct {
	Name         string                       `json:"name"`
	Version      string                       `json:"version"`
	Author       string                       `json:"author"`
	Description  string                       `json:"description"`
	License      string                       `json:"license,omitempty"`
	Repository   string                       `json:"repository,omitempty"`
	Tier         string                       `json:"tier,omitempty"`
	Tags         []string                     `json:"tags,omitempty"`
	Type         string                       `json:"type,omitempty"`
	Capabilities *installedPluginCapabilities `json:"capabilities,omitempty"`
	IaCProvider  *RegistryIaCProvider         `json:"iacProvider,omitempty"`
	// RequiredSecrets carries through from the registry manifest so
	// `wfctl secrets setup --plugin <name>` can find the declared
	// secrets in the on-disk plugin.json. Earlier writeInstalledManifest
	// versions dropped this field, leaving secrets setup --plugin
	// reporting "declares no required_secrets[]" even when the
	// upstream manifest declared it.
	RequiredSecrets []PluginRequiredSecret `json:"required_secrets,omitempty"`
	// SecretTargets carries through from the registry manifest so
	// manifest-backed secrets setup can respect provider-specific target models.
	SecretTargets []PluginSecretTarget `json:"secret_targets,omitempty"`
	// RequiredConfig carries through non-secret plugin setup requirements so
	// `wfctl vars setup --plugin <name>` can configure provider variables.
	RequiredConfig []PluginRequiredConfig `json:"required_config,omitempty"`
	// ConfigTargets carries through non-secret variable/config target metadata.
	ConfigTargets []PluginConfigTarget `json:"config_targets,omitempty"`
}

type installedPluginCapabilities struct {
	ModuleTypes   []string             `json:"moduleTypes,omitempty"`
	StepTypes     []string             `json:"stepTypes,omitempty"`
	TriggerTypes  []string             `json:"triggerTypes,omitempty"`
	ResourceTypes []string             `json:"resourceTypes,omitempty"`
	IaCProvider   *RegistryIaCProvider `json:"iacProvider,omitempty"`
	// CLICommands flow through to plugin.json so BuildCLIRegistry can
	// discover plugin-provided wfctl subcommands after install. Earlier
	// versions of writeInstalledManifest dropped this field, leaving
	// the installed manifest stripped of cliCommands even when the
	// registry manifest declared them — `wfctl <plugin-cmd>` then
	// reported `unknown command` post-install.
	CLICommands []RegistryCLICommand `json:"cliCommands,omitempty"`
	// BuildHooks flow through for the same reason: the build-hook
	// dispatcher reads them from the installed plugin.json, not the
	// registry manifest.
	BuildHooks []RegistryBuildHook `json:"buildHooks,omitempty"`
}

// writeInstalledManifest writes a full plugin.json compatible with the engine's
// plugin.PluginManifest so that ExternalPluginManager.LoadPlugin() can validate it.
func writeInstalledManifest(path string, m *RegistryManifest) error {
	pj := installedPluginJSON{
		Name:            m.Name,
		Version:         m.Version,
		Author:          m.Author,
		Description:     m.Description,
		License:         m.License,
		Repository:      m.Repository,
		Tier:            m.Tier,
		Tags:            m.Keywords,
		Type:            m.Type,
		IaCProvider:     m.IaCProvider,
		RequiredSecrets: m.RequiredSecrets,
		SecretTargets:   m.SecretTargets,
		RequiredConfig:  m.RequiredConfig,
		ConfigTargets:   m.ConfigTargets,
	}
	if m.Capabilities != nil {
		pj.Capabilities = &installedPluginCapabilities{
			ModuleTypes:   m.Capabilities.ModuleTypes,
			StepTypes:     m.Capabilities.StepTypes,
			TriggerTypes:  m.Capabilities.TriggerTypes,
			ResourceTypes: m.Capabilities.ResourceTypes,
			IaCProvider:   m.Capabilities.IaCProvider,
			CLICommands:   m.Capabilities.CLICommands,
			BuildHooks:    m.Capabilities.BuildHooks,
		}
	}
	data, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0640) //nolint:gosec // G306: plugin.json is user-owned output
}

// ensurePluginBinary finds the executable binary in destDir and renames it to
// match the plugin name. ExternalPluginManager expects <dir>/<name>/<name>.
// GoReleaser tarballs typically contain binaries named like
// "workflow-plugin-admin-darwin-arm64" after stripTopDir, so we rename to "admin".
func ensurePluginBinary(destDir, pluginName string) error {
	expectedPath := filepath.Join(destDir, pluginName)
	if info, err := os.Stat(expectedPath); err == nil && !info.IsDir() {
		return nil // already correctly named
	}

	// Find the largest executable file in the directory (the plugin binary).
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return err
	}
	var bestName string
	var bestSize int64
	for _, e := range entries {
		if e.IsDir() || e.Name() == "plugin.json" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Skip non-executable files
		if info.Mode()&0111 == 0 {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			bestName = e.Name()
		}
	}
	if bestName == "" {
		return fmt.Errorf("no executable binary found in %s", destDir)
	}
	return os.Rename(filepath.Join(destDir, bestName), expectedPath)
}

// preparePluginStagingDir removes any leftover staging directory and creates a
// fresh one alongside destDir (same filesystem). The caller must call
// commitPluginStagingDir on success. On failure the returned cleanup func
// removes the staging directory.
func preparePluginStagingDir(destDir string) (stagingDir string, cleanup func(), err error) {
	stagingDir = destDir + ".installing"
	if removeErr := os.RemoveAll(stagingDir); removeErr != nil {
		return "", nil, fmt.Errorf("clean staging dir %s: %w", stagingDir, removeErr)
	}
	// Ensure the parent directory exists so MkdirAll only needs to create the
	// staging leaf.
	if mkErr := os.MkdirAll(filepath.Dir(destDir), 0750); mkErr != nil {
		return "", nil, fmt.Errorf("create plugin base dir: %w", mkErr)
	}
	if mkErr := os.Mkdir(stagingDir, 0750); mkErr != nil {
		return "", nil, fmt.Errorf("create staging dir %s: %w", stagingDir, mkErr)
	}
	return stagingDir, func() { os.RemoveAll(stagingDir) }, nil //nolint:errcheck
}

// commitPluginStagingDir replaces destDir with stagingDir. To preserve the
// existing installation if the final rename fails, the old destDir is first
// renamed to a trash location on the same filesystem. Only after the new
// directory is successfully renamed into place is the trash removed.
//
//  1. Rename destDir → destDir+".uninstalling"  (no-op if destDir absent)
//  2. Rename stagingDir → destDir
//  3. On step-2 failure: restore destDir+".uninstalling" → destDir
//  4. On step-2 success: remove destDir+".uninstalling"
//
// On success stagingDir no longer exists on disk; the deferred cleanup
// returned by preparePluginStagingDir becomes a harmless no-op.
func commitPluginStagingDir(stagingDir, destDir string) error {
	trashDir := destDir + ".uninstalling"
	// Remove any leftover trash from a previous interrupted commit.
	if err := os.RemoveAll(trashDir); err != nil {
		return fmt.Errorf("clean trash dir %s: %w", trashDir, err)
	}

	// Move the existing install out of the way before installing the new one.
	// If destDir does not exist yet (first install) we skip this step.
	hasExisting := false
	if _, statErr := os.Stat(destDir); statErr == nil {
		hasExisting = true
		if err := os.Rename(destDir, trashDir); err != nil {
			return fmt.Errorf("preserve existing plugin dir %s: %w", destDir, err)
		}
	}

	// Move staging into the final location.
	if err := os.Rename(stagingDir, destDir); err != nil {
		// Best-effort restore: move the old install back if we preserved it.
		if hasExisting {
			_ = os.Rename(trashDir, destDir) //nolint:errcheck
		}
		return fmt.Errorf("install plugin dir %s: %w", destDir, err)
	}

	// New install is in place — remove the old one (best effort).
	_ = os.RemoveAll(trashDir) //nolint:errcheck
	return nil
}

// verifyInstalledVersion checks that the plugin.json in dir declares the
// expected version. It normalises "v" prefixes before comparing, so "v1.0.8"
// and "1.0.8" are treated as equal.
func verifyInstalledVersion(dir, expectedVersion string) error {
	installedVersion := readInstalledVersion(dir)
	norm := func(v string) string { return strings.TrimPrefix(v, "v") }
	if norm(installedVersion) != norm(expectedVersion) {
		return fmt.Errorf("installed plugin.json version %q does not match expected %q", installedVersion, expectedVersion)
	}
	return nil
}

// verifyInstalledPlugin validates the installed plugin.json using the engine's
// manifest loader and checks that the binary exists and is executable.
func verifyInstalledPlugin(destDir, pluginName string) error {
	manifestPath := filepath.Join(destDir, "plugin.json")
	binaryPath := filepath.Join(destDir, pluginName)

	// Check manifest exists and is valid for the engine.
	manifest, err := engineplugin.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("manifest validation: %w", err)
	}

	// Check binary exists and is executable.
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary not found at %s: %w", binaryPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("binary path %s is a directory", binaryPath)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("binary %s is not executable", binaryPath)
	}

	return nil
}

// readInstalledInfo reads version, type, and description from a plugin.json in the given directory.
func readInstalledInfo(dir string) (version, pluginType, description string) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return "unknown", "", ""
	}
	var m installedPluginJSON
	if err := json.Unmarshal(data, &m); err != nil {
		return "unknown", "", ""
	}
	version = m.Version
	if version == "" {
		version = "unknown"
	}
	pluginType = m.Type
	description = m.Description
	return
}

// readInstalledVersion reads the version from a plugin.json in the given directory.
func readInstalledVersion(dir string) string {
	v, _, _ := readInstalledInfo(dir)
	return v
}
