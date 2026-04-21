package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestDefaultResolveIaCProvider_IsNotPlaceholder verifies the default
// resolveIaCProvider no longer returns the old "no in-process provider loader"
// stub message.
func TestDefaultResolveIaCProvider_IsNotPlaceholder(t *testing.T) {
	t.Setenv("WFCTL_PLUGIN_DIR", t.TempDir()) // empty dir — no plugins
	_, err := resolveIaCProvider(context.Background(), "any-provider", nil)
	if err == nil {
		t.Skip("resolveIaCProvider succeeded unexpectedly")
	}
	if strings.Contains(err.Error(), "no in-process provider loader") {
		t.Errorf("default resolveIaCProvider still returns old placeholder message: %v", err)
	}
}

// TestDefaultResolveIaCProvider_NoPluginDir verifies the error hints at
// 'wfctl plugin install' when the plugin directory does not exist.
func TestDefaultResolveIaCProvider_NoPluginDir(t *testing.T) {
	t.Setenv("WFCTL_PLUGIN_DIR", filepath.Join(t.TempDir(), "nonexistent"))
	_, err := resolveIaCProvider(context.Background(), "fake-provider", nil)
	if err == nil {
		t.Fatal("expected error when plugin dir does not exist")
	}
	if !strings.Contains(err.Error(), "wfctl plugin install") {
		t.Errorf("expected hint to run 'wfctl plugin install', got: %v", err)
	}
}

// TestDefaultResolveIaCProvider_NoMatchingPlugin verifies the error hints at
// 'wfctl plugin install' when no plugin declares the requested iacProvider name.
func TestDefaultResolveIaCProvider_NoMatchingPlugin(t *testing.T) {
	dir := t.TempDir()
	pluginSubDir := filepath.Join(dir, "some-plugin")
	if err := os.MkdirAll(pluginSubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"some-plugin","version":"0.1.0","capabilities":{"iacProvider":{"name":"other-provider"}}}`
	if err := os.WriteFile(filepath.Join(pluginSubDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("WFCTL_PLUGIN_DIR", dir)
	_, err := resolveIaCProvider(context.Background(), "digitalocean", nil)
	if err == nil {
		t.Fatal("expected error when no matching plugin found")
	}
	if !strings.Contains(err.Error(), "wfctl plugin install") {
		t.Errorf("expected hint to run 'wfctl plugin install', got: %v", err)
	}
}

// TestDefaultResolveIaCProvider_MatchingPlugin_NotLoaded verifies that when a
// matching plugin is found (correct iacProvider.name) but the plugin binary is
// absent, the error is specific about the plugin name rather than silently
// falling through to a generic message.
func TestDefaultResolveIaCProvider_MatchingPlugin_NotLoaded(t *testing.T) {
	dir := t.TempDir()
	pluginSubDir := filepath.Join(dir, "workflow-plugin-digitalocean")
	if err := os.MkdirAll(pluginSubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"workflow-plugin-digitalocean","version":"0.1.0","capabilities":{"iacProvider":{"name":"digitalocean"}}}`
	if err := os.WriteFile(filepath.Join(pluginSubDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	// No binary present → load will fail.

	t.Setenv("WFCTL_PLUGIN_DIR", dir)
	_, err := resolveIaCProvider(context.Background(), "digitalocean", nil)
	if err == nil {
		t.Fatal("expected error when plugin binary is absent")
	}
	if !strings.Contains(err.Error(), "workflow-plugin-digitalocean") {
		t.Errorf("expected plugin name in error message, got: %v", err)
	}
}

// TestPluginDeployProvider_LazyResolution verifies that resolveIaCProvider is
// NOT called at construction time and IS called on the first Deploy.
func TestPluginDeployProvider_LazyResolution(t *testing.T) {
	callCount := 0
	orig := resolveIaCProvider
	defer func() { resolveIaCProvider = orig }()

	driver := &fakeResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, error) {
		callCount++
		return fake, nil
	}

	cfg := makePluginTestConfig("fake-cloud", "fake-provider")
	p, err := newDeployProvider("fake-cloud", cfg)
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}

	if callCount != 0 {
		t.Errorf("resolveIaCProvider called at construction (%d times); want 0", callCount)
	}

	deployCfg := DeployConfig{
		AppName:  "my-app",
		ImageTag: "registry.example.com/myapp:v1",
		Env:      &config.CIDeployEnvironment{},
	}
	if err := p.Deploy(context.Background(), deployCfg); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if callCount != 1 {
		t.Errorf("resolveIaCProvider call count after first Deploy: got %d, want 1", callCount)
	}

	// Second Deploy must reuse cached provider.
	if err := p.Deploy(context.Background(), deployCfg); err != nil {
		t.Fatalf("Deploy (second): %v", err)
	}
	if callCount != 1 {
		t.Errorf("resolveIaCProvider called again on second Deploy: got %d total calls, want 1", callCount)
	}
}

// TestPluginDeployProvider_ResourceTypeFromModule verifies the resource type
// is taken from the infra module's Type field, not hardcoded.
func TestPluginDeployProvider_ResourceTypeFromModule(t *testing.T) {
	driver := &fakeResourceDriver{}
	fake := &fakeIaCProvider{
		name:    "fake-cloud",
		drivers: map[string]interfaces.ResourceDriver{"infra.container_service": driver},
	}

	orig := resolveIaCProvider
	defer func() { resolveIaCProvider = orig }()
	resolveIaCProvider = func(_ context.Context, _ string, _ map[string]any) (interfaces.IaCProvider, error) {
		return fake, nil
	}

	cfg := makePluginTestConfig("fake-cloud", "fake-provider")
	p, err := newDeployProvider("fake-cloud", cfg)
	if err != nil {
		t.Fatalf("newDeployProvider: %v", err)
	}

	pp, ok := p.(*pluginDeployProvider)
	if !ok {
		t.Fatalf("expected *pluginDeployProvider, got %T", p)
	}
	if pp.resourceType != "infra.container_service" {
		t.Errorf("resourceType = %q; want %q", pp.resourceType, "infra.container_service")
	}
}
