package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// reservedCLICommands is the set of static wfctl command names that plugins
// cannot shadow. Any plugin declaring one of these names is rejected at startup.
var reservedCLICommands = map[string]struct{}{
	"plugin":    {},
	"build":     {},
	"infra":     {},
	"ci":        {},
	"deploy":    {},
	"tenant":    {},
	"config":    {},
	"api":       {},
	"contract":  {},
	"diff":      {},
	"dev":       {},
	"generate":  {},
	"git":       {},
	"help":      {},
	"init":      {},
	"inspect":   {},
	"list":      {},
	"mcp":       {},
	"modernize": {},
	"pipeline":  {},
	"registry":  {},
	"template":  {},
	"update":    {},
	"validate":  {},
	"version":   {},
}

// isReservedCLICommand reports whether name is a reserved static wfctl command.
func isReservedCLICommand(name string) bool {
	_, ok := reservedCLICommands[name]
	return ok
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

// BuildCLIRegistry scans pluginsDir for installed plugin.json manifests and
// builds a command-name → entry map.
//
// Returns an error if:
//   - A plugin declares a reserved command name.
//   - Two plugins declare the same command name (conflict).
func BuildCLIRegistry(pluginsDir string) (CLIRegistry, error) {
	manifests, err := LoadPluginManifests(pluginsDir)
	if err != nil {
		return nil, fmt.Errorf("load plugin manifests: %w", err)
	}

	registry := make(CLIRegistry)

	// Sort plugin names for deterministic conflict reporting.
	names := make([]string, 0, len(manifests))
	for n := range manifests {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, pluginName := range names {
		manifest := manifests[pluginName]
		for _, cmd := range manifest.Capabilities.CLICommands {
			name := cmd.Name
			if name == "" {
				continue
			}
			if isReservedCLICommand(name) {
				return nil, fmt.Errorf(
					"plugin %q declares CLI command %q which is a reserved wfctl command name; "+
						"rename the command in the plugin manifest",
					pluginName, name,
				)
			}
			if existing, ok := registry[name]; ok {
				return nil, fmt.Errorf(
					"CLI command conflict: both plugin %q and plugin %q declare command %q; "+
						"uninstall one of them or rename the command in one plugin manifest",
					existing.PluginName, pluginName, name,
				)
			}
			binaryPath := filepath.Join(pluginsDir, pluginName, pluginName)
			registry[name] = &CLIRegistryEntry{
				Command:     name,
				PluginName:  pluginName,
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
	cmd := exec.Command(entry.BinaryPath, cmdArgs...)
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

// formatCLIHelp returns a help string listing all dynamic commands.
func (r CLIRegistry) formatCLIHelp() string {
	if len(r) == 0 {
		return ""
	}
	names := make([]string, 0, len(r))
	for n := range r {
		names = append(names, n)
	}
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("Plugin commands:\n")
	for _, n := range names {
		e := r[n]
		desc := e.Description
		if desc == "" {
			desc = fmt.Sprintf("(from plugin %s)", e.PluginName)
		}
		fmt.Fprintf(&sb, "  %-20s %s\n", n, desc)
	}
	return sb.String()
}
