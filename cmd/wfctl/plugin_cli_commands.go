package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/GoCodeAlone/workflow/config"
)

// reservedCLICommands is the set of static wfctl command names that plugins
// cannot shadow. Any plugin declaring one of these names is rejected at startup.
var reservedCLICommands = map[string]struct{}{
	"api":             {},
	"audit":           {},
	"build":           {},
	"build-ui":        {},
	"ci":              {},
	"compat":          {},
	"config":          {},
	"contract":        {},
	"deploy":          {},
	"dev":             {},
	"diff":            {},
	"dns-policy":      {},
	"docs":            {},
	"dsl-reference":   {},
	"editor-bundle":   {},
	"editor-schemas":  {},
	"expr-migrate":    {},
	"generate":        {},
	"git":             {},
	"help":            {},
	"infra":           {},
	"init":            {},
	"inspect":         {},
	"list":            {},
	"logs":            {},
	"manifest":        {},
	"mcp":             {},
	"migrate":         {},
	"migrations":      {},
	"modernize":       {},
	"override":        {},
	"pipeline":        {},
	"plugin":          {},
	"plugin-registry": {},
	"ports":           {},
	"publish":         {},
	"registry":        {},
	"run":             {},
	"scaffold":        {},
	"schema":          {},
	"secrets":         {},
	"security":        {},
	"snippets":        {},
	"template":        {},
	"tenant":          {},
	"test":            {},
	"ui":              {},
	"update":          {},
	"validate":        {},
	"version":         {},
	"wizard":          {},
}

// isReservedCLICommand reports whether name is a reserved static wfctl command.
func isReservedCLICommand(name string) bool {
	if _, ok := reservedCLICommands[name]; ok {
		return true
	}
	return false
}

// CLIRegistryEntry is a resolved CLI command handler for a plugin.
type CLIRegistryEntry struct {
	Command     string // top-level command name
	PluginName  string // owning plugin
	BinaryPath  string // path to plugin binary
	Description string
}

// CLIRegistry maps command names to their registry entries.
type CLIRegistry map[string]*CLIRegistryEntry

// pluginDirEntry pairs a plugin's on-disk directory name with its parsed
// manifest. Both pieces are needed to resolve the binary path: setup-plugins
// + `wfctl plugin install` extract tarballs to short directory names (e.g.
// `data/plugins/payments`), while the binary inside is named after the
// manifest (e.g. `workflow-plugin-payments`). Earlier code used
// `manifest.Name` for both directory and binary, which only worked when
// the two happened to match.
type pluginDirEntry struct {
	dirName  string
	manifest *config.PluginManifestFile
}

// BuildCLIRegistry scans pluginsDir for installed plugin.json manifests and
// builds a command-name → entry map.
//
// Returns an error if:
//   - A plugin declares a reserved command name.
//   - Two plugins declare the same command name (conflict).
func BuildCLIRegistry(pluginsDir string) (CLIRegistry, error) {
	registry := make(CLIRegistry)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return nil, fmt.Errorf("read plugins dir %q: %w", pluginsDir, err)
	}

	// Walk subdirs once, capturing both the dir name and the parsed manifest
	// per plugin. Iterating LoadPluginManifests's keyed map would lose the
	// dir name (the map key collapses on manifest.Name) and we need both to
	// build a correct binary path.
	plugins := make([]pluginDirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath := filepath.Join(pluginsDir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // plugin dir without manifest — skip
		}
		var manifest config.PluginManifestFile
		if err := json.Unmarshal(data, &manifest); err != nil {
			continue // malformed manifest — skip
		}
		plugins = append(plugins, pluginDirEntry{dirName: entry.Name(), manifest: &manifest})
	}

	// Sort by dir name for deterministic conflict reporting.
	sort.Slice(plugins, func(i, j int) bool { return plugins[i].dirName < plugins[j].dirName })

	for _, p := range plugins {
		manifestName := p.manifest.Name
		if manifestName == "" {
			manifestName = p.dirName
		}
		for _, cmd := range p.manifest.Capabilities.CLICommands {
			name := cmd.Name
			if name == "" {
				continue
			}
			if isReservedCLICommand(name) {
				return nil, fmt.Errorf(
					"plugin %q declares CLI command %q which is a reserved wfctl command name; "+
						"rename the command in the plugin manifest",
					manifestName, name,
				)
			}
			if existing, ok := registry[name]; ok {
				return nil, fmt.Errorf(
					"CLI command conflict: both plugin %q and plugin %q declare command %q; "+
						"uninstall one of them or rename the command in one plugin manifest",
					existing.PluginName, manifestName, name,
				)
			}
			// Binary path uses the on-disk directory name TWICE because
			// `wfctl plugin install` calls ensurePluginBinary(destDir,
			// pluginName) which RENAMES the largest executable in destDir
			// to match the (short) plugin name. After install the binary
			// lives at destDir/{shortName}/{shortName}. Earlier code joined
			// destDir+manifestName, which only works in the rare case
			// where the dir name and manifest name happen to coincide.
			binaryPath := filepath.Join(pluginsDir, p.dirName, p.dirName)
			registry[name] = &CLIRegistryEntry{
				Command:     name,
				PluginName:  manifestName,
				BinaryPath:  binaryPath,
				Description: cmd.Description,
			}
		}
	}
	return registry, nil
}

// DispatchCLICommand invokes the plugin binary for the given dynamic command.
// args should include the command name and any subsequent arguments.
// The plugin binary is called as: <binary> --wfctl-cli <args...>
// The plugin's stdout/stderr are inherited.
func DispatchCLICommand(entry *CLIRegistryEntry, args []string) error {
	cmdArgs := append([]string{"--wfctl-cli"}, args...)
	cmd := exec.Command(entry.BinaryPath, cmdArgs...) //nolint:gosec // BinaryPath comes from validated plugin manifest, not user input
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("plugin %s command %s: %w", entry.PluginName, entry.Command, err)
	}
	return nil
}

// LookupCLICommand returns the registry entry for the given command name, or
// nil if not found in the dynamic registry. Callers should check static commands
// first before calling this function.
func (r CLIRegistry) LookupCLICommand(name string) *CLIRegistryEntry {
	return r[name]
}
