package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCLIPlugin creates a fake plugin directory with a plugin.json that
// declares one or more CLI commands.
func writeCLIPlugin(t *testing.T, pluginsDir, name string, commands []string) {
	t.Helper()
	dir := filepath.Join(pluginsDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	// Build cliCommands JSON fragment.
	var cmdParts []string
	for _, cmd := range commands {
		cmdParts = append(cmdParts, `{"name":"`+cmd+`","description":"desc"}`)
	}
	manifest := `{"name":"` + name + `","version":"1.0.0","capabilities":{"moduleTypes":[],"stepTypes":[],"triggerTypes":[],"cliCommands":[` +
		strings.Join(cmdParts, ",") + `]}}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
}

func TestPluginCLIRegistry_TwoPluginsTwoCommands(t *testing.T) {
	dir := t.TempDir()
	writeCLIPlugin(t, dir, "supply-chain", []string{"supply-chain"})
	writeCLIPlugin(t, dir, "migrate", []string{"migrate"})

	reg, err := BuildCLIRegistry(dir)
	if err != nil {
		t.Fatalf("BuildCLIRegistry: %v", err)
	}
	if _, ok := reg["supply-chain"]; !ok {
		t.Error("supply-chain command should be registered")
	}
	if _, ok := reg["migrate"]; !ok {
		t.Error("migrate command should be registered")
	}
}

func TestPluginCLIRegistry_ReservedNameRejected(t *testing.T) {
	dir := t.TempDir()
	writeCLIPlugin(t, dir, "bad-plugin", []string{"build"}) // "build" is reserved

	_, err := BuildCLIRegistry(dir)
	if err == nil {
		t.Error("expected error for reserved command name 'build'")
	}
	if !strings.Contains(err.Error(), "reserved") {
		t.Errorf("error should mention 'reserved', got: %v", err)
	}
}

func TestPluginCLIRegistry_ConflictError(t *testing.T) {
	dir := t.TempDir()
	writeCLIPlugin(t, dir, "plugin-a", []string{"data"})
	writeCLIPlugin(t, dir, "plugin-b", []string{"data"}) // same command

	_, err := BuildCLIRegistry(dir)
	if err == nil {
		t.Error("expected error for duplicate command 'data'")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Errorf("error should mention 'conflict', got: %v", err)
	}
}

func TestPluginCLIRegistry_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	reg, err := BuildCLIRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reg) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(reg))
	}
}

func TestStaticCommandWins(t *testing.T) {
	// "validate" is a static command — it should be in the reserved list.
	if !isReservedCLICommand("validate") {
		t.Error("validate should be in the reserved list")
	}
}
