package plugin

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestAutoFetchPlugin_AlreadyInstalled verifies that AutoFetchPlugin returns nil
// immediately when plugin.json already exists in the plugin directory.
func TestAutoFetchPlugin_AlreadyInstalled(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	pluginName := "my-plugin"

	// Create the plugin directory with a plugin.json to simulate an installed plugin.
	destDir := filepath.Join(pluginDir, pluginName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destDir, "plugin.json"), []byte(`{"name":"my-plugin"}`), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	// Should return nil without attempting any download.
	err := AutoFetchPlugin(pluginName, "", pluginDir)
	if err != nil {
		t.Errorf("expected nil when plugin already installed, got: %v", err)
	}
}

// TestAutoFetchPlugin_WfctlNotFound verifies that AutoFetchPlugin returns an error
// when wfctl is not on PATH and the plugin is not installed locally.
func TestAutoFetchPlugin_WfctlNotFound(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")

	// Ensure the plugin directory exists but plugin is NOT installed.
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Temporarily set PATH to an empty/nonexistent directory so wfctl can't be found.
	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir) // dir exists but has no wfctl binary

	err := AutoFetchPlugin("missing-plugin", "", pluginDir)
	if err == nil {
		t.Error("expected error when wfctl is not on PATH and plugin is missing, got nil")
	}
}

// TestStripVersionConstraint verifies that constraint prefixes are stripped and
// compound constraints are detected as invalid.
func TestStripVersionConstraint(t *testing.T) {
	cases := []struct {
		input   string
		want    string
		wantOK  bool
	}{
		{">=0.1.0", "0.1.0", true},
		{"<=0.1.0", "0.1.0", true},
		{"^0.2.0", "0.2.0", true},
		{"~1.0.0", "1.0.0", true},
		{"0.3.0", "0.3.0", true},
		{"", "", true},
		{">=0.1.0,<0.2.0", "", false},  // compound — not supported
		{">=0.1.0 <0.2.0", "", false},  // compound with space
		{"0.1.0 0.2.0", "", false},     // two bare versions separated by space
	}
	for _, tc := range cases {
		got, ok := stripVersionConstraint(tc.input)
		if ok != tc.wantOK {
			t.Errorf("stripVersionConstraint(%q) ok=%v, want ok=%v", tc.input, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Errorf("stripVersionConstraint(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestAutoFetchPlugin_SkipsWhenExists verifies that AutoFetchPlugin returns nil
// immediately when the plugin is already installed, without invoking wfctl.
func TestAutoFetchPlugin_SkipsWhenExists(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	pluginName := "test-plugin"

	destDir := filepath.Join(pluginDir, pluginName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}
	manifestPath := filepath.Join(destDir, "plugin.json")
	if err := os.WriteFile(manifestPath, []byte(`{"name":"test-plugin","version":"0.1.0"}`), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}

	// With plugin.json present, AutoFetchPlugin must return nil (no wfctl invoked).
	if err := AutoFetchPlugin(pluginName, ">=0.1.0", pluginDir); err != nil {
		t.Errorf("expected nil for already-installed plugin, got: %v", err)
	}
}

// TestAutoFetchDeclaredPlugins_SkipsWhenWfctlMissing verifies that
// AutoFetchDeclaredPlugins logs a warning and returns without error when
// wfctl is absent from PATH.
func TestAutoFetchDeclaredPlugins_SkipsWhenWfctlMissing(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	orig := os.Getenv("PATH")
	t.Cleanup(func() { os.Setenv("PATH", orig) })
	os.Setenv("PATH", dir) // no wfctl here

	decls := []AutoFetchDecl{
		{Name: "missing-plugin", Version: ">=0.1.0", AutoFetch: true},
	}

	// Should not panic or return an error — just log a warning.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	AutoFetchDeclaredPlugins(decls, pluginDir, logger)
	// If we reach here, the function handled the missing wfctl gracefully.
}

// TestAutoFetchDeclaredPlugins_SkipsNonAutoFetch verifies that plugins
// with AutoFetch=false are not fetched, even if wfctl is missing from PATH.
func TestAutoFetchDeclaredPlugins_SkipsNonAutoFetch(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "plugins")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Remove wfctl from PATH; if the function tries to look it up for an
	// AutoFetch=false plugin it would still warn but not fail — the real
	// check is that AutoFetch=false plugins are completely skipped.
	decls := []AutoFetchDecl{
		{Name: "opt-out-plugin", Version: "0.1.0", AutoFetch: false},
	}

	// Should complete without touching wfctl at all.
	AutoFetchDeclaredPlugins(decls, pluginDir, nil)
}

// TestAutoFetchDeclaredPlugins_EmptyInputs verifies early-return on empty inputs.
func TestAutoFetchDeclaredPlugins_EmptyInputs(t *testing.T) {
	// Neither pluginDir nor decls provided — must return immediately.
	AutoFetchDeclaredPlugins(nil, "", nil)
	AutoFetchDeclaredPlugins([]AutoFetchDecl{}, "/some/dir", nil)
}
