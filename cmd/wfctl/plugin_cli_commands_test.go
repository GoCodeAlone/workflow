package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCLIPlugin creates a fake plugin directory with a plugin.json that
// declares one or more CLI commands. Uses dirName == manifestName for the
// simple case; see writeCLIPluginNamed for the dir-vs-manifest-mismatch.
func writeCLIPlugin(t *testing.T, pluginsDir, name string, commands []string) {
	t.Helper()
	writeCLIPluginNamed(t, pluginsDir, name, name, commands)
}

// writeCLIPluginNamed builds a plugin where the on-disk directory name and
// the manifest name can differ — matches the real-world install convention
// (short dir name like "payments" + full manifest name like
// "workflow-plugin-payments").
//
// `wfctl plugin install` runs ensurePluginBinary post-extract which renames
// the executable to match the (short) install dir name, so the stub binary
// here is named after dirName rather than manifestName.
func writeCLIPluginNamed(t *testing.T, pluginsDir, dirName, manifestName string, commands []string) {
	t.Helper()
	dir := filepath.Join(pluginsDir, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var cmdParts []string
	for _, cmd := range commands {
		cmdParts = append(cmdParts, `{"name":"`+cmd+`","description":"desc"}`)
	}
	manifest := `{"name":"` + manifestName + `","version":"1.0.0","capabilities":{"moduleTypes":[],"stepTypes":[],"triggerTypes":[],"cliCommands":[` +
		strings.Join(cmdParts, ",") + `]}}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, dirName), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write stub binary: %v", err)
	}
}

func TestPluginCLIRegistry_TwoPluginsTwoCommands(t *testing.T) {
	dir := t.TempDir()
	writeCLIPlugin(t, dir, "supply-chain", []string{"supply-chain"})
	writeCLIPlugin(t, dir, "data-migrate", []string{"data-migrate"})

	reg, err := BuildCLIRegistry(dir)
	if err != nil {
		t.Fatalf("BuildCLIRegistry: %v", err)
	}
	if _, ok := reg["supply-chain"]; !ok {
		t.Error("supply-chain command should be registered")
	}
	if _, ok := reg["data-migrate"]; !ok {
		t.Error("data-migrate command should be registered")
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

func TestPluginCLIRegistry_AllStaticCommandsReserved(t *testing.T) {
	for name := range commands {
		if !isReservedCLICommand(name) {
			t.Fatalf("static command %q should be reserved", name)
		}
	}
}

// TestPluginCLIRegistry_DirVsManifestNameMismatch is the regression test for
// the binary-path bug. setup-plugins (and `wfctl plugin install`) extract
// tarballs to short directory names like `data/plugins/payments`. After
// extraction `ensurePluginBinary` renames the largest executable to match
// the (short) install dir name. So the binary post-install lives at
// `<dir>/<shortName>/<shortName>` regardless of what the tarball or the
// manifest call it.
//
// The earlier path computation went through two iterations:
//  1. workflow#591 joined manifest.Name twice → broke for short dirs.
//  2. workflow#595 joined dirName + manifest.Name → broke because
//     ensurePluginBinary renames the binary AWAY from manifest.Name.
//
// This test pins both sides: dir name + binary file name = the install
// dir name; manifest name flows into PluginName for log/audit purposes.
func TestPluginCLIRegistry_DirVsManifestNameMismatch(t *testing.T) {
	dir := t.TempDir()
	writeCLIPluginNamed(t, dir, "payments", "workflow-plugin-payments", []string{"payments"})

	reg, err := BuildCLIRegistry(dir)
	if err != nil {
		t.Fatalf("BuildCLIRegistry: %v", err)
	}
	entry, ok := reg["payments"]
	if !ok {
		t.Fatalf("expected `payments` command registered, got %v", reg)
	}
	wantBin := filepath.Join(dir, "payments", "payments")
	if entry.BinaryPath != wantBin {
		t.Errorf("BinaryPath = %q, want %q", entry.BinaryPath, wantBin)
	}
	if _, err := os.Stat(entry.BinaryPath); err != nil {
		t.Errorf("BinaryPath %q does not point at a real file: %v", entry.BinaryPath, err)
	}
	if entry.PluginName != "workflow-plugin-payments" {
		t.Errorf("PluginName = %q, want %q (manifest name)", entry.PluginName, "workflow-plugin-payments")
	}
}

// TestPluginCLIRegistry_EmptyManifestNameUsesDirName covers the fallback path:
// when manifest.Name is empty, the dir name is used as both the registry
// PluginName and the binary file name.
func TestPluginCLIRegistry_EmptyManifestNameUsesDirName(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "anonymous")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	manifest := `{"version":"1.0.0","capabilities":{"cliCommands":[{"name":"anon","description":"desc"}]}}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "anonymous"), []byte(""), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	reg, err := BuildCLIRegistry(dir)
	if err != nil {
		t.Fatalf("BuildCLIRegistry: %v", err)
	}
	entry, ok := reg["anon"]
	if !ok {
		t.Fatalf("expected `anon` command registered")
	}
	wantBin := filepath.Join(dir, "anonymous", "anonymous")
	if entry.BinaryPath != wantBin {
		t.Errorf("BinaryPath = %q, want %q", entry.BinaryPath, wantBin)
	}
}
