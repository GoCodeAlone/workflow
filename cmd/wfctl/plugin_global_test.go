package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestPluginInstallGlobalLocalDoesNotWriteLockfile(t *testing.T) {
	cwd := chdirTemp(t)
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)

	src := writeLocalPluginSource(t, "global-local", "1.0.0")
	if err := runPluginInstall([]string{"-g", "--local", src}); err != nil {
		t.Fatalf("runPluginInstall -g --local: %v", err)
	}

	if _, err := os.Stat(filepath.Join(global, "global-local", "plugin.json")); err != nil {
		t.Fatalf("global plugin not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".wfctl-lock.yaml")); !os.IsNotExist(err) {
		t.Fatalf("global install wrote .wfctl-lock.yaml, err=%v", err)
	}
}

func TestPluginListGlobalReadsGlobalDir(t *testing.T) {
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "global-list", "1.0.0")

	out, err := captureStdout(t, func() error {
		return runPluginList([]string{"--global"})
	})
	if err != nil {
		t.Fatalf("runPluginList --global: %v", err)
	}
	if !strings.Contains(out, "global-list") {
		t.Fatalf("runPluginList --global output = %q, want global-list", out)
	}
}

func TestPluginInfoGlobalReadsGlobalDir(t *testing.T) {
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "global-info", "2.3.4")

	out, err := captureStdout(t, func() error {
		return runPluginInfo([]string{"-g", "global-info"})
	})
	if err != nil {
		t.Fatalf("runPluginInfo -g: %v", err)
	}
	if !strings.Contains(out, "Name:         global-info") || !strings.Contains(out, "Version:      2.3.4") {
		t.Fatalf("runPluginInfo -g output = %q, want global plugin info", out)
	}
}

func TestPluginRemoveGlobalDoesNotMutateProjectState(t *testing.T) {
	cwd := chdirTemp(t)
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)
	writeInstalledPlugin(t, global, "global-remove", "1.0.0")

	manifest := "version: 1\nplugins:\n  - name: global-remove\n    version: 0.9.0\n    source: github.com/example/global-remove\n"
	lockfile := "version: 1\nplugins:\n  global-remove:\n    version: 0.9.0\n    source: github.com/example/global-remove\n"
	if err := os.WriteFile(filepath.Join(cwd, "wfctl.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".wfctl-lock.yaml"), []byte(lockfile), 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	if err := runPluginRemove([]string{"-g", "global-remove"}); err != nil {
		t.Fatalf("runPluginRemove -g: %v", err)
	}
	if _, err := os.Stat(filepath.Join(global, "global-remove")); !os.IsNotExist(err) {
		t.Fatalf("global plugin dir still exists or unexpected stat error: %v", err)
	}
	gotManifest, err := os.ReadFile(filepath.Join(cwd, "wfctl.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(gotManifest) != manifest {
		t.Fatalf("manifest mutated:\n%s", gotManifest)
	}
	gotLockfile, err := os.ReadFile(filepath.Join(cwd, ".wfctl-lock.yaml"))
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	if string(gotLockfile) != lockfile {
		t.Fatalf("lockfile mutated:\n%s", gotLockfile)
	}
}

func TestPluginUpdateGlobalRejectsProjectVersionPin(t *testing.T) {
	cwd := chdirTemp(t)
	global := t.TempDir()
	t.Setenv("WFCTL_GLOBAL_PLUGIN_DIR", global)

	manifest := "version: 1\nplugins:\n  - name: global-update\n    version: 0.9.0\n    source: github.com/example/global-update\n"
	if err := os.WriteFile(filepath.Join(cwd, "wfctl.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	err := runPluginUpdate([]string{"-g", "-version", "1.0.0", "global-update"})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with --global") {
		t.Fatalf("runPluginUpdate -g -version error = %v, want cannot combine error", err)
	}
	gotManifest, err := os.ReadFile(filepath.Join(cwd, "wfctl.yaml"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if string(gotManifest) != manifest {
		t.Fatalf("manifest mutated:\n%s", gotManifest)
	}
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	return dir
}

func writeLocalPluginSource(t *testing.T, name, version string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), minimalPluginJSON(name, version), 0o640); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(src, name), []byte("#!/bin/sh\necho plugin\n"), 0o750); err != nil {
		t.Fatalf("write plugin binary: %v", err)
	}
	return src
}

func writeInstalledPlugin(t *testing.T, root, name, version string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir installed plugin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), minimalPluginJSON(name, version), 0o640); err != nil {
		t.Fatalf("write installed plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\necho plugin\n"), 0o750); err != nil {
		t.Fatalf("write installed plugin binary: %v", err)
	}
}
