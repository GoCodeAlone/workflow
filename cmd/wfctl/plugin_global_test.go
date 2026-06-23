package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultGlobalPluginDirUsesOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "wfctl-global")
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", override)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("HOME", t.TempDir())

	if got := defaultGlobalPluginDir(); got != override {
		t.Fatalf("defaultGlobalPluginDir() = %q, want override %q", got, override)
	}
}

func TestDefaultGlobalPluginDirUsesXDGDataHome(t *testing.T) {
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", "")
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("HOME", t.TempDir())

	want := filepath.Join(xdg, "wfctl", "plugins")
	if got := defaultGlobalPluginDir(); got != want {
		t.Fatalf("defaultGlobalPluginDir() = %q, want %q", got, want)
	}
}

func TestDefaultGlobalPluginDirFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".local", "share", "wfctl", "plugins")
	if got := defaultGlobalPluginDir(); got != want {
		t.Fatalf("defaultGlobalPluginDir() = %q, want %q", got, want)
	}
}

func TestResolvePluginDirUsesProjectLocalByDefault(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom")
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", filepath.Join(t.TempDir(), "global"))

	if got := resolvePluginDir(custom, false); got != custom {
		t.Fatalf("resolvePluginDir(custom, false) = %q, want %q", got, custom)
	}
}

func TestResolvePluginDirUsesGlobalWhenRequested(t *testing.T) {
	global := filepath.Join(t.TempDir(), "global")
	custom := filepath.Join(t.TempDir(), "custom")
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)

	if got := resolvePluginDir(custom, true); got != global {
		t.Fatalf("resolvePluginDir(custom, true) = %q, want %q", got, global)
	}
}
